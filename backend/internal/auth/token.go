// Package auth 提供 JWT 签发/校验与 bcrypt 密码哈希工具（纯逻辑，不依赖 gin）。
package auth

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

// JWT 设计（阶段 1）：
//   - 算法固定 HS256，密钥来自配置 jwt.secret（缺省为开发默认值，生产必须修改）。
//   - 会员与管理员共用同一密钥，但用 aud（受众）区分：会员 token aud="member"，
//     管理员 token aud="admin"；校验时按路由要求断言 aud，防止两类 token 互相冒用。
//   - iss 固定 "lyidc-oem"；sub 为 "member:<id>" / "admin:<id>"；iat/exp 为 Unix 秒。
const (
	// Issuer 是 token 的签发者标识（iss）。
	Issuer = "lyidc-oem"
	// AudienceMember 是会员 token 的受众（aud）。
	AudienceMember = "member"
	// AudienceAdmin 是管理员 token 的受众（aud）。
	AudienceAdmin = "admin"
)

// ErrInvalidToken 表示 token 缺失、格式错误、签名不匹配、已过期或受众不符。
var ErrInvalidToken = errors.New("凭证无效")

// MemberClaims 是会员 token 的 claims：member_id + username。
type MemberClaims struct {
	MemberID uint64 `json:"member_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// AdminClaims 是管理员 token 的 claims：admin_id + role（并附带 username 方便审计）。
type AdminClaims struct {
	AdminID  uint64 `json:"admin_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// TokenManager 负责签发与校验两类 token。
type TokenManager struct {
	secret []byte
	ttl    time.Duration
}

// NewTokenManager 构造 TokenManager；配置缺省时回退到 config 包中的开发默认值。
func NewTokenManager(cfg config.JWTConfig) *TokenManager {
	secret := strings.TrimSpace(cfg.Secret)
	if secret == "" {
		secret = config.DefaultJWTSecret
	}
	hours := cfg.ExpireHours
	if hours <= 0 || hours > config.MaxJWTExpireHours {
		hours = config.DefaultJWTExpireHours
	}
	return &TokenManager{secret: []byte(secret), ttl: time.Duration(hours) * time.Hour}
}

// TTL 返回 token 有效期。
func (m *TokenManager) TTL() time.Duration { return m.ttl }

// IssueMemberToken 为会员签发 HS256 token，返回 token 字符串与过期时间。
func (m *TokenManager) IssueMemberToken(member *model.Member, now time.Time) (string, time.Time, error) {
	expiresAt := now.Add(m.ttl)
	claims := MemberClaims{
		MemberID: member.ID,
		Username: member.Username,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer,
			Subject:   fmt.Sprintf("member:%d", member.ID),
			Audience:  jwt.ClaimStrings{AudienceMember},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("签发会员 token 失败: %w", err)
	}
	return token, expiresAt, nil
}

// IssueAdminToken 为管理员签发 HS256 token，返回 token 字符串与过期时间。
func (m *TokenManager) IssueAdminToken(admin *model.Admin, now time.Time) (string, time.Time, error) {
	expiresAt := now.Add(m.ttl)
	claims := AdminClaims{
		AdminID:  admin.ID,
		Username: admin.Username,
		Role:     admin.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer,
			Subject:   fmt.Sprintf("admin:%d", admin.ID),
			Audience:  jwt.ClaimStrings{AudienceAdmin},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("签发管理员 token 失败: %w", err)
	}
	return token, expiresAt, nil
}

// VerifyMemberToken 校验会员 token：签名、iss、aud="member"、过期时间与必要 claims。
func (m *TokenManager) VerifyMemberToken(raw string) (*MemberClaims, error) {
	claims := &MemberClaims{}
	if err := m.parse(raw, AudienceMember, claims); err != nil {
		return nil, err
	}
	if claims.MemberID == 0 || strings.TrimSpace(claims.Username) == "" {
		return nil, fmt.Errorf("%w: 缺少 member_id 或 username", ErrInvalidToken)
	}
	return claims, nil
}

// VerifyAdminToken 校验管理员 token：签名、iss、aud="admin"、过期时间与必要 claims。
func (m *TokenManager) VerifyAdminToken(raw string) (*AdminClaims, error) {
	claims := &AdminClaims{}
	if err := m.parse(raw, AudienceAdmin, claims); err != nil {
		return nil, err
	}
	if claims.AdminID == 0 || strings.TrimSpace(claims.Username) == "" {
		return nil, fmt.Errorf("%w: 缺少 admin_id 或 username", ErrInvalidToken)
	}
	if !model.IsValidAdminRole(claims.Role) {
		return nil, fmt.Errorf("%w: 角色 %q 非法", ErrInvalidToken, claims.Role)
	}
	return claims, nil
}

// parse 按指定受众解析并校验 token，强制 HS256、iss 与 exp。
func (m *TokenManager) parse(raw, audience string, claims jwt.Claims) error {
	parsed, err := jwt.ParseWithClaims(strings.TrimSpace(raw), claims,
		func(*jwt.Token) (any, error) { return m.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(Issuer),
		jwt.WithAudience(audience),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if !parsed.Valid {
		return ErrInvalidToken
	}
	return nil
}
