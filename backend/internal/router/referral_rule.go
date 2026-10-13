package router

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
)

// errReferralRule 表示邀请返佣规则不成立（对应 40002，契约 18.5）；
// 格式类问题仍走 40001（validate.go 口径）。
var errReferralRule = errors.New("邀请返佣规则不成立")

// 邀请码取值口径（契约 18.5）：8 位 [A-Za-z0-9]，由服务端惰性生成，
// 这里只做最宽松的形状校验，真正判定靠 members.invite_code 精确匹配。
const (
	minInviteCodeLen = 4
	maxInviteCodeLen = 32
)

// validateInviteCodeShape 校验邀请码写法：长度 4-32、只含字母数字下划线连字符。
// 空串调用方应先按「未提供」处理，不会走到这里。
func validateInviteCodeShape(code string) error {
	trimmed := strings.TrimSpace(code)
	if trimmed == "" {
		return fmt.Errorf("%w: 邀请码不能为空", errReferralRule)
	}
	if len(trimmed) < minInviteCodeLen || len(trimmed) > maxInviteCodeLen {
		return fmt.Errorf("%w: 邀请码长度需在 %d-%d 之间",
			errReferralRule, minInviteCodeLen, maxInviteCodeLen)
	}
	for _, ch := range trimmed {
		switch {
		case ch >= 'A' && ch <= 'Z', ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9',
			ch == '_', ch == '-':
		default:
			return fmt.Errorf("%w: 邀请码只能包含字母、数字、下划线与连字符", errReferralRule)
		}
	}
	return nil
}

// validateRebatePercentInput 校验管理员填的返佣比例（0.00-100.00，两位小数）。
// 格式非法 → 40001（pricing.ErrFormat）；越界 → 40002（errReferralRule）。
func validateRebatePercentInput(field, value string) (int64, error) {
	hundredths, err := pricing.ParsePercent(value)
	if err != nil {
		if errors.Is(err, pricing.ErrFormat) {
			return 0, fmt.Errorf("%w: %s 格式不正确（需为 0.00-100.00 的百分比），收到 %q",
				pricing.ErrFormat, field, value)
		}
		return 0, fmt.Errorf("%w: %s %v", errReferralRule, field, err)
	}
	return hundredths, nil
}

// buildInviteLink 拼站内邀请链接：{站点地址}/register?invite={邀请码}。
// 站点地址未配置时返回空串（前端据此隐藏链接，只展示邀请码）。
func buildInviteLink(siteURL, inviteCode string) string {
	if siteURL == "" || inviteCode == "" {
		return ""
	}
	return siteURL + "/register?invite=" + inviteCode
}

// rebatePercentFromRaw 解析 updateProduct 的 rebate_percent 三态（契约 18.2）：
//   - raw 为 null → 返回指向空串的指针（调用方落 NULL = 不覆盖）；
//   - raw 为字符串 → 校验 0.00-100.00 后返回该值；
//   - 其它类型 → 40001。
func rebatePercentFromRaw(raw json.RawMessage) (*string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "null" {
		empty := ""
		return &empty, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("%w: rebate_percent 需为百分比字符串或 null", pricing.ErrFormat)
	}
	value = strings.TrimSpace(value)
	if _, err := validateRebatePercentInput("rebate_percent", value); err != nil {
		return nil, err
	}
	return &value, nil
}

// memberRebateOverrideFromRaw 解析 PUT /admin/members/:id 的 rebate_percent_override 三态
// （契约 6.3，R10-B）：
//   - raw 缺席（len == 0）→ set=false（不改）；
//   - raw 为 null → set=true, value=nil（清空，回到走全局）；
//   - raw 为字符串 → set=true, value=&校验后的值（0.00-100.00）；
//   - 其它类型 → 40001。
func memberRebateOverrideFromRaw(raw json.RawMessage) (set bool, value *string, err error) {
	if len(raw) == 0 {
		return false, nil, nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "null" {
		return true, nil, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return false, nil, fmt.Errorf("%w: rebate_percent_override 需为百分比字符串或 null", pricing.ErrFormat)
	}
	text = strings.TrimSpace(text)
	normalized, err := validateRebatePercentInput("rebate_percent_override", text)
	if err != nil {
		return false, nil, err
	}
	formatted := pricing.FormatPercent(normalized)
	return true, &formatted, nil
}

// couponCap 是邀请人自建券的折扣百分比上限（契约 18.5，R10-B）。
//
//	上限 = min(settings.referral.max_coupon_percent, 邀请人的生效返佣比例)
//
// 「邀请人的生效返佣比例」= 会员级 rebate_percent_override 优先，否则全局 default_percent
// （即 18.2 三级优先级的**前两级**；商品级不参与——券是跨商品通用的）。
// 任一取值在库里损坏（越界/非法）时按 0 处理（宁可不发券，也不放行倒挂）。
type couponCap struct {
	// Cap 是生效上限（百分之 ×100 的整数，0-10000）。
	Cap int64
	// EffectivePercent 是邀请人的生效返佣比例（百分之 ×100 的整数）。
	EffectivePercent int64
	// MaxCouponPercent 是设置里的全局闸门（百分之 ×100 的整数）。
	MaxCouponPercent int64
	// EffectiveSource 是生效比例的来源文案（「会员级特批」/「全局默认」），用于拒绝 message。
	EffectiveSource string
}

// describe 把上限写成拒绝 message（含当前上限值与两个来源，契约 18.5）。
func (c couponCap) describe() string {
	return fmt.Sprintf("折扣不能超过 %s（上限 = min(设置 %s, 生效比例 %s，来源：%s)）",
		pricing.FormatPercent(c.Cap),
		pricing.FormatPercent(c.MaxCouponPercent),
		pricing.FormatPercent(c.EffectivePercent),
		c.EffectiveSource)
}

// resolveCouponCap 计算邀请人自建券的折扣上限（契约 18.5）。
func resolveCouponCap(ref settings.Referral, member *model.Member) couponCap {
	maxCoupon := int64(0)
	if hundredths, err := pricing.ParsePercent(ref.MaxCouponPercent); err == nil {
		maxCoupon = hundredths
	}

	effective := int64(0)
	source := "全局默认"
	if member != nil && member.RebatePercentOverride != nil {
		if hundredths, err := pricing.ParsePercent(*member.RebatePercentOverride); err == nil {
			effective = hundredths
			source = "会员级特批"
		}
	}
	if source == "全局默认" {
		if hundredths, err := pricing.ParsePercent(ref.DefaultPercent); err == nil {
			effective = hundredths
		}
	}

	cap := maxCoupon
	if effective < cap {
		cap = effective
	}
	return couponCap{
		Cap:              cap,
		EffectivePercent: effective,
		MaxCouponPercent: maxCoupon,
		EffectiveSource:  source,
	}
}
