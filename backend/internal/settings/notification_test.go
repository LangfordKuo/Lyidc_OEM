package settings

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// email.smtp 设置（阶段 6b）
// ---------------------------------------------------------------------------

// TestSMTPPasswordThreeState 验证 SMTP 口令的三态语义：省略=保持、新值=替换、空串=清空。
func TestSMTPPasswordThreeState(t *testing.T) {
	base := SMTP{Enabled: true, Host: "smtp.example.com", Port: 587, Username: "mailer",
		Password: "super-secret", From: "noreply@oem.example.com", Encryption: EncryptionStartTLS}

	kept, err := base.Apply(SMTPUpdate{FromName: strptr("示例站点")})
	if err != nil {
		t.Fatalf("Apply 省略 password 失败: %v", err)
	}
	if kept.Password != base.Password {
		t.Fatalf("省略 password 后 = %q，期望保持", kept.Password)
	}

	replaced, err := base.Apply(SMTPUpdate{Password: strptr("brand-new")})
	if err != nil {
		t.Fatalf("Apply 替换 password 失败: %v", err)
	}
	if replaced.Password != "brand-new" {
		t.Fatalf("替换后 password = %q", replaced.Password)
	}

	// 清空口令必须同时清空 username（否则触发「配了用户名没口令」的规则拒绝）。
	cleared, err := base.Apply(SMTPUpdate{Password: strptr(""), Username: strptr("")})
	if err != nil {
		t.Fatalf("Apply 清空 password 失败: %v", err)
	}
	if cleared.Password != "" || cleared.Username != "" {
		t.Fatalf("清空后 = %+v", cleared)
	}
}

// TestSMTPEnabledRequiresFields 验证启用时的必填校验与认证口径。
func TestSMTPEnabledRequiresFields(t *testing.T) {
	if _, err := (SMTP{}).Apply(SMTPUpdate{Enabled: boolptr(true)}); !errors.Is(err, ErrRule) {
		t.Fatalf("缺少 host/from 时 err=%v，期望 ErrRule", err)
	}

	// 只配 host 仍不完整（缺 from）。
	if _, err := (SMTP{Host: "smtp.example.com"}).Apply(SMTPUpdate{Enabled: boolptr(true)}); !errors.Is(err, ErrRule) {
		t.Fatalf("缺少 from 时 err=%v，期望 ErrRule", err)
	}

	// 配了用户名却没口令 → 规则拒绝（避免静默发不出信）。
	withoutPassword := SMTP{Enabled: true, Host: "smtp.example.com", From: "a@b.com", Username: "mailer"}
	if err := withoutPassword.Validate(); !errors.Is(err, ErrRule) {
		t.Fatalf("username 无 password 时 err=%v，期望 ErrRule", err)
	}

	// 关闭状态下可以留空（草稿态）。
	if err := (SMTP{Host: "smtp.example.com"}).Validate(); err != nil {
		t.Fatalf("未启用时不应校验必填: %v", err)
	}
}

// TestSMTPFormatValidation 验证加密方式、端口与发件人格式校验。
func TestSMTPFormatValidation(t *testing.T) {
	base := SMTP{Enabled: false, Host: "smtp.example.com", From: "noreply@oem.example.com"}

	if _, err := base.Apply(SMTPUpdate{Encryption: strptr("tls")}); !errors.Is(err, ErrFormat) {
		t.Fatalf("非法 encryption err=%v，期望 ErrFormat", err)
	}
	if _, err := base.Apply(SMTPUpdate{Port: intptr(70000)}); !errors.Is(err, ErrFormat) {
		t.Fatalf("越界 port err=%v，期望 ErrFormat", err)
	}
	if _, err := base.Apply(SMTPUpdate{From: strptr("not-an-email")}); !errors.Is(err, ErrFormat) {
		t.Fatalf("非法 from err=%v，期望 ErrFormat", err)
	}
}

// TestSMTPPortDefaultsAndUsable 验证端口缺省与可用性判定。
func TestSMTPPortDefaultsAndUsable(t *testing.T) {
	cases := []struct {
		encryption string
		want       int
	}{
		{"", DefaultSMTPPortNone},
		{EncryptionNone, DefaultSMTPPortNone},
		{EncryptionStartTLS, DefaultSMTPPortStartTLS},
		{EncryptionSSL, DefaultSMTPPortSSL},
	}
	for _, tc := range cases {
		value := SMTP{Host: "smtp.example.com", Encryption: tc.encryption}
		if got := value.PortOr(); got != tc.want {
			t.Errorf("encryption=%q PortOr=%d，期望 %d", tc.encryption, got, tc.want)
		}
	}

	usable := SMTP{Enabled: true, Host: "smtp.example.com", From: "a@b.com", Encryption: EncryptionSSL}
	if !usable.Usable() {
		t.Error("配置齐全且启用时应可用")
	}
	if (SMTP{Host: "smtp.example.com", From: "a@b.com"}).Usable() {
		t.Error("未启用不应可用")
	}
	if usable.Endpoint() != "smtp.example.com:465" {
		t.Errorf("Endpoint=%q", usable.Endpoint())
	}
}

// TestSMTPReaderDefaultsAndFingerprint 验证读取缺省与指纹（未配置时 Enabled=false、空指纹）。
func TestSMTPReaderDefaultsAndFingerprint(t *testing.T) {
	reader := NewReader(fakeSource{})
	state, err := reader.SMTP(context.Background())
	if err != nil {
		t.Fatalf("读取未配置的 SMTP 失败: %v", err)
	}
	if state.Value.Enabled || state.Fingerprint != "" || state.Meta.Exists {
		t.Fatalf("未配置时 = %+v，期望零值与空指纹", state)
	}

	stored := `{"enabled":true,"host":"smtp.example.com","port":465,"username":"mailer",` +
		`"password":"secret","from":"noreply@oem.example.com","from_name":"站点","encryption":"ssl"}`
	reader = NewReader(fakeSource{values: map[string]string{KeyEmailSMTP: stored}})
	state, err = reader.SMTP(context.Background())
	if err != nil {
		t.Fatalf("读取 SMTP 失败: %v", err)
	}
	if !state.Value.Usable() || state.Value.Password != "secret" || state.Value.PortOr() != 465 {
		t.Fatalf("解析结果 = %+v", state.Value)
	}
	if state.Fingerprint != stored {
		t.Errorf("指纹应等于库内原文")
	}
}

// TestSMTPDecodeCorrupt 验证库内非法 JSON 的报错口径。
func TestSMTPDecodeCorrupt(t *testing.T) {
	reader := NewReader(fakeSource{values: map[string]string{KeyEmailSMTP: "{not json"}})
	if _, err := reader.SMTP(context.Background()); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("损坏的值 err=%v，期望 ErrCorrupt", err)
	}
}

// ---------------------------------------------------------------------------
// notifications 设置（阶段 6b）
// ---------------------------------------------------------------------------

// TestNotificationDefaults 验证未配置时的缺省开关（全开 + 提前 7 天）。
func TestNotificationDefaults(t *testing.T) {
	reader := NewReader(fakeSource{})
	state, err := reader.Notifications(context.Background())
	if err != nil {
		t.Fatalf("读取缺省通知开关失败: %v", err)
	}
	want := DefaultNotificationSettings()
	if state.Value != want {
		t.Fatalf("缺省值 = %+v，期望 %+v", state.Value, want)
	}
	if state.Value.ReminderDaysOr() != 7 {
		t.Errorf("缺省提醒天数 = %d，期望 7", state.Value.ReminderDaysOr())
	}
}

// TestNotificationApplyAndValidation 验证局部更新与天数范围校验。
func TestNotificationApplyAndValidation(t *testing.T) {
	base := DefaultNotificationSettings()

	updated, err := base.Apply(NotificationUpdate{ExpiryReminderDays: intptr(3)})
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if updated.ExpiryReminderDays != 3 || !updated.InAppEnabled {
		t.Fatalf("局部更新结果 = %+v", updated)
	}

	if _, err := base.Apply(NotificationUpdate{ExpiryReminderDays: intptr(0)}); !errors.Is(err, ErrFormat) {
		t.Fatalf("天数 0 err=%v，期望 ErrFormat", err)
	}
	if _, err := base.Apply(NotificationUpdate{ExpiryReminderDays: intptr(31)}); !errors.Is(err, ErrFormat) {
		t.Fatalf("天数 31 err=%v，期望 ErrFormat", err)
	}

	off, err := base.Apply(NotificationUpdate{InAppEnabled: boolptr(false)})
	if err != nil {
		t.Fatalf("关闭站内开关失败: %v", err)
	}
	if off.InAppEnabled {
		t.Fatal("站内开关应被关闭")
	}
}

// TestNotificationDecodePartialAndCorrupt 验证「缺字段的库内值」按缺省补齐、非法 JSON 报错。
func TestNotificationDecodePartialAndCorrupt(t *testing.T) {
	reader := NewReader(fakeSource{values: map[string]string{KeyNotifications: `{"inapp_enabled":false}`}})
	state, err := reader.Notifications(context.Background())
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if state.Value.InAppEnabled {
		t.Error("库内显式关闭的站内开关应生效")
	}
	if !state.Value.EmailEnabled || state.Value.ReminderDaysOr() != DefaultExpiryReminderDays {
		t.Errorf("未提供的字段应取缺省: %+v", state.Value)
	}

	corrupt := NewReader(fakeSource{values: map[string]string{KeyNotifications: "[]"}})
	if _, err := corrupt.Notifications(context.Background()); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("损坏的值 err=%v，期望 ErrCorrupt", err)
	}
}

// TestNotificationEncodeRoundTrip 验证编码后可被解析回原值（落库口径）。
func TestNotificationEncodeRoundTrip(t *testing.T) {
	value := NotificationSettings{
		InAppEnabled: false, EmailEnabled: true, ExpiryReminderEnabled: false, ExpiryReminderDays: 14,
	}
	encoded, err := value.Encode()
	if err != nil {
		t.Fatalf("Encode 失败: %v", err)
	}
	if !strings.Contains(encoded, `"expiry_reminder_days":14`) {
		t.Fatalf("编码结果 = %s", encoded)
	}
	reader := NewReader(fakeSource{values: map[string]string{KeyNotifications: encoded}})
	state, err := reader.Notifications(context.Background())
	if err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if state.Value != value {
		t.Fatalf("往返后 = %+v，期望 %+v", state.Value, value)
	}
}
