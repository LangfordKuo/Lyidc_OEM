package router

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
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
