package router

import (
	"errors"
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/updates"
)

// updatesHandler 处理系统版本与更新检查接口（R13-A，契约 19.3）：仅 admin 角色（契约 19.6）。
//
// 成功与 GitHub 侧失败都返回 HTTP 200：失败原因在 data.status / data.message 中，
// 前端据此展示失败态与重试按钮；HTTP 错误码只用于鉴权与本地库故障。
type updatesHandler struct {
	service *updates.Service
	logger  *slog.Logger
}

// getCachedUpdate 处理 GET /api/v1/admin/updates：只读缓存，绝不发出网请求。
func (h *updatesHandler) getCachedUpdate(c *gin.Context) {
	view, err := h.service.Cached(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, view)
}

// checkUpdate 处理 GET /api/v1/admin/updates/check：强制访问 GitHub，成功或失败都写入缓存。
func (h *updatesHandler) checkUpdate(c *gin.Context) {
	view, err := h.service.Check(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, view)
}

// fail 处理本地读写失败：库内设置值损坏按内部错误上报，其余按数据库错误（与后台设置接口一致）。
func (h *updatesHandler) fail(c *gin.Context, err error) {
	if errors.Is(err, settings.ErrCorrupt) {
		failInternal(c, h.logger, err)
		return
	}
	failDB(c, h.logger, err)
}
