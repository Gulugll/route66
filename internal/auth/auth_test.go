package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── 内存 fake:单测的存储实现,和 worker_test 的 fakeRepo 同一思路 ──

type fakeStore struct {
	mu       sync.Mutex
	users    map[string]*User // username → user
	sessions map[string]*Session
	nextID   int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{users: map[string]*User{}, sessions: map[string]*Session{}}
}

func (f *fakeStore) CreateUser(_ context.Context, u *User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.users[u.Username]; ok {
		return ErrUsernameTaken
	}
	f.nextID++
	u.ID = f.nextID
	f.users[u.Username] = u
	return nil
}

func (f *fakeStore) GetUserByName(_ context.Context, username string) (User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.users[username]; ok {
		return *u, nil
	}
	return User{}, ErrNotFound
}

func (f *fakeStore) GetUserByID(_ context.Context, id int64) (User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.users {
		if u.ID == id {
			return *u, nil
		}
	}
	return User{}, ErrNotFound
}

func (f *fakeStore) ListUsers(_ context.Context) ([]User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]User, 0, len(f.users))
	for _, u := range f.users {
		out = append(out, *u)
	}
	return out, nil
}

func (f *fakeStore) CreateSession(_ context.Context, s *Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[s.Token] = s
	return nil
}

func (f *fakeStore) GetSession(_ context.Context, token string) (Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.sessions[token]; ok {
		return *s, nil
	}
	return Session{}, ErrNotFound
}

func (f *fakeStore) DeleteSession(_ context.Context, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessions, token)
	return nil
}

// ── 测试 ──

// TestRegisterLoginLogout 走通"注册 → 登录 → 认人 → 登出 → 认不出"一条龙。
// 这条链是登录系统的主干,任何一环断了这里先红。
func TestRegisterLoginLogout(t *testing.T) {
	svc := NewService(newFakeStore())
	ctx := context.Background()

	if _, err := svc.Register(ctx, "李小明", "secret66"); err != nil {
		t.Fatalf("register: %v", err)
	}

	u, token, err := svc.Login(ctx, "李小明", "secret66")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if u.Role != RoleUser {
		t.Errorf("新用户角色 = %q, want %q", u.Role, RoleUser)
	}
	if len(token) != 64 || strings.ContainsAny(token, "ghijklmnopqrstuvwxyz") {
		t.Errorf("token 应为 64 位 hex, got %q", token)
	}

	got, err := svc.UserFromToken(ctx, token)
	if err != nil || got.Username != "李小明" {
		t.Fatalf("UserFromToken = %v, %v", got, err)
	}

	if err := svc.Logout(ctx, token); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := svc.UserFromToken(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Errorf("登出后 token 应失效, got %v", err)
	}
}

// TestLoginNoUserEnumeration:用户名不存在和密码错误必须返回**同一个**错误 ——
// 区分了就等于帮攻击者验证"这个用户名存在"。
func TestLoginNoUserEnumeration(t *testing.T) {
	svc := NewService(newFakeStore())
	ctx := context.Background()
	_, _ = svc.Register(ctx, "realuser", "secret66")

	_, _, errNoUser := svc.Login(ctx, "ghost", "whatever")
	_, _, errBadPass := svc.Login(ctx, "realuser", "wrongpass")
	if !errors.Is(errNoUser, ErrBadCredentials) || !errors.Is(errBadPass, ErrBadCredentials) {
		t.Fatalf("两种失败应返回同一个 ErrBadCredentials: %v / %v", errNoUser, errBadPass)
	}
}

// TestRegisterValidation:用户名/密码的基本防线。
func TestRegisterValidation(t *testing.T) {
	svc := NewService(newFakeStore())
	ctx := context.Background()

	cases := []struct {
		name, pass string
		wantErr    bool
	}{
		{"ab", "secret66", true},                             // 用户名太短
		{"小明", "secret66", true},                             // 1 个字符(中文也算 1 个 rune)
		{strings.Repeat("好名字", 11), "secret66", true},        // 33 rune,超上限
		{strings.Repeat("好名字", 10) + "x", "secret66", false}, // 31 rune,合法
		{"okname", "12345", true},                            // 密码太短
		{"okname", "secret66", false},                        // 正常
		{"  okname  ", "secret66", true},                     // 去空格后与已有用户重名
	}
	for _, c := range cases {
		_, err := svc.Register(ctx, c.name, c.pass)
		if (err != nil) != c.wantErr {
			t.Errorf("Register(%q,%q) err=%v, wantErr=%v", c.name, c.pass, err, c.wantErr)
		}
	}
}

// TestUsernameTaken:重名注册返回 ErrUsernameTaken。
func TestUsernameTaken(t *testing.T) {
	svc := NewService(newFakeStore())
	ctx := context.Background()
	_, _ = svc.Register(ctx, "taken", "secret66")
	if _, err := svc.Register(ctx, "taken", "other66"); !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("err = %v, want ErrUsernameTaken", err)
	}
}

// TestExpiredSession:过期的会话必须无效,且会被顺手清理。
// "顺便删"不是可有可无 —— sessions 表不被死 token 撑大就靠它。
func TestExpiredSession(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store)
	ctx := context.Background()
	_, _ = svc.Register(ctx, "user1", "secret66")

	// 手工塞一个已过期的会话(正常流程不会产生这种会话)
	store.CreateSession(ctx, &Session{Token: "dead", UserID: 1,
		ExpiresAt: time.Now().Add(-time.Minute)})

	if _, err := svc.UserFromToken(ctx, "dead"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("过期会话应无效, got %v", err)
	}
	if _, ok := store.sessions["dead"]; ok {
		t.Error("过期会话应被顺手删除")
	}
}

// TestSeedAdmin:种子管理员 —— 不存在则创建为 admin;已存在则**不动**它
// (环境变量改了不该悄悄重置已有账号的密码/角色)。
func TestSeedAdmin(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store)
	ctx := context.Background()

	if err := svc.EnsureSeedAdmin(ctx, "admin", "first-pass"); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	u, err := store.GetUserByName(ctx, "admin")
	if err != nil || u.Role != RoleAdmin {
		t.Fatalf("种子账号 = %v, %v", u, err)
	}

	// 第二次种子:密码不同,但已有账号必须原样不动
	if err := svc.EnsureSeedAdmin(ctx, "admin", "changed-pass"); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if _, _, err := svc.Login(ctx, "admin", "first-pass"); err != nil {
		t.Errorf("已有账号密码被种子覆盖了: %v", err)
	}
}

// TestMaskToken:掩码规则 —— 能对上是哪个会话,但看不出内容。
func TestMaskToken(t *testing.T) {
	if got := MaskToken("abcdef1234567890"); got != "abcd****7890" {
		t.Errorf("MaskToken = %q", got)
	}
	if got := MaskToken("short"); got != "*****" {
		t.Errorf("短 token 应全掩码, got %q", got)
	}
}
