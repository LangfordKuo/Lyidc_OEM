package auth

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestHashPasswordUsesDefaultCostAndVerifies(t *testing.T) {
	hash, err := HashPassword("alice123456")
	if err != nil {
		t.Fatalf("HashPassword() 返回错误: %v", err)
	}
	if hash == "alice123456" || strings.Contains(hash, "alice123456") {
		t.Fatalf("哈希结果泄露了明文: %q", hash)
	}

	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		t.Fatalf("bcrypt.Cost() 返回错误: %v", err)
	}
	if cost != bcrypt.DefaultCost {
		t.Errorf("bcrypt cost = %d, 期望 %d", cost, bcrypt.DefaultCost)
	}

	if !VerifyPassword(hash, "alice123456") {
		t.Error("VerifyPassword() 对正确密码返回 false")
	}
	if VerifyPassword(hash, "alice123457") {
		t.Error("VerifyPassword() 对错误密码返回 true")
	}
	if VerifyPassword("not-a-hash", "alice123456") {
		t.Error("VerifyPassword() 对非法哈希返回 true")
	}
}

func TestHashPasswordUsesRandomSalt(t *testing.T) {
	first, err := HashPassword("alice123456")
	if err != nil {
		t.Fatalf("HashPassword() 返回错误: %v", err)
	}
	second, err := HashPassword("alice123456")
	if err != nil {
		t.Fatalf("HashPassword() 返回错误: %v", err)
	}
	if first == second {
		t.Error("两次哈希结果相同，缺少随机盐")
	}
}

func TestValidatePasswordLength(t *testing.T) {
	tests := []struct {
		name    string
		plain   string
		wantErr bool
	}{
		{name: "空", plain: "", wantErr: true},
		{name: "7 字节", plain: "1234567", wantErr: true},
		{name: "8 字节", plain: "12345678"},
		{name: "中文 6 字节", plain: "密码", wantErr: true},
		{name: "中文 12 字节", plain: "密码密码"},
		{name: "72 字节", plain: strings.Repeat("a", 72)},
		{name: "73 字节", plain: strings.Repeat("a", 73), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePasswordLength(tt.plain)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePasswordLength(%q) err=%v, 期望错误=%t", tt.plain, err, tt.wantErr)
			}
		})
	}
}
