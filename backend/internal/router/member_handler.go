package router

import (
	"errors"
	"fmt"
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

// memberHandler 处理会员端认证与账号接口。
type memberHandler struct {
	store  *store.Store
	tokens *auth.TokenManager
	logger *slog.Logger
}

// registerRequest 是 POST /auth/register 请求体。
type registerRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
	// InviteCode 是邀请码（可选，契约 18.5）：nil 或缺省表示不走邀请注册；
	// 提供了值就必须解析成功（非法/不存在/自邀一律 40002，不静默忽略）。
	InviteCode *string `json:"invite_code"`
}

// loginRequest 是 POST /auth/login 请求体。
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// updateProfileRequest 是 PUT /members/me 请求体，字段可缺省（nil 表示不修改）。
type updateProfileRequest struct {
	Nickname *string `json:"nickname"`
	Phone    *string `json:"phone"`
	Email    *string `json:"email"`
}

// changePasswordRequest 是 POST /members/me/password 请求体。
type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// register 处理 POST /api/v1/auth/register：注册会员并返回会员信息。
//
// 邀请码（契约 18.5）：提供 invite_code 时按 members.invite_code 精确匹配解析邀请人，
// 非法/不存在/自邀一律 40002 明确报错（不静默忽略）；解析成功则写入 inviter_id。
// **首绑不改**：注册时绑定的邀请人永久生效，后续任何路径都不改绑。
func (h *memberHandler) register(c *gin.Context) {
	var req registerRequest
	if !bindJSON(c, &req) {
		return
	}

	username := strings.TrimSpace(req.Username)
	email := strings.TrimSpace(req.Email)
	// R18（契约 23.3）：失败路径也把**尝试的用户名**带进操作日志（成功路径稍后覆盖为账号本身）；
	// 密码绝不进日志（请求体整体不落库）。
	setOplogActor(c, 0, username)
	if err := validateUsername(username); err != nil {
		response.Fail(c, response.CodeInvalidParam, err.Error())
		return
	}
	if err := validateEmail(email); err != nil {
		response.Fail(c, response.CodeInvalidParam, err.Error())
		return
	}
	if err := auth.ValidatePasswordLength(req.Password); err != nil {
		response.Fail(c, response.CodeInvalidParam, err.Error())
		return
	}

	ctx := c.Request.Context()
	var inviterID uint64
	if req.InviteCode != nil {
		code := strings.TrimSpace(*req.InviteCode)
		if err := validateInviteCodeShape(code); err != nil {
			failRuleError(c, err)
			return
		}
		inviter, err := h.store.MemberByInviteCode(ctx, code)
		switch {
		case errors.Is(err, store.ErrNotFound):
			response.Fail(c, response.CodeValidationFailed, "邀请码不存在或已失效")
			return
		case err != nil:
			failDB(c, h.logger, err)
			return
		}
		if referralSelfInvite(inviter, username, email) {
			failRuleError(c, fmt.Errorf("%w: 不能使用自己的邀请码", errReferralRule))
			return
		}
		inviterID = inviter.ID
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		failInternal(c, h.logger, err)
		return
	}

	member := &model.Member{
		Username:      username,
		Email:         email,
		PasswordHash:  hash,
		Nickname:      username,
		Status:        model.StatusActive,
		Balance:       model.ZeroMoney,
		InviterID:     inviterID,
		RebateBalance: model.ZeroMoney,
	}
	if err := h.store.CreateMember(ctx, member); err != nil {
		switch {
		case errors.Is(err, store.ErrUsernameTaken):
			response.Fail(c, response.CodeConflict, "用户名已被占用")
		case errors.Is(err, store.ErrEmailTaken):
			response.Fail(c, response.CodeConflict, "邮箱已被占用")
		case errors.Is(err, store.ErrDuplicate):
			response.Fail(c, response.CodeConflict, response.Message(response.CodeConflict))
		default:
			failDB(c, h.logger, err)
		}
		return
	}

	// R18：成功留痕（操作人 = 新注册的账号本身）。
	setOplogActor(c, member.ID, member.Username)
	summary := "注册成功"
	if inviterID != 0 {
		summary += "（邀请注册，邀请人 ID " + strconv.FormatUint(inviterID, 10) + "）"
	}
	setOplogSummary(c, summary)
	response.Success(c, newMemberView(member))
}

// login 处理 POST /api/v1/auth/login：校验密码并签发会员 token。
//
// R18（契约 23.3）：登录成功与失败都进操作日志（会员端写操作）；失败只记**尝试的用户名**。
func (h *memberHandler) login(c *gin.Context) {
	var req loginRequest
	if !bindJSON(c, &req) {
		return
	}

	username := strings.TrimSpace(req.Username)
	setOplogActor(c, 0, username)
	if username == "" || req.Password == "" {
		response.Fail(c, response.CodeInvalidParam, "用户名和密码不能为空")
		return
	}

	ctx := c.Request.Context()
	member, err := h.store.MemberByUsername(ctx, username)
	switch {
	case errors.Is(err, store.ErrNotFound):
		setOplogSummary(c, "用户名或密码错误")
		response.Fail(c, response.CodeUnauthorized, "用户名或密码错误")
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	if !auth.VerifyPassword(member.PasswordHash, req.Password) {
		setOplogSummary(c, "用户名或密码错误")
		response.Fail(c, response.CodeUnauthorized, "用户名或密码错误")
		return
	}
	if member.Status != model.StatusActive {
		setOplogActor(c, member.ID, member.Username)
		setOplogSummary(c, "账号已被禁用")
		response.Fail(c, response.CodeForbidden, msgAccountDisabled)
		return
	}

	now := time.Now().UTC()
	if err := h.store.TouchMemberLogin(ctx, member.ID, now); err != nil {
		h.logger.Warn("更新会员最后登录时间失败", "error", err, "member_id", member.ID)
	}
	member.LastLoginAt = &now

	token, expiresAt, err := h.tokens.IssueMemberToken(member, now)
	if err != nil {
		failInternal(c, h.logger, err)
		return
	}

	// R18：成功留痕（操作人 = 该会员账号本身）。
	setOplogActor(c, member.ID, member.Username)
	setOplogSummary(c, "登录成功")
	response.Success(c, memberLoginView{
		Token:     token,
		ExpiresAt: formatTime(expiresAt),
		Member:    newMemberView(member),
	})
}

// me 处理 GET /api/v1/members/me：返回当前会员信息（中间件已完成鉴权）。
func (h *memberHandler) me(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	response.Success(c, newMemberView(member))
}

// updateMe 处理 PUT /api/v1/members/me：更新昵称/手机号/邮箱。
func (h *memberHandler) updateMe(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}

	var req updateProfileRequest
	if !bindJSON(c, &req) {
		return
	}

	var upd store.MemberProfileUpdate
	if req.Nickname != nil {
		nickname := strings.TrimSpace(*req.Nickname)
		if err := validateNickname(nickname); err != nil {
			response.Fail(c, response.CodeInvalidParam, err.Error())
			return
		}
		upd.Nickname = &nickname
	}
	if req.Phone != nil {
		phone := strings.TrimSpace(*req.Phone)
		if err := validatePhone(phone); err != nil {
			response.Fail(c, response.CodeInvalidParam, err.Error())
			return
		}
		upd.Phone = &phone
	}
	if req.Email != nil {
		email := strings.TrimSpace(*req.Email)
		if err := validateEmail(email); err != nil {
			response.Fail(c, response.CodeInvalidParam, err.Error())
			return
		}
		upd.Email = &email
	}
	if upd.Nickname == nil && upd.Phone == nil && upd.Email == nil {
		response.Fail(c, response.CodeInvalidParam, "至少提供一个需要修改的字段（nickname/phone/email）")
		return
	}

	ctx := c.Request.Context()
	if upd.Email != nil {
		taken, err := h.store.MemberEmailTaken(ctx, *upd.Email, member.ID)
		if err != nil {
			failDB(c, h.logger, err)
			return
		}
		if taken {
			response.Fail(c, response.CodeConflict, "邮箱已被占用")
			return
		}
	}

	updated, err := h.store.UpdateMemberProfile(ctx, member.ID, upd)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	case errors.Is(err, store.ErrEmailTaken):
		response.Fail(c, response.CodeConflict, "邮箱已被占用")
		return
	case errors.Is(err, store.ErrUsernameTaken), errors.Is(err, store.ErrDuplicate):
		response.Fail(c, response.CodeConflict, response.Message(response.CodeConflict))
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	// R18：只记**字段名**，不记字段值（新邮箱/手机号属个人信息，日志只作留痕不复制资料）。
	setOplogSummary(c, "已更新资料字段："+strings.Join(changedProfileFields(upd), "、"))
	response.Success(c, newMemberView(updated))
}

// changedProfileFields 返回本次资料更新涉及的字段名（供操作日志摘要）。
func changedProfileFields(upd store.MemberProfileUpdate) []string {
	fields := make([]string, 0, 3)
	if upd.Nickname != nil {
		fields = append(fields, "nickname")
	}
	if upd.Phone != nil {
		fields = append(fields, "phone")
	}
	if upd.Email != nil {
		fields = append(fields, "email")
	}
	return fields
}

// changePassword 处理 POST /api/v1/members/me/password：旧密码校验通过后更新密码。
func (h *memberHandler) changePassword(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}

	var req changePasswordRequest
	if !bindJSON(c, &req) {
		return
	}
	if err := auth.ValidatePasswordLength(req.NewPassword); err != nil {
		response.Fail(c, response.CodeInvalidParam, err.Error())
		return
	}
	if !auth.VerifyPassword(member.PasswordHash, req.OldPassword) {
		response.Fail(c, response.CodeUnauthorized, "旧密码不正确")
		return
	}

	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		failInternal(c, h.logger, err)
		return
	}

	ctx := c.Request.Context()
	if err := h.store.UpdateMemberPassword(ctx, member.ID, hash); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
			return
		}
		failDB(c, h.logger, err)
		return
	}

	// R18：只记「改密成功」，新旧密码都不落库。
	setOplogSummary(c, "密码修改成功（旧密码校验通过）")
	response.Success(c, nil)
}
