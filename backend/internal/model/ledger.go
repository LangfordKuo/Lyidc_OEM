package model

import "time"

// 流水类型取值（ledger.type）。refund / adjust 为预留，本批不产生。
const (
	// LedgerTypeRecharge 充值入账（amount 为正）。
	LedgerTypeRecharge = "recharge"
	// LedgerTypeOrderPay 余额支付订单扣款（amount 为负）。
	LedgerTypeOrderPay = "order_pay"
	// LedgerTypeRefund 退款（预留）。
	LedgerTypeRefund = "refund"
	// LedgerTypeAdjust 人工调整（预留）。
	LedgerTypeAdjust = "adjust"
)

// 流水关联单据类型取值（ledger.ref_type）。
const (
	// LedgerRefRecharge 关联充值单（ref_id = recharges.id）。
	LedgerRefRecharge = "recharge"
	// LedgerRefOrder 关联订单（ref_id = orders.id）。
	LedgerRefOrder = "order"
)

// IsValidLedgerType 判断流水类型是否在 ledger.type 枚举内。
func IsValidLedgerType(kind string) bool {
	switch kind {
	case LedgerTypeRecharge, LedgerTypeOrderPay, LedgerTypeRefund, LedgerTypeAdjust:
		return true
	default:
		return false
	}
}

// Ledger 是 ledger 表的 GORM 模型（余额流水，只增不改）。
//
// 金额语义（契约见 docs/api-contract.md 第 12 节）：
//   - Amount 为**有符号**金额：入账为正（充值）、出账为负（余额支付订单）；
//   - BalanceBefore / BalanceAfter 记录变动前后余额，满足 BalanceAfter = BalanceBefore + Amount。
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
