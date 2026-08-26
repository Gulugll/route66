package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis 用 Redis 实现 Cache 接口。相比进程内 map 的两个优势:
//  1. 服务重启缓存不丢(数据在 Redis 进程里,不在服务进程里)
//  2. 多实例共享同一份缓存(所有实例连同一个 Redis)
//
// Get/Set 的接口和 Memory 完全一样——amap 客户端感知不到区别,
// 这正是上一步定义 Cache 接口的意义:换实现,零改动调用方。
type Redis struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedis 创建 Redis 缓存。addr 形如 "localhost:6379"。
// 构造时做一次 Ping 验证连通性——连不上直接返回 error,
// 由装配层(main)决定怎么降级,不在这个包里处理。
func NewRedis(addr string, ttl time.Duration) (*Redis, error) {
	client := redis.NewClient(&redis.Options{Addr: addr})

	// go-redis 用 context 控制每次命令的边界;这里给 Ping 设 3 秒上限,
	// 避免 Redis 没起时客户端卡住。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return &Redis{client: client, ttl: ttl}, nil
}

// keyPrefix 给业务 key 加前缀:避免和 Redis 里其他业务的数据撞名,
// 也方便排查(redis-cli KEYS 'dist:*' 一眼看到所有距离缓存)。
// key 本身由 amap 构造(带出行方式),这里只套一层前缀。
const keyPrefix = "dist:"

func (r *Redis) Get(key string) (float64, bool) {
	v, err := r.client.Get(context.Background(), keyPrefix+key).Float64()
	if err != nil {
		// 没命中 / Redis 暂时不可用,都算"没有"。
		// 缓存系统必须容忍后端故障:宁可多打一次高德,也不能因此报错。
		return 0, false
	}
	return v, true
}

func (r *Redis) Set(key string, km float64) {
	// Set 带 TTL:go-redis 的第三个参数就是过期时间,到期自动删除,
	// 对应内存版的 expires 逻辑——TTL 由 Redis 自己管,不用我们清。
	ctx := context.Background()
	r.client.Set(ctx, keyPrefix+key, km, r.ttl)
}
