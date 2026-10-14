package model

import "time"

// 操作日志通道（operation_logs.channel，R18 契约第 23 节）。
const (
	// OplogChannelAdmin 管理员通道：管理端**所有写操作**（含登录成功与失败）。
	OplogChannelAdmin = "admin"
	// OplogChannelMember 会员通道：会员端**所有写操作**（注册/登录/下单/支付/续费/工单/资料等）。
	OplogChannelMember = "member"
	// OplogChannelScheduler 定时任务通道：每轮扫描一条汇总 + 有实际动作时的明细。
	OplogChannelScheduler = "scheduler"
)

// 操作日志结果（operation_logs.status）。
const (
	// OplogStatusSuccess 成功（HTTP 2xx/3xx，或定时任务本轮无基础设施错误）。
	OplogStatusSuccess = "success"
	// OplogStatusFailed 失败（HTTP 4xx/5xx，或定时任务本轮未跑完）。
	OplogStatusFailed = "failed"
)

// IsValidOplogChannel 判断通道取值是否合法（列表筛选与清空接口共用）。
func IsValidOplogChannel(channel string) bool {
	switch channel {
	case OplogChannelAdmin, OplogChannelMember, OplogChannelScheduler:
		return true
	default:
		return false
	}
}

// IsValidOplogStatus 判断结果取值是否合法（列表筛选用）。
func IsValidOplogStatus(status string) bool {
	return status == OplogStatusSuccess || status == OplogStatusFailed
}

// OperationLog 是 operation_logs 表的 GORM 模型（R18，迁移 0019）。
//
// 语义要点（契约 23.1 / 23.2）：三通道各自独立；ActorName 是**快照**（账号改名或删除后日志仍可读）；
// Summary 是**已脱敏**的可读业务摘要（密码、API 密钥、JWT、SMTP 口令绝不落库，请求体整体不落库）；
// 本表只允许整体清空（按通道或全部），**不提供按条删除或修改**。
type OperationLog struct {
	ID         uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	Channel    string    `gorm:"column:channel"`
	ActorID    uint64    `gorm:"column:actor_id"`
	ActorName  string    `gorm:"column:actor_name"`
	Action     string    `gorm:"column:action"`
	Method     string    `gorm:"column:method"`
	Path       string    `gorm:"column:path"`
	HTTPStatus uint16    `gorm:"column:http_status"`
	Status     string    `gorm:"column:status"`
	IP         string    `gorm:"column:ip"`
	DurationMS uint64    `gorm:"column:duration_ms"`
	Summary    string    `gorm:"column:summary"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

// TableName 返回操作日志表名。
func (OperationLog) TableName() string { return "operation_logs" }
