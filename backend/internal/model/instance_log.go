package model

import "time"

// 实例操作审计的操作者类型（instance_operation_logs.actor_type）。
const (
	// ActorTypeMember 会员本人发起的操作（会员端电源/重装/改密/续费下单）。
	ActorTypeMember = "member"
	// ActorTypeAdmin 管理员发起的操作（暂停/恢复/同步）。
	ActorTypeAdmin = "admin"
	// ActorTypeSystem 系统自动发起的操作（自动开通、自动续费、到期暂停扫描）。
	ActorTypeSystem = "system"
)

// 实例操作审计的动作取值（instance_operation_logs.action）。
const (
	// ActionCreate 开通（交付成功/失败，actor=system）。
	ActionCreate = "create"
	// ActionPowerOn 开机。
	ActionPowerOn = "power_on"
	// ActionPowerOff 关机（软）。
	ActionPowerOff = "power_off"
	// ActionReboot 重启（软）。
	ActionReboot = "reboot"
	// ActionHardOff 强制关机。
	ActionHardOff = "hard_off"
	// ActionHardReboot 强制重启。
	ActionHardReboot = "hard_reboot"
	// ActionReinstall 重装系统。
	ActionReinstall = "reinstall"
	// ActionResetPassword 重置密码。
	ActionResetPassword = "reset_password"
	// ActionSuspend 暂停（管理员手动或到期扫描自动；actor_type 区分来源）。
	ActionSuspend = "suspend"
	// ActionUnsuspend 恢复（管理员手动或续费后自动）。
	ActionUnsuspend = "unsuspend"
	// ActionSync 上游状态同步（管理端）。
	ActionSync = "sync"
	// ActionRenew 续费交付（actor=system）。
	ActionRenew = "renew"
)

// 实例操作审计的结果取值（instance_operation_logs.status）。
const (
	// InstanceOpSuccess 操作成功。
	InstanceOpSuccess = "success"
	// InstanceOpFail 操作失败（失败尝试同样留痕，message 记录脱敏原因）。
	InstanceOpFail = "fail"
)

// InstanceOperationLog 是 instance_operation_logs 表的 GORM 模型（阶段 5b）。
//
// 审计范围（契约 15.x）：会员端操作（电源/重装/改密）、管理端操作（暂停/恢复/同步）、
// 系统自动操作（开通/续费/到期暂停）——无论成功失败都留痕；
// Message 为脱敏结果说明，**不含密码与密钥**；ActorID 在系统操作时为 0。
type InstanceOperationLog struct {
	ID         uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	InstanceID uint64    `gorm:"column:instance_id"`
	ActorType  string    `gorm:"column:actor_type"`
	ActorID    uint64    `gorm:"column:actor_id"`
	Action     string    `gorm:"column:action"`
	Status     string    `gorm:"column:status"`
	Message    string    `gorm:"column:message"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

// TableName 返回实例操作审计表名。
func (InstanceOperationLog) TableName() string { return "instance_operation_logs" }
