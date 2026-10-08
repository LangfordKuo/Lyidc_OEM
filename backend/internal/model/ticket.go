package model

import "time"

// 工单状态取值（tickets.status，阶段 6a 契约 16.2 定稿）。
const (
	// TicketStatusOpen 待客服处理：新单初始态，也是会员回复后的状态。
	TicketStatusOpen = "open"
	// TicketStatusReplied 待会员：管理员**公开**回复后的状态（内部备注不改变状态）。
	TicketStatusReplied = "replied"
	// TicketStatusClosed 已关闭（终态）：不可再回复，会员需新开工单。
	TicketStatusClosed = "closed"
)

// 工单分类取值（tickets.category）。
const (
	// TicketCategoryTechnical 技术。
	TicketCategoryTechnical = "technical"
	// TicketCategoryBilling 财务。
	TicketCategoryBilling = "billing"
	// TicketCategoryOther 其他。
	TicketCategoryOther = "other"
)

// 工单消息作者类型取值（ticket_messages.author_type）。
const (
	// TicketAuthorMember 会员。
	TicketAuthorMember = "member"
	// TicketAuthorAdmin 管理员（客服同属该类型）。
	TicketAuthorAdmin = "admin"
)

// TicketTradeNoPrefix 是工单号前缀（复用契约 12.2.5 的本地单号规则：前缀 + UTC 时间 + 6 位随机）。
const TicketTradeNoPrefix = "T"

// TicketCategories 是分类的全部取值（顺序即契约文档与错误提示的展示顺序）。
var TicketCategories = []string{TicketCategoryTechnical, TicketCategoryBilling, TicketCategoryOther}

// IsValidTicketStatus 判断工单状态是否在 tickets.status 枚举内。
func IsValidTicketStatus(status string) bool {
	switch status {
	case TicketStatusOpen, TicketStatusReplied, TicketStatusClosed:
		return true
	default:
		return false
	}
}

// IsValidTicketCategory 判断工单分类是否在 tickets.category 枚举内。
func IsValidTicketCategory(category string) bool {
	switch category {
	case TicketCategoryTechnical, TicketCategoryBilling, TicketCategoryOther:
		return true
	default:
		return false
	}
}

// IsTicketOpen 判断工单是否处于「未关闭」状态（open / replied）。
//
// 用途（契约 16.4 第 1 条）：未关闭工单数参与提单上限（20）计数；
// closed 为终态，不再计入上限。
func IsTicketOpen(status string) bool {
	return status == TicketStatusOpen || status == TicketStatusReplied
}

// Ticket 是 tickets 表的 GORM 模型（阶段 6a 工单主表，契约 16.1）。
//
// 字段语义：
//   - TradeNo 是本地工单号（T + UTC 时间 + 6 位随机），唯一键 uk_tickets_trade_no；
//   - InstanceID 可选：非空时必须是**该会员自己的**实例（防越权关联，见契约 16.4 第 3 条）；
//   - LastReplyAt 是最近一条**消息**的时间（含管理员内部备注），列表排序与待办定位锚点；
//   - ClosedAt 仅在 status=closed 时非空。
type Ticket struct {
	ID uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	// TradeNo 唯一；单号冲突由 store 翻译为 ErrTradeNoTaken（createWithTradeNo 换号重试）。
	TradeNo     string     `gorm:"column:trade_no"`
	MemberID    uint64     `gorm:"column:member_id"`
	InstanceID  *uint64    `gorm:"column:instance_id"`
	Category    string     `gorm:"column:category;default:other"`
	Subject     string     `gorm:"column:subject"`
	Status      string     `gorm:"column:status;default:open"`
	LastReplyAt time.Time  `gorm:"column:last_reply_at"`
	ClosedAt    *time.Time `gorm:"column:closed_at"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
}

// TableName 返回工单表名。
func (Ticket) TableName() string { return "tickets" }

// TicketMessage 是 ticket_messages 表的 GORM 模型（阶段 6a 工单消息，契约 16.1）。
//
// Internal 为管理员内部备注：仅管理端可见，会员端接口一律过滤（契约 16.3）。
// AuthorID 按 AuthorType 解释为 members.id 或 admins.id。
type TicketMessage struct {
	ID         uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	TicketID   uint64    `gorm:"column:ticket_id"`
	AuthorType string    `gorm:"column:author_type"`
	AuthorID   uint64    `gorm:"column:author_id"`
	Content    string    `gorm:"column:content"`
	Internal   bool      `gorm:"column:internal"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

// TableName 返回工单消息表名。
func (TicketMessage) TableName() string { return "ticket_messages" }
