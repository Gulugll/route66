package main

import (
	"context"
	"log"
	"time"

	"awesomeProject/internal/amap"
	"awesomeProject/internal/api"
	"awesomeProject/internal/auth"
	"awesomeProject/internal/cache"
	"awesomeProject/internal/community"
	"awesomeProject/internal/config"
	"awesomeProject/internal/geo"
	"awesomeProject/internal/matrix"
	"awesomeProject/internal/nominatim"
	"awesomeProject/internal/photon"
	"awesomeProject/internal/queue"
	"awesomeProject/internal/repo"
	"awesomeProject/internal/settings"
	"awesomeProject/internal/valhalla"
	"awesomeProject/internal/worker"
)

// main 是装配层:读配置、拼好各包、启动。
// 外部依赖没配/挂了,对应功能降级/不启用,服务其余部分照常跑:
//  1. 缓存:优先 Redis(跨重启/多实例共享),连不上降级进程内 map
//  2. 距离:高德客户端常驻,key 动态取(管理端 DB > env 兜底),没 key 走 haversine
//  3. 数据库:配了 MYSQL_DSN 才启用认证/异步任务/管理端;没配则同步 /plan 照常
//  4. 管理端:独立端口(7801)单独一个引擎,和用户端(7800)同进程
func main() {
	cfg := config.Load()
	ctx := context.Background() // worker 的生命周期;进程退出时随之结束

	// —— 缓存层装配:Redis 优先,连不上降级内存 ——
	// 现在无条件装配:key 本身也运行时可变,
	var distCache cache.Cache
	if redisCache, err := cache.NewRedis(cfg.RedisAddr, 24*time.Hour); err != nil {
		log.Printf("redis unavailable (%v), using in-memory cache", err)
		distCache = cache.NewMemory(24 * time.Hour)
	} else {
		distCache = redisCache
		log.Printf("redis cache enabled at %s", cfg.RedisAddr)
	}

	// —— 配置中心 + 数据库:MYSQL_DSN 是认证/异步/管理端的总开关 ——
	// keyProvider 即使没有 DB 也要存在:它的 fallback 直接给 env 值,用户没有配置回退env值
	// amap.Client 拿到的永远是同一个查询接口,DB 配没配对它透明
	var keyProvider *settings.Provider
	var authSvc *auth.Service
	var communitySvc *community.Service
	var plansRepo repo.TaskRepo
	var taskQueue *queue.Queue

	if cfg.MySQLDSN != "" { //数据库有值
		gormRepo, err := repo.NewGorm(cfg.MySQLDSN)
		if err == nil {
			err = gormRepo.Migrate(ctx)
		}
		if err != nil {
			log.Printf("mysql unavailable (%v), auth/async/admin disabled", err)
		} else {
			log.Printf("mysql connected, tasks/users/app_settings ready")
			plansRepo = gormRepo

			authStore := auth.NewGormStore(gormRepo.DB())
			if err := authStore.Migrate(ctx); err != nil {
				log.Printf("auth migrate failed: %v", err)
			} else {
				// 登录限速:Redis 可达才装配;不可达则不限速地跑(fail open)。
				// 上线规模默认每 IP 每分钟 10 次失败。
				authOpts := []auth.Option{}
				if rl, err := auth.NewRedisLoginLimiter(cfg.RedisAddr, 10, time.Minute); err != nil {
					log.Printf("login limiter unavailable (%v), login rate limiting disabled", err)
				} else {
					authOpts = append(authOpts, auth.WithLimiter(rl))
					log.Printf("login rate limit enabled: 10 failures/min per IP (redis %s)", cfg.RedisAddr)
				}
				authSvc = auth.NewService(authStore, authOpts...)
				// 过期会话周期清理:惰性删除覆盖不到的死会话靠这里兜底
				go authSvc.RunSessionCleanup(ctx, time.Hour)

				// 社区模块:复用同一个 gorm 连接,迁移与 auth 同一时机。
				// 迁移失败只降级不启动失败 —— /community/* 不注册,其余功能照常
				communityStore := community.NewGormStore(gormRepo.DB())
				if err := communityStore.Migrate(ctx); err != nil {
					log.Printf("community migrate failed: %v", err)
				} else {
					communitySvc = community.NewService(communityStore)
				}
				// 种子管理员:env 给了就确保存在;没给则跳过(管理端仍可注册普通用户,
				// 但没人能进管理 API 403)
				if cfg.AdminUser != "" && cfg.AdminPass != "" {
					if err := authSvc.EnsureSeedAdmin(ctx, cfg.AdminUser, cfg.AdminPass); err != nil {
						log.Printf("seed admin failed: %v", err)
					} else {
						log.Printf("admin account ensured: %s", cfg.AdminUser)
					}
				} else {
					log.Printf("ADMIN_USER/ADMIN_PASSWORD not set, no admin account created")
				}
			}

			settingStore := settings.NewGormSettingStore(gormRepo.DB())
			if err := settingStore.Migrate(ctx); err != nil {
				log.Printf("settings migrate failed: %v", err)
			}
			keyProvider = settings.NewProvider(settingStore, envFallback(cfg))
		}
	} else { //数据库连接失败，env兜底
		keyProvider = settings.NewProvider(nil, envFallback(cfg)) // 纯 env 模式
		log.Printf("MYSQL_DSN not set: auth/async/admin disabled (sync /plan unaffected)")
	}

	// —— 距离层装配:来源可选,valhalla 默认,amap 遗留门控 ——
	// valhalla(自托管,无 key)是默认来源;DISTANCE_SOURCE=amap 切回高德
	// (key 动态取:管理端 DB > env 兜底,改 key 不重启即生效)。
	amapClient := amap.NewClient(cfg.AmapKey, distCache).WithKeyFn(func() string {
		return keyProviderValue(keyProvider, settings.KeyAmapRest, cfg.AmapKey)
	})
	var src geo.Source
	switch cfg.DistanceSource {
	case "amap":
		src = amapClient
		log.Printf("distance source: amap (legacy)")
	default:
		src = valhalla.NewClient(cfg.ValhallaURL, distCache)
		log.Printf("distance source: valhalla at %s", cfg.ValhallaURL)
	}
	matrixService := matrix.NewWithSource(src)

	// —— 异步任务装配:复用同一个 PG;Redis Stream 做传送带 ——
	if plansRepo != nil {
		taskQueue = queue.New(cfg.RedisAddr, "plan_tasks", "workers", "")
		go func() {
			if err := worker.Run(ctx, taskQueue, plansRepo, matrixService, src); err != nil &&
				err != context.Canceled {
				log.Printf("[worker] exited: %v", err)
			}
		}()
		log.Printf("async plan tasks enabled (mysql + stream plan_tasks)")
	}

	// —— 用户端引擎(:7800)——
	envFallbacks := map[string]string{
		settings.KeyAmapRest:   cfg.AmapKey,
		settings.KeyAmapJS:     cfg.AmapJSKey,
		settings.KeyAmapJSSec:  cfg.AmapJSSec,
		settings.KeyLLMBaseURL: cfg.LLMBaseURL,
		settings.KeyLLMAPIKey:  cfg.LLMAPIKey,
		settings.KeyLLMModel:   cfg.LLMModel,
	}
	opts := []api.Option{
		api.WithSettings(keyProvider),
		api.WithEnvFallbacks(envFallbacks),
	}
	if authSvc != nil {
		opts = append(opts, api.WithAuth(authSvc))
		if communitySvc != nil {
			opts = append(opts, api.WithCommunity(communitySvc))
		}
		// 注册限流:与登录限流同一个类型、不同参数(5 次/时/IP)——
		// 注册是"创建账号"入口,灌垃圾账号的代价比试密码高,窗口更严。
		if rrl, err := auth.NewRedisLoginLimiter(cfg.RedisAddr, 5, time.Hour); err != nil {
			log.Printf("register limiter unavailable (%v), register rate limiting disabled", err)
		} else {
			opts = append(opts, api.WithRegisterLimiter(rrl))
			log.Printf("register rate limit enabled: 5/hour per IP (redis %s)", cfg.RedisAddr)
		}
	}
	// —— 搜索来源:默认 Photon(公共实例在大陆网络可达,Nominatim 不可达) ——
	var searcher api.Searcher
	switch cfg.SearchSource {
	case "nominatim":
		searcher = nominatim.NewClient(cfg.NominatimURL)
		log.Printf("search source: nominatim at %s", cfg.NominatimURL)
	default:
		searcher = photon.NewClient(cfg.PhotonURL)
		log.Printf("search source: photon at %s", cfg.PhotonURL)
	}
	router := api.NewRouter(matrixService, src, plansRepo, taskQueue,
		append(opts, api.WithSearcher(searcher))...)

	// —— 两个引擎,任一退出即整个进程退出——
	errCh := make(chan error, 2)
	go func() {
		log.Printf("listening on :%s", cfg.Port)
		errCh <- router.Run(":" + cfg.Port)
	}()

	// —— 管理端引擎(:7801)——
	// 依赖 auth(角色门禁)+ settings(key 读写);没配 PG 就没有这两样,
	if cfg.AdminPort != "" && authSvc != nil {
		adminRouter := api.NewAdminRouter(authSvc, keyProvider, "admin", envFallbacks)
		go func() {
			log.Printf("admin listening on :%s", cfg.AdminPort)
			errCh <- adminRouter.Run(":" + cfg.AdminPort)
		}()
	} else {
		log.Printf("admin server disabled (needs ADMIN_PORT + MYSQL_DSN)")
	}
	log.Fatalf("server exited: %v", <-errCh)
}

// envFallback 生成"配置名 → env 值"的兜底查询,交给 Provider。
// settings 包自己不读环境变量,这个约定在这里兑现。
func envFallback(cfg config.Config) func(string) string {
	return func(name string) string {
		switch name {
		case settings.KeyAmapRest:
			return cfg.AmapKey
		case settings.KeyAmapJS:
			return cfg.AmapJSKey
		case settings.KeyAmapJSSec:
			return cfg.AmapJSSec
		case settings.KeyLLMBaseURL:
			return cfg.LLMBaseURL
		case settings.KeyLLMAPIKey:
			return cfg.LLMAPIKey
		case settings.KeyLLMModel:
			return cfg.LLMModel
		}
		return ""
	}
}

// keyProviderValue 统一的取值入口:Provider 查询失败(DB 抖动)时回落 env ——
// 配置读取失败不该把距离计算一起打挂,兜底。
func keyProviderValue(p *settings.Provider, name, envValue string) string {
	if p == nil {
		return envValue
	}
	v, err := p.Get(context.Background(), name)
	if err != nil || v == "" {
		return envValue
	}
	return v
}
