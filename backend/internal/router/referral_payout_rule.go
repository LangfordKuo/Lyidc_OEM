package router

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
)

// 出账申请（提现 / 转余额）的字段约束与敏感数据脱敏纯逻辑（契约 18.1.1 / 18.5）。
//
// 错误分类沿用既有口径：格式类 → 40001（pricing.ErrFormat），
// 规则类（取值合法但违反范围/状态机）→ 40002（errPayoutRule）。

// errPayoutRule 表示出账申请规则不成立（对应 40002）。
var errPayoutRule = errors.New("出账申请规则不成立")

// 出账申请字段长度约束（对应 referral_payouts 列宽）。
const (
	maxPayoutAccountRunes = 64
	maxPayoutRemarkRunes  = 255
)

// validatePayoutAmount 校验申请金额：格式（非负十进制、最多两位小数）与范围（> 0）。
// 返回整数分。格式非法 → 40001；≤ 0 → 40002。
func validatePayoutAmount(value string) (int64, error) {
	cents, err := pricing.ParseAmount(value)
	if err != nil {
		return 0, fmt.Errorf("%w: 金额格式不正确（需为十进制数，最多两位小数）：%q",
			pricing.ErrFormat, value)
	}
	if cents <= 0 {
		return 0, fmt.Errorf("%w: 申请金额必须大于 0，收到 %q", errPayoutRule, value)
	}
	return cents, nil
}

// validatePayoutAccount 校验敏感账号字段（收款人姓名 / 收款账号）：
// 去首尾空白后必须非空，且按 rune 计不超过 64。
func validatePayoutAccount(field, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%w: %s 不能为空", pricing.ErrFormat, field)
	}
	if utf8.RuneCountInString(trimmed) > maxPayoutAccountRunes {
		return "", fmt.Errorf("%w: %s 不能超过 %d 个字符", pricing.ErrFormat, field, maxPayoutAccountRunes)
	}
	return trimmed, nil
}

// validatePayoutRemark 校验备注长度（按 rune 计，可空）。
func validatePayoutRemark(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if utf8.RuneCountInString(trimmed) > maxPayoutRemarkRunes {
		return "", fmt.Errorf("%w: 备注不能超过 %d 个字符", pricing.ErrFormat, maxPayoutRemarkRunes)
	}
	return trimmed, nil
}

// validatePayoutChannel 校验到账渠道（仅 kind=withdraw）。
func validatePayoutChannel(channel string) (string, error) {
	trimmed := strings.ToLower(strings.TrimSpace(channel))
	switch trimmed {
	case "wechat", "alipay":
		return trimmed, nil
	default:
		return "", fmt.Errorf("%w: channel 只能是 wechat 或 alipay，收到 %q", pricing.ErrFormat, channel)
	}
}

// ── 敏感数据脱敏（契约 18.5「敏感数据脱敏口径」）─────────────────────────
//
// 对齐仓库既有脱敏范式（SMTP 口令：只进库、对外一律掩码）：
// 库内存原文（线下打款要核对收款人），接口回显 / 日志 / 审计一律走这里。

// maskAccountName 脱敏收款人姓名：姓氏（首个 rune）保留 + "**"；单字名整体 "**"；空串返回空串。
// 例：「张三」→「张**」、「张」→「**」、"" → ""。
func maskAccountName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) < 2 {
		return "**"
	}
	return string(runes[0]) + "**"
}

// maskAccountNo 脱敏收款账号：
//   - 长度 ≥ 6：前 2 位 + "****" + 后 2 位；
//   - 2 ≤ 长度 < 6：首 1 位 + "****" + 末 1 位；
//   - 长度 < 2：整体 "****"。
//
// 例：「6222021234567890」→「62****90」、「6222」→「6****2」、「6」→「****」。
func maskAccountNo(no string) string {
	trimmed := strings.TrimSpace(no)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	switch {
	case len(runes) >= 6:
		return string(runes[:2]) + "****" + string(runes[len(runes)-2:])
	case len(runes) >= 2:
		return string(runes[:1]) + "****" + string(runes[len(runes)-1:])
	default:
		return "****"
	}
}

// maskedAccountNote 生成只含脱敏信息的备注片段（日志/审计用，绝不含原文）。
func maskedAccountNote(channel, accountName, accountNo string) string {
	return fmt.Sprintf("渠道 %s，收款人 %s，账号 %s",
		channel, maskAccountName(accountName), maskAccountNo(accountNo))
}

// ── 状态机 ────────────────────────────────────────────────────────────────

// payoutAction 是管理端审核动作枚举（契约 18.5）。
type payoutAction string

const (
	payoutActionApprove payoutAction = "approve"
	payoutActionReject  payoutAction = "reject"
)

// parsePayoutAction 解析审核动作（仅 approve / reject）。
func parsePayoutAction(raw string) (payoutAction, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(payoutActionApprove):
		return payoutActionApprove, nil
	case string(payoutActionReject):
		return payoutActionReject, nil
	default:
		return "", fmt.Errorf("%w: action 只能是 approve 或 reject，收到 %q", pricing.ErrFormat, raw)
	}
}

// payoutStatusText 把状态机冲突翻译成可读 message（含当前状态）。
func payoutStatusText(status string) string {
	switch status {
	case "pending":
		return "待审核"
	case "approved":
		return "已通过"
	case "rejected":
		return "已驳回"
	case "paid":
		return "已打款"
	default:
		return status
	}
}

// payoutStateConflictMessage 组装状态机冲突的拒绝 message（40002）。
func payoutStateConflictMessage(current, attempted string) string {
	return fmt.Sprintf("当前状态为「%s」，无法执行「%s」",
		payoutStatusText(current), attempted)
}
