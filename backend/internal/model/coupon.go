package model

import "time"

// 优惠码状态取值（coupons.status）。折扣类型（coupons.type）的取值见
// pricing 包的 CouponTypePercent / CouponTypeFixed（折扣计算与类型绑定）。
const (
	// CouponStatusOn 启用：校验接口按正常流程判断其余条件。
	CouponStatusOn = "on"
	// CouponStatusOff 停用：校验接口直接返回 valid=false（reason=disabled）。
	// 停用取代删除，本阶段不提供 DELETE 接口。
	CouponStatusOff = "off"
)

// Coupon 是 coupons 表的 GORM 模型（优惠码规则；折扣应用与使用记账留订单阶段）。
//
// 字段语义（契约见 docs/api-contract.md 优惠码章节）：
//   - Code 唯一（大小写不敏感，列级别 COLLATE utf8mb4_general_ci）；
//   - Value 对 percent 是百分比（0 < x <= 100），对 fixed 是减免金额（> 0）；
//   - CyclesJSON 是适用周期的 JSON 数组文本，空数组表示全部 6 周期；
//   - StartsAt / ExpiresAt 为 UTC，NULL 分别表示立即生效 / 永不过期；
//   - MaxUses 为 0 表示不限次数；UsedCount 本阶段只读不增。
type Coupon struct {
	ID         uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	Code       string     `gorm:"column:code"`
	Type       string     `gorm:"column:type"`
	Value      Money      `gorm:"column:value"`
	CyclesJSON string     `gorm:"column:cycles_json"`
	StartsAt   *time.Time `gorm:"column:starts_at"`
	ExpiresAt  *time.Time `gorm:"column:expires_at"`
	MaxUses    int        `gorm:"column:max_uses"`
	UsedCount  int        `gorm:"column:used_count"`
	Status     string     `gorm:"column:status"`
	Comment    string     `gorm:"column:comment"`
	CreatedAt  time.Time  `gorm:"column:created_at"`
	UpdatedAt  time.Time  `gorm:"column:updated_at"`
}

// TableName 返回优惠码表名。
func (Coupon) TableName() string { return "coupons" }

// IsValidCouponStatus 判断优惠码状态是否在 coupons.status 枚举内。
func IsValidCouponStatus(status string) bool {
	switch status {
	case CouponStatusOn, CouponStatusOff:
		return true
	default:
		return false
	}
}
