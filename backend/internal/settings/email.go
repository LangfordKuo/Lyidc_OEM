package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// KeyEmailSMTP 是邮件（SMTP）设置的键（阶段 6b，契约 17.3）。
const KeyEmailSMTP = "email.smtp"

// 传输加密方式取值（email.smtp.encryption）。
const (
	// EncryptionNone 明文 SMTP（通常 25 端口，内网/自建 MTA；本地回环上允许带认证）。
	EncryptionNone = "none"
	// EncryptionStartTLS 先明文建连再 STARTTLS 升级（通常 587 端口，推荐）。
	EncryptionStartTLS = "starttls"
	// EncryptionSSL 建连即 TLS（implicit TLS，通常 465 端口）。
	EncryptionSSL = "ssl"
)

// SMTPEncryptions 是加密方式全集（顺序即文档与错误提示的展示顺序）。
var SMTPEncryptions = []string{EncryptionNone, EncryptionStartTLS, EncryptionSSL}

// SMTP 端口缺省值（按加密方式推导，Port 未填时使用）。
const (
	// DefaultSMTPPortNone 明文 SMTP 缺省端口。
	DefaultSMTPPortNone = 25
	// DefaultSMTPPortStartTLS STARTTLS 缺省端口。
	DefaultSMTPPortStartTLS = 587
	// DefaultSMTPPortSSL 隐式 TLS 缺省端口。
	DefaultSMTPPortSSL = 465
)

// SMTP 是 email.smtp 设置的取值结构（契约 17.3）。
//
// Password 是 SMTP 认证口令，只进数据库与内存，对外一律掩码（MaskSecret）。
type SMTP struct {
	// Enabled 是否启用邮件通知；启用时 host / from 必须齐全（见 Missing）。
	Enabled bool `json:"enabled"`
	// Host SMTP 服务器主机名（不含端口）。
	Host string `json:"host"`
	// Port 端口；0 表示按 Encryption 取缺省（25 / 587 / 465）。
	Port int `json:"port"`
	// Username 认证用户名（可空表示不做认证；非空时必须提供 Password）。
	Username string `json:"username"`
	// Password 认证口令（三态脱敏，接口永不回显明文）。
	Password string `json:"password"`
	// From 发件人地址（信封与 From 头都用它）。
	From string `json:"from"`
	// FromName 发件人显示名（可空，非 ASCII 自动做 RFC 2047 编码）。
	FromName string `json:"from_name"`
	// Encryption 取 none / starttls / ssl；空值按 none 处理。
	Encryption string `json:"encryption"`
}

// SMTPUpdate 是 PUT /api/v1/admin/settings/email/smtp 的更新意图：nil 表示不修改。
//
// Password 用 *string 承接三态语义（与 payment.epay 的 key 一致）：
// 键缺席（nil）= 保持不变、给新值 = 替换、空串 = 清空。
type SMTPUpdate struct {
	Enabled    *bool
	Host       *string
	Port       *int
	Username   *string
	Password   *string
	From       *string
	FromName   *string
	Encryption *string
}

// Missing 返回启用邮件时不能为空的字段名（按契约固定顺序）。
func (s SMTP) Missing() []string {
	var missing []string
	if s.Host == "" {
		missing = append(missing, "host")
	}
	if s.From == "" {
		missing = append(missing, "from")
	}
	return missing
}

// Usable 判断邮件当前是否可用：已启用、必填项齐全且加密方式合法。
func (s SMTP) Usable() bool {
	if !s.Enabled || len(s.Missing()) > 0 {
		return false
	}
	return IsValidSMTPEncryption(s.NormalizedEncryption())
}

// PortOr 返回实际使用的端口（未填时按加密方式取缺省）。
func (s SMTP) PortOr() int {
	if s.Port > 0 {
		return s.Port
	}
	switch s.NormalizedEncryption() {
	case EncryptionSSL:
		return DefaultSMTPPortSSL
	case EncryptionStartTLS:
		return DefaultSMTPPortStartTLS
	default:
		return DefaultSMTPPortNone
	}
}

// NormalizedEncryption 返回归一化后的加密方式（空值按 none）。
func (s SMTP) NormalizedEncryption() string {
	value := strings.ToLower(strings.TrimSpace(s.Encryption))
	if value == "" {
		return EncryptionNone
	}
	return value
}

// Endpoint 返回 "host:port" 形式的连接地址（host 为空时返回空串），仅用于展示与日志。
func (s SMTP) Endpoint() string {
	if strings.TrimSpace(s.Host) == "" {
		return ""
	}
	return fmt.Sprintf("%s:%d", s.Host, s.PortOr())
}

// IsValidSMTPEncryption 判断加密方式是否在枚举内。
func IsValidSMTPEncryption(value string) bool {
	for _, valid := range SMTPEncryptions {
		if value == valid {
			return true
		}
	}
	return false
}

// Apply 把更新意图合并到当前值，并对合并结果做整体校验。
// 校验失败返回 ErrFormat / ErrRule（上层映射 40001 / 40002），此时设置保持不变。
func (s SMTP) Apply(upd SMTPUpdate) (SMTP, error) {
	merged := s
	if upd.Enabled != nil {
		merged.Enabled = *upd.Enabled
	}
	if upd.Host != nil {
		merged.Host = strings.TrimSpace(*upd.Host)
	}
	if upd.Port != nil {
		merged.Port = *upd.Port
	}
	if upd.Username != nil {
		merged.Username = strings.TrimSpace(*upd.Username)
	}
	if upd.Password != nil {
		// 口令不做 TrimSpace：首尾空白可能是口令的一部分（与商户密钥同口径）。
		merged.Password = *upd.Password
	}
	if upd.From != nil {
		merged.From = strings.TrimSpace(*upd.From)
	}
	if upd.FromName != nil {
		merged.FromName = strings.TrimSpace(*upd.FromName)
	}
	if upd.Encryption != nil {
		merged.Encryption = strings.ToLower(strings.TrimSpace(*upd.Encryption))
	}
	if err := merged.Validate(); err != nil {
		return SMTP{}, err
	}
	return merged, nil
}

// Validate 校验取值：加密方式与端口范围；from 非空时必须是合法邮箱；
// enabled=true 时 host / from 缺一即拒绝（ErrRule，消息列出缺失项）；
// 配置了认证用户名却没有口令同样拒绝（避免「配了用户名却发不出信」的静默故障）。
func (s SMTP) Validate() error {
	if encryption := strings.TrimSpace(s.Encryption); encryption != "" && !IsValidSMTPEncryption(s.NormalizedEncryption()) {
		return fmt.Errorf("%w: encryption 只能是 %s，收到 %q",
			ErrFormat, strings.Join(SMTPEncryptions, " / "), s.Encryption)
	}
	if s.Port < 0 || s.Port > 65535 {
		return fmt.Errorf("%w: port 需在 1-65535 之间（留空按加密方式取缺省），收到 %d", ErrFormat, s.Port)
	}
	if s.From != "" && !looksLikeEmail(s.From) {
		return fmt.Errorf("%w: from 格式不正确，收到 %q", ErrFormat, s.From)
	}
	if s.Username != "" && s.Password == "" {
		return fmt.Errorf("%w: 配置了 username 时必须提供 password（不需要认证时请清空 username）", ErrRule)
	}
	if !s.Enabled {
		return nil
	}
	if missing := s.Missing(); len(missing) > 0 {
		return fmt.Errorf("%w: enabled=true 时以下字段不能为空：%s", ErrRule, strings.Join(missing, "、"))
	}
	return nil
}

// Encode 返回落库用的 JSON 文本。
func (s SMTP) Encode() (string, error) { return encode(s) }

// SMTPState 是 email.smtp 的读取结果。
type SMTPState struct {
	Value SMTP
	Meta  Meta
	// Fingerprint 是库内 JSON 原文（未配置为空串），供调用方判断「设置是否变化」。
	Fingerprint string
}

// SMTP 读取 email.smtp 设置；未配置时返回缺省值（Enabled=false）与空指纹。
func (r *Reader) SMTP(ctx context.Context) (SMTPState, error) {
	raw, meta, err := r.raw(ctx, KeyEmailSMTP)
	if err != nil {
		return SMTPState{}, err
	}
	value, err := decodeSMTP(raw)
	if err != nil {
		return SMTPState{}, err
	}
	return SMTPState{Value: value, Meta: meta, Fingerprint: raw}, nil
}

// decodeSMTP 解析库内 JSON；空值按未配置处理。
func decodeSMTP(raw string) (SMTP, error) {
	var value SMTP
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "null" {
		return value, nil
	}
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return SMTP{}, fmt.Errorf("%w: %s 的值不是合法 JSON：%v", ErrCorrupt, KeyEmailSMTP, err)
	}
	value.Host = strings.TrimSpace(value.Host)
	value.Username = strings.TrimSpace(value.Username)
	value.From = strings.TrimSpace(value.From)
	value.FromName = strings.TrimSpace(value.FromName)
	value.Encryption = strings.ToLower(strings.TrimSpace(value.Encryption))
	return value, nil
}
