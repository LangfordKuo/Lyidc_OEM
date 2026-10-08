// Package email 实现通知邮件的发送（阶段 6b，契约 17.3）。
//
// 设计要点：
//   - **只用标准库**（net/smtp + crypto/tls），不引入外部依赖；
//   - Sender 是接口：通知服务按后台 SMTP 设置动态构造 SMTP 发送器，测试注入假实现
//     断言收件人/主题/正文，无需真实 SMTP；
//   - 同步发送（调用方决定是否异步），失败返回错误由调用方留痕（email_logs）；
//   - 正文纯文本 + base64（避免中文与点号开头的行被 SMTP 传输改写），主题与显示名
//     按 RFC 2047 编码；
//   - 任何日志与错误信息都不含认证口令（口令只在 smtp.PlainAuth 内部使用）。
package email

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// 传输加密方式取值（与 settings.Encryption* 一一对应，本包不反向依赖 settings）。
const (
	// EncryptionNone 明文 SMTP。
	EncryptionNone = "none"
	// EncryptionStartTLS 明文建连后 STARTTLS 升级。
	EncryptionStartTLS = "starttls"
	// EncryptionSSL 建连即 TLS（implicit TLS）。
	EncryptionSSL = "ssl"
)

// DefaultTimeout 是单次发送的整体超时缺省值（连接 + 握手 + 投递）。
const DefaultTimeout = 15 * time.Second

// Message 是一封待发送的纯文本邮件。
type Message struct {
	// To 收件人地址（单个；通知均为单收件人）。
	To string
	// Subject 主题（非 ASCII 自动 RFC 2047 编码）。
	Subject string
	// Body 正文（纯文本；换行可用 \n，发送时统一为 CRLF）。
	Body string
	// From 发件人地址。
	From string
	// FromName 发件人显示名（可空）。
	FromName string
}

// Sender 是邮件发送能力（通知服务依赖它，测试可注入假实现）。
type Sender interface {
	// Send 同步发送一封邮件；失败返回错误（错误信息不含认证口令）。
	Send(ctx context.Context, msg Message) error
}

// SMTPConfig 是 SMTP 发送器的构造参数。
type SMTPConfig struct {
	Host string
	Port int
	// Username / Password 为空表示不做认证（内网 MTA 常见）。
	Username string
	Password string
	From     string
	FromName string
	// Encryption 取 none / starttls / ssl；空值按 none。
	Encryption string
	// Timeout 单次发送超时；<=0 时取 DefaultTimeout。
	Timeout time.Duration
	// TLSConfig 可选：TLS 握手的基配置（nil 时按 ServerName=Host 校验证书）。
	// 供部署方接入私有 CA（测试也用它注入自签证书的 RootCAs）。
	TLSConfig *tls.Config
}

// SMTPSender 是基于 net/smtp 的 Sender 实现。
type SMTPSender struct {
	cfg SMTPConfig
}

// NewSMTP 构造 SMTP 发送器。
func NewSMTP(cfg SMTPConfig) *SMTPSender {
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	return &SMTPSender{cfg: cfg}
}

// Send 实现 Sender：建连 →（STARTTLS / 隐式 TLS）→（可选认证）→ MAIL/RCPT/DATA → QUIT。
//
// 错误信息只包含 SMTP 阶段与服务器应答（脱敏：不含口令），供 email_logs 留痕。
func (s *SMTPSender) Send(ctx context.Context, msg Message) error {
	if strings.TrimSpace(msg.To) == "" {
		return errors.New("收件人地址为空")
	}
	from := strings.TrimSpace(msg.From)
	if from == "" {
		return errors.New("发件人地址为空")
	}
	host := strings.TrimSpace(s.cfg.Host)
	if host == "" {
		return errors.New("SMTP 服务器未配置")
	}

	deadline := time.Now().Add(s.cfg.Timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}

	conn, err := s.dial(host, deadline, msg)
	if err != nil {
		return err
	}

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("SMTP 握手失败：%v", err)
	}
	defer func() { _ = client.Close() }()

	if s.encryption() == EncryptionStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP 服务器不支持 STARTTLS（可把 encryption 改为 none 或 ssl）")
		}
		if err := client.StartTLS(s.tlsConfig(host)); err != nil {
			return fmt.Errorf("STARTTLS 升级失败：%v", err)
		}
	}

	if s.cfg.Username != "" {
		// PlainAuth 在非 TLS 连接上只允许本地回环地址（标准库的安全保护）；
		// 远程明文 SMTP + 认证会被拒，请改用 starttls / ssl。
		auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP 认证失败（用户名 %s）：%v", s.cfg.Username, err)
		}
	}

	if err := client.Mail(from); err != nil {
		return fmt.Errorf("SMTP MAIL FROM 被拒绝：%v", err)
	}
	if err := client.Rcpt(msg.To); err != nil {
		return fmt.Errorf("SMTP RCPT TO 被拒绝：%v", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA 被拒绝：%v", err)
	}
	if _, err := writer.Write(BuildRFC5322(Message{
		To:       msg.To,
		Subject:  msg.Subject,
		Body:     msg.Body,
		From:     from,
		FromName: msg.FromName,
	})); err != nil {
		_ = writer.Close()
		return fmt.Errorf("SMTP 正文写入失败：%v", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("SMTP 投递失败：%v", err)
	}
	// QUIT 失败不影响「已投递」的事实（服务器可能已关闭连接）。
	_ = client.Quit()
	return nil
}

// dial 建立到底层 SMTP 服务器的连接（ssl 为隐式 TLS，其余为明文，随后按需 STARTTLS）。
func (s *SMTPSender) dial(host string, deadline time.Time, _ Message) (net.Conn, error) {
	address := net.JoinHostPort(host, strconv.Itoa(s.port()))
	dialer := &net.Dialer{Deadline: deadline, Timeout: s.cfg.Timeout}

	if s.encryption() == EncryptionSSL {
		conn, err := tls.DialWithDialer(dialer, "tcp", address, s.tlsConfig(host))
		if err != nil {
			return nil, fmt.Errorf("SMTP 连接失败（ssl %s）：%v", address, err)
		}
		if err := conn.SetDeadline(deadline); err != nil {
			_ = conn.Close()
			return nil, err
		}
		return conn, nil
	}

	conn, err := dialer.DialContext(context.Background(), "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("SMTP 连接失败（%s）：%v", address, err)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// tlsConfig 返回 TLS 配置（调用方给了基配置则在副本上补 ServerName）。
func (s *SMTPSender) tlsConfig(host string) *tls.Config {
	var config *tls.Config
	if s.cfg.TLSConfig != nil {
		config = s.cfg.TLSConfig.Clone()
	} else {
		config = &tls.Config{}
	}
	if config.ServerName == "" {
		config.ServerName = host
	}
	return config
}

// encryption 返回归一化后的加密方式。
func (s *SMTPSender) encryption() string {
	switch strings.ToLower(strings.TrimSpace(s.cfg.Encryption)) {
	case EncryptionSSL:
		return EncryptionSSL
	case EncryptionStartTLS:
		return EncryptionStartTLS
	default:
		return EncryptionNone
	}
}

// port 返回实际端口（未配置时按加密方式取缺省）。
func (s *SMTPSender) port() int {
	if s.cfg.Port > 0 {
		return s.cfg.Port
	}
	switch s.encryption() {
	case EncryptionSSL:
		return 465
	case EncryptionStartTLS:
		return 587
	default:
		return 25
	}
}

// BuildRFC5322 组装完整的邮件报文（CRLF 行尾；正文 base64；主题与显示名按需编码）。
//
// 导出供测试与排查使用（把「最终发出去的字节」变成可断言、可打印的纯函数）。
func BuildRFC5322(msg Message) []byte {
	var buffer bytes.Buffer

	from := strings.TrimSpace(msg.From)
	if name := strings.TrimSpace(msg.FromName); name != "" {
		from = encodeHeader(name) + " <" + from + ">"
	}
	writeHeader(&buffer, "From", from)
	writeHeader(&buffer, "To", strings.TrimSpace(msg.To))
	writeHeader(&buffer, "Subject", encodeHeader(strings.TrimSpace(msg.Subject)))
	writeHeader(&buffer, "Date", time.Now().UTC().Format(time.RFC1123Z))
	writeHeader(&buffer, "MIME-Version", "1.0")
	writeHeader(&buffer, "Content-Type", "text/plain; charset=UTF-8")
	writeHeader(&buffer, "Content-Transfer-Encoding", "base64")
	buffer.WriteString("\r\n")

	// 正文统一为 CRLF 后整体 base64（76 字符换行），避免中文、长行与「.」开头的行被传输层改写。
	body := strings.ReplaceAll(strings.ReplaceAll(msg.Body, "\r\n", "\n"), "\n", "\r\n")
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	for len(encoded) > 76 {
		buffer.WriteString(encoded[:76])
		buffer.WriteString("\r\n")
		encoded = encoded[76:]
	}
	buffer.WriteString(encoded)
	buffer.WriteString("\r\n")
	return buffer.Bytes()
}

// writeHeader 写一行头部（值中的换行会被剥掉，防止头部注入）。
func writeHeader(buffer *bytes.Buffer, key, value string) {
	value = strings.NewReplacer("\r", " ", "\n", " ").Replace(value)
	buffer.WriteString(key)
	buffer.WriteString(": ")
	buffer.WriteString(value)
	buffer.WriteString("\r\n")
}

// encodeHeader 按 RFC 2047 编码头部取值：纯 ASCII 可打印直接返回，否则用
// `=?UTF-8?B?<base64>?=` 编码词（中文主题与站点名走这条路径）。
func encodeHeader(value string) string {
	if value == "" {
		return ""
	}
	if isASCIIPrintable(value) {
		return value
	}
	return mime.BEncoding.Encode("UTF-8", value)
}

// isASCIIPrintable 判断取值是否为纯 ASCII 可打印字符（可安全直出头部）。
func isASCIIPrintable(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, char := range value {
		if char < 0x20 || char > 0x7e {
			return false
		}
	}
	return true
}
