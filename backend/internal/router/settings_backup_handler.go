package router

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/dbbackup"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
)

// 备份目录设置接口（R15，契约 21.10）：`db_backup` 键的读取与修改，**仅 admin**（含读取）。
//
// 与 R14 的关系：R14 只在备份分区**展示**当前目录；这里补上编辑入口。保存时即执行与运行时
// **同一套**环境校验（直接调用 internal/dbbackup 的解析/Web 根/符号链接判定与写入探针），
// 不允许出现「保存成功但一备份就失败」的配置；校验不过**不落库**。
//
// R14 的四条备份接口形状与语义不因本文件改变（21.3 只是加了指向 21.10 的说明）。

// dbBackupSettingsView 是 db_backup 设置的对外视图（契约 21.10）。
type dbBackupSettingsView struct {
	// Dir 是设置项原值：空串表示用缺省目录。
	Dir string `json:"dir"`
	// Default 表示当前用缺省目录（Dir 留空）。
	Default bool `json:"default"`
	// DefaultDir 是缺省目录的绝对路径（与当前是否在用无关，供界面展示「留空 = 用这个」）。
	DefaultDir string `json:"default_dir"`
	// ResolvedDir 是当前配置解析后的绝对路径（Dir 留空时等于 DefaultDir）。
	ResolvedDir string `json:"resolved_dir"`
	// OutsideWebRoot 表示目录不在 Web 根（dist/）内；false 时备份接口拒绝写入。
	OutsideWebRoot bool `json:"outside_web_root"`
	// Exists / Writable 描述目录当前是否存在、写入探针是否通过。
	Exists   bool `json:"exists"`
	Writable bool `json:"writable"`
	// Message 是需要管理员注意的原因（面向管理员、已脱敏）；一切正常时为空串。
	Message   string  `json:"message"`
	UpdatedBy *uint64 `json:"updated_by"`
	UpdatedAt *string `json:"updated_at"`
}

// dbBackupSettingsUpdateRequest 是 PUT /admin/settings/db_backup 请求体（契约 21.10）：
// dir **必须提供**（用指针区分「键缺席」与「空串」）；空串或纯空白 = 恢复缺省目录。
type dbBackupSettingsUpdateRequest struct {
	Dir *string `json:"dir"`
}

// getDBBackupSettings 处理 GET /api/v1/admin/settings/db_backup（契约 21.10）：
// 配置原值 + 解析后的绝对路径 + 目录可用性。**不创建目录、不写库**。
func (h *settingsHandler) getDBBackupSettings(c *gin.Context) {
	if !h.backupReady(c) {
		return
	}
	state, err := h.reader.DBBackup(c.Request.Context())
	if err != nil {
		h.failSettings(c, err)
		return
	}
	response.Success(c, dbBackupSettingsViewOf(state.Value, h.backups.InspectDir(state.Value.Dir), state.Meta))
}

// updateDBBackupSettings 处理 PUT /api/v1/admin/settings/db_backup（契约 21.10）：
// 保存前完成环境校验（解析 → Web 根拒绝 → 按需创建 0750 → 写入探针），校验通过才落库；
// 失败给出可读原因（40002）且**该次保存不落库**。
func (h *settingsHandler) updateDBBackupSettings(c *gin.Context) {
	if !h.backupReady(c) {
		return
	}
	admin, ok := adminFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}

	var req dbBackupSettingsUpdateRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.Dir == nil {
		response.Fail(c, response.CodeInvalidParam, "请提供 dir（留空表示恢复缺省目录）")
		return
	}
	value := settings.DBBackup{Dir: strings.TrimSpace(*req.Dir)}
	// 形状校验（控制字符）先做：40001，且不碰文件系统。
	if err := value.Validate(); err != nil {
		failRuleError(c, err)
		return
	}

	// 环境校验（契约 21.10 第 2 条）：任一步失败都在这里返回，绝不落库。
	report, err := h.backups.PrepareDir(value.Dir)
	if err != nil {
		h.logger.Warn("备份目录设置被拒绝（未落库）",
			"admin_id", admin.ID, "dir", value.Dir, "error", err)
		response.Fail(c, response.CodeValidationFailed, err.Error())
		return
	}

	encoded, err := value.Encode()
	if err != nil {
		failInternal(c, h.logger, err)
		return
	}
	setting, err := h.store.UpsertSetting(c.Request.Context(), settings.KeyDBBackup, encoded, admin.ID)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	h.logger.Info("后台设置已更新", "key", settings.KeyDBBackup, "admin_id", admin.ID,
		"dir", value.Dir, "resolved_dir", report.Dir, "default", report.Default)
	response.Success(c, dbBackupSettingsViewOf(value, report, settingMeta(setting)))
}

// backupReady 判断备份服务是否可用（数据库句柄不可用时构造不出备份服务）：
// 与 21.3 的降级口径一致——接口明确拒绝、不 panic，其余功能不受影响。
func (h *settingsHandler) backupReady(c *gin.Context) bool {
	if h.backups != nil {
		return true
	}
	response.Fail(c, response.CodeInternalError, "数据库备份服务不可用")
	return false
}

// dbBackupSettingsViewOf 组装备份目录设置视图（配置值来自设置项，可用性来自目录报告）。
func dbBackupSettingsViewOf(value settings.DBBackup, report dbbackup.DirReport, meta settings.Meta) dbBackupSettingsView {
	view := dbBackupSettingsView{
		Dir:            value.Dir,
		Default:        report.Default,
		DefaultDir:     report.DefaultDir,
		ResolvedDir:    report.Dir,
		OutsideWebRoot: report.OutsideWebRoot,
		Exists:         report.Exists,
		Writable:       report.Writable,
		Message:        report.Message,
		UpdatedBy:      meta.UpdatedBy,
	}
	if meta.UpdatedAt != nil {
		formatted := formatTime(*meta.UpdatedAt)
		view.UpdatedAt = &formatted
	}
	return view
}
