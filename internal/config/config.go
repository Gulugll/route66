package config

import "os"

// Config 一份服务配置。集中在一个类型里,装配层(main)从这里取,谁都不用直接读环境变量。
type Config struct {
	Port      string // 监听端口
	AmapKey   string // 高德 API key,暂时空着,Phase 1 用
	RedisAddr string // Redis 地址,Phase 1 用
}

func Load() Config {
	return Config{
		Port:      getenv("PORT", "7800"),
		AmapKey:   os.Getenv("AMAP_KEY"),
		RedisAddr: getenv("REDIS_ADDR", "localhost:6379"),
	}
}

// getenv 读环境变量,没设就用默认值。
func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
