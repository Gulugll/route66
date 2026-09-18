// auth_protect_test.go —— API 级登录墙的测试。
//
// 核心用例 TestAPIRequiresAuth 钉住的是这条不变量:
// 业务接口的保护必须落在 API 层 —— 页面有登录墙不够,curl 绕过前端
// 也必须被 401 拦住。同时验证"带合法 cookie 的同一请求照常 200",
// 防止保护把正常用户也拦死。
//
// 测试不需要真数据库:auth.Store 的内存 fake 让路由"装配了认证",
// 这正是 Store 接口存在的意义(和 repo.TaskRepo 的 fakeRepo 同一思路)。
package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"awesomeProject/internal/amap"
	"awesomeProject/internal/auth"
	"awesomeProject/internal/matrix"
)

// memAuthStore auth.Store 的内存实现。
// 测试是串行的(gin 同包测试默认不并行),不加锁。
type memAuthStore struct {
	users    map[string]auth.User // 按用户名索引
	sessions map[string]auth.Session
	nextID   int64
}

func newMemAuthStore() *memAuthStore {
	return &memAuthStore{
		users:    map[string]auth.User{},
		sessions: map[string]auth.Session{},
	}
}

func (m *memAuthStore) CreateUser(ctx context.Context, u *auth.User) error {
	if _, ok := m.users[u.Username]; ok {
		return auth.ErrUsernameTaken // Service 把它翻译成防枚举的统一文案
	}
	m.nextID++
	u.ID = m.nextID
	m.users[u.Username] = *u
	return nil
}

func (m *memAuthStore) GetUserByName(ctx context.Context, username string) (auth.User, error) {
	u, ok := m.users[username]
	if !ok {
		return auth.User{}, auth.ErrNotFound
	}
	return u, nil
}

func (m *memAuthStore) GetUserByID(ctx context.Context, id int64) (auth.User, error) {
	for _, u := range m.users {
		if u.ID == id {
			return u, nil
		}
	}
	return auth.User{}, auth.ErrNotFound
}

func (m *memAuthStore) ListUsers(ctx context.Context) ([]auth.User, error) {
	out := make([]auth.User, 0, len(m.users))
	for _, u := range m.users {
		out = append(out, u)
	}
	return out, nil
}

func (m *memAuthStore) CreateSession(ctx context.Context, s *auth.Session) error {
	m.sessions[s.Token] = *s
	return nil
}

func (m *memAuthStore) GetSession(ctx context.Context, token string) (auth.Session, error) {
	s, ok := m.sessions[token]
	if !ok {
		return auth.Session{}, auth.ErrNotFound
	}
	return s, nil
}

func (m *memAuthStore) DeleteSession(ctx context.Context, token string) error {
	delete(m.sessions, token)
	return nil
}

// newAuthedTestRouter 装配了认证的路由 + 一个已注册并登录的测试用户。
// 返回值里的 cookie 直接塞进请求头就是合法登录态。
// 业务上这相当于"一个刚注册的正常用户",能走通所有受保护接口。
func newAuthedTestRouter(t *testing.T, withAmap bool, failPaths ...string) (*gin.Engine, string) {
	t.Helper()
	svc := auth.NewService(newMemAuthStore())
	_, err := svc.Register(context.Background(), "testuser", "test-pass-66")
	if err != nil {
		t.Fatalf("注册测试用户失败: %v", err)
	}
	_, token, err := svc.Login(context.Background(), "testuser", "test-pass-66")
	if err != nil {
		t.Fatalf("登录测试用户失败: %v", err)
	}
	cookie := auth.CookieName + "=" + token

	var r *gin.Engine
	if !withAmap {
		r = NewRouter(matrix.New(), nil, nil, nil, WithAuth(svc))
	} else {
		am := fakeAmapServer(t, failPaths...)
		cl := amap.NewClientWithBase("test-key", am.URL, nil)
		r = NewRouter(matrix.NewWithAmap(cl), cl, nil, nil, WithAuth(svc))
	}
	return r, cookie
}

// TestAPIRequiresAuth —— API 级登录墙的本体测试。
// 三个断言层层递进:没 cookie 401 → 带上 cookie 同一请求 200 →
// 伪造就一个 cookie 还是 401(只认会话表里的真 token)。
func TestAPIRequiresAuth(t *testing.T) {
	r, cookie := newAuthedTestRouter(t, true)
	body := threePointsBody("")

	// 1) 无 cookie:合法的业务请求也必须 401 —— 这就是要测的墙
	code, resp := doJSON(t, r, http.MethodPost, "/plan", body)
	if code != http.StatusUnauthorized {
		t.Fatalf("未登录调 /plan 应 401, got %d body=%s", code, resp)
	}
	if !strings.Contains(resp, "请先登录") {
		t.Errorf("401 响应应带可行动的中文提示, got %s", resp)
	}

	// 2) 伪造 cookie:token 不在会话表里,和没带一样
	code, _ = doJSON(t, r, http.MethodPost, "/plan", body, auth.CookieName+"=forged-token")
	if code != http.StatusUnauthorized {
		t.Fatalf("伪造 cookie 应 401, got %d", code)
	}

	// 3) 合法 cookie:同一请求放行 —— 保护不能拦死正常用户
	code, resp = doJSON(t, r, http.MethodPost, "/plan", body, cookie)
	if code != http.StatusOK {
		t.Fatalf("带合法 cookie 应 200, got %d body=%s", code, resp)
	}

	// 4) /search /route 同墙:保护必须覆盖全部业务接口,不能漏挂
	code, _ = doJSON(t, r, http.MethodGet, "/search?q=x", "", cookie)
	if code != http.StatusOK {
		t.Errorf("/search 未带保护或合法 cookie 被拦: %d", code)
	}
	code, _ = doJSON(t, r, http.MethodGet, "/route?origin=116.4,39.9&dest=116.41,39.91&mode=transit", "", cookie)
	if code != http.StatusOK {
		t.Errorf("/route 未带保护或合法 cookie 被拦: %d", code)
	}
}

// TestGuestModeStillWorks 钉住降级语义:auth 未装配(没配 PG_DSN)时,
// 业务接口保持游客可用 —— 服务不能因为没配库就把自己锁死。
func TestGuestModeStillWorks(t *testing.T) {
	r := newTestRouter(t, true)
	code, body := doJSON(t, r, http.MethodPost, "/plan", threePointsBody(""))
	if code != http.StatusOK {
		t.Fatalf("游客模式(未装配 auth)下 /plan 应照常 200, got %d body=%s", code, body)
	}
}
