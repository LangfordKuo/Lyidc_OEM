package model

import "time"

// 实例状态取值（instances.status）。
//
// 阶段 5a 只产生 active（交付成功）；suspended / cancelled / terminated
// 由阶段 5b（暂停、终止申请、到期处理）维护。
const (
	// InstanceStatusActive 正常（已交付）。
	InstanceStatusActive = "active"
	// InstanceStatusSuspended 已暂停（阶段 5b 预留）。
	InstanceStatusSuspended = "suspended"
	// InstanceStatusCancelled 已申请终止（阶段 5b 预留）。
	InstanceStatusCancelled = "cancelled"
	// InstanceStatusTerminated 已终止（阶段 5b 预留）。
	InstanceStatusTerminated = "terminated"
)

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
