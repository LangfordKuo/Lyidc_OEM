package model

import "time"

// 实例状态取值（instances.status）。
//
// 阶段 5a 只产生 active（交付成功）；阶段 5b 起 suspended 由管理端暂停与
// 到期扫描写入；terminated 由阶段 5c（终止流程）收敛写入。
const (
	// InstanceStatusActive 正常（已交付；可执行电源/重装/改密等操作）。
	InstanceStatusActive = "active"
	// InstanceStatusSuspended 已暂停（管理端手动暂停或到期未续费自动暂停；不可执行会员端操作）。
	InstanceStatusSuspended = "suspended"
	// InstanceStatusCancelled 迁移 0007 的预留枚举值；阶段 5c 定稿**不再写入**——
	// 「取消申请在途」以 cancel_status=pending 标记表达，status 保持 active / suspended 原值
	// （见契约 15.8.1，与本文件 InstanceCancel* 常量配套）。
	InstanceStatusCancelled = "cancelled"
	// InstanceStatusTerminated 已终止（上游主机已删除，本地已收敛；全部操作拒绝）。
	InstanceStatusTerminated = "terminated"
)

// 取消申请状态取值（instances.cancel_status；契约 15.8.1）。
const (
	// InstanceCancelNone 无取消申请（默认）。
	InstanceCancelNone = "none"
	// InstanceCancelPending 申请在途：已提交上游，等待上游处理（到期取消可能持续到账单周期结束）。
	InstanceCancelPending = "pending"
	// InstanceCancelDone 已终止：上游主机已删除（或 domainstatus=Deleted/Terminated），本地已收敛为 terminated。
	InstanceCancelDone = "done"
)

// 取消方式取值（instances.cancel_type；提交上游时映射为上游 /host/cancel 的 type 口径）。
const (
	// CancelTypeImmediate 立即取消 → 上游 Immediate。
	CancelTypeImmediate = "immediate"
	// CancelTypeEndOfBilling 到期取消（等到账单周期结束）→ 上游 Endofbilling。
	CancelTypeEndOfBilling = "end_of_billing"
)

// IsValidCancelType 判断取消方式是否在枚举内。
func IsValidCancelType(cancelType string) bool {
	switch cancelType {
	case CancelTypeImmediate, CancelTypeEndOfBilling:
		return true
	default:
		return false
	}
}

// IsInstanceCancellable 判断实例是否可提交/收敛取消申请（契约 15.8.1）：
// active（正常）与 suspended（已暂停仍允许申请终止——否则被暂停的实例将无法退订）；
// terminated 一律拒绝；已有在途申请（cancel_status=pending）由调用方按幂等分支处理。
func IsInstanceCancellable(status string) bool {
	return status == InstanceStatusActive || status == InstanceStatusSuspended
}

// IsInstanceOperable 判断实例是否处于可执行会员端操作（电源/重装/改密）的状态。
// 定稿矩阵（契约 15.2）：仅 active 可操作；suspended / cancelled / terminated 一律拒绝。
func IsInstanceOperable(status string) bool {
	return status == InstanceStatusActive
}

// IsInstanceRenewable 判断实例是否可发起续费（契约 15.4）：
// active（正常续费）与 suspended（欠费暂停后补缴续费，成功后续费交付会尝试恢复）。
// 阶段 5c 追加：**有在途取消申请（cancel_status=pending）时不可续费**（契约 15.8.4），
// 该判定在 handler 层单独执行（需要 cancel_status，不能只看 status）。
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
	UpstreamStatus string `gorm:"column:upstream_status"`
	DedicatedIP    string `gorm:"column:dedicated_ip"`
	AssignedIPs    string `gorm:"column:assigned_ips"`
	Port           int    `gorm:"column:port"`
	Username       string `gorm:"column:username"`
	Password       string `gorm:"column:password"`
	// 取消申请标记（迁移 0009，契约 15.8）：与 Status 正交——pending 期间 Status 保持原值。
	// default 标签保证 INSERT 时零值走库默认（cancel_status 是 ENUM，显式写空串会被 MySQL 拒绝）。
	CancelRequestID   int        `gorm:"column:cancel_request_id;default:0"`
	CancelType        string     `gorm:"column:cancel_type;default:''"`
	CancelStatus      string     `gorm:"column:cancel_status;default:none"`
	CancelReason      string     `gorm:"column:cancel_reason;default:''"`
	CancelRequestedAt *time.Time `gorm:"column:cancel_requested_at"`
	CreatedAt         time.Time  `gorm:"column:created_at"`
	UpdatedAt         time.Time  `gorm:"column:updated_at"`
}

// TableName 返回实例表名。
func (Instance) TableName() string { return "instances" }
