// middleware.go —— gin 中间件:从 cookie 认人 + 角色门禁。
//
// 三个中间件按"拦得多严"排队:
//
//	OptionalAuth  尽力认人,认不出也放行 —— 公开接口用它,
//	              认出的用户放 gin.Context,handler 想用就取,没有也不碍事
//	RequireAuth   没登录就 401 —— 需要知道"是谁"的接口用
//	RequireAdmin  非 admin 一律 403 —— 管理端全部路由必须挂它
//
// ⚠️ 为什么 RequireAdmin 是硬红线而不是"管理页是隐藏 URL":cookie 按 host
// 隔离不按端口隔离,用户端(7800)的登录态会自动带到管理端(7801) ——
// 攻击边界上"隐藏 URL"等于没有,每条路由的角色校验才是真边界。
package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// cUserKey 存进 gin.Context 的键名。小写+包名前缀,避免和业务 key 撞名。
const cUserKey = "auth.user"

// currentUser 从 gin.Context 取出 OptionalAuth 认出的用户;没登录返回 false。
func currentUser(c *gin.Context) (User, bool) {
	v, ok := c.Get(cUserKey)
	if !ok {
		return User{}, false
	}
	u, ok := v.(User)
	return u, ok
}

// CurrentUser handler 里取"当前登录用户"(OptionalAuth 挂上去的)。
// 第二个返回值 false = 游客。
func CurrentUser(c *gin.Context) (User, bool) { return currentUser(c) }

// tokenFromCookie 从请求里取会话 token。空字符串表示"没带 cookie"。
func tokenFromCookie(c *gin.Context) string {
	cookie, err := c.Cookie(CookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie)
}

// OptionalAuth 尽力认人。任何失败(没 cookie / 过期 / 用户被删)都静默放行 ——
// 它的职责是"有机会就把用户挂到上下文",不是拦截。
func (s *Service) OptionalAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if u, err := s.UserFromToken(c.Request.Context(), tokenFromCookie(c)); err == nil {
			c.Set(cUserKey, u)
		}
		c.Next()
	}
}

// RequireAuth 强制登录。401(未认证)而不是 403(已认证但权限不够)——
// 这两个状态码的语义区别要让前端能区分"该去登录"和"该去找管理员"。
func (s *Service) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := currentUser(c); !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "请先登录"})
			return
		}
		c.Next()
	}
}

// RequireAdmin 强制 admin 角色。注意它依赖 OptionalAuth 先跑
// (gin 的中间件链按注册顺序执行),单独挂它等于没挂。
func (s *Service) RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		u, ok := currentUser(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "请先登录"})
			return
		}
		if u.Role != RoleAdmin {
			// 403:你是合法用户,但这扇门不对你开。文案不透露"还差多少"。
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
			return
		}
		c.Next()
	}
}

// SetSessionCookie 把会话写进 HttpOnly cookie。
//
// HttpOnly:JS 读不到 —— XSS 就算偷走了页面上的东西也偷不走会话;
// SameSite=Lax:跨站 GET 导航会带上,跨站 POST 不会 —— CSRF 的基础防线;
// 故意不设 Secure:本地教学是 http://localhost,设了 Secure 浏览器会拒发。
// 上 HTTPS 时必须补上,TODO 注释就是给那一刻的。
func SetSessionCookie(c *gin.Context, token string, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/", // 全站有效:用户端和管理端两个"目录"都要带上
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// Secure: true, // 上 HTTPS 后打开
	})
}

// ClearSessionCookie 登出用:除了删库里会话,还要让浏览器把 cookie 扔掉 ——
// MaxAge=-1 是浏览器删除 cookie 的标准指令。
func ClearSessionCookie(c *gin.Context) {
	SetSessionCookie(c, "", -1)
}
