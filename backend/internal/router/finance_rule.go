package router

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
)

// 财务（充值/余额/下单）相关的纯校验逻辑。
//
// 错误分类沿用定价/优惠码的约定：格式类（金额写法非法、类型不符）→ 40001，
// 规则类（取值合法但越界或状态不允许）→ 40002。

var (
	// errFinanceFormat 财务参数格式非法（对应 40001）。
	errFinanceFormat = errors.New("财务参数格式非法")
	// errFinanceRule 财务规则不成立（对应 40002）。
	errFinanceRule = errors.New("财务规则不成立")
)

// 充值金额边界（契约 12.4）：1.00 ~ 50000.00。
const (
	minRechargeCents int64 = 100
	maxRechargeCents int64 = 5_000_000
)

// maxPaymentSubjectRunes 是传给渠道的支付标题长度上限（超出按字符截断）。
const maxPaymentSubjectRunes = 64

// truncateRunes 按字符（rune）截断字符串，避免把多字节字符切坏。
func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

// parseMoneyBounds 解析金额字段（非负十进制、最多两位小数）并校验区间，返回整数分。
func parseMoneyBounds(field, value string, minCents, maxCents int64) (int64, error) {
	cents, err := pricing.ParseAmount(value)
	if err != nil {
		return 0, fmt.Errorf("%w: %s %v", errFinanceFormat, field, err)
	}
	if cents < minCents || cents > maxCents {
		return 0, fmt.Errorf("%w: %s 需在 %s ~ %s 之间（两位小数），收到 %q",
			errFinanceRule, field, pricing.FormatAmount(minCents), pricing.FormatAmount(maxCents), value)
	}
	return cents, nil
}

// parseUint64Query 解析正整数查询参数（成员 ID 等）；空串返回 0（表示不过滤）。
func parseUint64Query(field, raw string) (uint64, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, nil
	}
	value, err := strconv.ParseUint(trimmed, 10, 64)
	if err != nil || value == 0 {
		return 0, fmt.Errorf("%w: %s 必须为正整数", errFinanceFormat, field)
	}
	return value, nil
}
