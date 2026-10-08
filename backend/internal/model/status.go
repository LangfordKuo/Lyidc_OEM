package model

// 账号状态取值（members.status / admins.status）。
const (
	// StatusActive 表示账号正常可用。
	StatusActive = "active"
	// StatusDisabled 表示账号已被管理员禁用：登录与携带 token 的请求都会被拒绝（code=403）。
	StatusDisabled = "disabled"
)

// 管理员角色取值（admins.role）。
const (
	// RoleAdmin 超级管理员：拥有全部管理权限。
	RoleAdmin = "admin"
	// RoleFinance 财务：可查看会员列表与修改会员状态。
	RoleFinance = "finance"
	// RoleSupport 客服：只能查看，不能调用改状态类接口（403）。
	RoleSupport = "support"
)

// IsValidAdminRole 判断角色是否在 admins.role 枚举内。
func IsValidAdminRole(role string) bool {
	switch role {
	case RoleAdmin, RoleFinance, RoleSupport:
		return true
	default:
		return false
	}
}
