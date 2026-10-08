package router

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
)

// ---------------------------------------------------------------------------
// 阶段 6b：SMTP 设置、测试邮件与通知开关（管理端接口，仅 admin）
// ---------------------------------------------------------------------------

const (
	emailSMTPPath        = "/api/v1/admin/settings/email/smtp"
	emailTestPath        = "/api/v1/admin/settings/email/test"
	notificationCfgPath  = "/api/v1/admin/settings/notifications"
	testSMTPPassword     = "smtp-secret-2026"
	testSMTPFrom         = "noreply@oem.example.com"
	testSMTPHost         = "smtp.example.com"
	testSMTPUser         = "mailer@oem.example.com"
	testSMTPAdminPasswd  = "password123"
	testSMTPAdminAccount = "smtpadmin"
)

// putJSON 发起 PUT 请求。
func putJSON(t *testing.T, engine http.Handler, path, token string, body any) (*httptest.ResponseRecorder, apiEnvelope) {
	t.Helper()
	return doAPI(t, engine, http.MethodPut, path, token, body)
}

// TestEmailSMTPSettingsLifecycle 覆盖 SMTP 设置的三态口令、掩码与局部更新。
func TestEmailSMTPSettingsLifecycle(t *testing.T) {
	gdb := testDatabase(t)
	engine, _, _ := newTicketNotifyEngine(t, gdb)
	seedAdmin(t, gdb, testSMTPAdminAccount, testSMTPAdminPasswd, model.RoleAdmin, model.StatusActive)
	token, _ := loginAdmin(t, engine, testSMTPAdminAccount, testSMTPAdminPasswd)

	// 未配置：缺省视图（未启用、端口按加密方式取缺省、口令未配置）。
	rec, envelope := doAPI(t, engine, http.MethodGet, emailSMTPPath, token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("读取 SMTP 设置失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	view := decodeData[emailSMTPView](t, envelope)
	if view.Enabled || view.PasswordConfigured || view.Encryption != "none" || view.PortEffective != 25 {
		t.Fatalf("未配置时的视图 = %+v", view)
	}

	// 首次启用：写入完整配置（含口令）。
	rec, envelope = putJSON(t, engine, emailSMTPPath, token, map[string]any{
		"enabled": true, "host": testSMTPHost, "port": 465, "username": testSMTPUser,
		"password": testSMTPPassword, "from": testSMTPFrom, "from_name": "示例云主机",
		"encryption": "ssl",
	})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("写入 SMTP 设置失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	view = decodeData[emailSMTPView](t, envelope)
	if !view.Enabled || !view.PasswordConfigured || view.PortEffective != 465 || view.Encryption != "ssl" {
		t.Fatalf("写入后的视图 = %+v", view)
	}
	if view.PasswordMasked != settings.MaskSecret(testSMTPPassword) {
		t.Fatalf("口令掩码 = %q，期望 %q", view.PasswordMasked, settings.MaskSecret(testSMTPPassword))
	}
	if strings.Contains(toJSON(t, view), testSMTPPassword) {
		t.Fatal("响应中不得出现口令明文")
	}

	// 局部更新（只改 from_name）：口令保持不变。
	rec, envelope = putJSON(t, engine, emailSMTPPath, token, map[string]any{"from_name": "新显示名"})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("局部更新失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	view = decodeData[emailSMTPView](t, envelope)
	if view.FromName != "新显示名" || !view.PasswordConfigured || view.Host != testSMTPHost {
		t.Fatalf("局部更新结果 = %+v", view)
	}
	// 库内命令明文保存（与 payment.epay 的 key 同口径：只进库与内存）。
	var stored model.Setting
	if err := gdb.Where("`key` = ?", settings.KeyEmailSMTP).Take(&stored).Error; err != nil {
		t.Fatalf("读库失败: %v", err)
	}
	if !strings.Contains(stored.Value, testSMTPPassword) {
		t.Fatalf("库内应保存口令原文: %s", stored.Value)
	}
	if stored.UpdatedBy == nil || *stored.UpdatedBy == 0 {
		t.Fatal("设置应记录最后修改的管理员")
	}

	// 清空口令必须同时清空用户名（否则被规则拒绝）。
	rec, envelope = putJSON(t, engine, emailSMTPPath, token, map[string]any{"password": ""})
	if envelope.Code != response.CodeValidationFailed {
		t.Fatalf("只清口令应被拒绝: code=%d body=%s", envelope.Code, rec.Body.String())
	}
	rec, envelope = putJSON(t, engine, emailSMTPPath, token,
		map[string]any{"password": "", "username": "", "enabled": false})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("清空口令失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if view = decodeData[emailSMTPView](t, envelope); view.PasswordConfigured {
		t.Fatalf("清空后口令仍配置: %+v", view)
	}
}

// TestEmailSMTPSettingsValidation 覆盖字段校验（40001 格式类 / 40002 规则类）与「至少一个字段」。
func TestEmailSMTPSettingsValidation(t *testing.T) {
	gdb := testDatabase(t)
	engine, _, _ := newTicketNotifyEngine(t, gdb)
	seedAdmin(t, gdb, "smtpvalid", "password123", model.RoleAdmin, model.StatusActive)
	token, _ := loginAdmin(t, engine, "smtpvalid", "password123")

	cases := []struct {
		name string
		body map[string]any
		code int
	}{
		{"空请求体", map[string]any{}, response.CodeInvalidParam},
		{"启用但缺 host", map[string]any{"enabled": true, "from": testSMTPFrom}, response.CodeValidationFailed},
		{"启用但缺 from", map[string]any{"enabled": true, "host": testSMTPHost}, response.CodeValidationFailed},
		{"非法加密方式", map[string]any{"encryption": "tls"}, response.CodeInvalidParam},
		{"端口越界", map[string]any{"port": 70000}, response.CodeInvalidParam},
		{"发件人格式错误", map[string]any{"from": "not-an-email"}, response.CodeInvalidParam},
		{"有用户名无口令", map[string]any{"username": "mailer"}, response.CodeValidationFailed},
	}
	for _, tc := range cases {
		rec, envelope := putJSON(t, engine, emailSMTPPath, token, tc.body)
		if envelope.Code != tc.code {
			t.Errorf("%s: code=%d（HTTP %d），期望 %d（body=%s）",
				tc.name, envelope.Code, rec.Code, tc.code, rec.Body.String())
		}
	}
}

// TestEmailSMTPSettingsPermissions 覆盖角色矩阵：仅 admin（含读取），未登录 401。
func TestEmailSMTPSettingsPermissions(t *testing.T) {
	gdb := testDatabase(t)
	engine, _, _ := newTicketNotifyEngine(t, gdb)
	seedAdmin(t, gdb, "smtpfin", "password123", model.RoleFinance, model.StatusActive)
	seedAdmin(t, gdb, "smtpsup", "password123", model.RoleSupport, model.StatusActive)
	seedAdmin(t, gdb, "smtpadmin2", "password123", model.RoleAdmin, model.StatusActive)

	for _, tc := range []struct{ name, account string }{
		{"finance", "smtpfin"},
		{"support", "smtpsup"},
	} {
		token, _ := loginAdmin(t, engine, tc.account, "password123")
		rec, envelope := doAPI(t, engine, http.MethodGet, emailSMTPPath, token, nil)
		if rec.Code != http.StatusForbidden || envelope.Code != response.CodeForbidden {
			t.Errorf("%s 读取 SMTP 设置 = %d/%d，期望 403", tc.name, rec.Code, envelope.Code)
		}
		rec, envelope = putJSON(t, engine, emailSMTPPath, token, map[string]any{"host": testSMTPHost})
		if rec.Code != http.StatusForbidden || envelope.Code != response.CodeForbidden {
			t.Errorf("%s 写入 SMTP 设置 = %d/%d，期望 403", tc.name, rec.Code, envelope.Code)
		}
		rec, envelope = doAPI(t, engine, http.MethodPost, emailTestPath, token, map[string]any{"to": testAdminEmail})
		if rec.Code != http.StatusForbidden || envelope.Code != response.CodeForbidden {
			t.Errorf("%s 发送测试邮件 = %d/%d，期望 403", tc.name, rec.Code, envelope.Code)
		}
	}

	rec, envelope := doAPI(t, engine, http.MethodGet, emailSMTPPath, "", nil)
	if rec.Code != http.StatusUnauthorized || envelope.Code != response.CodeUnauthorized {
		t.Fatalf("未登录读取 = %d/%d，期望 401", rec.Code, envelope.Code)
	}
}

// TestSendTestEmail 覆盖测试邮件接口：未配置时明确报错、配置后收信与留痕、收件人回退站点邮箱。
func TestSendTestEmail(t *testing.T) {
	gdb := testDatabase(t)
	engine, _, sender := newTicketNotifyEngine(t, gdb)
	seedAdmin(t, gdb, "smtptest", "password123", model.RoleAdmin, model.StatusActive)
	token, _ := loginAdmin(t, engine, "smtptest", "password123")
	seedSiteSetting(t, gdb, testSiteNameForEmail, testAdminEmail)

	// 未配置 SMTP：40002 明确报错，且不写留痕。
	rec, envelope := doAPI(t, engine, http.MethodPost, emailTestPath, token, map[string]any{"to": testAdminEmail})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("未配置时 = %d/%d，期望 40002", rec.Code, envelope.Code)
	}
	if !strings.Contains(envelope.Message, "未配置") {
		t.Fatalf("提示文案 = %q", envelope.Message)
	}
	if sender.count() != 0 || len(emailLogsFromDB(t, gdb)) != 0 {
		t.Fatal("未配置时不应发信或写留痕")
	}

	// 配置 SMTP（走接口，覆盖真实写入路径）。
	rec, envelope = putJSON(t, engine, emailSMTPPath, token, map[string]any{
		"enabled": true, "host": testSMTPHost, "port": 587, "username": testSMTPUser,
		"password": testSMTPPassword, "from": testSMTPFrom, "from_name": "示例云主机",
		"encryption": "starttls",
	})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("配置 SMTP 失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 显式收件人。
	rec, envelope = doAPI(t, engine, http.MethodPost, emailTestPath, token,
		map[string]any{"to": "someone@example.com"})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("发送测试邮件失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	result := decodeData[emailTestView](t, envelope)
	if !result.Sent || result.To != "someone@example.com" {
		t.Fatalf("测试邮件响应 = %+v", result)
	}
	mail := lastEmailTo(t, sender, "someone@example.com")
	if !strings.Contains(mail.Subject, testSiteNameForEmail) || !strings.Contains(mail.Subject, "SMTP 测试邮件") {
		t.Fatalf("测试邮件主题 = %q", mail.Subject)
	}
	if !strings.Contains(mail.Body, "发件服务器") || !strings.Contains(mail.Body, "smtp.example.com:587") {
		t.Fatalf("测试邮件正文 = %q", mail.Body)
	}

	// 省略 to：回退站点 admin_email。
	rec, envelope = doAPI(t, engine, http.MethodPost, emailTestPath, token, map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("缺省收件人发送失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := decodeData[emailTestView](t, envelope); got.To != testAdminEmail {
		t.Fatalf("缺省收件人 = %q，期望站点 admin_email", got.To)
	}

	// 留痕：全部成功。
	logs := emailLogsFromDB(t, gdb)
	if len(logs) != 2 {
		t.Fatalf("邮件留痕 = %d 条，期望 2", len(logs))
	}
	for i := range logs {
		if logs[i].Status != model.EmailStatusSuccess || logs[i].Subject == "" {
			t.Fatalf("留痕异常: %+v", logs[i])
		}
	}

	// 发送失败：50003 + fail 留痕（仍不含口令）。
	sender.setFail(errors.New("SMTP 连接失败（smtp.example.com:587）：dial tcp: i/o timeout"))
	rec, envelope = doAPI(t, engine, http.MethodPost, emailTestPath, token,
		map[string]any{"to": "someone@example.com"})
	if rec.Code != http.StatusInternalServerError || envelope.Code != response.CodeEmailFailed {
		t.Fatalf("发送失败时 = %d/%d，期望 50004", rec.Code, envelope.Code)
	}
	if strings.Contains(envelope.Message, testSMTPPassword) {
		t.Fatal("失败提示不得包含口令")
	}
	logs = emailLogsFromDB(t, gdb)
	if logs[0].Status != model.EmailStatusFail || !strings.Contains(logs[0].ErrorText, "timeout") {
		t.Fatalf("失败留痕异常: %+v", logs[0])
	}

	// 收件人全空（未传 to 且站点未配 admin_email）。
	seedSiteSetting(t, gdb, testSiteNameForEmail, "")
	rec, envelope = doAPI(t, engine, http.MethodPost, emailTestPath, token, map[string]any{})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("无收件人时 = %d/%d，期望 40001", rec.Code, envelope.Code)
	}
}

// TestNotificationSettingsEndpoints 覆盖通知开关接口：缺省值、局部更新、范围校验与角色矩阵。
func TestNotificationSettingsEndpoints(t *testing.T) {
	gdb := testDatabase(t)
	engine, _, _ := newTicketNotifyEngine(t, gdb)
	seedAdmin(t, gdb, "notifcfg", "password123", model.RoleAdmin, model.StatusActive)
	seedAdmin(t, gdb, "notifcfgsup", "password123", model.RoleSupport, model.StatusActive)
	token, _ := loginAdmin(t, engine, "notifcfg", "password123")

	// 未配置：全开 + 提前 7 天。
	rec, envelope := doAPI(t, engine, http.MethodGet, notificationCfgPath, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("读取通知开关失败: HTTP %d", rec.Code)
	}
	view := decodeData[notificationSettingsView](t, envelope)
	if !view.InAppEnabled || !view.EmailEnabled || !view.ExpiryReminderEnabled || view.ExpiryReminderDays != 7 {
		t.Fatalf("缺省通知开关 = %+v", view)
	}

	// 局部更新：只改天数与站内开关。
	rec, envelope = putJSON(t, engine, notificationCfgPath, token,
		map[string]any{"expiry_reminder_days": 14, "inapp_enabled": false})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("更新通知开关失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	view = decodeData[notificationSettingsView](t, envelope)
	if view.ExpiryReminderDays != 14 || view.InAppEnabled || !view.EmailEnabled {
		t.Fatalf("更新后 = %+v", view)
	}

	// 范围校验。
	for _, days := range []int{0, 31, -1} {
		rec, envelope = putJSON(t, engine, notificationCfgPath, token, map[string]any{"expiry_reminder_days": days})
		if envelope.Code != response.CodeInvalidParam {
			t.Errorf("days=%d code=%d（HTTP %d），期望 40001", days, envelope.Code, rec.Code)
		}
	}
	// 空请求体。
	if _, envelope = putJSON(t, engine, notificationCfgPath, token, map[string]any{}); envelope.Code != response.CodeInvalidParam {
		t.Errorf("空请求体 code=%d，期望 40001", envelope.Code)
	}

	// 角色矩阵：support 403。
	supportToken, _ := loginAdmin(t, engine, "notifcfgsup", "password123")
	rec, envelope = doAPI(t, engine, http.MethodGet, notificationCfgPath, supportToken, nil)
	if rec.Code != http.StatusForbidden || envelope.Code != response.CodeForbidden {
		t.Fatalf("support 读取通知开关 = %d/%d，期望 403", rec.Code, envelope.Code)
	}
}
