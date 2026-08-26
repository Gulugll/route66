// Package cache 提供"距离缓存"抽象。
//
// 为什么需要缓存:高德 API 有免费调用额度,而 TSP 求解会反复问"同一点对的距离"
// (2-opt/模拟退火每轮迭代都要查矩阵)。命中缓存就不用再发外部请求。
//
// 为什么 key 是 string:缓存的 key 结构是业务的事(amap 知道"驾车"和"步行"
// 同一个点对是两个不同 key),cache 只负责"存值取值",不掺和业务——单一职责。
package cache

import (
	"sync"
	"time"
)

// Cache 缓存"字符串 key → 距离(公里)"。
// key 由调用方(amap)构造,比如 "driving|113.3,23.1->113.3,23.1",
// 带出行方式前缀,不同方式的同一对点互不干扰。
type Cache interface {
	Get(key string) (km float64, ok bool)
	Set(key string, km float64)
}

// Memory 最简单的内存实现:一个 map + 一把锁 + 过期时间。
// map 存键值, sync.Mutex 保证并发安全(gin 是并发处理请求的),
// TTL 让旧数据过期,避免"路修好了距离还缓存着"的脏数据。
type Memory struct {
	mu  sync.Mutex
	m   map[string]memEntry
	ttl time.Duration
}

type memEntry struct {
	km      float64
	expires time.Time
}

func NewMemory(ttl time.Duration) *Memory {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &Memory{m: make(map[string]memEntry), ttl: ttl}
}

// Get 查询缓存。没命中或已过期都算"没有"。
func (c *Memory) Get(key string) (float64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.m[key]
	if !ok {
		return 0, false
	}
	if time.Now().After(e.expires) {
		delete(c.m, key) // 过期即删,map 不无限膨胀
		return 0, false
	}
	return e.km, true
}

func (c *Memory) Set(key string, km float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = memEntry{km: km, expires: time.Now().Add(c.ttl)}
}
