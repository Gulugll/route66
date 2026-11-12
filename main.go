package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
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

	// —— 配置中心 + 统一存储:SQLite 常开（用户/会话/社区/任务/设置同库）——
	// 认证双模式：NO_AUTH=true（默认）= 本地用户免登录；false = Casdoor OIDC。
	var keyProvider *settings.Provider
	var authSvc *auth.Service
	var oidcSvc *auth.OIDCService

	if err := os.MkdirAll(filepath.Dir(cfg.SQLitePath), 0o755); err != nil {
		log.Fatalf("创建数据目录失败: %v", err)
	}
	gormRepo, err := repo.NewGorm(cfg.SQLitePath)
	if err != nil {
		log.Fatalf("sqlite 初始化失败: %v", err)
	}
	if err := gormRepo.Migrate(ctx); err != nil {
		log.Fatalf("tasks 表迁移失败: %v", err)
	}
	plansRepo := gormRepo
	log.Printf("sqlite ready at %s", cfg.SQLitePath)

	authStore := auth.NewGormStore(gormRepo.DB())
	if err := authStore.Migrate(ctx); err != nil {
		log.Fatalf("users/sessions 表迁移失败: %v", err)
	}
	if err := auth.MigrateOIDCStates(ctx, gormRepo.DB()); err != nil {
		log.Fatalf("oidc_states 表迁移失败: %v", err)
	}

	if cfg.NoAuth {
		// 独立部署：本地用户（admin）自动放行，登录墙/角色门禁退化为单机语义
		authSvc = auth.NewService(authStore)
		localUser, err := authSvc.EnsureLocalUser(ctx)
		if err != nil {
			log.Fatalf("本地用户初始化失败: %v", err)
		}
		authSvc = auth.NewService(authStore, auth.WithLocalMode(localUser))
		log.Printf("独立部署模式：本地用户 %s 免登录", localUser.Nickname)
	} else {
		auth.CookieSecure = true // HTTPS 部署：会话 cookie 加 Secure
		authSvc = auth.NewService(authStore)
		oidcSvc, err = auth.NewOIDCService(ctx, gormRepo.DB(), authStore, auth.OIDCConfig{
			Issuer: cfg.OIDCIssuer, ClientID: cfg.OIDCClientID, ClientSecret: cfg.OIDCClientSecret,
			RedirectURL: cfg.OIDCRedirectURL, AdminSub: cfg.OIDCAdminSub, AllowedSubs: cfg.OIDCAllowedSubs, AllowAll: cfg.OIDCAllowAll,
		})
		if err != nil {
			log.Fatalf("OIDC 初始化失败: %v", err)
		}
		log.Printf("联合部署模式：OIDC issuer %s", cfg.OIDCIssuer)
	}
	// 过期会话周期清理（两种模式共用）
	go authSvc.RunSessionCleanup(ctx, time.Hour)

	communityStore := community.NewGormStore(gormRepo.DB())
	if err := communityStore.Migrate(ctx); err != nil {
		log.Fatalf("community 表迁移失败: %v", err)
	}
	communitySvc := community.NewService(communityStore)

	settingStore := settings.NewGormSettingStore(gormRepo.DB())
	if err := settingStore.Migrate(ctx); err != nil {
		log.Printf("settings migrate failed: %v", err)
	}
	keyProvider = settings.NewProvider(settingStore, envFallback(cfg))

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

	// —— 异步任务装配:SQLite 存任务档案,Redis Stream 做传送带 ——
	var taskQueue *queue.Queue
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
		if oidcSvc != nil {
			opts = append(opts, api.WithOIDC(oidcSvc))
		}
		if communitySvc != nil {
			opts = append(opts, api.WithCommunity(communitySvc))
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
