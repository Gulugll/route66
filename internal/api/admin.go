// admin.go —— 管理端(:7801)的路由与 handler。
//
// 和用户端的分工:用户端是"用 key 的人",管理端是"管 key 的人"。
// 两端口一进程的价值在网络层(将来只把 7801 绑内网/防火墙),
// 但应用层的 RequireAdmin 一条都不能少 —— cookie 不按端口隔离,
// 用户端的登录态会自动带到管理端,靠"URL 隐藏"什么也挡不住(见 §3.1 设计稿)。
//
// 回显红线:key 一律掩码后回显(4088****557)。管理页是给人看的,
// 不需要完整 key;完整值只在 PUT 时进入服务端,永不返回给任何浏览器。
package api

import (
	"errors"
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"

	"awesomeProject/internal/auth"
	"awesomeProject/internal/settings"
)

// NewAdminRouter 组装管理端引擎。staticDir 为空 = 不托管管理页(纯 API)。
// envFallbacks 是"配置名 → env 兜底值"映射,回显来源(none/env)时要用 ——
// handler 不直接读环境变量,映射由 main 从 config 注入。
func NewAdminRouter(a *auth.Service, sp *settings.Provider, staticDir string,
	envFallbacks map[string]string) *gin.Engine {
	s := &Server{auth: a, settings: sp, envFallbacks: envFallbacks}

	router := gin.Default()
	router.GET("/admin/healthz", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// OptionalAuth 放最外层:登录接口自己不需要"已登录",
	// 但后面的管理 API 全部依赖它认出的人来做角色判断
	router.Use(s.auth.OptionalAuth())

	router.POST("/admin/login", s.login) // 复用用户端的 login:同 cookie 名,天然共享登录态
	router.POST("/admin/logout", s.logout)
	router.GET("/admin/api/me", s.me)

	adminAPI := router.Group("/admin/api", s.auth.RequireAdmin())
	{
		adminAPI.GET("/keys", s.adminGetKeys)
		adminAPI.PUT("/keys", s.adminPutKeys)
		adminAPI.GET("/users", s.adminListUsers)
	}

	if staticDir != "" {
		abs, err := filepath.Abs(staticDir)
		if err == nil {
			router.NoRoute(gin.WrapH(http.FileServer(http.Dir(abs))))
		}
	}
	return router
}

// maskKey 掩码规则:留头尾各 4 位,中间打星;太短就全打星。
// 让运维能认出"是哪把 key"(和高德控制台列表里的显示方式一致),
// 又不至于截屏一次就把 key 泄出去。
func maskKey(v string) string {
	if len(v) <= 8 {
		return "****"
	}
	return v[:4] + "****" + v[len(v)-4:]
}

// keyEntry 管理页看到的每个配置项:掩码值 + 来源。
// source 帮管理员回答一个关键问题:"我现在用的这把 key 到底是谁给的?"——
// db = 管理端配的;env = 兜底默认值;none = 两处都没有(功能会不可用)。
type keyEntry struct {
	Value  string `json:"value"`
	Source string `json:"source"` // db | env | none
}

func (s *Server) keyEntry(c *gin.Context, name string, envValue string) (keyEntry, error) {
	dbValue, hasDB, err := s.settings.HasDBValue(c.Request.Context(), name)
	if err != nil {
		return keyEntry{}, err
	}
	if hasDB {
		return keyEntry{Value: maskKey(dbValue), Source: "db"}, nil
	}
	if envValue != "" {
		return keyEntry{Value: maskKey(envValue), Source: "env"}, nil
	}
	return keyEntry{Value: "", Source: "none"}, nil
}

// adminGetKeys 列出三个配置项(掩码)。env 兜底值从装配层塞进
// s.envFallbacks,这里只读 map —— handler 不直接摸环境变量。
func (s *Server) adminGetKeys(c *gin.Context) {
	out := gin.H{}
	for name := range s.envFallbacks {
		entry, err := s.keyEntry(c, name, s.envFallbacks[name])
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "读取配置失败: " + err.Error()})
			return
		}
		out[name] = entry
	}
	c.JSON(http.StatusOK, out)
}

// adminPutKeys 写配置。请求体只接受三个白名单键;值为空串 = 清除该项(回落 env)。
// 未出现在请求体里的键不动 —— 部分更新,管理页一次改一个框也不会互相覆盖。
func (s *Server) adminPutKeys(c *gin.Context) {
	var req map[string]string
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数格式错误"})
		return
	}
	for name, value := range req {
		if err := s.settings.Set(c.Request.Context(), name, value); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, settings.ErrUnknownKey) {
				status = http.StatusBadRequest
			}
			c.JSON(status, gin.H{"error": err.Error()})
			return
		}
	}
	// 返回更新后的掩码视图,管理页改完立即看到新状态
	s.adminGetKeys(c)
}

// adminListUsers 用户列表。教学规模全量返回,不分页 ——
// 等用户多到需要分页的那天,再顺手加 LIMIT/OFFSET,现在做是过度设计。
func (s *Server) adminListUsers(c *gin.Context) {
	users, err := s.auth.ListUsers(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询用户失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"users": users})
}
