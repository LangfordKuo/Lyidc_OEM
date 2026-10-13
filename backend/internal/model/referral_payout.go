package model

import "time"

// 出账申请类型取值（referral_payouts.kind，契约 18.1.1）。
const (
	// PayoutKindWithdraw 提现（到微信/支付宝，线下打款）。
	PayoutKindWithdraw = "withdraw"
	// PayoutKindConvert 转余额（邀请返现余额 → 消费余额）。
	PayoutKindConvert = "convert"
)

// 提现到账渠道取值（referral_payouts.channel，仅 kind=withdraw 使用）。
const (
	// PayoutChannelWechat 微信。
	PayoutChannelWechat = "wechat"
	// PayoutChannelAlipay 支付宝。
	PayoutChannelAlipay = "alipay"
)

// 出账申请状态取值（referral_payouts.status）。
//
// 状态机（契约 18.1.1）：
//
//	withdraw: pending → approved → paid；pending → rejected
//	convert:  pending → approved；    pending → rejected
//
// rejected / paid 是终态；重复审核/重复打款幂等（不产生第二次资金动作）。
const (
	// PayoutStatusPending 待审核。
	PayoutStatusPending = "pending"
	// PayoutStatusApproved 已通过（convert 已换算完成；withdraw 待线下打款）。
	PayoutStatusApproved = "approved"
	// PayoutStatusRejected 已驳回（终态）。
	PayoutStatusRejected = "rejected"
	// PayoutStatusPaid 已打款（终态，仅 kind=withdraw）。
	PayoutStatusPaid = "paid"
)

// ReferralPayout 是 referral_payouts 表的 GORM 模型（提现与转余额申请，契约 18.1.1）。
//
// 字段语义：
//   - Kind 区分 withdraw / convert，两类共用同一套状态机与审核路径；
//   - Amount 恒 > 0；提交校验在行锁下按「可用余额」判定（见 store 包）；
//   - Channel / AccountName / AccountNo 仅 kind=withdraw 有值，kind=convert 恒为空串；
//     AccountName / AccountNo 是**敏感**字段：库内存原文，接口/日志/审计一律脱敏；
//   - Status 的 pending→approved 条件更新是「审核只生效一次」的幂等锚点；
//   - ReviewedAt / PaidAt 首次写入后**不覆盖**（重复审核/打款只回 already=true）。
type ReferralPayout struct {
	ID          uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	Kind        string     `gorm:"column:kind"`
	MemberID    uint64     `gorm:"column:member_id"`
	Amount      Money      `gorm:"column:amount"`
	Channel     string     `gorm:"column:channel"`
	AccountName string     `gorm:"column:account_name"`
	AccountNo   string     `gorm:"column:account_no"`
	Status      string     `gorm:"column:status"`
	Remark      string     `gorm:"column:remark"`
	ReviewNote  string     `gorm:"column:review_note"`
	ReviewedBy  uint64     `gorm:"column:reviewed_by"`
	ReviewedAt  *time.Time `gorm:"column:reviewed_at"`
	PaidAt      *time.Time `gorm:"column:paid_at"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
}

// TableName 返回出账申请表名。
func (ReferralPayout) TableName() string { return "referral_payouts" }

// IsValidPayoutKind 判断申请类型是否在 referral_payouts.kind 枚举内。
func IsValidPayoutKind(kind string) bool {
	return kind == PayoutKindWithdraw || kind == PayoutKindConvert
}

// IsValidPayoutStatus 判断申请状态是否在 referral_payouts.status 枚举内。
func IsValidPayoutStatus(status string) bool {
	switch status {
	case PayoutStatusPending, PayoutStatusApproved, PayoutStatusRejected, PayoutStatusPaid:
		return true
	default:
		return false
	}
}

// IsValidPayoutChannel 判断到账渠道是否在 referral_payouts.channel 枚举内。
func IsValidPayoutChannel(channel string) bool {
	return channel == PayoutChannelWechat || channel == PayoutChannelAlipay
}
