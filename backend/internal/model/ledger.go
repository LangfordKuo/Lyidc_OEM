package model

import "time"

// 流水类型取值（ledger.type）。refund / adjust 为预留，本批不产生。
// 新增类型必须同步迁移 ENUM（0006 / 0015）与 IsValidLedgerType（幂等集合）。
const (
	// LedgerTypeRecharge 充值入账（amount 为正）。
	LedgerTypeRecharge = "recharge"
	// LedgerTypeOrderPay 余额支付订单扣款（amount 为负）。
	LedgerTypeOrderPay = "order_pay"
	// LedgerTypeRefund 退款（预留）。
	LedgerTypeRefund = "refund"
	// LedgerTypeAdjust 人工调整（预留）。
	LedgerTypeAdjust = "adjust"
	// LedgerTypeReferralRebate 邀请返佣：amount 为正 = 发放到返现余额、
	// 为负 = 订单取消冲回（契约 18.3 / 18.4）。
	// 注意：该类型的 BalanceBefore / BalanceAfter 记的是 **rebate_balance** 的前后值，
	// 不是消费余额 balance（两个账户完全独立）。
	LedgerTypeReferralRebate = "referral_rebate"
)

// 流水关联单据类型取值（ledger.ref_type）。
//
// R10-B 起：凡动 **rebate_balance** 的账目一律 type=referral_rebate（金额有符号），
// 靠 ref_type 区分单据来源（见 docs/api-contract.md 18.1.1）；转换的消费侧入账
// 用 type=adjust + ref_type=referral_conversion。
const (
	// LedgerRefRecharge 关联充值单（ref_id = recharges.id）。
	LedgerRefRecharge = "recharge"
	// LedgerRefOrder 关联订单（ref_id = orders.id）。
	LedgerRefOrder = "order"
	// LedgerRefReferralRebate 关联返佣流水（ref_id = referral_rebates.id）。
	LedgerRefReferralRebate = "referral_rebate"
	// LedgerRefReferralWithdrawal 关联提现申请（ref_id = referral_payouts.id）：
	// 冻结时负向、驳回退回时正向。
	LedgerRefReferralWithdrawal = "referral_withdrawal"
	// LedgerRefReferralConversion 关联转余额申请（ref_id = referral_payouts.id）：
	// 返现侧负向（type=referral_rebate）、消费侧正向（type=adjust）。
	LedgerRefReferralConversion = "referral_conversion"
)

// IsValidLedgerType 判断流水类型是否在 ledger.type 枚举内。
func IsValidLedgerType(kind string) bool {
	switch kind {
	case LedgerTypeRecharge, LedgerTypeOrderPay, LedgerTypeRefund, LedgerTypeAdjust,
		LedgerTypeReferralRebate:
		return true
	default:
		return false
	}
}

// Ledger 是 ledger 表的 GORM 模型（余额流水，只增不改）。
//
// 金额语义（契约见 docs/api-contract.md 第 12 / 18 节）：
//   - Amount 为**有符号**金额：入账为正（充值 / 返佣发放）、出账为负（余额支付订单 / 返佣冲回）；
//   - BalanceBefore / BalanceAfter 记录变动前后余额，满足 BalanceAfter = BalanceBefore + Amount；
//   - type=referral_rebate 时前后余额记的是 rebate_balance（见 LedgerTypeReferralRebate）。
type Ledger struct {
	ID            uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	MemberID      uint64    `gorm:"column:member_id"`
	Type          string    `gorm:"column:type"`
	Amount        Money     `gorm:"column:amount"`
	BalanceBefore Money     `gorm:"column:balance_before"`
	BalanceAfter  Money     `gorm:"column:balance_after"`
	RefType       string    `gorm:"column:ref_type"`
	RefID         uint64    `gorm:"column:ref_id"`
	Note          string    `gorm:"column:note"`
	CreatedAt     time.Time `gorm:"column:created_at"`
}

// TableName 返回流水表名。
func (Ledger) TableName() string { return "ledger" }
