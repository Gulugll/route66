package main

import (
	"log"
	"time"

	"awesomeProject/internal/amap"
	"awesomeProject/internal/api"
	"awesomeProject/internal/cache"
	"awesomeProject/internal/config"
	"awesomeProject/internal/matrix"
)

// main 是装配层:读配置、拼好各包、启动。只在这里做"接线",不写业务逻辑。
// 两级降级,每级都有日志:
//   1. 缓存:优先 Redis(跨重启/多实例共享),连不上降级进程内 map
//   2. 距离:优先高德(真实路网),失败降级 haversine 直线
// 这就是"外部依赖要有边界"——任何一个挂了,服务都照常跑。
func main() {
	cfg := config.Load()

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

	router := api.NewRouter(matrixService, amapClient)

	log.Printf("listening on :%s", cfg.Port)
	log.Fatal(router.Run(":" + cfg.Port))
}
