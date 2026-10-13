package router

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/auth"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/dbbackup"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
)

// dbBackupHandler 处理数据库备份与恢复接口（R14，契约 21.3）。
//
// 四条硬约束都落在这里：
//   - **只输出元信息**：没有任何接口返回备份文件内容（不提供下载，契约 21.2）；
//   - **只接受备份 ID**：路径由服务在自己的白名单（备份目录）里解析，`../` 与绝对路径在
//     ID 形状校验阶段就被拒（契约 21.4）；
//   - **全部仅 admin 角色**（finance / support 一律 403，含读取）；
//   - **恢复需要二次确认 + 当前管理员密码**（bcrypt 校验，契约 21.5）。
//
// 日志只记元信息与结果：备份内容含管理员密码哈希，任何日志、错误信息与响应都不含内容。
type dbBackupHandler struct {
	// service 为 nil 表示备份服务未接入（数据库句柄不可用），接口明确拒绝。
	service *dbbackup.Service
	logger  *slog.Logger
}

// restoreBackupRequest 是 POST /api/v1/admin/db-backups/:id/restore 的请求体（契约 21.5）。
type restoreBackupRequest struct {
	// Confirm 必须为 true：恢复是覆盖式且不可撤销的操作。
	Confirm bool `json:"confirm"`
	// Password 是**当前登录管理员**的密码（bcrypt 校验；不用会员密码、不用上游口令）。
	Password string `json:"password"`
}

// listBackups 处理 GET /api/v1/admin/db-backups（契约 21.3）：目录状态 + 元信息列表 + 总占用。
// 绝不读取备份文件内容。
func (h *dbBackupHandler) listBackups(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	view, err := h.service.List(c.Request.Context())
	if err != nil {
		h.fail(c, err, "读取备份列表失败")
		return
	}
	response.Success(c, view)
}

// createBackup 处理 POST /api/v1/admin/db-backups（契约 21.3）：立即备份一次（同步返回元信息）。
// 同一时刻至多一个写操作，重复提交返回 409。
func (h *dbBackupHandler) createBackup(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	admin, ok := adminFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	record, err := h.service.Create(c.Request.Context(), admin.Username)
	if err != nil {
		h.fail(c, err, "备份失败")
		return
	}
	response.Success(c, record)
}

// restoreBackup 处理 POST /api/v1/admin/db-backups/:id/restore（契约 21.5）：
// 覆盖式恢复，需 confirm=true 与当前管理员密码。任一不满足即拒绝并给出明确原因。
func (h *dbBackupHandler) restoreBackup(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	admin, ok := adminFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	var req restoreBackupRequest
	if !bindJSON(c, &req) {
		return
	}
	if !req.Confirm {
		response.Fail(c, response.CodeValidationFailed,
			"恢复会用备份内容覆盖当前数据库且不可撤销，请显式确认后再提交")
		return
	}
	if strings.TrimSpace(req.Password) == "" {
		response.Fail(c, response.CodeValidationFailed, "请输入当前管理员密码以确认恢复")
		return
	}
	if !auth.VerifyPassword(admin.PasswordHash, req.Password) {
		// 密码校验失败与「未确认」同码（40002），文案明确；不回显任何凭据内容。
		h.logger.Warn("数据库恢复被拒绝：管理员密码校验未通过", "admin_id", admin.ID)
		response.Fail(c, response.CodeValidationFailed, "管理员密码不正确，恢复已被拒绝")
		return
	}

	id := c.Param("id")
	result, err := h.service.Restore(c.Request.Context(), id)
	if err != nil {
		h.fail(c, err, "恢复失败")
		return
	}
	h.logger.Info("数据库恢复接口完成",
		"admin_id", admin.ID, "backup_id", result.BackupID,
		"tables", result.TableCount, "dropped_tables", len(result.DroppedTables),
		"from_schema_version", result.FromSchemaVersion, "to_schema_version", result.ToSchemaVersion,
		"migrated", result.Migrated)
	response.Success(c, result)
}

// deleteBackup 处理 DELETE /api/v1/admin/db-backups/:id（契约 21.3）：只按 ID 删，不接受路径。
func (h *dbBackupHandler) deleteBackup(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	record, err := h.service.Delete(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err, "删除备份失败")
		return
	}
	h.logger.Info("数据库备份已删除", "backup_id", record.ID)
	response.Success(c, gin.H{"deleted": true, "id": record.ID, "file_name": record.FileName})
}

// ready 判断备份服务是否可用。
func (h *dbBackupHandler) ready(c *gin.Context) bool {
	if h.service != nil {
		return true
	}
	response.Fail(c, response.CodeInternalError, "数据库备份服务不可用")
	return false
}

// fail 把服务层错误映射为业务错误码（契约 21.3）：
// 不存在 → 404；目录非法 / 备份不兼容 → 40002（message 面向管理员且已脱敏）；
// 并发冲突 → 409；其余（磁盘、SQL 执行失败）→ 50005，message 只带语句种类与表名。
func (h *dbBackupHandler) fail(c *gin.Context, err error, fallback string) {
	switch {
	case errors.Is(err, dbbackup.ErrNotFound):
		response.Fail(c, response.CodeNotFound, "备份不存在或备份文件已删除")
	case errors.Is(err, dbbackup.ErrBusy):
		response.Fail(c, response.CodeConflict, err.Error())
	case errors.Is(err, dbbackup.ErrDirUnusable), errors.Is(err, dbbackup.ErrIncompatible):
		response.Fail(c, response.CodeValidationFailed, err.Error())
	default:
		h.logger.Error("数据库备份操作失败", "error", err, "path", c.Request.URL.Path)
		response.Fail(c, response.CodeBackupFailed, fallback+"："+err.Error())
	}
}
