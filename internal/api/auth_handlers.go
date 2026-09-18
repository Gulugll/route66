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
	"net/http"

	"github.com/gin-gonic/gin"

	"awesomeProject/internal/auth"
	"awesomeProject/internal/settings"
)

// register 注册新用户。成功后**顺便登录**(发会话 cookie)——
// 注册成功再让人打一遍密码是纯折磨,业界主流做法也是注册即登录。
// 复用 Login 而不是另写一份"发会话"逻辑:登录的入口永远只有一个。
func (s *Server) register(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数格式错误"})
		return
	}
	_, err := s.auth.Register(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, auth.ErrUsernameTaken) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	s.loginAndStartSession(c, req.Username, req.Password)
}

// login 登录。凭据错返回 401,文案由 auth 包统一(防用户名枚举)。
func (s *Server) login(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数格式错误"})
		return
	}
	s.loginAndStartSession(c, req.Username, req.Password)
}

// loginAndStartSession 登录/注册成功的共同尾部:发 cookie + 返回用户信息。
// token 只进 HttpOnly cookie,**绝不**放进 JSON body —— 一旦进了 body,
// 前端就得存 localStorage,XSS 一偷一个准,HttpOnly 的意义就没了。
func (s *Server) loginAndStartSession(c *gin.Context, username, password string) {
	u, token, err := s.auth.Login(c.Request.Context(), username, password)
	if err != nil {
		if errors.Is(err, auth.ErrBadCredentials) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "登录失败: " + err.Error()})
		return
	}
	auth.SetSessionCookie(c, token, int(auth.SessionTTL.Seconds()))
	c.JSON(http.StatusOK, gin.H{"user": u})
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

// publicConfig 下发浏览器渲染地图需要的公开配置(JS key + 安全密钥)。
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
		"js_key": jsKey,
		"js_sec": jsSec,
	})
}
