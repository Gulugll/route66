// limiter.go —— 登录限速:按来源 IP 的失败计数器,存 Redis。
//
// 为什么限速放 auth 包而不是 handler:防爆破和登录是同一条安全线,
// 属于登录业务本身;api 层只负责把 ErrLoginThrottled 翻译成 429。
//
// 设计取舍:
//   - 只数失败,登录成功即清零:办公室 NAT 后面一群人正常登录不该被连坐;
//     成功登录也计数的话,10 个人同一分钟登录就会误伤。
//   - Redis 挂了 fail open(放行 + 日志):限速器是防御件,它故障
//     不能把登录主流程一起打死 —— 可用性优先,日志里留排查线索。
//   - 只按 IP 一个维度:分布式撞库(多 IP 打同一个账号)靠它挡不住,
//     要挡再加"按用户名"维度的计数器,接口不用变。
package auth

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrLoginThrottled 触发限速。handler 遇到它必须返回 429。
var ErrLoginThrottled = errors.New("尝试过于频繁，请稍后再试")

// LoginLimiter 登录限速接口。Redis 管线上,内存 fake 管单测
// (和 Store 接口同一思路:业务只依赖接口)。
type LoginLimiter interface {
	// Check 登录尝试前的闸门:nil=放行,ErrLoginThrottled=拒绝。
	Check(ctx context.Context, ip string) error
	// RecordFailure 记一次登录失败。实现内部吞掉错误:限速数据写失败
	// 不该影响登录主流程。
	RecordFailure(ctx context.Context, ip string)
	// Reset 登录成功后清零 —— 正常用户偶尔输错留下的计数不该一直背在身上。
	Reset(ctx context.Context, ip string)
}

// RedisLoginLimiter 固定窗口失败计数器:窗口内失败达上限即拒绝后续尝试。
type RedisLoginLimiter struct {
	client *redis.Client
	limit  int           // 窗口内允许的失败次数上限
	window time.Duration // 计数窗口
}

// NewRedisLoginLimiter 创建限速器。构造时 Ping 验证连通性 —— 连不上返回
// error,由装配层(main)决定"不限速地跑"还是整体不启动。
func NewRedisLoginLimiter(addr string, limit int, window time.Duration) (*RedisLoginLimiter, error) {
	client := redis.NewClient(&redis.Options{Addr: addr})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return &RedisLoginLimiter{client: client, limit: limit, window: window}, nil
}

// limiterKey 每个 IP 一把钥匙,前缀隔离业务(和 dist: 前缀同一约定),
// redis-cli KEYS 'login_fail:*' 一眼看到所有计数。
func limiterKey(ip string) string { return "login_fail:" + ip }

func (l *RedisLoginLimiter) Check(ctx context.Context, ip string) error {
	n, err := l.client.Get(ctx, limiterKey(ip)).Int()
	if errors.Is(err, redis.Nil) {
		return nil // 还没有失败记录,直接放行
	}
	if err != nil {
		log.Printf("[auth] limiter check failed (fail open): %v", err)
		return nil
	}
	if n >= l.limit {
		return ErrLoginThrottled
	}
	return nil
}

func (l *RedisLoginLimiter) RecordFailure(ctx context.Context, ip string) {
	// INCR + EXPIRE 打包成事务:计数和 TTL 原子生效,
	// 不会出现"计了数但忘了设 TTL"的永生 key。
	// EXPIRE 每次都刷:攻击不停窗口就顺延(更严);停手 window 后自动解封。
	pipe := l.client.TxPipeline()
	pipe.Incr(ctx, limiterKey(ip))
	pipe.Expire(ctx, limiterKey(ip), l.window)
	if _, err := pipe.Exec(ctx); err != nil {
		log.Printf("[auth] limiter record failed: %v", err)
	}
}

func (l *RedisLoginLimiter) Reset(ctx context.Context, ip string) {
	if err := l.client.Del(ctx, limiterKey(ip)).Err(); err != nil {
		log.Printf("[auth] limiter reset failed: %v", err)
	}
}
