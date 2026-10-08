package model

import "time"

// 实例状态取值（instances.status）。
//
// 阶段 5a 只产生 active（交付成功）；阶段 5b 起 suspended 由管理端暂停与
// 到期扫描写入；cancelled / terminated 由阶段 5c（终止流程）维护。
const (
	// InstanceStatusActive 正常（已交付；可执行电源/重装/改密等操作）。
	InstanceStatusActive = "active"
	// InstanceStatusSuspended 已暂停（管理端手动暂停或到期未续费自动暂停；不可执行会员端操作）。
	InstanceStatusSuspended = "suspended"
	// InstanceStatusCancelled 已申请终止（阶段 5c 预留）。
	InstanceStatusCancelled = "cancelled"
	// InstanceStatusTerminated 已终止（阶段 5c 预留）。
	InstanceStatusTerminated = "terminated"
)

// IsInstanceOperable 判断实例是否处于可执行会员端操作（电源/重装/改密）的状态。
// 定稿矩阵（契约 15.2）：仅 active 可操作；suspended / cancelled / terminated 一律拒绝。
func IsInstanceOperable(status string) bool {
	return status == InstanceStatusActive
}

// IsInstanceRenewable 判断实例是否可发起续费（契约 15.4）：
// active（正常续费）与 suspended（欠费暂停后补缴续费，成功后续费交付会尝试恢复）。
func IsInstanceRenewable(status string) bool {
	return status == InstanceStatusActive || status == InstanceStatusSuspended
}

// IsValidInstanceStatus 判断实例状态是否在 instances.status 枚举内。
func IsValidInstanceStatus(status string) bool {
	switch status {
	case InstanceStatusActive, InstanceStatusSuspended,
		InstanceStatusCancelled, InstanceStatusTerminated:
		return true
	default:
		return false
	}
}

// Instance 是 instances 表的 GORM 模型（订单交付成功后的上游主机记录，阶段 5a）。
//
// 字段归属（契约见 docs/api-contract.md 第 14 节）：
//   - 订单快照：MemberID / OrderID / ProductID / ProductName / BillingCycle；
//   - 开通参数：HostID（上游主机 ID）、Name（提交上游的 host 主机名）；
//   - 上游同步字段（开通后回读 hostinfo）：NextDueDate / Status 对应的 UpstreamStatus /
//     DedicatedIP / AssignedIPs / Port / Username / Password；
//   - 敏感字段：Username / Password 仅会员本人（详情接口）可见，不写日志、不进管理端列表。
type Instance struct {
	ID           uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	MemberID     uint64     `gorm:"column:member_id"`
	OrderID      uint64     `gorm:"column:order_id"`
	HostID       int        `gorm:"column:host_id"`
	ProductID    uint64     `gorm:"column:product_id"`
	ProductName  string     `gorm:"column:product_name"`
	Name         string     `gorm:"column:name"`
	BillingCycle string     `gorm:"column:billing_cycle"`
	NextDueDate  *time.Time `gorm:"column:next_due_date"`
	Status       string     `gorm:"column:status"`
	// UpstreamStatus 是上游 domainstatus 原文（如 Active），仅作同步展示。
	UpstreamStatus string    `gorm:"column:upstream_status"`
	DedicatedIP    string    `gorm:"column:dedicated_ip"`
	AssignedIPs    string    `gorm:"column:assigned_ips"`
	Port           int       `gorm:"column:port"`
	Username       string    `gorm:"column:username"`
	Password       string    `gorm:"column:password"`
	CreatedAt      time.Time `gorm:"column:created_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at"`
}

// TableName 返回实例表名。
func (Instance) TableName() string { return "instances" }
