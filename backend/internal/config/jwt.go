package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"
)

// MinJWTSecretLength 是 jwt.secret 的最短长度（按去除首尾空白后的**字符数**计）。
//
// 32 是 HS256 对称密钥的下限口径：本系统的会员 token 与管理员 token **共用同一密钥**
// （契约 1.1，仅以 aud 区分），密钥一弱就等于超级管理员身份可被离线伪造，因此
// 「非默认但很短」的密钥同样必须当成弱值处置（契约 25.1）。
const MinJWTSecretLength = 32

// IsWeakSecret 判断当前 jwt.secret 是否为「弱密钥」：空串、等于开发默认值
// DefaultJWTSecret、或去除首尾空白后长度不足 MinJWTSecretLength 任一成立即为弱。
//
// 与 UsesDefaultSecret 的分工：后者只判「恰好等于开发默认值」，是既有的启动告警口径；
// 本函数是启动安全基线（契约 25.1）的判定口径，覆盖面更宽（含留空与过短）。
func (j JWTConfig) IsWeakSecret() bool {
	secret := strings.TrimSpace(j.Secret)
	return secret == "" || secret == DefaultJWTSecret || utf8.RuneCountInString(secret) < MinJWTSecretLength
}

// GenerateJWTSecret 生成 32 字节随机 JWT 密钥（base64url 无填充，43 字符）。
//
// 安装向导「完成」步骤与已安装实例的弱密钥自动轮换**共用本函数**，两处密钥强度同口径
// （契约 25.1）；调用方负责保证密钥不进日志与响应。
func GenerateJWTSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成 JWT 密钥失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// JWTSecretRotation 描述一次「弱密钥自动轮换」的结果。
type JWTSecretRotation struct {
	// Secret 是本次进程应当使用的密钥；未发生轮换时等于传入的原值。
	Secret string
	// Rotated 表示确实生成了新密钥（传入的密钥是弱值）。
	Rotated bool
	// Written 表示新密钥已成功写回配置文件；Rotated 为 false 时恒为 false。
	Written bool
	// WriteErr 是未能写回的原因（Rotated=true 且 Written=false 时非空），只用于日志。
	WriteErr error
}

// RotateWeakJWTSecret 在 jwt.secret 为弱密钥时生成强随机密钥，并把它写回配置文件
// （契约 25.1，P1 安全整改 finding#01）。
//
// 语义要点：
//   - 密钥不是弱值时**完全不碰文件**，原样返回（幂等：轮换过一次后再调用不会再改文件，
//     因此第二次启动不会重复轮换）；
//   - 写回复用 ResolveWritePath / CheckWritable / SaveMerged，因此**只改 jwt.secret 这一个标量**，
//     既有键、值、书写顺序与注释风格全部保持不变；
//   - 写回失败**不返回错误、也不回滚已生成的密钥**：Rotated=true 且 Written=false，
//     Secret 仍是强密钥，调用方据此改用「仅本次进程有效」的密钥并给出人工处置指引，
//     服务照常启动（绝不因为写不了文件而拒绝启动）。
func RotateWeakJWTSecret(jwt JWTConfig, path string) JWTSecretRotation {
	rotation := JWTSecretRotation{Secret: jwt.Secret}
	if !jwt.IsWeakSecret() {
		return rotation
	}

	secret, err := GenerateJWTSecret()
	if err != nil {
		// 生成失败：保持原密钥，由调用方按「弱密钥且无法自动处置」告警，仍不阻断启动。
		rotation.WriteErr = err
		return rotation
	}
	rotation.Secret = secret
	rotation.Rotated = true

	if _, err := ResolveWritePath(path); err != nil {
		rotation.WriteErr = err
		return rotation
	}
	if err := CheckWritable(path); err != nil {
		rotation.WriteErr = err
		return rotation
	}
	if _, err := SaveMerged(path, SaveInput{JWTSecret: secret}); err != nil {
		rotation.WriteErr = err
		return rotation
	}

	rotation.Written = true
	return rotation
}
