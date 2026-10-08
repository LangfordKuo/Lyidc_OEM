package router

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/auth"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// gin 上下文键：中间件把通过校验的账号实体放入上下文，供后续 handler 使用。
const (
	contextMemberKey = "lyidc.member"
	contextAdminKey  = "lyidc.admin"
)

// 鉴权失败提示文案（见 docs/api-contract.md 认证与账号章节）。
const (
	msgInvalidCredential = "未携带或携带了无效的凭证"
	msgAccountDisabled   = "账号已被禁用"
	msgRoleForbidden     = "当前角色无权执行该操作"
)

// middleware 提供会员/管理员鉴权中间件。
type middleware struct {
	store  *store.Store
	tokens *auth.TokenManager
}

// requireMember 校验会员 token（aud="member"），并确认会员仍存在且状态为 active。
// 会员不存在 → 401；已禁用 → 403（禁用立即生效，无需等 token 过期）。
func (m *middleware) requireMember() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, ok := bearerToken(c.Request)
		if !ok {
			response.Abort(c, response.CodeUnauthorized, msgInvalidCredential)
			return
		}
		claims, err := m.tokens.VerifyMemberToken(raw)
		if err != nil {
			response.Abort(c, response.CodeUnauthorized, msgInvalidCredential)
			return
		}

		member, err := m.store.MemberByID(c.Request.Context(), claims.MemberID)
		switch {
		case errors.Is(err, store.ErrNotFound):
			response.Abort(c, response.CodeUnauthorized, msgInvalidCredential)
			return
		case err != nil:
			response.Abort(c, response.CodeDatabaseError, response.Message(response.CodeDatabaseError))
			return
		}

		if member.Status != model.StatusActive {
			response.Abort(c, response.CodeForbidden, msgAccountDisabled)
			return
		}

		c.Set(contextMemberKey, member)
		c.Next()
	}
}

// requireAdmin 校验管理员 token（aud="admin"），并确认管理员仍存在且状态为 active。
func (m *middleware) requireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, ok := bearerToken(c.Request)
		if !ok {
			response.Abort(c, response.CodeUnauthorized, msgInvalidCredential)
			return
		}
		claims, err := m.tokens.VerifyAdminToken(raw)
		if err != nil {
			response.Abort(c, response.CodeUnauthorized, msgInvalidCredential)
			return
		}

		admin, err := m.store.AdminByID(c.Request.Context(), claims.AdminID)
		switch {
		case errors.Is(err, store.ErrNotFound):
			response.Abort(c, response.CodeUnauthorized, msgInvalidCredential)
			return
		case err != nil:
			response.Abort(c, response.CodeDatabaseError, response.Message(response.CodeDatabaseError))
			return
		}

		if admin.Status != model.StatusActive {
			response.Abort(c, response.CodeForbidden, msgAccountDisabled)
			return
		}

		c.Set(contextAdminKey, admin)
		c.Next()
	}
}

// requireAdminRole 是角色守卫：必须挂在 requireAdmin 之后，角色不在白名单时返回 403。
func requireAdminRole(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}
	return func(c *gin.Context) {
		admin, ok := adminFromContext(c)
		if !ok {
			response.Abort(c, response.CodeUnauthorized, msgInvalidCredential)
			return
		}
		if _, ok := allowed[admin.Role]; !ok {
			response.Abort(c, response.CodeForbidden, msgRoleForbidden)
			return
		}
		c.Next()
	}
}

// memberFromContext 取出中间件放入的会员实体。
func memberFromContext(c *gin.Context) (*model.Member, bool) {
	value, ok := c.Get(contextMemberKey)
	if !ok {
		return nil, false
	}
	member, ok := value.(*model.Member)
	return member, ok
}

// adminFromContext 取出中间件放入的管理员实体。
func adminFromContext(c *gin.Context) (*model.Admin, bool) {
	value, ok := c.Get(contextAdminKey)
	if !ok {
		return nil, false
	}
	admin, ok := value.(*model.Admin)
	return admin, ok
}

// bearerToken 解析 `Authorization: Bearer <token>`，缺失或格式错误时返回 false。
func bearerToken(r *http.Request) (string, bool) {
	fields := strings.Fields(r.Header.Get("Authorization"))
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return "", false
	}
	if strings.TrimSpace(fields[1]) == "" {
		return "", false
	}
	return fields[1], true
}
