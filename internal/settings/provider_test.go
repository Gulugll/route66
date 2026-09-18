package settings

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// ── 内存 fake ──

type fakeStore struct {
	mu     sync.Mutex
	fields map[string]string
}

func newFakeStore() *fakeStore { return &fakeStore{fields: map[string]string{}} }

func (f *fakeStore) Get(_ context.Context, key string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.fields[key]
	return v, ok, nil
}

func (f *fakeStore) Set(_ context.Context, key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if value == "" {
		delete(f.fields, key)
		return nil
	}
	f.fields[key] = value
	return nil
}

func (f *fakeStore) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.fields, key)
	return nil
}

// TestPriorityDBOverEnv 核心承诺:DB 里配的 > env 兜底。
// DB 没配 → 用 env;DB 一配 → 立刻切到 DB 的值;DB 清除 → 回落 env。
// 这三条就是"env 只做兜底"的完整语义。
func TestPriorityDBOverEnv(t *testing.T) {
	store := newFakeStore()
	envValues := map[string]string{KeyAmapRest: "env-key"}
	p := NewProvider(store, func(name string) string { return envValues[name] })
	ctx := context.Background()

	// 1. DB 没配 → env 兜底
	if v, _ := p.Get(ctx, KeyAmapRest); v != "env-key" {
		t.Fatalf("DB 未配置时应回落 env, got %q", v)
	}

	// 2. DB 配了 → 用 DB 的(且缓存生效后,env 值再变也轮不到它)
	if err := p.Set(ctx, KeyAmapRest, "db-key"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	envValues[KeyAmapRest] = "env-key-changed" // 故意改 env,验证优先级不受影响
	if v, _ := p.Get(ctx, KeyAmapRest); v != "db-key" {
		t.Fatalf("DB 配置后应用 DB 值, got %q", v)
	}

	// 3. 管理端清除(空串)→ 回落 env
	if err := p.Set(ctx, KeyAmapRest, ""); err != nil {
		t.Fatalf("Set empty: %v", err)
	}
	if v, _ := p.Get(ctx, KeyAmapRest); v != "env-key-changed" {
		t.Fatalf("清除后应回落 env, got %q", v)
	}
}

// TestSetHotReload:Set 之后**不重启**(同一个 Provider 实例)再 Get,
// 拿到的就是新值 —— "热生效"的语义就这一句话。
func TestSetHotReload(t *testing.T) {
	p := NewProvider(newFakeStore(), func(string) string { return "" })
	ctx := context.Background()
	if _, _ = p.Get(ctx, KeyAmapJS); true { // 先 Get 一次,把"空"写进缓存
	}
	if err := p.Set(ctx, KeyAmapJS, "js-123"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v, _ := p.Get(ctx, KeyAmapJS); v != "js-123" {
		t.Fatalf("Set 后应立即读到新值, got %q(缓存没更新就是 bug)", v)
	}
}

// TestUnknownKey:白名单之外的配置名一律拒绝 ——
// 不给"往 settings 表塞任意键"留门。
func TestUnknownKey(t *testing.T) {
	p := NewProvider(newFakeStore(), func(string) string { return "" })
	if _, err := p.Get(context.Background(), "not_a_key"); !errors.Is(err, ErrUnknownKey) {
		t.Errorf("Get 未知键 err=%v, want ErrUnknownKey", err)
	}
	if err := p.Set(context.Background(), "not_a_key", "x"); !errors.Is(err, ErrUnknownKey) {
		t.Errorf("Set 未知键 err=%v, want ErrUnknownKey", err)
	}
}

// TestFallbackCacheConsistency:env 兜底的值也会进缓存 ——
// 这是刻意的设计(取值链只有一条,缓存不用区分来源),测试钉住行为。
func TestFallbackCacheConsistency(t *testing.T) {
	calls := 0
	p := NewProvider(newFakeStore(), func(string) string {
		calls++
		return "env-val"
	})
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if v, _ := p.Get(ctx, KeyAmapJSSec); v != "env-val" {
			t.Fatalf("got %q", v)
		}
	}
	if calls != 1 {
		t.Errorf("fallback 应只被调 1 次(后续走缓存), 实际 %d 次", calls)
	}
}
