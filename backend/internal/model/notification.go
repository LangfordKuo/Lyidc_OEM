package model

import "time"

// 通知接收方类型取值（notifications.recipient_type，阶段 6b 契约 17.1）。
const (
	// NotificationRecipientMember 会员（recipient_id 解释为 members.id）。
	NotificationRecipientMember = "member"
	// NotificationRecipientAdmin 管理员/客服（recipient_id 解释为 admins.id）。
	NotificationRecipientAdmin = "admin"
)

// 通知事件取值（notifications.event，契约 17.4 事件接线表的键）。
//
// 同一事件在会员侧与管理端侧的含义由 recipient_type 解释：
//   - ticket_replied 发给会员 = 客服公开回复了你的工单；发给管理员 = 会员回复了工单。
const (
	// NotificationEventOrderDelivered 交付成功（新购开通完成）→ 会员。
	NotificationEventOrderDelivered = "order_delivered"
	// NotificationEventOrderFailed 交付失败（上游开通失败）→ 会员。
	NotificationEventOrderFailed = "order_failed"
	// NotificationEventRenewSucceeded 续费成功 → 会员。
	NotificationEventRenewSucceeded = "renew_succeeded"
	// NotificationEventInstanceSuspended 到期自动暂停 → 会员。
	NotificationEventInstanceSuspended = "instance_suspended"
	// NotificationEventInstanceTerminated 终止收敛（上游已删除，本地转 terminated）→ 会员。
	NotificationEventInstanceTerminated = "instance_terminated"
	// NotificationEventTicketCreated 新工单 → 管理员/客服。
	NotificationEventTicketCreated = "ticket_created"
	// NotificationEventTicketReplied 工单新回复（双向，见上）。
	NotificationEventTicketReplied = "ticket_replied"
	// NotificationEventTicketClosed 工单被客服关闭 → 会员。
	NotificationEventTicketClosed = "ticket_closed"
	// NotificationEventExpiryReminder 到期前提醒 → 会员。
	NotificationEventExpiryReminder = "expiry_reminder"
)

// Notification 是 notifications 表的 GORM 模型（阶段 6b 站内通知，契约 17.1）。
//
// 一行 = 一个接收方的一条通知；ReadAt 为 nil 表示未读。
type Notification struct {
	ID            uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	RecipientType string     `gorm:"column:recipient_type"`
	RecipientID   uint64     `gorm:"column:recipient_id"`
	Event         string     `gorm:"column:event"`
	Title         string     `gorm:"column:title"`
	Content       string     `gorm:"column:content"`
	ReadAt        *time.Time `gorm:"column:read_at"`
	CreatedAt     time.Time  `gorm:"column:created_at"`
}

// TableName 返回通知表名。
func (Notification) TableName() string { return "notifications" }

// 邮件发送结果取值（email_logs.status）。
const (
	// EmailStatusSuccess 发送成功。
	EmailStatusSuccess = "success"
	// EmailStatusFail 发送失败（原因见 error 列，已脱敏）。
	EmailStatusFail = "fail"
)

// EmailLog 是 email_logs 表的 GORM 模型（阶段 6b 邮件留痕，契约 17.3）。
//
// 同步发送：每次调用都记一行（成功与失败都记）；ErrorText 已脱敏，绝不含 SMTP 密码。
type EmailLog struct {
	ID        uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	ToAddr    string    `gorm:"column:to_addr"`
	Subject   string    `gorm:"column:subject"`
	Status    string    `gorm:"column:status"`
	ErrorText string    `gorm:"column:error"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 返回邮件日志表名。
func (EmailLog) TableName() string { return "email_logs" }
