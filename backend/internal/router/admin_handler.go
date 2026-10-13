package router

import (
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/auth"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// 分页与提示文案常量。
const (
	defaultPage      = 1
	defaultPageSize  = 20
	maxPage          = 1000000
	maxPageSize      = 100
	msgMemberMissing = "会员不存在"
	msgStatusInvalid = "status 只能是 active 或 disabled"
)

// adminHandler 处理管理端认证与会员管理接口。
type adminHandler struct {
	store  *store.Store
	tokens *auth.TokenManager
	logger *slog.Logger
}

// adminLoginRequest 是 POST /admin/auth/login 请求体。
type adminLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// updateMemberStatusRequest 是 PUT /admin/members/:id/status 请求体。
type updateMemberStatusRequest struct {
	Status string `json:"status"`
}

// updateMemberRequest 是 PUT /admin/members/:id 请求体（契约 6.3，R10-B）。
// rebate_percent_override 三态：键缺席 = 不改 / null = 清空 / 字符串 = 设值。
type updateMemberRequest struct {
	RebatePercentOverride json.RawMessage `json:"rebate_percent_override"`
}

// provided 判断更新请求是否至少提供了一个字段。
func (r *updateMemberRequest) provided() bool {
	return len(r.RebatePercentOverride) > 0
}

// login 处理 POST /api/v1/admin/auth/login：校验管理员密码并签发管理员 token。
func (h *adminHandler) login(c *gin.Context) {
	var req adminLoginRequest
	if !bindJSON(c, &req) {
		return
	}

	username := strings.TrimSpace(req.Username)
	if username == "" || req.Password == "" {
		response.Fail(c, response.CodeInvalidParam, "用户名和密码不能为空")
		return
	}

	ctx := c.Request.Context()
	admin, err := h.store.AdminByUsername(ctx, username)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeUnauthorized, "用户名或密码错误")
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	if !auth.VerifyPassword(admin.PasswordHash, req.Password) {
		response.Fail(c, response.CodeUnauthorized, "用户名或密码错误")
		return
	}
	if admin.Status != model.StatusActive {
		response.Fail(c, response.CodeForbidden, msgAccountDisabled)
		return
	}

	now := time.Now().UTC()
	if err := h.store.TouchAdminLogin(ctx, admin.ID, now); err != nil {
		h.logger.Warn("更新管理员最后登录时间失败", "error", err, "admin_id", admin.ID)
	}
	admin.LastLoginAt = &now

	token, expiresAt, err := h.tokens.IssueAdminToken(admin, now)
	if err != nil {
		failInternal(c, h.logger, err)
		return
	}

	response.Success(c, adminLoginView{
		Token:     token,
		ExpiresAt: formatTime(expiresAt),
		Admin:     newAdminView(admin),
	})
}

// profile 处理 GET /api/v1/admin/profile：返回当前管理员信息（中间件已完成鉴权）。
func (h *adminHandler) profile(c *gin.Context) {
	admin, ok := adminFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	response.Success(c, newAdminView(admin))
}

// listMembers 处理 GET /api/v1/admin/members：分页 + username/email 模糊 + status 过滤。
func (h *adminHandler) listMembers(c *gin.Context) {
	page, err := intQuery(c, "page", defaultPage, 1, maxPage)
	if err != nil {
		response.Fail(c, response.CodeInvalidParam, err.Error())
		return
	}
	pageSize, err := intQuery(c, "page_size", defaultPageSize, 1, maxPageSize)
	if err != nil {
		response.Fail(c, response.CodeInvalidParam, err.Error())
		return
	}

	status := strings.TrimSpace(c.Query("status"))
	if status != "" && status != model.StatusActive && status != model.StatusDisabled {
		response.Fail(c, response.CodeInvalidParam, msgStatusInvalid)
		return
	}

	filter := store.MemberFilter{
		Page:     page,
		PageSize: pageSize,
		Username: strings.TrimSpace(c.Query("username")),
		Email:    strings.TrimSpace(c.Query("email")),
		Status:   status,
	}

	items, total, err := h.store.ListMembers(c.Request.Context(), filter)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	views := make([]memberView, 0, len(items))
	for i := range items {
		views = append(views, newMemberView(&items[i]))
	}
	response.Success(c, memberListView{
		Items:    views,
		Page:     page,
		PageSize: pageSize,
		Total:    total,
	})
}

// updateMemberStatus 处理 PUT /api/v1/admin/members/:id/status：启用/禁用会员。
// 角色守卫（admin/finance）在路由注册处挂载：support 返回 403。
func (h *adminHandler) updateMemberStatus(c *gin.Context) {
	memberID, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || memberID == 0 {
		response.Fail(c, response.CodeInvalidParam, "会员 ID 必须为正整数")
		return
	}

	var req updateMemberStatusRequest
	if !bindJSON(c, &req) {
		return
	}
	status := strings.TrimSpace(req.Status)
	if status != model.StatusActive && status != model.StatusDisabled {
		response.Fail(c, response.CodeInvalidParam, msgStatusInvalid)
		return
	}

	updated, err := h.store.UpdateMemberStatus(c.Request.Context(), memberID, status)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgMemberMissing)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}
	response.Success(c, newMemberView(updated))
}

// updateMember 处理 PUT /api/v1/admin/members/:id：管理端会员更新（契约 6.3，R10-B）。
// 本批只承载会员级返佣特批比例 rebate_percent_override（三态）。
// 角色守卫（admin/finance）在路由注册处挂载：support 返回 403。
func (h *adminHandler) updateMember(c *gin.Context) {
	memberID, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || memberID == 0 {
		response.Fail(c, response.CodeInvalidParam, "会员 ID 必须为正整数")
		return
	}

	var req updateMemberRequest
	if !bindJSON(c, &req) {
		return
	}
	if !req.provided() {
		response.Fail(c, response.CodeInvalidParam, "至少提供一个字段：rebate_percent_override")
		return
	}

	set, value, err := memberRebateOverrideFromRaw(req.RebatePercentOverride)
	if err != nil {
		failRuleError(c, err)
		return
	}
	if !set {
		// 三态里「缺席」已在 provided() 挡掉；走到这里说明字段在但解析不出语义。
		response.Fail(c, response.CodeInvalidParam,
			"rebate_percent_override 需为百分比字符串或 null")
		return
	}

	ctx := c.Request.Context()
	if _, err := h.store.MemberByID(ctx, memberID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			response.Fail(c, response.CodeNotFound, msgMemberMissing)
			return
		}
		failDB(c, h.logger, err)
		return
	}

	updated, err := h.store.SetMemberRebateOverride(ctx, memberID, value)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgMemberMissing)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	if value == nil {
		h.logger.Info("会员返佣特批比例已清空", "admin_id", mustAdminID(c), "member_id", memberID)
	} else {
		h.logger.Info("会员返佣特批比例已设置",
			"admin_id", mustAdminID(c), "member_id", memberID, "rebate_percent_override", *value)
	}
	response.Success(c, newMemberView(updated))
}

// mustAdminID 取当前管理员 ID（仅用于日志字段；未登录时返回 0）。
func mustAdminID(c *gin.Context) uint64 {
	if admin, ok := adminFromContext(c); ok {
		return admin.ID
	}
	return 0
}
