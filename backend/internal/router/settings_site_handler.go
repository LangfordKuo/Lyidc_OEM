package router

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
)

// 「站点对外地址」设置接口（R16，契约 22.4）：`settings.site` 键里的 `url` 一项的读写，**仅 admin**。
//
// 这个地址是**发给上游**的回推地址（`downstream_url`，见 22.2）：上游在主机状态变化时按它回调
// `POST /api/host/sync`。未配置（空串）时交付会按触发请求的 Host 推断，因此本项是「可选但推荐」的。
//
// 边界：`site` 键还承载向导写入的 `name` / `admin_email`，本文件**只读写 url**，
// 合并保存时另外两项原样保留（`settings.Site.Apply`）；接口不涉及任何密钥，无需脱敏。

// siteSettingsView 是「站点对外地址」的对外视图（契约 22.4）。
type siteSettingsView struct {
	// URL 是配置原值；空串表示未配置（交付时按请求 Host 推断）。
	URL string `json:"url"`
	// Configured 表示已配置（等价于 URL != ""），供界面直接展示「未配置」。
	Configured bool `json:"configured"`
	// URLRecommended 是按当前请求推断的地址（`scheme://host`），供界面提示「留空时用这个」；
	// 只做提示，不校验可达性（与支付回调的 notify_url_recommended 同口径）。
	URLRecommended string  `json:"url_recommended"`
	UpdatedBy      *uint64 `json:"updated_by"`
	UpdatedAt      *string `json:"updated_at"`
}

// siteSettingsUpdateRequest 是 PUT /admin/settings/site 请求体（契约 22.4）：
// url **必须提供**（用指针区分「键缺席」与「空串」）；空串 = 清空（回到按请求 Host 推断）。
type siteSettingsUpdateRequest struct {
	URL *string `json:"url"`
}

// getSiteSettings 处理 GET /api/v1/admin/settings/site（契约 22.4）。
func (h *settingsHandler) getSiteSettings(c *gin.Context) {
	state, err := h.reader.Site(c.Request.Context())
	if err != nil {
		h.failSettings(c, err)
		return
	}
	response.Success(c, siteSettingsViewOf(c, state.Value, state.Meta))
}

// updateSiteSettings 处理 PUT /api/v1/admin/settings/site（契约 22.4）：
// 校验通过才落库（地址非法一律 40002 + 可读原因），只改 url、保留 name / admin_email。
func (h *settingsHandler) updateSiteSettings(c *gin.Context) {
	admin, ok := adminFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}

	var req siteSettingsUpdateRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.URL == nil {
		response.Fail(c, response.CodeInvalidParam, "请提供 url（留空表示按请求推断）")
		return
	}

	ctx := c.Request.Context()
	current, err := h.reader.Site(ctx)
	if err != nil {
		h.failSettings(c, err)
		return
	}
	merged := current.Value.Apply(settings.SiteURLUpdate{URL: req.URL})
	if err := merged.ValidateURL(); err != nil {
		// 契约 22.4：地址非法统一 40002（这是要发给上游的业务地址，属规则不成立而非格式解析失败）。
		response.Fail(c, response.CodeValidationFailed,
			fmt.Sprintf("站点对外地址必须是带主机名的 http:// 或 https:// 地址，收到 %q", *req.URL))
		return
	}

	encoded, err := merged.Encode()
	if err != nil {
		failInternal(c, h.logger, err)
		return
	}
	setting, err := h.store.UpsertSetting(ctx, settings.KeySite, encoded, admin.ID)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	h.logger.Info("后台设置已更新", "key", settings.KeySite, "admin_id", admin.ID, "url", merged.URL)
	response.Success(c, siteSettingsViewOf(c, merged, settingMeta(setting)))
}

// siteSettingsViewOf 组装「站点对外地址」视图。
func siteSettingsViewOf(c *gin.Context, value settings.Site, meta settings.Meta) siteSettingsView {
	view := siteSettingsView{
		URL:            value.URL,
		Configured:     value.URL != "",
		URLRecommended: requestBaseURL(c),
		UpdatedBy:      meta.UpdatedBy,
	}
	if meta.UpdatedAt != nil {
		formatted := formatTime(*meta.UpdatedAt)
		view.UpdatedAt = &formatted
	}
	return view
}
