package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

const unitTestSecret = "unit-test-secret"

func testManager() *TokenManager {
	return NewTokenManager(config.JWTConfig{Secret: unitTestSecret, ExpireHours: 1})
}

func TestIssueMemberTokenCarriesRequiredClaims(t *testing.T) {
	manager := testManager()
	now := time.Now().UTC().Truncate(time.Second)

	token, expiresAt, err := manager.IssueMemberToken(&model.Member{ID: 42, Username: "alice"}, now)
	if err != nil {
		t.Fatalf("IssueMemberToken() 返回错误: %v", err)
	}
	if token == "" {
		t.Fatal("IssueMemberToken() 返回空 token")
	}
	if want := now.Add(time.Hour); !expiresAt.Equal(want) {
		t.Errorf("expiresAt = %s, 期望 %s", expiresAt, want)
	}

	claims, err := manager.VerifyMemberToken(token)
	if err != nil {
		t.Fatalf("VerifyMemberToken() 返回错误: %v", err)
	}
	if claims.MemberID != 42 {
		t.Errorf("member_id = %d, 期望 42", claims.MemberID)
	}
	if claims.Username != "alice" {
		t.Errorf("username = %q, 期望 alice", claims.Username)
	}
	if claims.Subject != "member:42" {
		t.Errorf("sub = %q, 期望 member:42", claims.Subject)
	}
	if claims.Issuer != Issuer {
		t.Errorf("iss = %q, 期望 %q", claims.Issuer, Issuer)
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != AudienceMember {
		t.Errorf("aud = %v, 期望 [%s]", claims.Audience, AudienceMember)
	}
	if claims.ExpiresAt == nil || !claims.ExpiresAt.Time.Equal(expiresAt.Truncate(time.Second)) {
		t.Errorf("exp = %v, 期望 %s", claims.ExpiresAt, expiresAt.Truncate(time.Second))
	}
}

func TestIssueAdminTokenCarriesRoleAndVerifies(t *testing.T) {
	manager := testManager()
	now := time.Now().UTC()

	token, _, err := manager.IssueAdminToken(&model.Admin{ID: 7, Username: "boss", Role: model.RoleFinance}, now)
	if err != nil {
		t.Fatalf("IssueAdminToken() 返回错误: %v", err)
	}

	claims, err := manager.VerifyAdminToken(token)
	if err != nil {
		t.Fatalf("VerifyAdminToken() 返回错误: %v", err)
	}
	if claims.AdminID != 7 || claims.Username != "boss" || claims.Role != model.RoleFinance {
		t.Errorf("claims = %+v, 期望 admin_id=7 username=boss role=finance", claims)
	}
	if claims.Subject != "admin:7" {
		t.Errorf("sub = %q, 期望 admin:7", claims.Subject)
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != AudienceAdmin {
		t.Errorf("aud = %v, 期望 [%s]", claims.Audience, AudienceAdmin)
	}
}

func TestVerifyRejectsCrossAudienceTokens(t *testing.T) {
	manager := testManager()
	now := time.Now().UTC()

	memberToken, _, err := manager.IssueMemberToken(&model.Member{ID: 1, Username: "alice"}, now)
	if err != nil {
		t.Fatalf("IssueMemberToken() 返回错误: %v", err)
	}
	adminToken, _, err := manager.IssueAdminToken(&model.Admin{ID: 1, Username: "boss", Role: model.RoleAdmin}, now)
	if err != nil {
		t.Fatalf("IssueAdminToken() 返回错误: %v", err)
	}

	if _, err := manager.VerifyAdminToken(memberToken); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("会员 token 通过管理员校验: err=%v, 期望 ErrInvalidToken", err)
	}
	if _, err := manager.VerifyMemberToken(adminToken); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("管理员 token 通过会员校验: err=%v, 期望 ErrInvalidToken", err)
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	manager := testManager()
	token, _, err := manager.IssueMemberToken(&model.Member{ID: 1, Username: "alice"}, time.Now().Add(-2*time.Hour))
	if err != nil {
		t.Fatalf("IssueMemberToken() 返回错误: %v", err)
	}

	if _, err := manager.VerifyMemberToken(token); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("过期 token 校验 err=%v, 期望 ErrInvalidToken", err)
	}
}

func TestVerifyRejectsTamperedTokenAndForeignKey(t *testing.T) {
	manager := testManager()
	token, _, err := manager.IssueMemberToken(&model.Member{ID: 1, Username: "alice"}, time.Now())
	if err != nil {
		t.Fatalf("IssueMemberToken() 返回错误: %v", err)
	}

	// 篡改签名的最后一个字符：替换为「与原字符必然不同」的 base64url 字符
	// （历史写法 token[:len-2]+"xx" 有约 0.1% 概率与原值完全相同——签名末两位本就是 xx 时
	// 篡改后 token 不变，用例会偶发失败；此处改为确定性构造）。
	last := token[len(token)-1]
	replacement := byte('A')
	if last == 'A' {
		replacement = 'B'
	}
	tampered := token[:len(token)-1] + string(replacement)
	if tampered == token {
		t.Fatal("篡改后的 token 与原值相同（构造错误）")
	}
	if _, err := manager.VerifyMemberToken(tampered); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("篡改 token 校验 err=%v, 期望 ErrInvalidToken", err)
	}

	other := NewTokenManager(config.JWTConfig{Secret: "another-secret", ExpireHours: 1})
	if _, err := other.VerifyMemberToken(token); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("异密钥校验 err=%v, 期望 ErrInvalidToken", err)
	}

	for _, raw := range []string{"", "not-a-token", "a.b.c"} {
		if _, err := manager.VerifyMemberToken(raw); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("VerifyMemberToken(%q) err=%v, 期望 ErrInvalidToken", raw, err)
		}
	}
}

func TestVerifyRejectsNoneAlgorithmAndMissingClaims(t *testing.T) {
	manager := testManager()
	now := time.Now()

	unsigned, err := jwt.NewWithClaims(jwt.SigningMethodNone, MemberClaims{
		MemberID: 1,
		Username: "alice",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer,
			Audience:  jwt.ClaimStrings{AudienceMember},
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
	}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("构造 alg=none token 失败: %v", err)
	}
	if _, err := manager.VerifyMemberToken(unsigned); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("alg=none token 校验 err=%v, 期望 ErrInvalidToken", err)
	}

	missing, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": Issuer,
		"aud": AudienceMember,
		"exp": now.Add(time.Hour).Unix(),
	}).SignedString([]byte(unitTestSecret))
	if err != nil {
		t.Fatalf("构造缺少 claims 的 token 失败: %v", err)
	}
	if _, err := manager.VerifyMemberToken(missing); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("缺少 member_id 的 token 校验 err=%v, 期望 ErrInvalidToken", err)
	}

	badRole, err := jwt.NewWithClaims(jwt.SigningMethodHS256, AdminClaims{
		AdminID:  1,
		Username: "boss",
		Role:     "boss",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer,
			Audience:  jwt.ClaimStrings{AudienceAdmin},
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
	}).SignedString([]byte(unitTestSecret))
	if err != nil {
		t.Fatalf("构造非法角色 token 失败: %v", err)
	}
	if _, err := manager.VerifyAdminToken(badRole); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("非法角色 token 校验 err=%v, 期望 ErrInvalidToken", err)
	}
}

func TestNewTokenManagerFallsBackToDefaults(t *testing.T) {
	manager := NewTokenManager(config.JWTConfig{})
	if want := time.Duration(config.DefaultJWTExpireHours) * time.Hour; manager.TTL() != want {
		t.Errorf("缺省 TTL = %s, 期望 %s", manager.TTL(), want)
	}

	token, _, err := manager.IssueMemberToken(&model.Member{ID: 1, Username: "alice"}, time.Now())
	if err != nil {
		t.Fatalf("使用缺省密钥签发失败: %v", err)
	}
	if _, err := manager.VerifyMemberToken(token); err != nil {
		t.Fatalf("使用缺省密钥校验失败: %v", err)
	}

	negative := NewTokenManager(config.JWTConfig{Secret: "s", ExpireHours: -5})
	if want := time.Duration(config.DefaultJWTExpireHours) * time.Hour; negative.TTL() != want {
		t.Errorf("非法 TTL 回退 = %s, 期望 %s", negative.TTL(), want)
	}
}
