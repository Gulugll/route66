package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
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

func (f *fakeStore) DeleteExpiredSessions(_ context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for tok, s := range f.sessions {
		if time.Now().After(s.ExpiresAt) {
			delete(f.sessions, tok)
			n++
		}
	}
	return n, nil
}

func (f *fakeStore) UpdateProfile(_ context.Context, userID int64, nickname, bio string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.users {
		if u.ID == userID {
			u.Nickname = nickname
			u.Bio = bio
			return nil
		}
	}
	return ErrNotFound
}

func (f *fakeStore) SetAvatarURL(_ context.Context, userID int64, avatarURL string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.users {
		if u.ID == userID {
			u.AvatarURL = avatarURL
			return nil
		}
	}
	return ErrNotFound
}

// fakeLimiter 限速器 fake:blocked 时拒绝,其余放行;记账三个动作供断言。
type fakeLimiter struct {
	mu       sync.Mutex
	blocked  bool
	checks   int
	failures int
	resets   int
}

func (f *fakeLimiter) Check(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks++
	if f.blocked {
		return ErrLoginThrottled
	}
	return nil
}

func (f *fakeLimiter) RecordFailure(context.Context, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures++
}

func (f *fakeLimiter) Reset(context.Context, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resets++
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

	u, token, err := svc.Login(ctx, "127.0.0.1", "李小明", "secret66")
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

	_, _, errNoUser := svc.Login(ctx, "127.0.0.1", "ghost", "whatever")
	_, _, errBadPass := svc.Login(ctx, "127.0.0.1", "realuser", "wrongpass")
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

// TestExpiredSession:过期的会话必须无效,且会被清理。
// 清理不可省略:sessions 表的体积控制依赖它。
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
// (环境变量变更不应重置已有账号的密码/角色)。
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
	if _, _, err := svc.Login(ctx, "127.0.0.1", "admin", "first-pass"); err != nil {
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

// TestCleanupExpiredSessions:周期清理只删过期行,有效会话一根汗毛不能动。
// 这是 sessions 表体积控制的兜底路径(UserFromToken 的顺手删覆盖不到死会话)。
func TestCleanupExpiredSessions(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store)
	ctx := context.Background()
	_, _ = svc.Register(ctx, "user1", "secret66")
	_, token, err := svc.Login(ctx, "127.0.0.1", "user1", "secret66")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	// 手工塞两个过期会话(正常流程不会产生这种会话)
	store.CreateSession(ctx, &Session{Token: "dead1", UserID: 1,
		ExpiresAt: time.Now().Add(-2 * time.Hour)})
	store.CreateSession(ctx, &Session{Token: "dead2", UserID: 1,
		ExpiresAt: time.Now().Add(-time.Minute)})

	n, err := svc.CleanupExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if n != 2 {
		t.Errorf("应清理 2 个过期会话, got %d", n)
	}
	for _, dead := range []string{"dead1", "dead2"} {
		if _, ok := store.sessions[dead]; ok {
			t.Errorf("过期会话 %s 未被清理", dead)
		}
	}
	if _, ok := store.sessions[token]; !ok {
		t.Error("有效会话不应被清理")
	}
}

// TestLoginThrottledBlocksBeforeCredentials:限速闸门必须在查库/算 bcrypt 之前 ——
// 用户名根本不存在,返回的也应该是限速错误而不是凭据错误,
// 证明请求压根没走到凭据校验那一步。
func TestLoginThrottledBlocksBeforeCredentials(t *testing.T) {
	lim := &fakeLimiter{blocked: true}
	svc := NewService(newFakeStore(), WithLimiter(lim))

	_, _, err := svc.Login(context.Background(), "1.2.3.4", "ghost", "whatever")
	if !errors.Is(err, ErrLoginThrottled) {
		t.Fatalf("被限速的登录应返回 ErrLoginThrottled, got %v", err)
	}
	lim.mu.Lock()
	defer lim.mu.Unlock()
	if lim.checks != 1 || lim.failures != 0 {
		t.Errorf("checks=%d failures=%d, want 1/0(被拒的尝试不记账)", lim.checks, lim.failures)
	}
}

// TestLoginRecordsFailureAndReset:限速的三条记账规则 ——
// 密码错记账、用户名不存在记账(同属失败尝试)、成功清零。
func TestLoginRecordsFailureAndReset(t *testing.T) {
	store := newFakeStore()
	lim := &fakeLimiter{}
	svc := NewService(store, WithLimiter(lim))
	ctx := context.Background()
	_, _ = svc.Register(ctx, "user1", "secret66")

	svc.Login(ctx, "1.2.3.4", "user1", "wrong") // 密码错
	svc.Login(ctx, "1.2.3.4", "ghost", "wrong") // 用户名不存在
	lim.mu.Lock()
	if lim.failures != 2 {
		t.Errorf("两次失败应各记一笔, failures=%d", lim.failures)
	}
	if lim.resets != 0 {
		t.Errorf("失败不应触发清零, resets=%d", lim.resets)
	}
	lim.mu.Unlock()

	if _, _, err := svc.Login(ctx, "1.2.3.4", "user1", "secret66"); err != nil {
		t.Fatalf("正确凭据应登录成功: %v", err)
	}
	lim.mu.Lock()
	defer lim.mu.Unlock()
	if lim.resets != 1 {
		t.Errorf("登录成功应清零计数, resets=%d", lim.resets)
	}
}

// TestLoginNoLimiterStillWorks:不挂限速器(Redis 不可用的降级形态)登录照常 ——
// fail open 不能把正常用户锁在门外。
func TestLoginNoLimiterStillWorks(t *testing.T) {
	svc := NewService(newFakeStore()) // 没 WithLimiter
	_, _ = svc.Register(context.Background(), "user1", "secret66")
	if _, _, err := svc.Login(context.Background(), "1.2.3.4", "user1", "secret66"); err != nil {
		t.Errorf("无限速器时登录应照常, got %v", err)
	}
}

// TestIsDuplicateKey 钉住"错误类型是契约"的唯一约束判定。
// 三个用例各有含义:1062 是 MySQL 唯一键冲突的真身;
// 1045 是 access denied(同为 MySQLError 但不是重复键)——证明不是
// "见 MySQLError 就认";最后一条是旧 PG 方案的报错文案——证明
// 退回字符串匹配时本测试会红(变异验证的锚点)。
func TestIsDuplicateKey(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"mysql 1062 duplicate entry", &mysql.MySQLError{Number: 1062}, true},
		{"mysql 1045 access denied", &mysql.MySQLError{Number: 1045}, false},
		{"legacy pg message string", errors.New("duplicate key value violates unique constraint"), false},
		{"plain error", errors.New("boom"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDuplicateKey(tc.err); got != tc.want {
				t.Fatalf("isDuplicateKey(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestUpdateProfile 钉住资料更新的校验与错误翻译。
// 长度用 rune 计数:中文昵称一个字就是一个 rune,len() 数字节会低估一倍以上。
func TestUpdateProfile(t *testing.T) {
	s := NewService(newFakeStore())
	ctx := context.Background()
	if _, err := s.Register(ctx, "profileuser", "pass123"); err != nil {
		t.Fatalf("register: %v", err)
	}
	u, err := s.store.GetUserByName(ctx, "profileuser")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}

	t.Run("正常更新", func(t *testing.T) {
		if err := s.UpdateProfile(ctx, u.ID, "小明", "喜欢走路上班"); err != nil {
			t.Fatalf("UpdateProfile: %v", err)
		}
		got, err := s.store.GetUserByID(ctx, u.ID)
		if err != nil {
			t.Fatalf("get user: %v", err)
		}
		if got.Nickname != "小明" || got.Bio != "喜欢走路上班" {
			t.Fatalf("nickname/bio 未更新: %+v", got)
		}
	})

	t.Run("昵称超长", func(t *testing.T) {
		long := strings.Repeat("名", 33)
		err := s.UpdateProfile(ctx, u.ID, long, "")
		if err == nil || !strings.Contains(err.Error(), "32") {
			t.Fatalf("期望昵称超长报错,得到: %v", err)
		}
	})

	t.Run("中文昵称 32 字合法", func(t *testing.T) {
		ok := strings.Repeat("字", 32)
		if err := s.UpdateProfile(ctx, u.ID, ok, ""); err != nil {
			t.Fatalf("32 个中文昵称应合法: %v", err)
		}
	})

	t.Run("简介超长", func(t *testing.T) {
		long := strings.Repeat("b", 201)
		if err := s.UpdateProfile(ctx, u.ID, "", long); err == nil {
			t.Fatal("期望简介超长报错")
		}
	})

	t.Run("用户不存在", func(t *testing.T) {
		if err := s.UpdateProfile(ctx, 99999, "x", ""); !errors.Is(err, ErrNotFound) {
			t.Fatalf("期望 ErrNotFound,得到: %v", err)
		}
	})
}

func TestSetAvatarURL(t *testing.T) {
	s := NewService(newFakeStore())
	ctx := context.Background()
	if _, err := s.Register(ctx, "avataruser", "pass123"); err != nil {
		t.Fatalf("register: %v", err)
	}
	u, err := s.store.GetUserByName(ctx, "avataruser")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	url := "/uploads/avatars/2/1.jpg"
	if err := s.SetAvatarURL(ctx, u.ID, url); err != nil {
		t.Fatalf("SetAvatarURL: %v", err)
	}
	got, _ := s.store.GetUserByID(ctx, u.ID)
	if got.AvatarURL != url {
		t.Fatalf("avatar_url 未更新: %q", got.AvatarURL)
	}
	if err := s.SetAvatarURL(ctx, 99999, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("期望 ErrNotFound,得到: %v", err)
	}
}
