package pricing

import (
	"errors"
	"fmt"
	"strings"
)

// 优惠码折扣计算的纯逻辑（与 HTTP/持久化解耦，便于单测）。
//
// 计费口径（与 docs/api-contract.md 优惠码章节一致）：
//   - percent：折扣 = 售价 × 百分比，四舍五入到分（half-up）；
//   - fixed：折扣 = min(减免金额, 售价)；
//   - 折扣一律封顶到售价（final 不为负），金额全链路整数分计算。

// 折扣类型取值（对应 coupons.type，与 model 包的枚举值一致）。
const (
	// CouponTypePercent 按比例折扣（value 为百分比，0 < x <= 100）。
	CouponTypePercent = "percent"
	// CouponTypeFixed 固定金额减免（value 为固定金额，> 0）。
	CouponTypeFixed = "fixed"
)

// maxCouponValueCents 是优惠码折扣值上限（对应 coupons.value 的 DECIMAL(12,2)，
// 即 9999999999.99）。
const maxCouponValueCents int64 = 999_999_999_999

// CouponDiscount 计算优惠码对某售价的折扣，返回折扣额与折后价（定点金额字符串）。
//
// price 为周期售价；kind 为 percent / fixed；value 为折扣值（percent 时是百分比，
// fixed 时是金额，均为非负十进制、最多两位小数的字符串）。
// 折扣不超过售价，故 final = price − discount 不为负。
func CouponDiscount(price, kind, value string) (discount string, final string, err error) {
	priceCents, err := ParseAmount(price)
	if err != nil {
		return "", "", fmt.Errorf("售价解析失败: %w", err)
	}
	valueCents, err := ParseCouponValue(value)
	if err != nil {
		return "", "", fmt.Errorf("折扣值解析失败: %w", err)
	}

	discountCents, finalCents, err := couponDiscountCents(priceCents, kind, valueCents)
	if err != nil {
		return "", "", err
	}
	return FormatAmount(discountCents), FormatAmount(finalCents), nil
}

// couponDiscountCents 是折扣计算的整数分实现。
//
// valueCents 对 percent 是百分比 × 100（如 12.50% → 1250），对 fixed 是金额的分。
// percent 的 valueCents 上限 10000 保证 priceCents × valueCents 不溢出 int64。
func couponDiscountCents(priceCents int64, kind string, valueCents int64) (int64, int64, error) {
	var discount int64
	switch kind {
	case CouponTypePercent:
		if valueCents > 10_000 {
			return 0, 0, errors.New("percent 折扣值不能超过 100.00")
		}
		// 折扣 = 售价 × 百分比，四舍五入到分（half-up）。
		discount = (priceCents*valueCents + 5_000) / 10_000
	case CouponTypeFixed:
		discount = valueCents
	default:
		return 0, 0, fmt.Errorf("不支持的折扣类型 %q（支持 %s / %s）",
			kind, CouponTypePercent, CouponTypeFixed)
	}

	if discount > priceCents {
		discount = priceCents
	}
	if discount < 0 {
		discount = 0
	}
	return discount, priceCents - discount, nil
}

// ParseCouponValue 解析优惠码折扣值为「百分之一单位」的整数（percent 时是百分比的
// 百分之一，fixed 时是金额的分）：非负十进制、最多两位小数、上限同 DECIMAL(12,2)。
// 取值范围（percent 的 0 < x <= 100、fixed 的 x > 0）由业务校验另行判断。
func ParseCouponValue(value string) (int64, error) {
	cents, err := ParseAmount(strings.TrimSpace(value))
	if err != nil {
		return 0, err
	}
	if cents > maxCouponValueCents {
		return 0, fmt.Errorf("折扣值超出上限（对应 DECIMAL(12,2) 的 9999999999.99）：%q", value)
	}
	return cents, nil
}
