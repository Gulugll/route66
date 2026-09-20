package config

import "os"

// Config 一份服务配置。集中在一个类型里,装配层(main)从这里取,谁都不用直接读环境变量。
type Config struct {
	Port      string // 用户端监听端口(7800)
	AdminPort string // 管理端监听端口(7801);空 = 管理端不启用
	AmapKey   string // 高德 Web服务 key —— 只是"兜底默认值",DB(app_settings)里配的优先
	AmapJSKey string // 高德 JS API key 兜底(浏览器渲染地图用);同样 DB 优先
	AmapJSSec string // JS key 的安全密钥兜底
	RedisAddr string // Redis 地址
	PGDSN     string // PostgreSQL 连接串;空 = 认证/异步任务/管理端不启用(同步 /plan 照常)
	AdminUser string // 管理员种子账号(P2 认证启用时,启动时确保存在)
	AdminPass string

	LLMBaseURL string // OpenAI 兼容服务地址;空 = agent 端点按缺省值兜底
	LLMAPIKey  string // LLM 密钥;空 = /agent 返回 503
	LLMModel   string // 模型名
}

func Load() Config {
	return Config{
		Port:      getenv("PORT", "7800"),
		AdminPort: os.Getenv("ADMIN_PORT"),
		AmapKey:   os.Getenv("AMAP_KEY"),
		AmapJSKey: os.Getenv("AMAP_JS_KEY"),
		AmapJSSec: os.Getenv("AMAP_JS_CODE"),
		RedisAddr: getenv("REDIS_ADDR", "localhost:6379"),
		PGDSN:     os.Getenv("PG_DSN"),
		AdminUser: os.Getenv("ADMIN_USER"),
		AdminPass: os.Getenv("ADMIN_PASSWORD"),

		LLMBaseURL: os.Getenv("LLM_BASE_URL"),
		LLMAPIKey:  os.Getenv("LLM_API_KEY"),
		LLMModel:   os.Getenv("LLM_MODEL"),
	}
}

// getenv 读环境变量,没设就用默认值。
func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
