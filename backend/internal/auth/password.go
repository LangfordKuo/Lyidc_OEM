package auth

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// 密码长度限制（bcrypt 只处理前 72 字节，超长直接拒绝，避免静默截断）。
const (
	// MinPasswordBytes 密码最少字节数。
	MinPasswordBytes = 8
	// MaxPasswordBytes 密码最多字节数（bcrypt 上限）。
	MaxPasswordBytes = 72
)

// ValidatePasswordLength 校验密码长度是否符合 8-72 字节。
func ValidatePasswordLength(plain string) error {
	switch {
	case len(plain) < MinPasswordBytes:
		return fmt.Errorf("密码至少 %d 个字节", MinPasswordBytes)
	case len(plain) > MaxPasswordBytes:
		return fmt.Errorf("密码最多 %d 个字节", MaxPasswordBytes)
	default:
		return nil
	}
}

// HashPassword 使用 bcrypt（DefaultCost=10）生成密码哈希。
func HashPassword(plain string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("生成密码哈希失败: %w", err)
	}
	return string(hash), nil
}

// VerifyPassword 校验明文密码与 bcrypt 哈希是否匹配。
func VerifyPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
