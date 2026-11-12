// oidc.go —— Casdoor/OIDC 联合登录（Authorization Code + PKCE + 一次性 state）。
//
// 密码体系已移除：用户身份由 issuer（如 Casdoor）认证后签发 id_token，
// 本地只做「验签 → 白名单 → 按 issuer+sub 建档 → 发服务端会话」。
// 本地 NoAuth 模式不经过这里（见 middleware.go 的 localMode 分支）。
package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
)

// OIDCConfig 联合登录所需配置（全部来自环境变量，见 config.Load）。
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	AdminSub     string   // 管理员 sub：自动入白名单 + 建档即 admin 角色
	AllowedSubs  []string // 额外允许登录的 sub 白名单；空 = 只有管理员
	AllowAll    bool     // true = 任何注册用户可登录（朋友开放模式）
}

// OIDCState 一次性登录 state（10 分钟过期，消费即删）。
type OIDCState struct {
	StateHash    string    `gorm:"column:state_hash;type:varchar(64);primaryKey"`
	Nonce        string    `gorm:"column:nonce;type:varchar(128);not null"`
	CodeVerifier string    `gorm:"column:code_verifier;type:varchar(128);not null"`
	ReturnTo     string    `gorm:"column:return_to;type:varchar(512)"`
	ExpiresAt    time.Time `gorm:"column:expires_at;not null"`
}

func (OIDCState) TableName() string { return "oidc_states" }

// OIDCService 联合登录服务。state 存 SQLite（与 users/sessions 同库）。
type OIDCService struct {
	db          *gorm.DB
	store       Store
	cfg         OIDCConfig
	oauthConfig oauth2.Config
	verifier    *oidc.IDTokenVerifier
	allowed     map[string]struct{}
}

// ErrInvalidState / ErrNotAllowed 登录失败分类，handler 据此定状态码。
var (
	ErrInvalidState    = errors.New("无效或过期 OIDC state")
	ErrNotAllowed      = errors.New("该账号未被允许访问")
	ErrInvalidReturnTo = errors.New("return_to 必须是本站相对路径")
)

func NewOIDCService(ctx context.Context, db *gorm.DB, store Store, cfg OIDCConfig) (*OIDCService, error) {
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("发现 OIDC provider 失败: %w", err)
	}
	allowed := make(map[string]struct{}, len(cfg.AllowedSubs)+1)
	for _, sub := range cfg.AllowedSubs {
		allowed[sub] = struct{}{}
	}
	allowed[cfg.AdminSub] = struct{}{}
	return &OIDCService{
		db:    db,
		store: store,
		cfg:   cfg,
		oauthConfig: oauth2.Config{
			ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, RedirectURL: cfg.RedirectURL,
			Endpoint: provider.Endpoint(), Scopes: []string{oidc.ScopeOpenID, "profile"},
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		allowed:  allowed,
	}, nil
}

// MigrateOIDCStates 建 oidc_states 表，与 users/sessions 同一迁移时机。
func MigrateOIDCStates(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).AutoMigrate(&OIDCState{})
}

func hashState(state string) string {
	sum := sha256.Sum256([]byte(state))
	return hex.EncodeToString(sum[:])
}

// validateReturnTo 防开放重定向：只允许本站相对路径。
func validateReturnTo(returnTo string) error {
	if returnTo == "" {
		return nil
	}
	if len(returnTo) > 2048 || !strings.HasPrefix(returnTo, "/") || strings.HasPrefix(returnTo, "//") ||
		strings.ContainsAny(returnTo, "\\\r\n") {
		return ErrInvalidReturnTo
	}
	return nil
}

// StartLogin 创建一次性 state，返回带 PKCE 的授权 URL。
// forceReauth=true 附加 prompt=login：SSO 会话有效也强制显示登录表单（退出后换账号用）。
func (s *OIDCService) StartLogin(ctx context.Context, returnTo string, forceReauth bool) (string, error) {
	if err := validateReturnTo(returnTo); err != nil {
		return "", err
	}
	state, err := newToken()
	if err != nil {
		return "", err
	}
	nonce, err := newToken()
	if err != nil {
		return "", err
	}
	verifier, err := newToken()
	if err != nil {
		return "", err
	}
	if returnTo == "" {
		returnTo = "/"
	}
	now := time.Now().UTC()
	if err := s.db.WithContext(ctx).Where("expires_at <= ?", now).Delete(&OIDCState{}).Error; err != nil {
		return "", fmt.Errorf("清理过期 state 失败: %w", err)
	}
	rec := &OIDCState{
		StateHash: hashState(state), Nonce: nonce, CodeVerifier: verifier,
		ReturnTo: returnTo, ExpiresAt: now.Add(10 * time.Minute),
	}
	if err := s.db.WithContext(ctx).Create(rec).Error; err != nil {
		return "", fmt.Errorf("保存 state 失败: %w", err)
	}
	opts := []oauth2.AuthCodeOption{oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)}
	if forceReauth {
		opts = append(opts, oauth2.SetAuthURLParam("prompt", "login"))
	}
	return s.oauthConfig.AuthCodeURL(state, opts...), nil
}

// CallbackResult 登录完成的结果：用户 + 会话 token + 回跳地址。
type CallbackResult struct {
	User     User
	Token    string
	ReturnTo string
}

// Callback 消费 state → 换 token → 验签 → 白名单 → 建档 → 发会话。
func (s *OIDCService) Callback(ctx context.Context, state, code, browserState string) (CallbackResult, error) {
	if browserState == "" || subtle.ConstantTimeCompare([]byte(state), []byte(browserState)) != 1 {
		return CallbackResult{}, ErrInvalidState
	}
	var rec OIDCState
	err := s.db.WithContext(ctx).First(&rec, "state_hash = ? AND expires_at > ?", hashState(state), time.Now().UTC()).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return CallbackResult{}, ErrInvalidState
	}
	if err != nil {
		return CallbackResult{}, fmt.Errorf("读取 state 失败: %w", err)
	}
	// 一次性：取出即删，重放/刷新回调页都会失效（这是特性不是 bug）
	if err := s.db.WithContext(ctx).Delete(&OIDCState{}, "state_hash = ?", rec.StateHash).Error; err != nil {
		return CallbackResult{}, fmt.Errorf("消费 state 失败: %w", err)
	}

	token, err := s.oauthConfig.Exchange(ctx, code, oauth2.VerifierOption(rec.CodeVerifier))
	if err != nil {
		return CallbackResult{}, fmt.Errorf("交换授权码失败: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return CallbackResult{}, fmt.Errorf("token 响应缺少 id_token")
	}
	idToken, err := s.verifyIDToken(ctx, rawIDToken)
	if err != nil {
		return CallbackResult{}, fmt.Errorf("验证 ID Token 失败: %w", err)
	}
	if idToken.Nonce != rec.Nonce {
		return CallbackResult{}, fmt.Errorf("nonce 校验失败")
	}
	if !s.isAllowed(idToken.Subject) {
		return CallbackResult{}, ErrNotAllowed
	}

	user, err := s.upsertOIDCUser(ctx, idToken)
	if err != nil {
		return CallbackResult{}, err
	}
	tokenStr, err := newToken()
	if err != nil {
		return CallbackResult{}, err
	}
	sess := &Session{Token: tokenStr, UserID: user.ID, ExpiresAt: time.Now().Add(SessionTTL)}
	if err := s.store.CreateSession(ctx, sess); err != nil {
		return CallbackResult{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return CallbackResult{User: user, Token: tokenStr, ReturnTo: rec.ReturnTo}, nil
}

// verifyIDToken 验签失败时用全新 provider 刷新 JWKS 缓存重试一次。
// 长期 verifier 缓存公钥，issuer 密钥轮换/JWKS 瞬时故障时旧缓存会持续失败。
func (s *OIDCService) verifyIDToken(ctx context.Context, rawIDToken string) (*oidc.IDToken, error) {
	idToken, err := s.verifier.Verify(ctx, rawIDToken)
	if err == nil {
		return idToken, nil
	}
	freshProvider, freshErr := oidc.NewProvider(ctx, s.cfg.Issuer)
	if freshErr != nil {
		return nil, err
	}
	freshToken, freshVerifyErr := freshProvider.Verifier(&oidc.Config{ClientID: s.cfg.ClientID}).Verify(ctx, rawIDToken)
	if freshVerifyErr != nil {
		return nil, err
	}
	log.Printf("OIDC JWKS 缓存刷新后验签通过（原错误: %v）", err)
	return freshToken, nil
}

func (s *OIDCService) isAllowed(sub string) bool {
	if s.cfg.AllowAll {
		return true
	}
	_, ok := s.allowed[sub]
	return ok
}

// upsertOIDCUser 按 issuer+sub 找/建本地用户。管理员 sub 建档即 admin 角色。
func (s *OIDCService) upsertOIDCUser(ctx context.Context, idToken *oidc.IDToken) (User, error) {
	u, err := s.store.GetUserBySub(ctx, idToken.Issuer, idToken.Subject)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return User{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	var claims struct {
		Name string `json:"name"`
	}
	_ = idToken.Claims(&claims)
	name := strings.TrimSpace(claims.Name)
	if name == "" {
		name = "user-" + idToken.Subject[:8]
	}
	role := RoleUser
	if idToken.Subject == s.cfg.AdminSub {
		role = RoleAdmin
	}
	u = User{
		Username: name, Nickname: name, Role: role,
		Issuer: idToken.Issuer, Sub: idToken.Subject,
		PasswordHash: "oidc", // 占位：密码体系已移除，列仅为兼容保留
	}
	if err := s.store.CreateUser(ctx, &u); err != nil {
		return User{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return u, nil
}
