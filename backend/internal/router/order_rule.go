package router

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
)

// 下单与优惠码应用的纯逻辑（与 HTTP、数据库解耦）。
//
// 优惠码应用口径（兑现阶段 3b 遗留，契约 12.5）：下单时校验并快照，**支付成功时**才计数；
// 下单失败/取消不占用次数，因此取消无需回退。

// maxOrderConfigBytes 对应 orders.config_json 的 VARCHAR(1024)。
const maxOrderConfigBytes = 1024

// couponReasonMessage 把 3b 的 reason 枚举翻译成下单接口的中文提示。
var couponReasonMessage = map[string]string{
	ReasonCouponDisabled:     "优惠码已停用",
	ReasonCouponNotStarted:   "优惠码尚未生效",
	ReasonCouponExpired:      "优惠码已过期",
	ReasonCouponUsedUp:       "优惠码使用次数已用尽",
	ReasonCycleNotApplicable: "优惠码不适用于该周期",
}

// validateOrderConfig 校验下单配置项：键必须是该商品**会员可见**的可配置项 id（`options[].id`），
// 值必须是该配置项某个可见可选值的 id（`values[].id`）（未知配置项/取值一律拒绝，契约 12.3）。
//
// 口径依据（阶段 5a 真机实测，契约 14.5）：上游直连下单接口按
// `product_config_options.id`（键）与 `product_config_options_sub.id`（值）查库取配置，
// 而 `upstream_id` 是上游「作为下游代理」场景的映射字段（本项目真实数据恒为 0）。
// 上游标记为隐藏（hidden != 0）的项与值不在会员端可见范围内，同样按「未知」拒绝；
// id 非正数的异常数据项同样视为不可用（避免拼出上游不认的 configoption）。
func validateOrderConfig(cache productConfigCache, config map[string]string) error {
	allowed := make(map[string]map[string]bool, len(cache.ConfigGroups))
	for _, group := range cache.ConfigGroups {
		for _, option := range group.Options {
			if option.Hidden != 0 || option.ID <= 0 {
				continue
			}
			values := make(map[string]bool, len(option.Values))
			for _, value := range option.Values {
				if value.Hidden != 0 || value.ID <= 0 {
					continue
				}
				values[strconv.Itoa(value.ID)] = true
			}
			allowed[strconv.Itoa(option.ID)] = values
		}
	}

	for key, value := range config {
		values, ok := allowed[key]
		if !ok {
			return fmt.Errorf("%w: 未知的配置项 %q（不在该商品的可配置项内）", errFinanceRule, key)
		}
		if !values[value] {
			return fmt.Errorf("%w: 配置项 %q 不支持所选值 %q", errFinanceRule, key, value)
		}
	}
	return nil
}

// encodeOrderConfig 把下单配置序列化为落库 JSON 文本（空配置输出 "{}"）。
// 超出列宽（1024 字节）时拒绝，避免落库被静默截断。
func encodeOrderConfig(config map[string]string) (string, error) {
	if len(config) == 0 {
		return "{}", nil
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("%w: 配置项序列化失败: %v", errFinanceFormat, err)
	}
	if len(encoded) > maxOrderConfigBytes {
		return "", fmt.Errorf("%w: 配置项内容过长（%d 字节，上限 %d）", errFinanceRule, len(encoded), maxOrderConfigBytes)
	}
	return string(encoded), nil
}

// decodeOrderConfig 解析库内 config_json；异常时返回空配置（视图不因此失败）。
func decodeOrderConfig(raw string, logger *slog.Logger) map[string]string {
	config := map[string]string{}
	if raw == "" || raw == "null" {
		return config
	}
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		logger.Warn("订单配置快照解析失败，按空配置输出", "error", err, "config_json", raw)
		return map[string]string{}
	}
	return config
}

// applyCoupon 校验优惠码并计算折扣（复用 3b 的判定顺序与折扣口径）。
//
// 返回优惠码实体、折扣额与折后价（均为定点小数字符串）。
// 不存在、无效、不适用于该周期一律返回 errFinanceRule（对外统一 40002 + 明确 message）。
func applyCoupon(coupon *model.Coupon, cycles []string, cycle, price string, now time.Time) (discount string, final string, err error) {
	if reason := couponInvalidReason(coupon, cycles, cycle, now); reason != "" {
		message, ok := couponReasonMessage[reason]
		if !ok {
			message = "优惠码不可用（" + reason + "）"
		}
		return "", "", fmt.Errorf("%w: %s", errFinanceRule, message)
	}

	discount, final, err = pricing.CouponDiscount(price, coupon.Type, string(coupon.Value))
	if err != nil {
		return "", "", fmt.Errorf("%w: 优惠码折扣计算失败：%v", errFinanceRule, err)
	}
	return discount, final, nil
}

// orderStatusMessage 把订单当前状态翻译成「不能支付/不能取消」的提示文案。
func orderStatusMessage(status string) string {
	switch status {
	case model.OrderStatusPaid:
		return "订单已支付"
	case model.OrderStatusProvisioning:
		return "订单正在交付中"
	case model.OrderStatusActive:
		return "订单已交付"
	case model.OrderStatusFailed:
		return "订单交付失败"
	case model.OrderStatusCancelled:
		return "订单已取消"
	default:
		return "订单当前状态不允许该操作（" + status + "）"
	}
}

// retryDeliveryMessage 把订单当前状态翻译成「不能重试交付」的提示文案（契约 14.4）。
func retryDeliveryMessage(status string) string {
	switch status {
	case model.OrderStatusPending:
		return "订单尚未支付"
	case model.OrderStatusProvisioning:
		return "订单正在交付中，请勿重复触发"
	case model.OrderStatusActive:
		return "订单已交付完成，无需重试"
	case model.OrderStatusCancelled:
		return "订单已取消"
	default:
		return "订单当前状态不允许重试交付（" + status + "）"
	}
}
