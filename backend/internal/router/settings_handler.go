package router

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/notify"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// settingsHandler 处理后台管理设置接口（仅 admin 角色，含读取）。
//
// 密钥口径（契约 12.1）：key / api_key / SMTP 口令写入后**绝不回显明文**，GET 只给
// `*_configured` 布尔与掩码（前 4 位 + `****`）；日志同样不含明文。
type settingsHandler struct {
	store  *store.Store
	reader *settings.Reader
	// notify 是通知能力（阶段 6b）：设置页的「发送测试邮件」经它发送并留痕。
	notify NotificationTrigger
	logger *slog.Logger
}

// epaySettingsView 是 payment.epay 设置的对外视图。
type epaySettingsView struct {
	Enabled       bool   `json:"enabled"`
	Gateway       string `json:"gateway"`
	PID           string `json:"pid"`
	KeyConfigured bool   `json:"key_configured"`
	KeyMasked     string `json:"key_masked"`
	NotifyURL     string `json:"notify_url"`
	// NotifyURLRecommended 是按当前请求 Host 推导的推荐回调地址（只做提示，不校验可达性）。
	NotifyURLRecommended string  `json:"notify_url_recommended"`
	ReturnURL            string  `json:"return_url"`
	UpdatedBy            *uint64 `json:"updated_by"`
	UpdatedAt            *string `json:"updated_at"`
}

// upstreamSettingsView 是 upstream 设置的对外视图。
type upstreamSettingsView struct {
	BaseURL          string  `json:"base_url"`
	Username         string  `json:"username"`
	APIKeyConfigured bool    `json:"api_key_configured"`
	APIKeyMasked     string  `json:"api_key_masked"`
	TimeoutSeconds   int     `json:"timeout_seconds"`
	UpdatedBy        *uint64 `json:"updated_by"`
	UpdatedAt        *string `json:"updated_at"`
}

// epaySettingsUpdateRequest 是 PUT /admin/settings/payment/epay 请求体：字段均可选但至少一个。
// key 三态：省略（键缺席或 null）=保持不变、给新值=替换、空串=清空。
type epaySettingsUpdateRequest struct {
	Enabled   *bool   `json:"enabled"`
	Gateway   *string `json:"gateway"`
	PID       *string `json:"pid"`
	Key       *string `json:"key"`
	NotifyURL *string `json:"notify_url"`
	ReturnURL *string `json:"return_url"`
}

// provided 判断请求是否至少提供了一个字段。
func (r epaySettingsUpdateRequest) provided() bool {
	return r.Enabled != nil || r.Gateway != nil || r.PID != nil || r.Key != nil ||
		r.NotifyURL != nil || r.ReturnURL != nil
}

// upstreamSettingsUpdateRequest 是 PUT /admin/settings/upstream 请求体：字段均可选但至少一个。
// api_key 三态语义同支付 key。
type upstreamSettingsUpdateRequest struct {
	BaseURL        *string `json:"base_url"`
	Username       *string `json:"username"`
	APIKey         *string `json:"api_key"`
	TimeoutSeconds *int    `json:"timeout_seconds"`
}

// provided 判断请求是否至少提供了一个字段。
func (r upstreamSettingsUpdateRequest) provided() bool {
	return r.BaseURL != nil || r.Username != nil || r.APIKey != nil || r.TimeoutSeconds != nil
}

// getEpaySettings 处理 GET /api/v1/admin/settings/payment/epay。
func (h *settingsHandler) getEpaySettings(c *gin.Context) {
	state, err := h.reader.Epay(c.Request.Context())
	if err != nil {
		h.failSettings(c, err)
		return
	}
	response.Success(c, epaySettingsViewOf(c, state.Value, state.Meta))
}

// updateEpaySettings 处理 PUT /api/v1/admin/settings/payment/epay：局部更新并立即生效。
func (h *settingsHandler) updateEpaySettings(c *gin.Context) {
	admin, ok := adminFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}

	var req epaySettingsUpdateRequest
	if !bindJSON(c, &req) {
		return
	}
	if !req.provided() {
		response.Fail(c, response.CodeInvalidParam,
			"至少提供一个字段：enabled / gateway / pid / key / notify_url / return_url")
		return
	}

	ctx := c.Request.Context()
	current, err := h.reader.Epay(ctx)
	if err != nil {
		h.failSettings(c, err)
		return
	}
	merged, err := current.Value.Apply(settings.EpayUpdate{
		Enabled:   req.Enabled,
		Gateway:   req.Gateway,
		PID:       req.PID,
		Key:       req.Key,
		NotifyURL: req.NotifyURL,
		ReturnURL: req.ReturnURL,
	})
	if err != nil {
		failRuleError(c, err)
		return
	}

	encoded, err := merged.Encode()
	if err != nil {
		failInternal(c, h.logger, err)
		return
	}
	setting, err := h.store.UpsertSetting(ctx, settings.KeyPaymentEpay, encoded, admin.ID)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	h.logger.Info("后台设置已更新", "key", settings.KeyPaymentEpay, "admin_id", admin.ID,
		"enabled", merged.Enabled, "key_changed", req.Key != nil)
	response.Success(c, epaySettingsViewOf(c, merged, settingMeta(setting)))
}

// getUpstreamSettings 处理 GET /api/v1/admin/settings/upstream。
func (h *settingsHandler) getUpstreamSettings(c *gin.Context) {
	state, err := h.reader.Upstream(c.Request.Context())
	if err != nil {
		h.failSettings(c, err)
		return
	}
	response.Success(c, upstreamSettingsViewOf(state.Value, state.Meta))
}

// updateUpstreamSettings 处理 PUT /api/v1/admin/settings/upstream：局部更新并立即生效
// （上游客户端会在下一次调用时发现设置指纹变化，重建客户端并丢弃旧 JWT 缓存）。
func (h *settingsHandler) updateUpstreamSettings(c *gin.Context) {
	admin, ok := adminFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}

	var req upstreamSettingsUpdateRequest
	if !bindJSON(c, &req) {
		return
	}
	if !req.provided() {
		response.Fail(c, response.CodeInvalidParam,
			"至少提供一个字段：base_url / username / api_key / timeout_seconds")
		return
	}

	ctx := c.Request.Context()
	current, err := h.reader.Upstream(ctx)
	if err != nil {
		h.failSettings(c, err)
		return
	}
	merged, err := current.Value.Apply(settings.UpstreamUpdate{
		BaseURL:        req.BaseURL,
		Username:       req.Username,
		APIKey:         req.APIKey,
		TimeoutSeconds: req.TimeoutSeconds,
	})
	if err != nil {
		failRuleError(c, err)
		return
	}

	encoded, err := merged.Encode()
	if err != nil {
		failInternal(c, h.logger, err)
		return
	}
	setting, err := h.store.UpsertSetting(ctx, settings.KeyUpstream, encoded, admin.ID)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	h.logger.Info("后台设置已更新", "key", settings.KeyUpstream, "admin_id", admin.ID,
		"base_url", merged.BaseURL, "api_key_changed", req.APIKey != nil)
	response.Success(c, upstreamSettingsViewOf(merged, settingMeta(setting)))
}

// emailSMTPView 是 email.smtp 设置的对外视图（口令只输出配置标志与掩码）。
type emailSMTPView struct {
	Enabled            bool    `json:"enabled"`
	Host               string  `json:"host"`
	Port               int     `json:"port"`
	PortEffective      int     `json:"port_effective"`
	Username           string  `json:"username"`
	PasswordConfigured bool    `json:"password_configured"`
	PasswordMasked     string  `json:"password_masked"`
	From               string  `json:"from"`
	FromName           string  `json:"from_name"`
	Encryption         string  `json:"encryption"`
	UpdatedBy          *uint64 `json:"updated_by"`
	UpdatedAt          *string `json:"updated_at"`
}

// emailSMTPUpdateRequest 是 PUT /admin/settings/email/smtp 请求体：字段均可选但至少一个。
// password 三态：省略（键缺席或 null）=保持不变、给新值=替换、空串=清空。
type emailSMTPUpdateRequest struct {
	Enabled    *bool   `json:"enabled"`
	Host       *string `json:"host"`
	Port       *int    `json:"port"`
	Username   *string `json:"username"`
	Password   *string `json:"password"`
	From       *string `json:"from"`
	FromName   *string `json:"from_name"`
	Encryption *string `json:"encryption"`
}

// provided 判断请求是否至少提供了一个字段。
func (r emailSMTPUpdateRequest) provided() bool {
	return r.Enabled != nil || r.Host != nil || r.Port != nil || r.Username != nil ||
		r.Password != nil || r.From != nil || r.FromName != nil || r.Encryption != nil
}

// emailTestRequest 是 POST /admin/settings/email/test 请求体。
type emailTestRequest struct {
	// To 收件地址；缺省时用 settings.site.admin_email（两者都空则 40001）。
	To string `json:"to"`
}

// notificationSettingsView 是 notifications 设置的对外视图。
type notificationSettingsView struct {
	InAppEnabled          bool    `json:"inapp_enabled"`
	EmailEnabled          bool    `json:"email_enabled"`
	ExpiryReminderEnabled bool    `json:"expiry_reminder_enabled"`
	ExpiryReminderDays    int     `json:"expiry_reminder_days"`
	UpdatedBy             *uint64 `json:"updated_by"`
	UpdatedAt             *string `json:"updated_at"`
}

// notificationSettingsUpdateRequest 是 PUT /admin/settings/notifications 请求体：字段均可选但至少一个。
type notificationSettingsUpdateRequest struct {
	InAppEnabled          *bool `json:"inapp_enabled"`
	EmailEnabled          *bool `json:"email_enabled"`
	ExpiryReminderEnabled *bool `json:"expiry_reminder_enabled"`
	ExpiryReminderDays    *int  `json:"expiry_reminder_days"`
}

// provided 判断请求是否至少提供了一个字段。
func (r notificationSettingsUpdateRequest) provided() bool {
	return r.InAppEnabled != nil || r.EmailEnabled != nil ||
		r.ExpiryReminderEnabled != nil || r.ExpiryReminderDays != nil
}

// getEmailSMTPSettings 处理 GET /api/v1/admin/settings/email/smtp。
func (h *settingsHandler) getEmailSMTPSettings(c *gin.Context) {
	state, err := h.reader.SMTP(c.Request.Context())
	if err != nil {
		h.failSettings(c, err)
		return
	}
	response.Success(c, emailSMTPViewOf(state.Value, state.Meta))
}

// updateEmailSMTPSettings 处理 PUT /api/v1/admin/settings/email/smtp：局部更新并立即生效
// （通知链路的发送器按设置指纹重建，改完下一次发信即用新参数）。
func (h *settingsHandler) updateEmailSMTPSettings(c *gin.Context) {
	admin, ok := adminFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}

	var req emailSMTPUpdateRequest
	if !bindJSON(c, &req) {
		return
	}
	if !req.provided() {
		response.Fail(c, response.CodeInvalidParam,
			"至少提供一个字段：enabled / host / port / username / password / from / from_name / encryption")
		return
	}

	ctx := c.Request.Context()
	current, err := h.reader.SMTP(ctx)
	if err != nil {
		h.failSettings(c, err)
		return
	}
	merged, err := current.Value.Apply(settings.SMTPUpdate{
		Enabled:    req.Enabled,
		Host:       req.Host,
		Port:       req.Port,
		Username:   req.Username,
		Password:   req.Password,
		From:       req.From,
		FromName:   req.FromName,
		Encryption: req.Encryption,
	})
	if err != nil {
		failRuleError(c, err)
		return
	}

	encoded, err := merged.Encode()
	if err != nil {
		failInternal(c, h.logger, err)
		return
	}
	setting, err := h.store.UpsertSetting(ctx, settings.KeyEmailSMTP, encoded, admin.ID)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	h.logger.Info("后台设置已更新", "key", settings.KeyEmailSMTP, "admin_id", admin.ID,
		"enabled", merged.Enabled, "endpoint", merged.Endpoint(),
		"encryption", merged.NormalizedEncryption(), "password_changed", req.Password != nil)
	response.Success(c, emailSMTPViewOf(merged, settingMeta(setting)))
}

// sendTestEmail 处理 POST /api/v1/admin/settings/email/test：按当前 SMTP 设置发送一封测试邮件。
//
// 同步发送（管理员等待结果）：成功返回 sent=true 与收件人；SMTP 未配置 → 40002 明确报错；
// 发送失败 → 50003（上游/外部服务调用失败）+ 已脱敏的原因（口令绝不出现）。
func (h *settingsHandler) sendTestEmail(c *gin.Context) {
	admin, ok := adminFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}

	var req emailTestRequest
	if !bindJSON(c, &req) {
		return
	}
	ctx := c.Request.Context()
	to := strings.TrimSpace(req.To)
	if to == "" {
		// 未显式指定收件人时回退站点管理员邮箱（安装向导时期望可空）。
		site, err := h.reader.Site(ctx)
		if err != nil {
			h.failSettings(c, err)
			return
		}
		to = strings.TrimSpace(site.Value.AdminEmail)
	}
	if to == "" {
		response.Fail(c, response.CodeInvalidParam, "请提供 to，或先在站点设置中填写 admin_email")
		return
	}

	err := h.notify.SendTestEmail(ctx, to)
	switch {
	case errors.Is(err, notify.ErrSMTPNotConfigured):
		response.Fail(c, response.CodeValidationFailed, err.Error())
		return
	case err != nil:
		h.logger.Warn("测试邮件发送失败", "error", err, "admin_id", admin.ID, "to", to)
		response.Fail(c, response.CodeEmailFailed, "测试邮件发送失败："+err.Error())
		return
	}
	h.logger.Info("测试邮件已发送", "admin_id", admin.ID, "to", to)
	response.Success(c, emailTestView{Sent: true, To: to})
}

// emailTestView 是测试邮件响应。
type emailTestView struct {
	Sent bool   `json:"sent"`
	To   string `json:"to"`
}

// getNotificationSettings 处理 GET /api/v1/admin/settings/notifications。
func (h *settingsHandler) getNotificationSettings(c *gin.Context) {
	state, err := h.reader.Notifications(c.Request.Context())
	if err != nil {
		h.failSettings(c, err)
		return
	}
	response.Success(c, notificationSettingsViewOf(state.Value, state.Meta))
}

// updateNotificationSettings 处理 PUT /api/v1/admin/settings/notifications：局部更新并立即生效。
func (h *settingsHandler) updateNotificationSettings(c *gin.Context) {
	admin, ok := adminFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}

	var req notificationSettingsUpdateRequest
	if !bindJSON(c, &req) {
		return
	}
	if !req.provided() {
		response.Fail(c, response.CodeInvalidParam,
			"至少提供一个字段：inapp_enabled / email_enabled / expiry_reminder_enabled / expiry_reminder_days")
		return
	}

	ctx := c.Request.Context()
	current, err := h.reader.Notifications(ctx)
	if err != nil {
		h.failSettings(c, err)
		return
	}
	merged, err := current.Value.Apply(settings.NotificationUpdate{
		InAppEnabled:          req.InAppEnabled,
		EmailEnabled:          req.EmailEnabled,
		ExpiryReminderEnabled: req.ExpiryReminderEnabled,
		ExpiryReminderDays:    req.ExpiryReminderDays,
	})
	if err != nil {
		failRuleError(c, err)
		return
	}

	encoded, err := merged.Encode()
	if err != nil {
		failInternal(c, h.logger, err)
		return
	}
	setting, err := h.store.UpsertSetting(ctx, settings.KeyNotifications, encoded, admin.ID)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	h.logger.Info("后台设置已更新", "key", settings.KeyNotifications, "admin_id", admin.ID,
		"inapp_enabled", merged.InAppEnabled, "email_enabled", merged.EmailEnabled,
		"expiry_reminder_enabled", merged.ExpiryReminderEnabled,
		"expiry_reminder_days", merged.ExpiryReminderDays)
	response.Success(c, notificationSettingsViewOf(merged, settingMeta(setting)))
}

// emailSMTPViewOf 组装 SMTP 设置视图（口令只输出配置标志与掩码）。
func emailSMTPViewOf(value settings.SMTP, meta settings.Meta) emailSMTPView {
	view := emailSMTPView{
		Enabled:            value.Enabled,
		Host:               value.Host,
		Port:               value.Port,
		PortEffective:      value.PortOr(),
		Username:           value.Username,
		PasswordConfigured: value.Password != "",
		PasswordMasked:     settings.MaskSecret(value.Password),
		From:               value.From,
		FromName:           value.FromName,
		Encryption:         value.NormalizedEncryption(),
		UpdatedBy:          meta.UpdatedBy,
	}
	if meta.UpdatedAt != nil {
		formatted := formatTime(*meta.UpdatedAt)
		view.UpdatedAt = &formatted
	}
	return view
}

// notificationSettingsViewOf 组装通知开关视图。
func notificationSettingsViewOf(value settings.NotificationSettings, meta settings.Meta) notificationSettingsView {
	view := notificationSettingsView{
		InAppEnabled:          value.InAppEnabled,
		EmailEnabled:          value.EmailEnabled,
		ExpiryReminderEnabled: value.ExpiryReminderEnabled,
		ExpiryReminderDays:    value.ReminderDaysOr(),
		UpdatedBy:             meta.UpdatedBy,
	}
	if meta.UpdatedAt != nil {
		formatted := formatTime(*meta.UpdatedAt)
		view.UpdatedAt = &formatted
	}
	return view
}

// failSettings 处理设置读取失败：库内值损坏按内部错误上报，其余按数据库错误。
func (h *settingsHandler) failSettings(c *gin.Context, err error) {
	if errors.Is(err, settings.ErrCorrupt) {
		failInternal(c, h.logger, err)
		return
	}
	failDB(c, h.logger, err)
}

// epaySettingsViewOf 组装易支付设置视图（密钥只输出配置标志与掩码）。
func epaySettingsViewOf(c *gin.Context, value settings.Epay, meta settings.Meta) epaySettingsView {
	view := epaySettingsView{
		Enabled:              value.Enabled,
		Gateway:              value.Gateway,
		PID:                  value.PID,
		KeyConfigured:        value.Key != "",
		KeyMasked:            settings.MaskSecret(value.Key),
		NotifyURL:            value.NotifyURL,
		NotifyURLRecommended: recommendedNotifyURL(c),
		ReturnURL:            value.ReturnURL,
		UpdatedBy:            meta.UpdatedBy,
	}
	if meta.UpdatedAt != nil {
		formatted := formatTime(*meta.UpdatedAt)
		view.UpdatedAt = &formatted
	}
	return view
}

// upstreamSettingsViewOf 组装上游设置视图（API 密钥只输出配置标志与掩码）。
func upstreamSettingsViewOf(value settings.Upstream, meta settings.Meta) upstreamSettingsView {
	view := upstreamSettingsView{
		BaseURL:          value.BaseURL,
		Username:         value.Username,
		APIKeyConfigured: value.APIKey != "",
		APIKeyMasked:     settings.MaskSecret(value.APIKey),
		TimeoutSeconds:   value.TimeoutSecondsOr(),
		UpdatedBy:        meta.UpdatedBy,
	}
	if meta.UpdatedAt != nil {
		formatted := formatTime(*meta.UpdatedAt)
		view.UpdatedAt = &formatted
	}
	return view
}

// settingMeta 把落库结果转成审计信息。
func settingMeta(setting *model.Setting) settings.Meta {
	if setting == nil {
		return settings.Meta{}
	}
	updatedAt := setting.UpdatedAt
	return settings.Meta{Exists: true, UpdatedBy: setting.UpdatedBy, UpdatedAt: &updatedAt}
}

// recommendedNotifyURL 按当前请求推导推荐的回调地址（供管理端界面直接复制）。
func recommendedNotifyURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host + "/api/v1/payments/epay/notify"
}
