// Package pricing 实现商品本地定价：上游价格缓存的解析 + 本地定价规则（products.pricing_json）
// 的解析、校验与计算。全部计算走「整数分」，避免浮点误差。
//
// 定价规则三种模式（可组合）：
//
//	{"mode":"upstream"}                        直接使用上游价
//	{"mode":"markup","markup_percent":10}       上游价 ×1.10，四舍五入 2 位小数
//	{"mode":"fixed","fixed":{"monthly":"25.00"}} 固定价覆盖，未覆盖的周期回退上游价
//	{"mode":"markup","markup_percent":10,
//	 "fixed":{"monthly":"25.00"}}              组合：固定价优先，其余周期按加价计算
//
// 单周期计算顺序：上游价 → 加价（若配置了 markup_percent）→ 固定价覆盖（若该周期有固定价）。
// 上游价不可用（缺省、非法或为负——上游用 -1.00 表示该周期不售）且无固定价覆盖时，
// 该周期不可售，Prices 返回空串（对外输出 null）。
package pricing

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
)

// 支持的计费周期（顺序即对外输出顺序）。
const (
	CycleMonthly      = "monthly"
	CycleQuarterly    = "quarterly"
	CycleSemiAnnually = "semiannual"
	CycleAnnually     = "annual"
)

// Cycles 是本阶段支持计算与展示的四个周期。
var Cycles = []string{CycleMonthly, CycleQuarterly, CycleSemiAnnually, CycleAnnually}

// 定价模式取值（Rule.Mode）。
const (
	// ModeUpstream 直接使用上游价。
	ModeUpstream = "upstream"
	// ModeMarkup 按 markup_percent 加价（可再叠加 fixed 覆盖）。
	ModeMarkup = "markup"
	// ModeFixed 按 fixed 覆盖价（未覆盖周期回退上游价）。
	ModeFixed = "fixed"
)

// 加价率取值范围（百分比，含两位小数）：-100% 表示白送到 0 元，上限 1000%。
const (
	minMarkupPercent = -100
	maxMarkupPercent = 1000
)

// maxAmountCents 是金额上限（对应 DECIMAL(14,2) 的 999999999999.99）。
const maxAmountCents int64 = 99_999_999_999_999

// 校验错误分类：router 据此区分 40001（格式）与 40002（业务规则）。
var (
	// ErrFormat 表示格式非法：JSON 解析失败、未知字段、模式/周期/金额写法不合法。
	ErrFormat = errors.New("定价规则格式非法")
	// ErrRule 表示规则本身不成立：缺少必需字段或字段组合互相矛盾。
	ErrRule = errors.New("定价规则不成立")
)

// Rule 是本地定价规则，对应 products.pricing_json。
type Rule struct {
	// Mode 取 upstream / markup / fixed，缺省（空串）按 upstream 处理。
	Mode string `json:"mode"`
	// MarkupPercent 是加价率（百分比，最多两位小数）；mode=markup 时必填，
	// mode=fixed 时可选（用于未被固定价覆盖的周期）。
	MarkupPercent *float64 `json:"markup_percent,omitempty"`
	// Fixed 是固定覆盖价（周期 → 金额字符串）；mode=fixed 时至少一项。
	Fixed map[string]string `json:"fixed,omitempty"`
}

// DefaultJSON 是未配置定价规则时的缺省值（等价于直接使用上游价）。
const DefaultJSON = `{"mode":"upstream"}`

// Default 返回缺省规则（直接使用上游价）。
func Default() Rule { return Rule{Mode: ModeUpstream} }

// IsValidCycle 判断是否为受支持的周期名。
func IsValidCycle(cycle string) bool {
	for _, item := range Cycles {
		if item == cycle {
			return true
		}
	}
	return false
}

// Parse 解析并校验定价规则。空串与 null 视为缺省规则（上游价）。
//
// 严格模式：出现未知字段（例如 markup_percent 拼写错误）直接报错，避免「少写一个字母 → 静默不加价」
// 这类会造成真实资金损失的误配。
func Parse(raw string) (Rule, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "null" {
		return Default(), nil
	}

	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.DisallowUnknownFields()

	var rule Rule
	if err := decoder.Decode(&rule); err != nil {
		return Rule{}, fmt.Errorf("%w: %v", ErrFormat, err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return Rule{}, err
	}
	if err := rule.normalize(); err != nil {
		return Rule{}, err
	}
	return rule, nil
}

// Normalize 校验并返回规范化后的 JSON（用于落库，保证库内写法统一）。
func Normalize(raw string) (string, error) {
	rule, err := Parse(raw)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(rule)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrFormat, err)
	}
	return string(encoded), nil
}

// Prices 按规则计算四个周期的本地售价。
//
// upstream 为周期 → 上游价格字符串（来自 UpstreamPrices.Prices，可缺周期）。
// 返回值每个周期都有键；空串表示该周期不可售。
func (r Rule) Prices(upstream map[string]string) map[string]string {
	out := make(map[string]string, len(Cycles))
	percent, hasMarkup := r.markupBasisPoints()

	for _, cycle := range Cycles {
		cents, available := parseUpstreamCents(upstream[cycle])
		if available && hasMarkup {
			cents = applyMarkup(cents, percent)
		}
		if fixedCents, ok := r.fixedCents(cycle); ok {
			cents, available = fixedCents, true
		}
		if !available {
			out[cycle] = ""
			continue
		}
		out[cycle] = FormatAmount(cents)
	}
	return out
}

// normalize 归一化模式名并做规则校验。
func (r *Rule) normalize() error {
	r.Mode = strings.ToLower(strings.TrimSpace(r.Mode))
	if r.Mode == "" {
		r.Mode = ModeUpstream
	}
	switch r.Mode {
	case ModeUpstream, ModeMarkup, ModeFixed:
	default:
		return fmt.Errorf("%w: mode 只能是 %s / %s / %s，收到 %q",
			ErrFormat, ModeUpstream, ModeMarkup, ModeFixed, r.Mode)
	}

	if err := r.validateMarkup(); err != nil {
		return err
	}
	return r.validateFixed()
}

// validateMarkup 校验加价率与模式的匹配关系。
func (r *Rule) validateMarkup() error {
	if r.MarkupPercent == nil {
		if r.Mode == ModeMarkup {
			return fmt.Errorf("%w: mode=markup 必须提供 markup_percent", ErrRule)
		}
		return nil
	}
	if r.Mode == ModeUpstream {
		return fmt.Errorf("%w: mode=upstream 不应带 markup_percent（如需加价请用 mode=markup）", ErrRule)
	}

	percent := *r.MarkupPercent
	if math.IsNaN(percent) || math.IsInf(percent, 0) {
		return fmt.Errorf("%w: markup_percent 必须是有限数值", ErrFormat)
	}
	if percent < minMarkupPercent || percent > maxMarkupPercent {
		return fmt.Errorf("%w: markup_percent 需在 %d 到 %d 之间，收到 %v",
			ErrRule, minMarkupPercent, maxMarkupPercent, percent)
	}
	if math.Round(percent*100) != percent*100 {
		return fmt.Errorf("%w: markup_percent 最多支持两位小数，收到 %v", ErrFormat, percent)
	}
	return nil
}

// validateFixed 校验固定价：模式匹配、周期名合法、金额合法。
func (r *Rule) validateFixed() error {
	if len(r.Fixed) == 0 {
		r.Fixed = nil
		if r.Mode == ModeFixed {
			return fmt.Errorf("%w: mode=fixed 必须提供至少一个周期的固定价", ErrRule)
		}
		return nil
	}
	if r.Mode == ModeUpstream {
		return fmt.Errorf("%w: mode=upstream 不应带 fixed（如需固定价请用 mode=fixed）", ErrRule)
	}

	for cycle, amount := range r.Fixed {
		if !IsValidCycle(cycle) {
			return fmt.Errorf("%w: 不支持的周期 %q（支持 %s）",
				ErrFormat, cycle, strings.Join(Cycles, " / "))
		}
		cents, err := ParseAmount(amount)
		if err != nil {
			return fmt.Errorf("%w: fixed[%s] %v", ErrFormat, cycle, err)
		}
		if cents < 0 {
			return fmt.Errorf("%w: fixed[%s] 不能为负数", ErrFormat, cycle)
		}
	}
	return nil
}

// markupBasisPoints 返回加价率的「万分之一」整数表示（10% → 1000）。
func (r Rule) markupBasisPoints() (int64, bool) {
	if r.MarkupPercent == nil {
		return 0, false
	}
	return int64(math.Round(*r.MarkupPercent * 100)), true
}

// fixedCents 返回某周期的固定价（分）。
func (r Rule) fixedCents(cycle string) (int64, bool) {
	amount, ok := r.Fixed[cycle]
	if !ok {
		return 0, false
	}
	cents, err := ParseAmount(amount)
	if err != nil {
		return 0, false
	}
	return cents, true
}

// applyMarkup 对金额（分）应用加价率（万分之一为单位），四舍五入到分。
//
// 拆成「整数倍 + 余数」两步计算，避免 cents × 加价因子 溢出 int64。
func applyMarkup(cents, percentBasisPoints int64) int64 {
	factor := 10000 + percentBasisPoints
	whole := factor / 10000
	frac := factor % 10000
	return cents*whole + (cents*frac+5000)/10000
}

// ParseAmount 把金额字符串解析为「分」，仅接受非负、最多两位小数的十进制写法。
func ParseAmount(amount string) (int64, error) {
	trimmed := strings.TrimSpace(amount)
	if trimmed == "" {
		return 0, errors.New("金额不能为空")
	}

	integer, fraction, hasDot := strings.Cut(trimmed, ".")
	if integer == "" || !isDigits(integer) {
		return 0, fmt.Errorf("金额格式不正确（需为非负十进制数，最多两位小数）：%q", amount)
	}
	if hasDot {
		if len(fraction) == 0 || len(fraction) > 2 || !isDigits(fraction) {
			return 0, fmt.Errorf("金额格式不正确（需为非负十进制数，最多两位小数）：%q", amount)
		}
	}
	switch len(fraction) {
	case 1:
		fraction += "0"
	case 0:
		fraction = "00"
	}

	cents := int64(0)
	for _, ch := range integer {
		cents = cents*10 + int64(ch-'0')
		if cents > maxAmountCents/100 {
			return 0, fmt.Errorf("金额超出上限：%q", amount)
		}
	}

	fractionCents := int64(0)
	for _, ch := range fraction {
		fractionCents = fractionCents*10 + int64(ch-'0')
	}

	cents = cents*100 + fractionCents
	if cents > maxAmountCents {
		return 0, fmt.Errorf("金额超出上限：%q", amount)
	}
	return cents, nil
}

// FormatAmount 把「分」格式化为两位小数的定点字符串。
func FormatAmount(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}

// parseUpstreamCents 解析上游价格：空串、非法写法与负值（上游用 -1.00 标记该周期不售）都视为不可用。
func parseUpstreamCents(amount string) (int64, bool) {
	cents, err := ParseAmount(amount)
	if err != nil || cents < 0 {
		return 0, false
	}
	return cents, true
}

// isDigits 判断字符串是否全为 ASCII 数字（且非空）。
func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

// ensureJSONEnd 确认 JSON 解析后没有多余内容。
func ensureJSONEnd(decoder *json.Decoder) error {
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: 规则结尾存在多余内容", ErrFormat)
	}
	return nil
}
