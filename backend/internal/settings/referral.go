package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
)

// KeyReferral 是邀请返佣设置的键（R10-A，契约 18.6）。
const KeyReferral = "referral"

// Referral 是 referral 设置的取值结构（契约 18.6）。
//
//   - Enabled 是返佣**总开关**：关闭时不发放任何返佣（会员端概览与邀请码仍可用）；
//   - DefaultPercent 是全局默认返佣比例（0.00-100.00），三级优先级的最后一级；
//   - MaxCouponPercent 是邀请人自建券的折扣上限口径，留待 R10-B 建券接口使用（本批只落字段与校验）。
type Referral struct {
	// Enabled 返佣总开关（关：支付成功不发放任何返佣）。
	Enabled bool `json:"enabled"`
	// DefaultPercent 全局默认返佣比例（0.00-100.00，两位小数）。
	DefaultPercent string `json:"default_percent"`
	// MaxCouponPercent 邀请人自建券折扣上限（0.00-100.00，两位小数；R10-B 使用）。
	MaxCouponPercent string `json:"max_coupon_percent"`
}

// DefaultReferralSettings 返回未配置时的缺省取值：总开关关闭、比例 0.00、自建券折扣上限 100.00。
func DefaultReferralSettings() Referral {
	return Referral{
		Enabled:          false,
		DefaultPercent:   "0.00",
		MaxCouponPercent: "100.00",
	}
}

// ReferralUpdate 是 PUT /api/v1/admin/settings/referral 的更新意图：nil 表示不修改。
type ReferralUpdate struct {
	Enabled          *bool
	DefaultPercent   *string
	MaxCouponPercent *string
}

// Apply 把更新意图合并到当前值，并对合并结果做整体校验。
func (r Referral) Apply(upd ReferralUpdate) (Referral, error) {
	merged := r
	if upd.Enabled != nil {
		merged.Enabled = *upd.Enabled
	}
	if upd.DefaultPercent != nil {
		merged.DefaultPercent = strings.TrimSpace(*upd.DefaultPercent)
	}
	if upd.MaxCouponPercent != nil {
		merged.MaxCouponPercent = strings.TrimSpace(*upd.MaxCouponPercent)
	}
	if err := merged.Validate(); err != nil {
		return Referral{}, err
	}
	return merged, nil
}

// Validate 校验取值：两个比例都必须是 0.00-100.00 的百分比（两位小数）。
// 格式非法 → ErrFormat（40001）；取值越界 → ErrRule（40002）。
func (r Referral) Validate() error {
	if err := validateRebatePercent("default_percent", r.DefaultPercent); err != nil {
		return err
	}
	return validateRebatePercent("max_coupon_percent", r.MaxCouponPercent)
}

// DefaultPercentOr 返回实际使用的全局默认比例（「百分之 ×100 的整数」；非法值按 0 处理）。
func (r Referral) DefaultPercentOr() int64 {
	hundredths, err := pricing.ParsePercent(r.DefaultPercent)
	if err != nil {
		return 0
	}
	return hundredths
}

// validateRebatePercent 校验单个百分比字段（0.00-100.00）。
func validateRebatePercent(field, value string) error {
	if value == "" {
		return fmt.Errorf("%w: %s 不能为空", ErrFormat, field)
	}
	if _, err := pricing.ParsePercent(value); err != nil {
		if errors.Is(err, pricing.ErrFormat) {
			return fmt.Errorf("%w: %s 格式不正确（需为 0.00-100.00 的百分比），收到 %q", ErrFormat, field, value)
		}
		return fmt.Errorf("%w: %s %v", ErrRule, field, err)
	}
	return nil
}

// Encode 返回落库用的 JSON 文本。
func (r Referral) Encode() (string, error) { return encode(r) }

// ReferralState 是 referral 设置的读取结果。
type ReferralState struct {
	Value Referral
	Meta  Meta
	// Fingerprint 是库内 JSON 原文（未配置为空串）。
	Fingerprint string
}

// Referral 读取 referral 设置；未配置时返回缺省值（总开关关闭、比例 0.00）。
func (r *Reader) Referral(ctx context.Context) (ReferralState, error) {
	raw, meta, err := r.raw(ctx, KeyReferral)
	if err != nil {
		return ReferralState{}, err
	}
	value, err := decodeReferral(raw)
	if err != nil {
		return ReferralState{}, err
	}
	return ReferralState{Value: value, Meta: meta, Fingerprint: raw}, nil
}

// decodeReferral 解析库内 JSON；空值按缺省取值处理（未配置 = 总开关关闭）。
func decodeReferral(raw string) (Referral, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "null" {
		return DefaultReferralSettings(), nil
	}
	value := DefaultReferralSettings()
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return Referral{}, fmt.Errorf("%w: %s 的值不是合法 JSON：%v", ErrCorrupt, KeyReferral, err)
	}
	value.DefaultPercent = strings.TrimSpace(value.DefaultPercent)
	value.MaxCouponPercent = strings.TrimSpace(value.MaxCouponPercent)
	if value.DefaultPercent == "" {
		value.DefaultPercent = DefaultReferralSettings().DefaultPercent
	}
	if value.MaxCouponPercent == "" {
		value.MaxCouponPercent = DefaultReferralSettings().MaxCouponPercent
	}
	return value, nil
}
