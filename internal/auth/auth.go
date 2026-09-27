// Package auth 收口"账号 + 会话"的全部业务:注册、登录、登出、从 token 认人。
//
// 设计沿袭项目里一贯的两条线:
//  1. 存储走接口(Store) —— GORM 实现管真库,内存 fake 管单测,
//     单测不依赖任何外部服务就能跑(worker_test 的 fakeRepo 是同一个思路);
//  2. gin 中间件也在这里,api 层只做"把中间件挂到哪个路由"的决定。
//
// 安全红线(改代码前先读一遍):
//   - 密码只存 bcrypt hash。bcrypt 自带随机盐,同一个密码两次哈希结果不同,
//     所以库里看不到"哪些用户密码相同";校验用 CompareHashAndPassword,
//     它是常数时间比较,不惧时序侧信道。
//   - 登录失败统一返回"用户名或密码错误",不区分"没这个人"和"密码错" ——
//     区分了就等于帮攻击者枚举用户名。
//   - 会话 token 用 crypto/rand 的 32 字节(64 个 hex 字符),与用户任何信息无关;
//     靠"猜出来"的概率是 2^-256。
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// ErrNotFound 统一的"查无此人/查无此会话"。调用方(登录 handler)
// 据此把两种失败合并成同一句话 —— 防用户名枚举。
var ErrNotFound = errors.New("not found")

// ErrUsernameTaken 注册时用户名已存在。
var ErrUsernameTaken = errors.New("username taken")

// SessionTTL 一次登录的有效期。到期后的 token 一律视为无效。
const SessionTTL = 7 * 24 * time.Hour

// CookieName 会话 cookie 的名字。两端(7800/7801)必须用同一个名字 ——
// cookie 按 host 隔离不按端口隔离,同名才能共享登录态(这是设计,不是巧合)。
const CookieName = "rp_session"

// 用户角色。user = 普通用户;admin = 能进管理端配 key。
// 角色存在 users 表里而不是"admins 表",因为"是不是管理员"是用户的属性,
// 一人一行,查询不需要 JOIN。
const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

// User 账号。gorm 标签 = 表结构唯一真相源(与 repo.Task 同一约定)。
type User struct {
	ID           int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	Username     string `gorm:"column:username;type:varchar(64);uniqueIndex;not null" json:"username"`
	PasswordHash string `gorm:"column:password_hash;type:text;not null" json:"-"`
	Role         string `gorm:"column:role;type:varchar(16);not null;default:'user'" json:"role"`
	// 社区身份层(M1)。可空:空昵称时前端回退显示 username。
	// 不带 not null —— AutoMigrate 对已有表加可空列无需回填默认值。
	Nickname  string    `gorm:"column:nickname;type:varchar(32)" json:"nickname"`
	AvatarURL string    `gorm:"column:avatar_url;type:varchar(255)" json:"avatar_url"`
	Bio       string    `gorm:"column:bio;type:varchar(200)" json:"bio"`
	CreatedAt time.Time `gorm:"column:created_at" json:"created_at"`
}

func (User) TableName() string { return "users" }

// Session 服务端会话。token 是主键,查会话就是一次主键命中。
// 不存"登录 IP/UA"之类的审计字段 —— 教学规模用不上,真要审计再加。
type Session struct {
	Token     string    `gorm:"column:token;type:varchar(64);primaryKey"`
	UserID    int64     `gorm:"column:user_id;not null;index"`
	ExpiresAt time.Time `gorm:"column:expires_at;not null"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (Session) TableName() string { return "sessions" }

// Store 存储接口。GORM 和内存 fake 各实现一份。
type Store interface {
	CreateUser(ctx context.Context, u *User) error
	GetUserByName(ctx context.Context, username string) (User, error)
	GetUserByID(ctx context.Context, id int64) (User, error)
	ListUsers(ctx context.Context) ([]User, error)
	CreateSession(ctx context.Context, s *Session) error
	GetSession(ctx context.Context, token string) (Session, error)
	DeleteSession(ctx context.Context, token string) error
	// DeleteExpiredSessions 删除所有已过期的会话,返回删除条数。
	// UserFromToken 的"顺手删"只覆盖仍被查询的 token,
	// 没人再用的死会话靠周期任务兜底(见 RunSessionCleanup)。
	DeleteExpiredSessions(ctx context.Context) (int64, error)
	// UpdateProfile 改昵称与简介;头像走 SetAvatarURL(上传成功后的尾部动作)。
	UpdateProfile(ctx context.Context, userID int64, nickname, bio string) error
	// SetAvatarURL 只更新头像 URL。
	SetAvatarURL(ctx context.Context, userID int64, avatarURL string) error
}

// GormStore 真库实现。表由 Migrate 建立。
type GormStore struct {
	db *gorm.DB
}

// NewGormStore 复用 repo.NewGorm 建好的连接 —— 一次连接,多包共用,
// 不给同一个数据库开两条连接池。
func NewGormStore(db *gorm.DB) *GormStore { return &GormStore{db: db} }

// Migrate 建 users/sessions 两张表。启动时跑一次,和 tasks 表的迁移同一个时机。
func (s *GormStore) Migrate(ctx context.Context) error {
	return s.db.WithContext(ctx).AutoMigrate(&User{}, &Session{})
}

// isDuplicateKey 判断是否唯一约束冲突。用 errors.As 拿 *mysql.MySQLError
// 的 Number(1062),不匹配报错文案 —— 文案是驱动实现细节,类型才是契约。
// 旧 PG 方案用 strings.Contains("duplicate key") 匹配文案,换库即静默失效。
func isDuplicateKey(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

func (s *GormStore) CreateUser(ctx context.Context, u *User) error {
	err := s.db.WithContext(ctx).Create(u).Error
	if isDuplicateKey(err) {
		// 把数据库的方言错误翻译成业务错误:调用方不该知道底层库的报错长什么样
		return ErrUsernameTaken
	}
	return err
}

func (s *GormStore) GetUserByName(ctx context.Context, username string) (User, error) {
	var u User
	err := s.db.WithContext(ctx).Where("username = ?", username).First(&u).Error
	return fromGorm(u, err)
}

func (s *GormStore) GetUserByID(ctx context.Context, id int64) (User, error) {
	var u User
	err := s.db.WithContext(ctx).First(&u, id).Error
	return fromGorm(u, err)
}

func (s *GormStore) ListUsers(ctx context.Context) ([]User, error) {
	var users []User
	err := s.db.WithContext(ctx).Order("id").Find(&users).Error
	return users, err
}

func (s *GormStore) CreateSession(ctx context.Context, sess *Session) error {
	return s.db.WithContext(ctx).Create(sess).Error
}

func (s *GormStore) GetSession(ctx context.Context, token string) (Session, error) {
	var sess Session
	err := s.db.WithContext(ctx).First(&sess, "token = ?", token).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Session{}, ErrNotFound
	}
	return sess, err
}

func (s *GormStore) DeleteSession(ctx context.Context, token string) error {
	return s.db.WithContext(ctx).Delete(&Session{}, "token = ?", token).Error
}

// DeleteExpiredSessions 一条 DELETE 扫全表过期行。expires_at 上有查询需求
// 但频率低(每小时一次),不为它单独建索引。
func (s *GormStore) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	res := s.db.WithContext(ctx).Where("expires_at < ?", time.Now()).Delete(&Session{})
	return res.RowsAffected, res.Error
}

func (s *GormStore) UpdateProfile(ctx context.Context, userID int64, nickname, bio string) error {
	res := s.db.WithContext(ctx).Model(&User{}).Where("id = ?", userID).
		Updates(map[string]any{"nickname": nickname, "bio": bio})
	// RowsAffected == 0 = 没这个人。GORM 的 Updates(struct) 会跳过零值字段,
	// 把昵称清成空串会被静默忽略 —— 所以这里必须用 map,这是 GORM 最经典的坑之一。
	if res.Error == nil && res.RowsAffected == 0 {
		return ErrNotFound
	}
	return res.Error
}

func (s *GormStore) SetAvatarURL(ctx context.Context, userID int64, avatarURL string) error {
	res := s.db.WithContext(ctx).Model(&User{}).Where("id = ?", userID).
		Update("avatar_url", avatarURL)
	if res.Error == nil && res.RowsAffected == 0 {
		return ErrNotFound
	}
	return res.Error
}

func fromGorm(u User, err error) (User, error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return User{}, ErrNotFound
	}
	return u, err
}

// Service 认证业务。只依赖 Store 接口,测试塞 fake;
// limiter 可为 nil(Redis 没配/挂了就不限速,fail open)。
type Service struct {
	store   Store
	limiter LoginLimiter
}

// Option 装配可选项。可变参让 NewService(store) 的旧调用保持兼容。
type Option func(*Service)

// WithLimiter 挂登录限速器。不挂 = 不限速(本地开发/Redis 不可用的降级形态)。
func WithLimiter(l LoginLimiter) Option { return func(s *Service) { s.limiter = l } }

func NewService(store Store, opts ...Option) *Service {
	s := &Service{store: store}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Register 注册新用户。用户名 3~32 个字符(中英文都可),密码至少 6 位。
// 返回创建好的用户(不含密码哈希以外的敏感信息)。
func (s *Service) Register(ctx context.Context, username, password string) (User, error) {
	u, err := s.register(ctx, username, password, RoleUser)
	return u, err
}

func (s *Service) register(ctx context.Context, username, password, role string) (User, error) {
	username = strings.TrimSpace(username)
	if n := utf8.RuneCountInString(username); n < 3 || n > 32 {
		return User{}, fmt.Errorf("用户名长度需在 3~32 个字符之间")
	}
	if len(password) < 6 {
		return User{}, fmt.Errorf("密码至少 6 位")
	}
	// bcrypt 成本 10:单次哈希约 50~100ms —— 故意的。
	// 密码哈希就该慢,登录一秒钟几次无所谓,攻击者离线爆破时每次都要付这个成本。
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	if err != nil {
		return User{}, fmt.Errorf("hash password: %w", err)
	}
	u := &User{Username: username, PasswordHash: string(hash), Role: role}
	if err := s.store.CreateUser(ctx, u); err != nil {
		if errors.Is(err, ErrUsernameTaken) {
			return User{}, err // 原样上抛,handler 翻译成 409
		}
		// 存储故障:包上统一标记,原文只在服务端日志可见
		return User{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return *u, nil
}

// Login 校验用户名密码,成功则创建会话并返回 token(将写入 HttpOnly cookie)。
// ip 是调用方(c.ClientIP())传来的来源地址,限速按它记账。
// 用户名不存在和密码错误返回**同一个**错误文案 —— 见包注释的安全红线。
func (s *Service) Login(ctx context.Context, ip, username, password string) (User, string, error) {
	if s.limiter != nil {
		if err := s.limiter.Check(ctx, ip); err != nil {
			// ErrLoginThrottled 原样上抛,handler 翻译成 429。
			// 被拒的尝试不再往下走:bcrypt 不算、库不查 —— 限速同时省了它们。
			return User{}, "", err
		}
	}
	u, err := s.store.GetUserByName(ctx, strings.TrimSpace(username))
	if errors.Is(err, ErrNotFound) {
		s.recordLoginFailure(ctx, ip)
		return User{}, "", ErrBadCredentials
	}
	if err != nil {
		// 存储故障不是用户的错,不计入限速
		return User{}, "", fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	// bcrypt 校验:即使攻击者拿到库,也只有 hash;
	// CompareHashAndPassword 内部是常数时间比较,不泄露"前几位对没对"
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		s.recordLoginFailure(ctx, ip)
		return User{}, "", ErrBadCredentials
	}
	token, err := newToken()
	if err != nil {
		return User{}, "", fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	sess := &Session{Token: token, UserID: u.ID, ExpiresAt: time.Now().Add(SessionTTL)}
	if err := s.store.CreateSession(ctx, sess); err != nil {
		return User{}, "", fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	if s.limiter != nil {
		s.limiter.Reset(ctx, ip) // 登录成功,该 IP 的失败计数清零
	}
	return u, token, nil
}

// recordLoginFailure 记失败但 limiter 缺席时是 no-op ——
// 降级形态下登录功能完整,只是不记账。
func (s *Service) recordLoginFailure(ctx context.Context, ip string) {
	if s.limiter != nil {
		s.limiter.RecordFailure(ctx, ip)
	}
}

// ErrBadCredentials 登录失败的统一错误。文案故意模糊 —— 防枚举。
var ErrBadCredentials = errors.New("用户名或密码错误")

// ErrStorageUnavailable 存储层(数据库)故障的统一包装。
// handler 遇到它必须给**固定文案** + 503,绝不能把 err.Error() 透给前端 ——
// 那里面是 DSN/表结构/驱动报错原文,属于内部细节(2026-09-18 实测泄漏过)。
var ErrStorageUnavailable = errors.New("storage unavailable")

// UpdateProfile 改昵称/简介。TrimSpace 后校验长度;rune 计数——
// len() 数字节,中文昵称会被低估一倍以上。
func (s *Service) UpdateProfile(ctx context.Context, userID int64, nickname, bio string) error {
	nickname = strings.TrimSpace(nickname)
	bio = strings.TrimSpace(bio)
	if n := utf8.RuneCountInString(nickname); n > 32 {
		return fmt.Errorf("昵称最多 32 个字符")
	}
	if n := utf8.RuneCountInString(bio); n > 200 {
		return fmt.Errorf("简介最多 200 个字符")
	}
	if err := s.store.UpdateProfile(ctx, userID, nickname, bio); err != nil {
		if errors.Is(err, ErrNotFound) {
			return err
		}
		return fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return nil
}

// SetAvatarURL 头像 URL 落库。文件落盘在 api 层(它才认识 multipart),
// 这里只管"库里那个 URL 变成什么"。
func (s *Service) SetAvatarURL(ctx context.Context, userID int64, avatarURL string) error {
	if err := s.store.SetAvatarURL(ctx, userID, avatarURL); err != nil {
		if errors.Is(err, ErrNotFound) {
			return err
		}
		return fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return nil
}

// Logout 删除会话。"退出登录"的本质是让服务端忘掉这个 token ——
// 只清浏览器 cookie 是不够的,token 本身还有效。
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.store.DeleteSession(ctx, token)
}

// UserFromToken 由 cookie 里的 token 还原用户。三种情况都算"未登录":
// token 不存在 / 会话过期 / 用户被删。
func (s *Service) UserFromToken(ctx context.Context, token string) (User, error) {
	if token == "" {
		return User{}, ErrNotFound
	}
	sess, err := s.store.GetSession(ctx, token)
	if err != nil {
		return User{}, ErrNotFound
	}
	if time.Now().After(sess.ExpiresAt) {
		// 过期会话顺手清掉:反正已经查出来了,不删也过不了校验,
		// 删了能让 sessions 表不被死 token 撑大
		_ = s.store.DeleteSession(ctx, token)
		return User{}, ErrNotFound
	}
	return s.store.GetUserByID(ctx, sess.UserID)
}

// CleanupExpiredSessions 清一轮过期会话,返回删除条数。
// 独立成方法是为了可测;RunSessionCleanup 是它的定时外壳。
func (s *Service) CleanupExpiredSessions(ctx context.Context) (int64, error) {
	return s.store.DeleteExpiredSessions(ctx)
}

// RunSessionCleanup 周期清理过期会话:启动立即清一次,之后每 every 清一次,
// 直到 ctx 取消(进程退出)。
// UserFromToken 的"顺手删"只覆盖"恰好还被查询"的过期 token;
// 无人再用的死会话(用户直接弃登录)只有这里能收,否则 sessions 表只增不减。
func (s *Service) RunSessionCleanup(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		n, err := s.CleanupExpiredSessions(ctx)
		if err != nil {
			log.Printf("[auth] session cleanup failed: %v", err)
		} else if n > 0 {
			log.Printf("[auth] cleaned %d expired sessions", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// EnsureSeedAdmin 确保种子管理员存在:已存在(同名)则跳过,不存在则创建。
// 解决"第一个 admin 哪来"的鸡生蛋问题 —— 靠 env 的 ADMIN_USER/ADMIN_PASSWORD。
// 账号已存在时**不更新密码**:环境变量变更不应重置线上账号。
func (s *Service) EnsureSeedAdmin(ctx context.Context, username, password string) error {
	_, err := s.store.GetUserByName(ctx, username)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	_, err = s.register(ctx, username, password, RoleAdmin)
	if errors.Is(err, ErrUsernameTaken) {
		return nil // 并发/重复启动时的兜底,同样视为"已存在"
	}
	return err
}

// ListUsers 全量用户列表(管理端用)。教学规模不分页,理由见 admin.go。
func (s *Service) ListUsers(ctx context.Context) ([]User, error) {
	return s.store.ListUsers(ctx)
}

// newToken 生成 32 字节随机数的 hex(64 字符)。
// crypto/rand 不是 math/rand:前者是密码学安全的,哪怕熵池有限也保证不可预测。
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// MaskToken 日志/调试时打印 token 用的掩码。完整 token 只存在于 cookie 和库里,
// 出现在任何日志里都是事故 —— 但排查问题时又需要能对上是哪一个会话。
func MaskToken(token string) string {
	if len(token) <= 8 {
		return strings.Repeat("*", len(token))
	}
	return token[:4] + "****" + token[len(token)-4:]
}
