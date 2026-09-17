package main

import (
	"context"
	"log"
	"time"

	"awesomeProject/internal/amap"
	"awesomeProject/internal/api"
	"awesomeProject/internal/cache"
	"awesomeProject/internal/config"
	"awesomeProject/internal/matrix"
	"awesomeProject/internal/queue"
	"awesomeProject/internal/repo"
	"awesomeProject/internal/worker"
)

// main 是装配层:读配置、拼好各包、启动。只在这里做"接线",不写业务逻辑。
// 装配策略一以贯之:外部依赖没配/挂了,对应功能降级或不启用,服务其余部分照常跑:
//   1. 缓存:优先 Redis(跨重启/多实例共享),连不上降级进程内 map
//   2. 距离:优先高德(真实路网),失败降级 haversine 直线
//   3. 异步任务:配了 MYSQL_DSN 才启用(Phase 2);没配则同步 /plan 照常,异步 /plans 不注册
func main() {
	cfg := config.Load()
	ctx := context.Background() // worker 的生命周期;进程退出时随之结束(教学规模不做优雅停机)

	// 这两个变量各跨二十多行,所以用完整词组而不是 m / am:
	// 短名的长度要和作用域匹配,否则读代码的人得一路往上翻才知道 m 是什么。
	var matrixService *matrix.Service
	var amapClient *amap.Client // 传给 /search 用;没 key 时保持 nil
	if cfg.AmapKey != "" {
		// —— 缓存层装配:Redis 优先,连不上降级内存 ——
		var distCache cache.Cache
		if redisCache, err := cache.NewRedis(cfg.RedisAddr, 24*time.Hour); err != nil {
			log.Printf("redis unavailable (%v), using in-memory cache", err)
			distCache = cache.NewMemory(24 * time.Hour)
		} else {
			distCache = redisCache
			log.Printf("redis cache enabled at %s", cfg.RedisAddr)
		}

		// —— 距离层装配:高德优先,失败降级 haversine ——
		amapClient = amap.NewClient(cfg.AmapKey, distCache)
		matrixService = matrix.NewWithAmap(amapClient)
		log.Printf("amap enabled (key set), fallback to haversine on failure")
	} else {
		matrixService = matrix.New()
		log.Printf("amap key not set (AMAP_KEY), using haversine straight-line distance")
	}

	// —— 异步任务装配(Phase 2):MySQL 存任务档案 + Redis Stream 做传送带 ——
	// 开关是 MYSQL_DSN:user:pass@tcp(host:3306)/dbname。没配则 /plans 不注册,
	// 同步 /plan 照常 —— 异步是"新增能力",不该成为服务的硬依赖。
	var plansRepo repo.TaskRepo
	var taskQueue *queue.Queue
	if cfg.MySQLDSN != "" {
		plansRepo, taskQueue = setupAsync(ctx, cfg, matrixService, amapClient)
	} else {
		log.Printf("MYSQL_DSN not set, async /plans disabled (sync /plan unaffected)")
	}

	router := api.NewRouter(matrixService, amapClient, plansRepo, taskQueue)

	log.Printf("listening on :%s", cfg.Port)
	log.Fatal(router.Run(":" + cfg.Port))
}

// setupAsync 把异步链路的四件套接起来:GORM 连接 → 建表 → 队列 → 后台 worker。
// 单独拆一个函数:main 的主干只该有"接线",逐步 Try 的样板代码收在子函数里。
// 任何一步失败都返回 (nil, nil) —— 异步整体禁用,服务照常起,日志里说清原因。
func setupAsync(ctx context.Context, cfg config.Config,
	m *matrix.Service, am *amap.Client) (repo.TaskRepo, *queue.Queue) {

	// 连接池参数在 NewGorm 内部配置;连接是懒建立的,不 Migrate/用一下不知道配没配对
	plansRepo, err := repo.NewGorm(cfg.MySQLDSN)
	if err == nil {
		err = plansRepo.Migrate(ctx)
	}
	if err != nil {
		log.Printf("async disabled: %v", err)
		return nil, nil
	}
	log.Printf("mysql connected, tasks table ready")

	// Stream 和距离缓存各持各的 Redis 连接:职责独立,互不牵连
	taskQueue := queue.New(cfg.RedisAddr, "plan_tasks", "workers", "")

	// 后台解算者:单个 goroutine 串行消费。要提吞吐是"多起几个 worker 进程"
	// 的事(消费组自动分摊),不是在这里加 goroutine 的事
	go func() {
		if err := worker.Run(ctx, taskQueue, plansRepo, m, am); err != nil &&
			err != context.Canceled {
			log.Printf("[worker] exited: %v", err)
		}
	}()
	log.Printf("async plan tasks enabled (mysql + stream plan_tasks)")
	return plansRepo, taskQueue
}
