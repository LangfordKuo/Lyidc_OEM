package router

import (
	"encoding/json"
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
//
// R19 批 2：读取结果入缓存（键 settings:site）——站点地址与身份无关（它本就是发给上游的
// 回推地址），改地址时主动失效。注意**只缓存与请求无关的取值与审计字段**：
// `url_recommended` 按当前请求 Host 现算，不进缓存。
func (h *settingsHandler) getSiteSettings(c *gin.Context) {
	ctx := c.Request.Context()
	if h.cache != nil {
		h.cache.configure(ctx)
		if raw, ok := h.cache.get(ctx, cacheKeySiteSettings); ok {
			var entry siteSettingsEntry
			if err := json.Unmarshal(raw, &entry); err == nil {
				response.Success(c, entry.viewOf(c))
				return
			}
			// 缓存体损坏：按未命中处理，继续回源（绝不按读不出的内容作答）。
		}
	}

	state, err := h.reader.Site(ctx)
	if err != nil {
		h.failSettings(c, err)
		return
	}
	if h.cache != nil {
		if payload, err := json.Marshal(siteSettingsEntryOf(state)); err == nil {
			h.cache.set(ctx, cacheKeySiteSettings, payload, cacheTTLSiteSettings)
		}
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
	if merged.URL == "" {
		setOplogSummary(c, "已保存站点对外地址设置（清空，交付时按请求推断）")
	} else {
		setOplogSummary(c, "已保存站点对外地址设置（"+merged.URL+"）")
	}
	// R19 批 2：站点设置的读取缓存立即失效（**尽力而为**，失败只记日志、TTL 兜底）。
	h.cache.invalidate(ctx, cachePrefixSiteSettings)
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
