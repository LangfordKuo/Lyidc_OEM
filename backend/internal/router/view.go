package router

import (
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

// memberView 是会员对外视图：绝不包含 password_hash。
type memberView struct {
	ID          uint64  `json:"id"`
	Username    string  `json:"username"`
	Email       string  `json:"email"`
	Nickname    string  `json:"nickname"`
	Phone       *string `json:"phone"`
	Status      string  `json:"status"`
	Balance     string  `json:"balance"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	LastLoginAt *string `json:"last_login_at"`
}

// adminView 是管理员对外视图：绝不包含 password_hash。
type adminView struct {
	ID          uint64  `json:"id"`
	Username    string  `json:"username"`
	Nickname    string  `json:"nickname"`
	Role        string  `json:"role"`
	Status      string  `json:"status"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	LastLoginAt *string `json:"last_login_at"`
}

// memberLoginView 是会员登录响应。
type memberLoginView struct {
	Token     string     `json:"token"`
	ExpiresAt string     `json:"expires_at"`
	Member    memberView `json:"member"`
}

// adminLoginView 是管理员登录响应。
type adminLoginView struct {
	Token     string    `json:"token"`
	ExpiresAt string    `json:"expires_at"`
	Admin     adminView `json:"admin"`
}

// memberListView 是会员分页列表响应。
type memberListView struct {
	Items    []memberView `json:"items"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
	Total    int64        `json:"total"`
}

// newMemberView 组装会员视图（时间统一输出 RFC3339 UTC）。
func newMemberView(member *model.Member) memberView {
	return memberView{
		ID:          member.ID,
		Username:    member.Username,
		Email:       member.Email,
		Nickname:    member.Nickname,
		Phone:       member.Phone,
		Status:      member.Status,
		Balance:     string(member.Balance),
		CreatedAt:   formatTime(member.CreatedAt),
		UpdatedAt:   formatTime(member.UpdatedAt),
		LastLoginAt: formatTimePtr(member.LastLoginAt),
	}
}

// newAdminView 组装管理员视图。
func newAdminView(admin *model.Admin) adminView {
	return adminView{
		ID:          admin.ID,
		Username:    admin.Username,
		Nickname:    admin.Nickname,
		Role:        admin.Role,
		Status:      admin.Status,
		CreatedAt:   formatTime(admin.CreatedAt),
		UpdatedAt:   formatTime(admin.UpdatedAt),
		LastLoginAt: formatTimePtr(admin.LastLoginAt),
	}
}

// formatTime 把时间统一格式化为 RFC3339（UTC，如 2026-10-08T06:18:31Z）。
func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// formatTimePtr 处理可空时间字段。
func formatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	formatted := formatTime(*t)
	return &formatted
}
