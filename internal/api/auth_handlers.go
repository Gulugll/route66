// auth_handlers.go —— /auth/* 与 /config/public 的 HTTP 编排。
//
// 业务规则全在 internal/auth(auth 包),这里只做三件事:
// 绑参数、调服务、定状态码。和 plan/search 的分工完全一致。
//
// 状态码语义(和中间件呼应,前端据此分流):
//
//	400 参数不合法      401 没登录/凭据错      409 用户名已被占用
package api

import (
	"errors"
	"log"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	"awesomeProject/internal/auth"
	"awesomeProject/internal/settings"
)

// oidcLogin 发起联合登录：302 跳转到 Casdoor 授权页。
// ?return_to=/xxx 登录后回跳（仅本站相对路径）；?prompt=login 强制重登（换账号）。
func (s *Server) oidcLogin(c *gin.Context) {
	loginURL, err := s.oidc.StartLogin(c.Request.Context(), c.Query("return_to"), c.Query("prompt") == "login")
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, auth.ErrInvalidReturnTo) {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	parsed, err := url.Parse(loginURL)
	if err != nil || parsed.Query().Get("state") == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "授权 URL 缺少 state"})
		return
	}
	// state 同时进 cookie，回调时与 URL 里的 state 比对（防 CSRF/会话固定）
	c.SetCookie(auth.StateCookieName, parsed.Query().Get("state"), 600, "/", "", auth.CookieSecure, true)
	c.Redirect(http.StatusFound, loginURL)
}

// oidcCallback 授权回调：验 state → 换 token → 建档 → 发会话 → 回跳。
func (s *Server) oidcCallback(c *gin.Context) {
	browserState := ""
	if cookie, err := c.Cookie(auth.StateCookieName); err == nil {
		browserState = cookie
	}
	c.SetCookie(auth.StateCookieName, "", -1, "/", "", auth.CookieSecure, true)
	result, err := s.oidc.Callback(c.Request.Context(), c.Query("state"), c.Query("code"), browserState)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, auth.ErrInvalidState) || errors.Is(err, auth.ErrNotAllowed) {
			status = http.StatusBadRequest
		}
		log.Printf("[auth] oidc callback error: %v", err)
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	auth.SetSessionCookie(c, result.Token, int(auth.SessionTTL.Seconds()))
	c.Redirect(http.StatusFound, result.ReturnTo)
}

// logout 登出:删服务端会话 + 让浏览器扔 cookie。两步缺一不可
// (只删 cookie 的话 token 本身还有效,等于"假装退出了")。
func (s *Server) logout(c *gin.Context) {
	if cookie, err := c.Cookie(auth.CookieName); err == nil {
		_ = s.auth.Logout(c.Request.Context(), cookie)
	}
	auth.ClearSessionCookie(c)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// me 当前登录用户。前端刷新页面后用它恢复登录态(不重放密码)。
func (s *Server) me(c *gin.Context) {
	u, ok := auth.CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": u})
}

// updateProfile 改昵称/简介(PATCH /auth/profile)。
// 状态码翻译照 register 同一模式:校验失败 400,存储故障 503 + 固定文案。
func (s *Server) updateProfile(c *gin.Context) {
	u, ok := auth.CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	var req struct {
		Nickname string `json:"nickname"`
		Bio      string `json:"bio"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数格式错误"})
		return
	}
	if err := s.auth.UpdateProfile(c.Request.Context(), u.ID, req.Nickname, req.Bio); err != nil {
		if errors.Is(err, auth.ErrStorageUnavailable) {
			log.Printf("[auth] profile storage error: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "服务暂时不可用，请稍后重试"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// publicConfig 下发浏览器渲染地图需要的公开配置:当前模式 + JS key + 安全密钥。
// mode 来自距离来源的 Kind(单一真相源):amap = 高德模式(前端切高德底图,GCJ-02);
// oss = 开源模式(OSM 底图,WGS-84)。JS key 只在 amap 模式有意义,一并下发。
// 这俩"公开"是因为 JS key 天生要暴露在浏览器里(靠域名白名单保护),
// 和必须捂在后端的 REST key 是两回事。管理端没配时回落 env(Provider 管),
// 两处都没配就返回空串 —— 前端据此显示"去设置里配 key"的空态。
func (s *Server) publicConfig(c *gin.Context) {
	ctx := c.Request.Context()
	jsKey, err := s.settings.Get(ctx, settings.KeyAmapJS)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取配置失败: " + err.Error()})
		return
	}
	jsSec, err := s.settings.Get(ctx, settings.KeyAmapJSSec)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取配置失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"mode":   s.src.Kind(),
		"js_key": jsKey,
		"js_sec": jsSec,
	})
}
