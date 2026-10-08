package email

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// 进程内 SMTP 接收器（不装依赖、不连外网）：应答 EHLO/STARTTLS/AUTH/MAIL/RCPT/DATA，
// 把收到的邮件原文与「是否走了 TLS」记录下来供断言。
// ---------------------------------------------------------------------------

type sinkMessage struct {
	from    string
	to      []string
	data    string
	usedTLS bool
}

type smtpSink struct {
	listener net.Listener
	tlsConf  *tls.Config
	// implicitTLS 为 true 时建连即 TLS（对应 ssl 直连）。
	implicitTLS bool
	// offerStartTLS 为 true 时在 EHLO 应答里声明 STARTTLS。
	offerStartTLS bool
	// authUser / authPass 非空时要求 AUTH PLAIN 通过（否则 535）。
	authUser string
	authPass string
	// rootCAs 是自签证书的信任池（客户端配置用；与 sink 用同一张证书）。
	rootCAs *x509.CertPool

	mu       sync.Mutex
	messages []sinkMessage
	errors   []error
}

type smtpSinkConfig struct {
	implicitTLS   bool
	offerStartTLS bool
	authUser      string
	authPass      string
}

func newSMTPSink(t *testing.T, cfg smtpSinkConfig) *smtpSink {
	t.Helper()

	var tlsConf *tls.Config
	var rootCAs *x509.CertPool
	if cfg.implicitTLS || cfg.offerStartTLS {
		tlsConf, rootCAs = selfSignedTLSConfig(t)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("启动假 SMTP 监听失败: %v", err)
	}
	sink := &smtpSink{
		listener:      listener,
		tlsConf:       tlsConf,
		implicitTLS:   cfg.implicitTLS,
		offerStartTLS: cfg.offerStartTLS,
		authUser:      cfg.authUser,
		authPass:      cfg.authPass,
		rootCAs:       rootCAs,
	}
	go sink.serve()
	t.Cleanup(func() { _ = listener.Close() })
	return sink
}

// hostPort 返回监听地址的 host 与 port（发送器配置用）。
func (s *smtpSink) hostPort(t *testing.T) (string, int) {
	t.Helper()
	address, ok := s.listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("监听地址类型异常: %T", s.listener.Addr())
	}
	return address.IP.String(), address.Port
}

// received 返回收到的第 index 封邮件（不存在时致测失败）。
func (s *smtpSink) received(t *testing.T, index int) sinkMessage {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if index >= len(s.messages) {
		t.Fatalf("假 SMTP 只收到 %d 封邮件，期望至少 %d 封", len(s.messages), index+1)
	}
	return s.messages[index]
}

// count 返回收到的邮件数。
func (s *smtpSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages)
}

func (s *smtpSink) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *smtpSink) record(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errors = append(s.errors, err)
}

// handle 处理一条 SMTP 会话（最小实现：覆盖本服务实际用到的命令）。
func (s *smtpSink) handle(raw net.Conn) {
	defer func() { _ = raw.Close() }()

	conn := raw
	usedTLS := false
	if s.implicitTLS {
		tlsConn := tls.Server(conn, s.tlsConf)
		if err := tlsConn.Handshake(); err != nil {
			s.record(fmt.Errorf("隐式 TLS 握手失败: %w", err))
			return
		}
		conn = tlsConn
		usedTLS = true
	}

	reader := bufio.NewReader(conn)
	if err := s.reply(conn, "220 sink ESMTP ready"); err != nil {
		return
	}

	var (
		from     string
		to       []string
		authed   = s.authUser == ""
		pending  bool
		authLine string
	)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		command := strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(command)

		switch {
		case pending:
			// 上一行是「AUTH PLAIN」（无参数），本行是 base64 凭据。
			pending = false
			authed = s.checkAuth(authLine, command)
			if err := s.reply(conn, authReply(authed)); err != nil {
				return
			}

		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			if err := s.reply(conn, s.ehloReply()); err != nil {
				return
			}

		case upper == "STARTTLS":
			if s.tlsConf == nil {
				_ = s.reply(conn, "502 STARTTLS not supported")
				continue
			}
			if err := s.reply(conn, "220 Ready to start TLS"); err != nil {
				return
			}
			tlsConn := tls.Server(conn, s.tlsConf)
			if err := tlsConn.Handshake(); err != nil {
				s.record(fmt.Errorf("STARTTLS 握手失败: %w", err))
				return
			}
			conn = tlsConn
			reader = bufio.NewReader(conn)
			usedTLS = true

		case strings.HasPrefix(upper, "AUTH PLAIN"):
			argument := strings.TrimSpace(command[len("AUTH PLAIN"):])
			if argument == "" {
				pending = true
				authLine = command
				if err := s.reply(conn, "334 "); err != nil {
					return
				}
				continue
			}
			authed = s.checkAuth(command, argument)
			if err := s.reply(conn, authReply(authed)); err != nil {
				return
			}

		case strings.HasPrefix(upper, "MAIL FROM"):
			from = extractAddress(command)
			if err := s.reply(conn, "250 OK"); err != nil {
				return
			}

		case strings.HasPrefix(upper, "RCPT TO"):
			to = append(to, extractAddress(command))
			if err := s.reply(conn, "250 OK"); err != nil {
				return
			}

		case upper == "DATA":
			if !authed {
				_ = s.reply(conn, "530 Authentication required")
				continue
			}
			if err := s.reply(conn, "354 End data with <CR><LF>.<CR><LF>"); err != nil {
				return
			}
			data, err := readData(reader)
			if err != nil {
				return
			}
			s.mu.Lock()
			s.messages = append(s.messages, sinkMessage{from: from, to: to, data: data, usedTLS: usedTLS})
			s.mu.Unlock()
			if err := s.reply(conn, "250 OK queued"); err != nil {
				return
			}
			from, to = "", nil

		case upper == "QUIT":
			_ = s.reply(conn, "221 Bye")
			return

		case upper == "RSET", upper == "NOOP":
			if err := s.reply(conn, "250 OK"); err != nil {
				return
			}

		default:
			if err := s.reply(conn, "250 OK"); err != nil {
				return
			}
		}
	}
}

// ehloReply 组装 EHLO 应答（声明 STARTTLS / AUTH 能力）。
func (s *smtpSink) ehloReply() string {
	lines := []string{"sink greets you"}
	if s.offerStartTLS {
		lines = append(lines, "STARTTLS")
	}
	lines = append(lines, "AUTH PLAIN")
	var builder strings.Builder
	for i, line := range lines {
		builder.WriteString("250")
		if i < len(lines)-1 {
			builder.WriteString("-")
		} else {
			builder.WriteString(" ")
		}
		builder.WriteString(line)
		builder.WriteString("\r\n")
	}
	return strings.TrimRight(builder.String(), "\r\n")
}

// checkAuth 校验 AUTH PLAIN 的凭据（argument 是 base64("\0user\0pass")）。
func (s *smtpSink) checkAuth(command, argument string) bool {
	_ = command
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(argument))
	if err != nil {
		return false
	}
	fields := strings.Split(string(raw), "\x00")
	if len(fields) != 3 {
		return false
	}
	return fields[1] == s.authUser && fields[2] == s.authPass
}

// authReply 返回 AUTH 应答（已认证时校验口令是否匹配过）。
func authReply(ok bool) string {
	if ok {
		return "235 2.7.0 Authentication successful"
	}
	return "535 5.7.8 Authentication credentials invalid"
}

// reply 写一行应答（多行由调用方拼好）。
func (s *smtpSink) reply(conn net.Conn, text string) error {
	if !strings.HasSuffix(text, "\r\n") {
		text += "\r\n"
	}
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := conn.Write([]byte(text))
	return err
}

// readData 读取 DATA 段（以单独的 "." 行结束）。
func readData(reader *bufio.Reader) (string, error) {
	var builder strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		if strings.TrimRight(line, "\r\n") == "." {
			return builder.String(), nil
		}
		builder.WriteString(line)
	}
}

// extractAddress 从 "MAIL FROM:<a@b>" 里取出地址。
func extractAddress(command string) string {
	start := strings.Index(command, "<")
	end := strings.LastIndex(command, ">")
	if start >= 0 && end > start {
		return command[start+1 : end]
	}
	return strings.TrimSpace(command)
}

// selfSignedTLSConfig 生成自签证书的 TLS 配置与对应的 RootCAs（供客户端信任）。
func selfSignedTLSConfig(t *testing.T) (*tls.Config, *x509.CertPool) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成测试私钥失败: %v", err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("生成自签证书失败: %v", err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("解析自签证书失败: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(parsed)
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}, pool
}

// ---------------------------------------------------------------------------
// 报文组装（纯函数）
// ---------------------------------------------------------------------------

func TestBuildRFC5322HeadersAndBody(t *testing.T) {
	raw := string(BuildRFC5322(Message{
		To:       "member@example.com",
		From:     "noreply@oem.example.com",
		FromName: "示例站点",
		Subject:  "订单交付成功",
		Body:     "第一行\n第二行",
	}))

	if !strings.Contains(raw, "To: member@example.com\r\n") {
		t.Errorf("缺少 To 头: %q", raw)
	}
	if !strings.Contains(raw, "Content-Type: text/plain; charset=UTF-8\r\n") {
		t.Errorf("缺少 Content-Type 头: %q", raw)
	}
	if !strings.Contains(raw, "Content-Transfer-Encoding: base64\r\n") {
		t.Errorf("缺少传输编码头: %q", raw)
	}
	// 非 ASCII 主题与显示名必须 RFC 2047 编码（避免被 SMTP 传输层破坏）。
	if strings.Contains(raw, "订单交付成功") {
		t.Errorf("中文主题应做 RFC 2047 编码: %q", raw)
	}
	if !strings.Contains(strings.ToLower(raw), "subject: =?utf-8?b?") {
		t.Errorf("主题缺少编码词前缀: %q", raw)
	}
	if !strings.Contains(strings.ToLower(raw), "from: =?utf-8?b?") {
		t.Errorf("显示名缺少编码词前缀: %q", raw)
	}

	// 正文：base64 解码后应为 CRLF 行的原文。
	body := raw[strings.Index(raw, "\r\n\r\n")+4:]
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(strings.TrimSpace(body), "\r\n", ""))
	if err != nil {
		t.Fatalf("正文不是合法 base64: %v", err)
	}
	if string(decoded) != "第一行\r\n第二行" {
		t.Errorf("正文 = %q, 期望 CRLF 原文", string(decoded))
	}
}

func TestBuildRFC5322StripsHeaderInjection(t *testing.T) {
	raw := string(BuildRFC5322(Message{
		To:      "member@example.com",
		From:    "noreply@oem.example.com",
		Subject: "正常主题\r\nBcc: evil@example.com",
		Body:    "正文",
	}))
	if strings.Contains(raw, "Bcc:") {
		t.Fatalf("主题里的换行必须被剥掉（防头部注入）: %q", raw)
	}
}

// ---------------------------------------------------------------------------
// SMTP 发送（明文 / STARTTLS / 隐式 TLS / 失败路径）
// ---------------------------------------------------------------------------

func TestSendPlainSMTP(t *testing.T) {
	sink := newSMTPSink(t, smtpSinkConfig{})
	host, port := sink.hostPort(t)

	sender := NewSMTP(SMTPConfig{Host: host, Port: port, Encryption: EncryptionNone, Timeout: 5 * time.Second})
	err := sender.Send(context.Background(), Message{
		To:      "member@example.com",
		Subject: "通知测试",
		Body:    "正文内容",
		From:    "noreply@oem.example.com",
	})
	if err != nil {
		t.Fatalf("发送失败: %v", err)
	}

	got := sink.received(t, 0)
	if got.from != "noreply@oem.example.com" {
		t.Errorf("MAIL FROM = %q", got.from)
	}
	if len(got.to) != 1 || got.to[0] != "member@example.com" {
		t.Errorf("RCPT TO = %v", got.to)
	}
	if got.usedTLS {
		t.Error("encryption=none 不应走 TLS")
	}
	assertBodyDecodes(t, got.data, "正文内容")
}

func TestSendStartTLSWithAuth(t *testing.T) {
	sink := newSMTPSink(t, smtpSinkConfig{offerStartTLS: true, authUser: "mailer", authPass: "s3cret"})
	host, port := sink.hostPort(t)

	sender := NewSMTP(SMTPConfig{
		Host: host, Port: port, Encryption: EncryptionStartTLS,
		Username: "mailer", Password: "s3cret",
		Timeout:   5 * time.Second,
		TLSConfig: &tls.Config{RootCAs: sink.rootCAs},
	})
	err := sender.Send(context.Background(), Message{
		To:      "ops@example.com",
		Subject: "starttls 测试",
		Body:    "body",
		From:    "noreply@oem.example.com",
	})
	if err != nil {
		t.Fatalf("STARTTLS 发送失败: %v", err)
	}
	if got := sink.received(t, 0); !got.usedTLS {
		t.Error("STARTTLS 升级后 usedTLS 应为 true")
	}
}

func TestSendImplicitTLS(t *testing.T) {
	sink := newSMTPSink(t, smtpSinkConfig{implicitTLS: true})
	host, port := sink.hostPort(t)

	sender := NewSMTP(SMTPConfig{
		Host: host, Port: port, Encryption: EncryptionSSL,
		Timeout:   5 * time.Second,
		TLSConfig: &tls.Config{RootCAs: sink.rootCAs},
	})
	if err := sender.Send(context.Background(), Message{
		To:   "ops@example.com",
		From: "noreply@oem.example.com",
		Body: "ssl 正文",
	}); err != nil {
		t.Fatalf("ssl 直连发送失败: %v", err)
	}
	if got := sink.received(t, 0); !got.usedTLS {
		t.Error("ssl 直连必须走 TLS")
	}
}

func TestSendFailsWhenStartTLSUnsupported(t *testing.T) {
	sink := newSMTPSink(t, smtpSinkConfig{}) // 不声明 STARTTLS
	host, port := sink.hostPort(t)

	sender := NewSMTP(SMTPConfig{Host: host, Port: port, Encryption: EncryptionStartTLS, Timeout: 5 * time.Second})
	err := sender.Send(context.Background(), Message{
		To: "ops@example.com", From: "noreply@oem.example.com", Body: "x",
	})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("STARTTLS 不可用时 err=%v，期望明确提示", err)
	}
	if sink.count() != 0 {
		t.Error("未升级 TLS 不应投递邮件")
	}
}

func TestSendFailsWhenAuthRejected(t *testing.T) {
	sink := newSMTPSink(t, smtpSinkConfig{offerStartTLS: true, authUser: "mailer", authPass: "right"})
	host, port := sink.hostPort(t)

	sender := NewSMTP(SMTPConfig{
		Host: host, Port: port, Encryption: EncryptionStartTLS,
		Username: "mailer", Password: "wrong",
		Timeout:   5 * time.Second,
		TLSConfig: &tls.Config{RootCAs: sink.rootCAs},
	})
	err := sender.Send(context.Background(), Message{
		To: "ops@example.com", From: "noreply@oem.example.com", Body: "x",
	})
	if err == nil || !strings.Contains(err.Error(), "认证失败") {
		t.Fatalf("认证失败时 err=%v", err)
	}
	if strings.Contains(err.Error(), "wrong") {
		t.Error("错误信息不得包含口令")
	}
}

func TestSendFailsWhenServerUnreachable(t *testing.T) {
	// 占一个端口后立即关闭：连接必然被拒。
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	sender := NewSMTP(SMTPConfig{Host: "127.0.0.1", Port: port, Encryption: EncryptionNone, Timeout: 2 * time.Second})
	err = sender.Send(context.Background(), Message{
		To: "ops@example.com", From: "noreply@oem.example.com", Body: "x",
	})
	if err == nil || !strings.Contains(err.Error(), "SMTP 连接失败") {
		t.Fatalf("服务器不可达时 err=%v", err)
	}
}

func TestSendValidatesRequiredFields(t *testing.T) {
	sender := NewSMTP(SMTPConfig{Host: "127.0.0.1", Port: 25})
	if err := sender.Send(context.Background(), Message{From: "a@b.com", Subject: "s"}); err == nil {
		t.Error("缺少收件人应报错")
	}
	if err := sender.Send(context.Background(), Message{To: "a@b.com", Subject: "s"}); err == nil {
		t.Error("缺少发件人应报错")
	}
	if err := NewSMTP(SMTPConfig{}).Send(context.Background(),
		Message{To: "a@b.com", From: "c@d.com"}); err == nil {
		t.Error("缺少 SMTP 服务器应报错")
	}
}

func TestPortDefaultsByEncryption(t *testing.T) {
	cases := []struct {
		encryption string
		want       int
	}{
		{"", 25},
		{EncryptionNone, 25},
		{EncryptionStartTLS, 587},
		{EncryptionSSL, 465},
	}
	for _, tc := range cases {
		sender := NewSMTP(SMTPConfig{Host: "smtp.example.com", Encryption: tc.encryption})
		if got := sender.port(); got != tc.want {
			t.Errorf("encryption=%q port=%d, 期望 %d", tc.encryption, got, tc.want)
		}
	}
}

// assertBodyDecodes 断言 DATA 原文中的 base64 正文解码后包含期望文本。
func assertBodyDecodes(t *testing.T, data, want string) {
	t.Helper()
	index := strings.Index(data, "\r\n\r\n")
	if index < 0 {
		t.Fatalf("DATA 缺少头部与正文的分隔: %q", data)
	}
	encoded := strings.ReplaceAll(strings.TrimSpace(data[index+4:]), "\r\n", "")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("正文不是合法 base64: %v", err)
	}
	if !strings.Contains(string(decoded), want) {
		t.Errorf("正文 = %q，期望包含 %q", string(decoded), want)
	}
}
