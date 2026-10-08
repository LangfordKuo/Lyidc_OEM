package router

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
)

// 优惠码的校验与判定纯逻辑（与 HTTP、数据库解耦，单测见 coupon_test.go）。
//
// 错误分类沿用定价规则的约定：格式类（非法 code/时间写法、未知周期/类型）→ 40001，
// 规则类（取值为合法格式但违反取值范围或先后关系）→ 40002。

// 字段长度约束（对应 coupons 表列宽）。
const (
	minCouponCodeLength = 3
	maxCouponCodeLength = 32
	// maxCouponCommentRunes 对应 coupons.comment 的 VARCHAR(255)（utf8mb4 下按字符计）。
	maxCouponCommentRunes = 255
	// maxCouponMaxUses 对应 coupons.max_uses 的 INT UNSIGNED 上限。
	maxCouponMaxUses = 4294967295
)

// 校验接口的 reason 枚举（契约固定 5 个，见 docs/api-contract.md 优惠码章节）。
const (
	// ReasonCouponDisabled 优惠码已停用（status=off）。
	ReasonCouponDisabled = "disabled"
	// ReasonCouponNotStarted 未到生效时间（now < starts_at）。
	ReasonCouponNotStarted = "not_started"
	// ReasonCouponExpired 已过期（now > expires_at）。
	ReasonCouponExpired = "expired"
	// ReasonCouponUsedUp 使用次数已达上限（used_count >= max_uses）。
	ReasonCouponUsedUp = "used_up"
	// ReasonCycleNotApplicable 该周期不适用：优惠码的适用周期集合不含该周期，
	// 或该商品在该周期本地不可售（无售价，无折扣可算）。
	ReasonCycleNotApplicable = "cycle_not_applicable"
)

var couponCodePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{3,32}$`)

// 优惠码校验的错误哨兵，供 failRuleError 区分 40001 与 40002。
var (
	errCouponFormat = errors.New("优惠码参数格式非法")
	errCouponRule   = errors.New("优惠码规则不成立")
)

// validateCouponCode 校验优惠码：3-32 位字母、数字、下划线或连字符。
func validateCouponCode(code string) error {
	if !couponCodePattern.MatchString(code) {
		return fmt.Errorf("%w: 优惠码需为 %d-%d 位字母、数字、下划线或连字符",
			errCouponFormat, minCouponCodeLength, maxCouponCodeLength)
	}
	return nil
}

// validateCouponType 校验折扣类型（只在创建时接受）。
func validateCouponType(kind string) error {
	switch kind {
	case pricing.CouponTypePercent, pricing.CouponTypeFixed:
		return nil
	default:
		return fmt.Errorf("%w: type 只能是 %s 或 %s，收到 %q",
			errCouponFormat, pricing.CouponTypePercent, pricing.CouponTypeFixed, kind)
	}
}

// validateCouponValue 校验折扣值：格式（非负十进制、最多两位小数、DECIMAL(12,2) 上限）
// 与范围（percent 的 0 < x <= 100、fixed 的 x > 0）。
// 返回「百分之一单位」的整数（percent 时是百分比的百分之一，如 12.5% → 1250）。
func validateCouponValue(kind, value string) (int64, error) {
	cents, err := pricing.ParseCouponValue(value)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", errCouponFormat, err)
	}
	switch kind {
	case pricing.CouponTypePercent:
		// 100.00% → 10_000（百分之一单位）。
		if cents <= 0 || cents > 10_000 {
			return 0, fmt.Errorf("%w: percent 的 value 需大于 0 且不超过 100（表示百分比），收到 %q",
				errCouponRule, value)
		}
	case pricing.CouponTypeFixed:
		if cents <= 0 {
			return 0, fmt.Errorf("%w: fixed 的 value 必须大于 0，收到 %q", errCouponRule, value)
		}
	}
	return cents, nil
}

// validateCouponMaxUses 校验最大使用次数（0 表示不限）。
func validateCouponMaxUses(maxUses int) error {
	if maxUses < 0 || maxUses > maxCouponMaxUses {
		return fmt.Errorf("%w: max_uses 需为 0（不限）到 %d 之间的整数", errCouponRule, maxCouponMaxUses)
	}
	return nil
}

// validateCouponComment 校验备注长度（按字符计，可空）。
func validateCouponComment(comment string) error {
	if utf8.RuneCountInString(comment) > maxCouponCommentRunes {
		return fmt.Errorf("%w: comment 长度不能超过 %d 个字符", errCouponFormat, maxCouponCommentRunes)
	}
	return nil
}

// validateCouponWindow 校验生效/过期时间的先后：两者都提供时必须 starts_at < expires_at。
func validateCouponWindow(startsAt, expiresAt *time.Time) error {
	if startsAt != nil && expiresAt != nil && !startsAt.Before(*expiresAt) {
		return fmt.Errorf("%w: starts_at 必须早于 expires_at", errCouponRule)
	}
	return nil
}

// normalizeCouponCycles 校验并规范化适用周期：去重后按标准周期顺序排列，
// 返回落库用的 JSON 数组文本（空数组表示全部 6 周期，不会返回 nil）。
func normalizeCouponCycles(cycles []string) (string, error) {
	seen := make(map[string]bool, len(cycles))
	for _, cycle := range cycles {
		cycle = strings.TrimSpace(cycle)
		if !pricing.IsValidCycle(cycle) {
			return "", fmt.Errorf("%w: 不支持的周期 %q（支持 %s）",
				errCouponFormat, cycle, strings.Join(pricing.Cycles, " / "))
		}
		seen[cycle] = true
	}

	normalized := make([]string, 0, len(seen))
	for _, cycle := range pricing.Cycles {
		if seen[cycle] {
			normalized = append(normalized, cycle)
		}
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("%w: 适用周期序列化失败: %v", errCouponFormat, err)
	}
	return string(encoded), nil
}

// parseCouponCycles 解析库内 cycles_json；空串与 null 按空数组（全部周期）处理。
func parseCouponCycles(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var cycles []string
	if err := json.Unmarshal([]byte(trimmed), &cycles); err != nil {
		return nil, fmt.Errorf("优惠码适用周期解析失败: %w", err)
	}
	return cycles, nil
}

// couponInvalidReason 按契约顺序判定优惠码对某周期的有效性：
// disabled → not_started → expired → used_up → cycle_not_applicable。
// 返回空串表示通过；调用方还需确认该商品该周期有本地售价。
func couponInvalidReason(coupon *model.Coupon, cycles []string, cycle string, now time.Time) string {
	if coupon.Status != model.CouponStatusOn {
		return ReasonCouponDisabled
	}
	if coupon.StartsAt != nil && now.Before(*coupon.StartsAt) {
		return ReasonCouponNotStarted
	}
	if coupon.ExpiresAt != nil && now.After(*coupon.ExpiresAt) {
		return ReasonCouponExpired
	}
	if coupon.MaxUses > 0 && coupon.UsedCount >= coupon.MaxUses {
		return ReasonCouponUsedUp
	}
	if !couponCyclesInclude(cycles, cycle) {
		return ReasonCycleNotApplicable
	}
	return ""
}

// couponCyclesInclude 判断周期是否在适用集合内（空集合表示全部周期）。
func couponCyclesInclude(cycles []string, cycle string) bool {
	if len(cycles) == 0 {
		return true
	}
	for _, item := range cycles {
		if item == cycle {
			return true
		}
	}
	return false
}

// parseCouponTime 解析 RFC3339 时间并转为 UTC；空串返回 nil（不限制该项）。
func parseCouponTime(raw string) (*time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return nil, fmt.Errorf("%w: 时间需为 RFC3339 格式（如 2026-10-08T00:00:00Z），收到 %q",
			errCouponFormat, raw)
	}
	utc := parsed.UTC()
	return &utc, nil
}

// parseCouponTimeRaw 解析更新请求里的 JSON 时间字段：null 表示清空（返回 nil），
// 字符串按 RFC3339 解析。
func parseCouponTimeRaw(raw json.RawMessage) (*time.Time, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "null" {
		return nil, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return nil, fmt.Errorf("%w: 时间字段需为 RFC3339 字符串或 null", errCouponFormat)
	}
	return parseCouponTime(text)
}
