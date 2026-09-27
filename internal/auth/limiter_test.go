// limiter_test.go —— RedisLoginLimiter 的集成测试,用真 Redis:
// INCR/TTL/DEL 的语义没法用 fake 模拟出来(worker_test 的 redisReachable 同一约定)。
package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"
)

func limiterRedisReachable() bool {
	conn, err := net.DialTimeout("tcp", "localhost:6379", 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// TestRedisLoginLimiter 钉住限速器的核心语义:
// 失败未达上限放行 → 达到上限拒绝 → Reset 清零后重新放行。
func TestRedisLoginLimiter(t *testing.T) {
	if !limiterRedisReachable() {
		t.Skip("本机 6379 没有可用的 Redis,跳过(启动方法见 README)")
	}
	rl, err := NewRedisLoginLimiter("localhost:6379", 3, time.Minute)
	if err != nil {
		t.Fatalf("创建限速器: %v", err)
	}
	ctx := context.Background()
	// 每次运行用独立 ip:测试之间、测试和真实服务互不吃计数
	ip := fmt.Sprintf("test:%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = rl.client.Del(ctx, limiterKey(ip)).Err() })

	for i := 0; i < 3; i++ {
		if err := rl.Check(ctx, ip); err != nil {
			t.Fatalf("第 %d 次失败不应触发限速, got %v", i+1, err)
		}
		rl.RecordFailure(ctx, ip)
	}
	if err := rl.Check(ctx, ip); !errors.Is(err, ErrLoginThrottled) {
		t.Fatalf("3 次失败后应被限速, got %v", err)
	}

	rl.Reset(ctx, ip)
	if err := rl.Check(ctx, ip); err != nil {
		t.Fatalf("Reset 后应重新放行, got %v", err)
	}
}

// TestRedisLoginLimiterTTL:失败计数必须带 TTL —— 否则一次失败背一辈子。
// 直接查 Redis 的 TTL 验证,不看实现细节。
func TestRedisLoginLimiterTTL(t *testing.T) {
	if !limiterRedisReachable() {
		t.Skip("本机 6379 没有可用的 Redis,跳过(启动方法见 README)")
	}
	rl, err := NewRedisLoginLimiter("localhost:6379", 3, time.Minute)
	if err != nil {
		t.Fatalf("创建限速器: %v", err)
	}
	ctx := context.Background()
	ip := fmt.Sprintf("ttl:%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = rl.client.Del(ctx, limiterKey(ip)).Err() })

	rl.RecordFailure(ctx, ip)
	ttl, err := rl.client.TTL(ctx, limiterKey(ip)).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 || ttl > time.Minute {
		t.Errorf("计数 key 应带 0 < TTL <= window 的过期时间, got %v", ttl)
	}
}
