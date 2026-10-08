package router

import (
	"errors"
	"log/slog"
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
func (h *memberHandler) register(c *gin.Context) {
	var req registerRequest
	if !bindJSON(c, &req) {
		return
	}

	username := strings.TrimSpace(req.Username)
	email := strings.TrimSpace(req.Email)
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

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		failInternal(c, h.logger, err)
		return
	}

	member := &model.Member{
		Username:     username,
		Email:        email,
		PasswordHash: hash,
		Nickname:     username,
		Status:       model.StatusActive,
		Balance:      model.ZeroMoney,
	}
	if err := h.store.CreateMember(c.Request.Context(), member); err != nil {
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

	response.Success(c, newMemberView(member))
}

// login 处理 POST /api/v1/auth/login：校验密码并签发会员 token。
func (h *memberHandler) login(c *gin.Context) {
	var req loginRequest
	if !bindJSON(c, &req) {
		return
	}

	username := strings.TrimSpace(req.Username)
	if username == "" || req.Password == "" {
		response.Fail(c, response.CodeInvalidParam, "用户名和密码不能为空")
		return
	}

	ctx := c.Request.Context()
	member, err := h.store.MemberByUsername(ctx, username)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeUnauthorized, "用户名或密码错误")
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	if !auth.VerifyPassword(member.PasswordHash, req.Password) {
		response.Fail(c, response.CodeUnauthorized, "用户名或密码错误")
		return
	}
	if member.Status != model.StatusActive {
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

	response.Success(c, newMemberView(updated))
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

	response.Success(c, nil)
}
