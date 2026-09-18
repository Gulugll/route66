// Package settings 提供"运行时可改的配置"(目前就是高德的两把 key + 安全密钥)。
//
// 优先级约定(全项目的单一真相源,别处不许再发明):
//
//	DB(app_settings 表,管理端写入)  >  env(只做兜底默认值)
//
// 为什么不每次请求都查库:管理端改一次 key,后面成千上万次请求读的都是同一个值 ——
// 典型的"读多写少",用读写锁缓存挡住,写路径(Set)负责把缓存一起更新,
// 做到"改完立即生效,不用重启"。这就是 KeyProvider 能热生效的全部原理。
package settings

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// 合法配置名。管理端 API 只接受这三个名字,
// 白名单同时挡住了"往 settings 表塞任意键"的滥用。
const (
	KeyAmapRest  = "amap_rest_key" // 高德 Web服务 key(后端 REST 用,绝不下发前端)
	KeyAmapJS    = "amap_js_key"   // 高德 JS API key(浏览器渲染地图用,下发不泄密)
	KeyAmapJSSec = "amap_js_sec"   // JS key 的安全密钥(同样要下发浏览器)
)

// ErrUnknownKey 管理端试图读写白名单之外的配置名。
var ErrUnknownKey = errors.New("unknown setting key")

// Setting app_settings 表模型。key 是主键;value 允许空串 ——
// 空串有语义:"管理端显式清除了这项,请回落 env"(和"没配过"在 Get 的返回上等价)。
type Setting struct {
	Key       string    `gorm:"column:key;type:varchar(64);primaryKey"`
	Value     string    `gorm:"column:value;type:text;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (Setting) TableName() string { return "app_settings" }

// SettingStore 存储接口:GORM 实现 + 内存 fake,和 auth.Store 同一思路。
type SettingStore interface {
	Get(ctx context.Context, key string) (string, bool, error) // bool = 记录存在
	Set(ctx context.Context, key, value string) error          // value="" 等价于删除
	Delete(ctx context.Context, key string) error
}

// GormSettingStore 真库实现。
type GormSettingStore struct {
	db *gorm.DB
}

func NewGormSettingStore(db *gorm.DB) *GormSettingStore { return &GormSettingStore{db: db} }

func (s *GormSettingStore) Migrate(ctx context.Context) error {
	return s.db.WithContext(ctx).AutoMigrate(&Setting{})
}

func (s *GormSettingStore) Get(ctx context.Context, key string) (string, bool, error) {
	var rec Setting
	err := s.db.WithContext(ctx).First(&rec, "\"key\" = ?", key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return rec.Value, true, nil
}

func (s *GormSettingStore) Set(ctx context.Context, key, value string) error {
	// Upsert:PG 用 ON CONFLICT 一条语句完成"有则改无则插"。
	// value 为空串走 Delete,语义是"管理端清除了这项"。
	if value == "" {
		return s.Delete(ctx, key)
	}
	rec := Setting{Key: key, Value: value, UpdatedAt: time.Now()}
	return s.db.WithContext(ctx).
		Where("\"key\" = ?", key).
		Assign(rec).
		FirstOrCreate(&rec).Error
}

func (s *GormSettingStore) Delete(ctx context.Context, key string) error {
	return s.db.WithContext(ctx).Delete(&Setting{}, "\"key\" = ?", key).Error
}

// Provider 对外的读写门面。
// fallback 是"env 兜底查询":按配置名返回环境变量值,由 main 注入 ——
// settings 包自己不读环境变量(集中到 config.Load 是项目的既有约定)。
type Provider struct {
	store    SettingStore
	fallback func(name string) string

	mu    sync.RWMutex      // 保护 cache:读多写少,RWMutex 比互斥锁吞吐好
	cache map[string]string // name → 当前生效值(可能来自 DB 也可能来自 env)
}

func NewProvider(store SettingStore, fallback func(name string) string) *Provider {
	return &Provider{
		store:    store,
		fallback: fallback,
		cache:    map[string]string{},
	}
}

// Get 按"DB > env > 空"的优先级取值。结果进缓存,
// 所以这个方法可以放在每次请求的路径上,不用心疼。
// store 为 nil(纯 env 模式,没配 PG)时直接走 fallback,不报错 ——
// "没配数据库"不该让 key 查询这一条路也死掉。
func (p *Provider) Get(ctx context.Context, name string) (string, error) {
	if !isKnownKey(name) {
		return "", ErrUnknownKey
	}
	p.mu.RLock()
	if v, ok := p.cache[name]; ok {
		p.mu.RUnlock()
		return v, nil
	}
	p.mu.RUnlock()

	if p.store == nil {
		v := p.fallback(name)
		p.mu.Lock()
		p.cache[name] = v
		p.mu.Unlock()
		return v, nil
	}

	v, ok, err := p.store.Get(ctx, name)
	if err != nil {
		return "", fmt.Errorf("load setting %s: %w", name, err)
	}
	if !ok || v == "" {
		v = p.fallback(name) // DB 没配/被清空 → 回落 env 兜底
	}
	p.mu.Lock()
	p.cache[name] = v
	p.mu.Unlock()
	return v, nil
}

// Set 管理端写入。value 为空串 = 清除该项(回落 env)。
// 写库成功后立刻更新缓存 —— "热生效"就发生在这两行之间,不需要重启。
func (p *Provider) Set(ctx context.Context, name, value string) error {
	if !isKnownKey(name) {
		return ErrUnknownKey
	}
	if err := p.store.Set(ctx, name, strings.TrimSpace(value)); err != nil {
		return fmt.Errorf("save setting %s: %w", name, err)
	}
	p.mu.Lock()
	if value == "" {
		// 清除后要重新走"回落 env"的取值链,不能把空串缓存进去了事
		p.cache[name] = p.fallback(name)
	} else {
		p.cache[name] = strings.TrimSpace(value)
	}
	p.mu.Unlock()
	return nil
}

// HasDBValue 只回答"DB 里有没有这项"(不管 env 兜底)。
// 管理端回显来源(db/env/none)就靠它区分前两种。
func (p *Provider) HasDBValue(ctx context.Context, name string) (string, bool, error) {
	if !isKnownKey(name) {
		return "", false, ErrUnknownKey
	}
	return p.store.Get(ctx, name)
}

func isKnownKey(name string) bool {
	switch name {
	case KeyAmapRest, KeyAmapJS, KeyAmapJSSec:
		return true
	}
	return false
}
