package router

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/updates"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upgrade"
)

// updatesHandler 处理系统版本与更新检查接口（R13-A，契约 19.3）与一键升级（R13-B，契约 20）：
// 全部仅 admin 角色（契约 19.6）。
//
// 成功与 GitHub 侧失败都返回 HTTP 200：失败原因在 data.status / data.message 中，
// 前端据此展示失败态与重试按钮；HTTP 错误码只用于鉴权与本地库故障。
type updatesHandler struct {
	service *updates.Service
	// upgrade 是一键升级服务（R13-B，契约 20）；接入失败时为 nil，此时升级接口明确拒绝。
	upgrade *upgrade.Service
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

// applyUpdate 处理 POST /api/v1/admin/updates/apply（R13-B，契约 20.3）：
// 同步完成全部前置检查与二次确认，成功返回 202 + 任务视图，随后主进程退出让位给 helper。
func (h *updatesHandler) applyUpdate(c *gin.Context) {
	if h.upgrade == nil {
		response.Fail(c, response.CodeInternalError, "升级服务不可用（无法定位安装目录）")
		return
	}
	var req upgrade.ApplyRequest
	if !bindJSON(c, &req) {
		return
	}
	view, err := h.upgrade.Apply(c.Request.Context(), req)
	if err != nil {
		var rejected *upgrade.RejectError
		if errors.As(err, &rejected) {
			response.Fail(c, rejectCode(rejected.Kind), rejected.Message)
			return
		}
		// 本地故障（状态文件、工作目录、库）按数据库/内部错误上报。
		failInternal(c, h.logger, err)
		return
	}
	h.logger.Info("一键升级已发起", "job_id", view.ID,
		"from_version", view.FromVersion, "target_version", view.TargetVersion)
	setOplogSummary(c, fmt.Sprintf("已发起一键升级：%s → %s（任务 %s，主进程随后重启）",
		view.FromVersion, view.TargetVersion, view.ID))
	response.JSON(c, http.StatusAccepted, response.New(response.CodeSuccess, response.Message(response.CodeSuccess), view))
}

// getUpdateJob 处理 GET /api/v1/admin/updates/job（R13-B，契约 20.4）：从未升级过时 data 为 null。
func (h *updatesHandler) getUpdateJob(c *gin.Context) {
	if h.upgrade == nil {
		response.Success(c, nil)
		return
	}
	view, err := h.upgrade.Job(c.Request.Context())
	if err != nil {
		failInternal(c, h.logger, err)
		return
	}
	if view == nil {
		response.Success(c, nil)
		return
	}
	response.Success(c, view)
}

// rejectCode 把前置检查的拒绝分类映射为业务错误码（契约 20.3）。
func rejectCode(kind upgrade.RejectKind) int {
	switch kind {
	case upgrade.RejectInvalidParam:
		return response.CodeInvalidParam
	case upgrade.RejectConflict:
		return response.CodeConflict
	default:
		return response.CodeValidationFailed
	}
}

// fail 处理本地读写失败：库内设置值损坏按内部错误上报，其余按数据库错误（与后台设置接口一致）。
func (h *updatesHandler) fail(c *gin.Context, err error) {
	if errors.Is(err, settings.ErrCorrupt) {
		failInternal(c, h.logger, err)
		return
	}
	failDB(c, h.logger, err)
}
