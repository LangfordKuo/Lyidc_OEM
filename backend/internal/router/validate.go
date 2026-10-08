package router

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// 字段长度与格式约束（与 docs/api-contract.md 认证与账号章节一致）。
const (
	minUsernameLength = 3
	maxUsernameLength = 32
	maxEmailLength    = 128
	maxNicknameRunes  = 32
	minPhoneLength    = 5
	maxPhoneLength    = 32
)

var (
	usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{3,32}$`)
	emailPattern    = regexp.MustCompile(`^[^@\s]+@[^@\s.]+(\.[^@\s.]+)+$`)
	phonePattern    = regexp.MustCompile(`^[0-9+\-() ]{5,32}$`)
)

// validateUsername 校验用户名：3-32 位字母、数字或下划线。
func validateUsername(username string) error {
	if !usernamePattern.MatchString(username) {
		return fmt.Errorf("用户名需为 %d-%d 位字母、数字或下划线", minUsernameLength, maxUsernameLength)
	}
	return nil
}

// validateEmail 校验邮箱：基础格式 + 长度上限。
func validateEmail(email string) error {
	if email == "" {
		return errors.New("邮箱不能为空")
	}
	if len(email) > maxEmailLength {
		return fmt.Errorf("邮箱长度不能超过 %d 字节", maxEmailLength)
	}
	if !emailPattern.MatchString(email) {
		return errors.New("邮箱格式不正确")
	}
	return nil
}

// validateNickname 校验昵称：1-32 个字符（按 rune 计）。
func validateNickname(nickname string) error {
	if strings.TrimSpace(nickname) == "" {
		return errors.New("昵称不能为空")
	}
	if utf8.RuneCountInString(nickname) > maxNicknameRunes {
		return fmt.Errorf("昵称长度不能超过 %d 个字符", maxNicknameRunes)
	}
	return nil
}

// validatePhone 校验手机号：允许数字、+、-、括号与空格，长度 5-32 字节；
// 空字符串表示清空手机号，属合法输入。
func validatePhone(phone string) error {
	if phone == "" {
		return nil
	}
	if !phonePattern.MatchString(phone) {
		return fmt.Errorf("手机号只能包含数字、+、-、() 与空格，长度 %d-%d 字节", minPhoneLength, maxPhoneLength)
	}
	return nil
}
