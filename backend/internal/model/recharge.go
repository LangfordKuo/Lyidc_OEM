package model

import "time"

// 充值单状态取值（recharges.status）。
const (
	// RechargeStatusPending 待支付：回调到达时入账。
	RechargeStatusPending = "pending"
	// RechargeStatusPaid 已入账：幂等锚点，重复回调直接答复 success。
	RechargeStatusPaid = "paid"
	// RechargeStatusClosed 已关闭：本批不产生该状态（不自动关闭），回调命中时只告警不处理。
	RechargeStatusClosed = "closed"
)

// RechargeTradeNoPrefix 是充值单本地单号前缀（渠道 out_trade_no 直接复用本地单号）。
const RechargeTradeNoPrefix = "R"

// IsValidRechargeStatus 判断充值单状态是否在 recharges.status 枚举内。
func IsValidRechargeStatus(status string) bool {
	switch status {
	case RechargeStatusPending, RechargeStatusPaid, RechargeStatusClosed:
		return true
	default:
		return false
	}
}

// Recharge 是 recharges 表的 GORM 模型（阶段 4：余额充值单）。
//
// 字段语义（契约见 docs/api-contract.md 第 12 节）：
//   - TradeNo 是本地单号，同时作为渠道 out_trade_no；
//   - Amount 为充值金额（1.00 ~ 50000.00，定点小数字符串）；
//   - ChannelTradeNo / PaidAt 在支付成功时写入；
//   - ExpiresAt 可空，本批不自动关闭过期单（留后续批次）。
type Recharge struct {
	ID             uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	TradeNo        string     `gorm:"column:trade_no"`
	MemberID       uint64     `gorm:"column:member_id"`
	Amount         Money      `gorm:"column:amount"`
	Channel        string     `gorm:"column:channel"`
	Status         string     `gorm:"column:status"`
	ChannelTradeNo *string    `gorm:"column:channel_trade_no"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	PaidAt         *time.Time `gorm:"column:paid_at"`
	ExpiresAt      *time.Time `gorm:"column:expires_at"`
}

// TableName 返回充值单表名。
func (Recharge) TableName() string { return "recharges" }
