package config

import (
	"os"
	"strings"
)

// Config 一份服务配置。集中在一个类型里,装配层(main)从这里取,谁都不用直接读环境变量。
type Config struct {
	Port      string // 用户端监听端口(7800)
	AdminPort string // 管理端监听端口(7801);空 = 管理端不启用
	AmapKey   string // 高德 Web服务 key —— 只是"兜底默认值",DB(app_settings)里配的优先
	AmapJSKey string // 高德 JS API key 兜底(浏览器渲染地图用);同样 DB 优先
	AmapJSSec string // JS key 的安全密钥兜底
	RedisAddr string // Redis 地址
	// SQLitePath 统一存储（用户/会话/社区/任务/设置）。默认 data/route66.db
	SQLitePath string
	// NoAuth=true 独立部署（默认无登录，本地用户直接放行）；
	// false = 联合部署，走 Casdoor/OIDC 登录
	NoAuth           bool
	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string
	OIDCAdminSub     string   // 管理员 sub（自动入白名单 + admin 角色）
	OIDCAllowedSubs  []string // 额外允许登录的 sub 白名单
	AdminUser        string   // 管理员种子账号(P2 认证启用时,启动时确保存在)
	AdminPass        string

	LLMBaseURL string // OpenAI 兼容服务地址;空 = agent 端点按缺省值兜底
	LLMAPIKey  string // LLM 密钥;空 = /agent 返回 503
	LLMModel   string // 模型名

	DistanceSource string // 距离来源:valhalla(默认)| amap(遗留门控)
	ValhallaURL    string // Valhalla 根地址(自托管)
	SearchSource   string // 搜索来源:photon(默认,公网可达)| nominatim
	PhotonURL      string // Photon 根地址(可自托管)
	NominatimURL   string // Nominatim 根地址(可自托管)
}

func Load() Config {
	return Config{
		Port:       getenv("PORT", "7800"),
		AdminPort:  os.Getenv("ADMIN_PORT"),
		AmapKey:    os.Getenv("AMAP_KEY"),
		AmapJSKey:  os.Getenv("AMAP_JS_KEY"),
		AmapJSSec:  os.Getenv("AMAP_JS_CODE"),
		RedisAddr:  getenv("REDIS_ADDR", "localhost:6379"),
		SQLitePath: getenv("SQLITE_PATH", "data/route66.db"),
		NoAuth:     getenv("NO_AUTH", "true") != "false",

		OIDCIssuer:       os.Getenv("OIDC_ISSUER"),
		OIDCClientID:     os.Getenv("OIDC_CLIENT_ID"),
		OIDCClientSecret: os.Getenv("OIDC_CLIENT_SECRET"),
		OIDCRedirectURL:  os.Getenv("OIDC_REDIRECT_URL"),
		OIDCAdminSub:     os.Getenv("OIDC_ADMIN_SUB"),
		OIDCAllowedSubs:  splitList(os.Getenv("OIDC_ALLOWED_SUBS")),
		AdminUser:        os.Getenv("ADMIN_USER"),
		AdminPass:        os.Getenv("ADMIN_PASSWORD"),

		LLMBaseURL: os.Getenv("LLM_BASE_URL"),
		LLMAPIKey:  os.Getenv("LLM_API_KEY"),
		LLMModel:   os.Getenv("LLM_MODEL"),

		DistanceSource: getenv("DISTANCE_SOURCE", "valhalla"),
		ValhallaURL:    getenv("VALHALLA_URL", "http://localhost:8002"),
		SearchSource:   getenv("SEARCH_SOURCE", "photon"),
		PhotonURL:      getenv("PHOTON_URL", "https://photon.komoot.io"),
		NominatimURL:   getenv("NOMINATIM_URL", "https://nominatim.openstreetmap.org"),
	}
}

// getenv 读环境变量,没设就用默认值。
func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// splitList 逗号分隔列表 → 切片（去空白、丢空项）。
func splitList(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
