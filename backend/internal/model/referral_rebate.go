package model

import "time"

// 返佣比例来源取值（referral_rebates.ratio_source，契约 18.2 的三级优先级）。
const (
	// RebateSourceProduct 商品级：products.rebate_percent 非 NULL 时生效（最高优先）。
	RebateSourceProduct = "product"
	// RebateSourceMember 会员级：邀请人 members.rebate_percent_override 非 NULL 时生效。
	RebateSourceMember = "member"
	// RebateSourceGlobal 全局默认：settings referral.default_percent。
	RebateSourceGlobal = "global"
)

// 返佣流水状态取值（referral_rebates.status）。
const (
	// RebateStatusGranted 已发放：返佣已进邀请人的邀请返现余额。
	RebateStatusGranted = "granted"
	// RebateStatusRolledBack 已回滚：订单取消时冲回（写负向 ledger 并扣减返现余额）。
	RebateStatusRolledBack = "rolled_back"
)

// ReferralRebate 是 referral_rebates 表的 GORM 模型（邀请返佣流水）。
//
// 字段语义（契约见 docs/api-contract.md 第 18 节）：
//   - OrderID 唯一是**发放幂等锚点**：同一订单只发一次（重复回调不重复发放）；
//   - BaseAmount / DiscountAmount / RatioPercent / RatioSource 为发放时的口径快照，
//     事后改商品比例或会员特批不影响已发放记录；
//   - RebateAmount = max(BaseAmount × RatioPercent − DiscountAmount, 0)，下取整到分，恒 ≥ 0；
//   - Status 由 granted 翻 rolled_back 表示订单取消时的冲回（冲回只发生一次）。
type ReferralRebate struct {
	ID             uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	OrderID        uint64    `gorm:"column:order_id"`
	InviterID      uint64    `gorm:"column:inviter_id"`
	BuyerID        uint64    `gorm:"column:buyer_id"`
	OrderType      string    `gorm:"column:order_type"`
	BaseAmount     Money     `gorm:"column:base_amount"`
	DiscountAmount Money     `gorm:"column:discount_amount"`
	RatioPercent   Money     `gorm:"column:ratio_percent"`
	RatioSource    string    `gorm:"column:ratio_source"`
	RebateAmount   Money     `gorm:"column:rebate_amount"`
	Status         string    `gorm:"column:status"`
	Note           string    `gorm:"column:note"`
	CreatedAt      time.Time `gorm:"column:created_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at"`
}

// TableName 返回返佣流水表名。
func (ReferralRebate) TableName() string { return "referral_rebates" }

// IsValidRebateStatus 判断返佣状态是否在 referral_rebates.status 枚举内。
func IsValidRebateStatus(status string) bool {
	switch status {
	case RebateStatusGranted, RebateStatusRolledBack:
		return true
	default:
		return false
	}
}
