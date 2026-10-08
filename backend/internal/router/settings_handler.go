package router

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// settingsHandler 处理后台管理设置接口（仅 admin 角色，含读取）。
//
// 密钥口径（契约 12.1）：key / api_key 写入后**绝不回显明文**，GET 只给
// `*_configured` 布尔与掩码（前 4 位 + `****`）；日志同样不含明文。
type settingsHandler struct {
	store  *store.Store
	reader *settings.Reader
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
