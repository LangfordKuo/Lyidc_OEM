package pricing

import (
	"fmt"
	"strings"
)

// 返佣比例取值范围（百分比，含两位小数）：0.00 - 100.00。
const (
	MinRebatePercent = 0
	MaxRebatePercent = 100
)

// rebatePrecision 是比例的小数位（DECIMAL(5,2) → 两位小数）。
// 比例在内部以「百分之 ×100 的整数」表示：20.00% → 2000，便于整数分计算。
const rebatePrecision = 100

// ParseSignedAmount 把**有符号**金额字符串解析为「分」：可选前导 -，其余口径与 ParseAmount 一致。
// 用于邀请返现余额（rebate_balance，可为负）的读写；消费余额一律走 ParseAmount。
func ParseSignedAmount(amount string) (int64, error) {
	trimmed := strings.TrimSpace(amount)
	negative := false
	if strings.HasPrefix(trimmed, "-") {
		negative = true
		trimmed = trimmed[1:]
	}
	cents, err := ParseAmount(trimmed)
	if err != nil {
		return 0, fmt.Errorf("金额格式不正确（需为十进制数，最多两位小数，可带负号）：%q", amount)
	}
	if negative {
		return -cents, nil
	}
	return cents, nil
}

// ParsePercent 把返佣比例字符串解析为「百分之 ×100 的整数」：20.00 → 2000。
// 只接受 0.00-100.00（含两位小数），非法值一律拒绝（契约 18.2）。
func ParsePercent(percent string) (int64, error) {
	trimmed := strings.TrimSpace(percent)
	if trimmed == "" {
		return 0, fmt.Errorf("%w: 比例不能为空", ErrRule)
	}
	if strings.HasPrefix(trimmed, "-") {
		return 0, fmt.Errorf("%w: 比例不能为负，收到 %q", ErrRule, percent)
	}
	cents, err := ParseAmount(trimmed)
	if err != nil {
		return 0, fmt.Errorf("%w: 比例格式不正确（需为 0.00-100.00 的十进制数，最多两位小数），收到 %q", ErrFormat, percent)
	}
	if cents > MaxRebatePercent*rebatePrecision {
		return 0, fmt.Errorf("%w: 比例不能超过 100.00，收到 %q", ErrRule, percent)
	}
	return cents, nil
}

// FormatPercent 把「百分之 ×100 的整数」格式化为两位小数比例串（2000 → "20.00"）。
func FormatPercent(hundredths int64) string {
	sign := ""
	if hundredths < 0 {
		sign = "-"
		hundredths = -hundredths
	}
	return fmt.Sprintf("%s%d.%02d", sign, hundredths/100, hundredths%100)
}

// ComputeRebateCents 计算返佣（整数分，契约 18.2 公式）：
//
//		返佣 = max(订单原价 × 生效比例 − 本单优惠券减免, 0)
//
//	  - baseCents 是订单原价（orders.amount：基础价 + Σ 配置差价，不含券减免）；
//	  - discountCents 是本单优惠券减免（orders.discount_amount）；
//	  - ratioHundredths 是生效比例（百分之 ×100 的整数，0-10000）。
//
// 下取整到分：先做整数乘除（baseCents × ratio / 10000，向零截断即正数下取整），
// 再减券减免，最后地板到 0——公式自带地板，**永远不会出现负返佣**。
func ComputeRebateCents(baseCents, discountCents, ratioHundredths int64) int64 {
	if baseCents < 0 || discountCents < 0 {
		return 0
	}
	if ratioHundredths <= 0 {
		return 0
	}
	rebate := baseCents*ratioHundredths/10000 - discountCents
	if rebate < 0 {
		return 0
	}
	return rebate
}
