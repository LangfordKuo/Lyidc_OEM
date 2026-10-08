package settings

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Epay 是 payment.epay 设置的取值结构（易支付渠道参数，契约见 12.1，协议见 12.2）。
//
// 与调用方的关系：路由层把它映射成 payment.EpayConfig（避免本包反向依赖 payment 包）。
// Key 是商户密钥，只进数据库与内存，对外一律掩码。
type Epay struct {
	// Enabled 是否启用易支付渠道；启用时 gateway / pid / key / notify_url 必须齐全。
	Enabled bool `json:"enabled"`
	// Gateway 渠道网关地址（如 https://pay.example.com，不带结尾斜杠）。
	Gateway string `json:"gateway"`
	// PID 商户 ID。
	PID string `json:"pid"`
	// Key 商户密钥（签名用）。
	Key string `json:"key"`
	// NotifyURL 异步回调完整地址（推荐 http(s)://<系统域名>/api/v1/payments/epay/notify）。
	NotifyURL string `json:"notify_url"`
	// ReturnURL 同步跳转目标（前端页面），可空；空时本服务输出默认提示页。
	ReturnURL string `json:"return_url"`
}

// EpayUpdate 是 PUT /api/v1/admin/settings/payment/epay 的更新意图：nil 表示不修改。
//
// Key 用 *string 承接三态语义（契约 12.1）：键缺席（nil）= 保持不变、
// 提供新值 = 替换、空串 = 清空。
type EpayUpdate struct {
	Enabled   *bool
	Gateway   *string
	PID       *string
	Key       *string
	NotifyURL *string
	ReturnURL *string
}

// Missing 返回启用渠道时不能为空的字段名（按契约固定顺序）。
func (e Epay) Missing() []string {
	var missing []string
	if e.Gateway == "" {
		missing = append(missing, "gateway")
	}
	if e.PID == "" {
		missing = append(missing, "pid")
	}
	if e.Key == "" {
		missing = append(missing, "key")
	}
	if e.NotifyURL == "" {
		missing = append(missing, "notify_url")
	}
	return missing
}

// Usable 判断渠道当前是否可用：已启用且必填项齐全。
func (e Epay) Usable() bool { return e.Enabled && len(e.Missing()) == 0 }

// Apply 把更新意图合并到当前值，并对合并结果做整体校验。
// 校验失败返回 ErrFormat / ErrRule（上层映射 40001 / 40002），此时设置保持不变。
func (e Epay) Apply(upd EpayUpdate) (Epay, error) {
	merged := e
	if upd.Enabled != nil {
		merged.Enabled = *upd.Enabled
	}
	if upd.Gateway != nil {
		merged.Gateway = strings.TrimSpace(*upd.Gateway)
	}
	if upd.PID != nil {
		merged.PID = strings.TrimSpace(*upd.PID)
	}
	if upd.Key != nil {
		merged.Key = strings.TrimSpace(*upd.Key)
	}
	if upd.NotifyURL != nil {
		merged.NotifyURL = strings.TrimSpace(*upd.NotifyURL)
	}
	if upd.ReturnURL != nil {
		merged.ReturnURL = strings.TrimSpace(*upd.ReturnURL)
	}
	if err := merged.Validate(); err != nil {
		return Epay{}, err
	}
	return merged, nil
}

// Validate 校验取值：URL 字段必须是合法 http(s)（留空表示未配置，合法）；
// enabled=true 时 gateway / pid / key / notify_url 缺一即拒绝（ErrRule，消息列出缺失项）。
func (e Epay) Validate() error {
	if err := validateHTTPURL("gateway", e.Gateway); err != nil {
		return err
	}
	if err := validateHTTPURL("notify_url", e.NotifyURL); err != nil {
		return err
	}
	if err := validateHTTPURL("return_url", e.ReturnURL); err != nil {
		return err
	}
	if !e.Enabled {
		return nil
	}
	if missing := e.Missing(); len(missing) > 0 {
		return fmt.Errorf("%w: enabled=true 时以下字段不能为空：%s", ErrRule, strings.Join(missing, "、"))
	}
	return nil
}

// Encode 返回落库用的 JSON 文本。
func (e Epay) Encode() (string, error) { return encode(e) }

// decodeEpay 解析库内 JSON；空值按未配置处理。
func decodeEpay(raw string) (Epay, error) {
	var value Epay
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "null" {
		return value, nil
	}
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return Epay{}, fmt.Errorf("%w: %s 的值不是合法 JSON：%v", ErrCorrupt, KeyPaymentEpay, err)
	}
	value.Gateway = strings.TrimSpace(value.Gateway)
	value.PID = strings.TrimSpace(value.PID)
	value.Key = strings.TrimSpace(value.Key)
	value.NotifyURL = strings.TrimSpace(value.NotifyURL)
	value.ReturnURL = strings.TrimSpace(value.ReturnURL)
	return value, nil
}
