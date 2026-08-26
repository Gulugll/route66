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

	var m *matrix.Service
	var am *amap.Client // 传给 /search 用;没 key 时保持 nil
	if cfg.AmapKey != "" {
		// —— 缓存层装配:Redis 优先,连不上降级内存 ——
		var c cache.Cache
		if rc, err := cache.NewRedis(cfg.RedisAddr, 24*time.Hour); err != nil {
			log.Printf("redis unavailable (%v), using in-memory cache", err)
			c = cache.NewMemory(24 * time.Hour)
		} else {
			c = rc
			log.Printf("redis cache enabled at %s", cfg.RedisAddr)
		}

		// —— 距离层装配:高德优先,失败降级 haversine ——
		am = amap.NewClient(cfg.AmapKey, c)
		m = matrix.NewWithAmap(am)
		log.Printf("amap enabled (key set), fallback to haversine on failure")
	} else {
		m = matrix.New()
		log.Printf("amap key not set (AMAP_KEY), using haversine straight-line distance")
	}

	r := api.NewRouter(m, am)

	log.Printf("listening on :%s", cfg.Port)
	log.Fatal(r.Run(":" + cfg.Port))
}
