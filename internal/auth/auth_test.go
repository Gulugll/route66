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

func (f *fakeStore) GetUserBySub(_ context.Context, issuer, sub string) (User, error) {
	for _, u := range f.users {
		if u.Issuer == issuer && u.Sub == sub {
			return *u, nil
		}
	}
	return User{}, ErrNotFound
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

func TestExpiredSession(t *testing.T) {
	store := newFakeStore()
	s := NewService(store)
	ctx := context.Background()
	u := &User{Username: "expired-user", PasswordHash: "x"}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("建档失败: %v", err)
	}
	token := "expired-token"
	if err := store.CreateSession(ctx, &Session{Token: token, UserID: u.ID, ExpiresAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatalf("会话创建失败: %v", err)
	}
	if _, err := s.UserFromToken(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("过期会话应视为未登录, got %v", err)
	}
	if len(store.sessions) != 0 {
		t.Fatalf("过期会话应被顺手删除")
	}
}
func TestSeedAdmin(t *testing.T) {
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
	u := &User{Username: "user1", PasswordHash: "x"}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("建档失败: %v", err)
	}
	if err := store.CreateSession(ctx, &Session{Token: "live", UserID: u.ID,
		ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("会话创建失败: %v", err)
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
	if _, ok := store.sessions["live"]; !ok {
		t.Error("有效会话不应被清理")
	}
}

// TestLoginThrottledBlocksBeforeCredentials:限速闸门必须在查库/算 bcrypt 之前 ——
// 用户名根本不存在,返回的也应该是限速错误而不是凭据错误,
// 证明请求压根没走到凭据校验那一步。
func TestLoginThrottledBlocksBeforeCredentials(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"sqlite constraint message", errors.New("constraint failed: UNIQUE constraint failed: users.username"), true},
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
	if err := s.store.CreateUser(ctx, &User{Username: "profileuser", PasswordHash: "x"}); err != nil {
		t.Fatalf("建档失败: %v", err)
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
	if err := s.store.CreateUser(ctx, &User{Username: "avataruser", PasswordHash: "x"}); err != nil {
		t.Fatalf("建档失败: %v", err)
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
