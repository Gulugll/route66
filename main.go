package main

import (
	"context"
	"log"
	"time"

	"awesomeProject/internal/amap"
	"awesomeProject/internal/api"
	"awesomeProject/internal/auth"
	"awesomeProject/internal/cache"
	"awesomeProject/internal/config"
	"awesomeProject/internal/matrix"
	"awesomeProject/internal/queue"
	"awesomeProject/internal/repo"
	"awesomeProject/internal/settings"
	"awesomeProject/internal/worker"
)

// main 是装配层:读配置、拼好各包、启动。
// 外部依赖没配/挂了,对应功能降级/不启用,服务其余部分照常跑:
//  1. 缓存:优先 Redis(跨重启/多实例共享),连不上降级进程内 map
//  2. 距离:高德客户端常驻,key 动态取(管理端 DB > env 兜底),没 key 走 haversine
//  3. 数据库:配了 PG_DSN 才启用认证/异步任务/管理端;没配则同步 /plan 照常
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

	// —— 配置中心 + 数据库:PG_DSN 是认证/异步/管理端的总开关 ——
	// keyProvider 即使没有 DB 也要存在:它的 fallback 直接给 env 值,用户没有配置回退env值
	// amap.Client 拿到的永远是同一个查询接口,DB 配没配对它透明
	var keyProvider *settings.Provider
	var authSvc *auth.Service
	var plansRepo repo.TaskRepo
	var taskQueue *queue.Queue

	if cfg.PGDSN != "" { //数据库有值
		gormRepo, err := repo.NewGorm(cfg.PGDSN)
		if err == nil {
			err = gormRepo.Migrate(ctx)
		}
		if err != nil {
			log.Printf("postgres unavailable (%v), auth/async/admin disabled", err)
		} else {
			log.Printf("postgres connected, tasks/users/app_settings ready")
			plansRepo = gormRepo

			authStore := auth.NewGormStore(gormRepo.DB())
			if err := authStore.Migrate(ctx); err != nil {
				log.Printf("auth migrate failed: %v", err)
			} else {
				authSvc = auth.NewService(authStore)
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
		log.Printf("PG_DSN not set: auth/async/admin disabled (sync /plan unaffected)")
	}

	// —— 距离层装配:客户端常驻,key 动态取 ——
	// keyFn 每次请求现查 Provider(DB>env,自带缓存):管理端改 key 不重启即生效
	amapClient := amap.NewClient(cfg.AmapKey, distCache).WithKeyFn(func() string {
		return keyProviderValue(keyProvider, settings.KeyAmapRest, cfg.AmapKey)
	})
	matrixService := matrix.NewWithAmap(amapClient)

	// —— 异步任务装配:复用同一个 PG;Redis Stream 做传送带 ——
	if plansRepo != nil {
		taskQueue = queue.New(cfg.RedisAddr, "plan_tasks", "workers", "")
		go func() {
			if err := worker.Run(ctx, taskQueue, plansRepo, matrixService, amapClient); err != nil &&
				err != context.Canceled {
				log.Printf("[worker] exited: %v", err)
			}
		}()
		log.Printf("async plan tasks enabled (postgres + stream plan_tasks)")
	}

	// —— 用户端引擎(:7800)——
	envFallbacks := map[string]string{
		settings.KeyAmapRest:  cfg.AmapKey,
		settings.KeyAmapJS:    cfg.AmapJSKey,
		settings.KeyAmapJSSec: cfg.AmapJSSec,
	}
	opts := []api.Option{
		api.WithSettings(keyProvider),
		api.WithEnvFallbacks(envFallbacks),
	}
	if authSvc != nil {
		opts = append(opts, api.WithAuth(authSvc))
	}
	router := api.NewRouter(matrixService, amapClient, plansRepo, taskQueue, opts...)

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
		log.Printf("admin server disabled (needs ADMIN_PORT + PG_DSN)")
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
