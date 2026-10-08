package settings

import (
	"encoding/json"
	"fmt"
	"strings"
)

// 上游超时取值边界（与阶段 2 config.yaml 时代的约定保持一致）。
const (
	// DefaultUpstreamTimeoutSeconds 单次上游请求缺省超时（秒）。
	DefaultUpstreamTimeoutSeconds = 5
	// MinUpstreamTimeoutSeconds 上游请求超时下限（秒）。
	MinUpstreamTimeoutSeconds = 1
	// MaxUpstreamTimeoutSeconds 上游请求超时上限（秒）。
	MaxUpstreamTimeoutSeconds = 120
)

// Upstream 是 upstream 设置的取值结构（上游「魔方财务系统」对接参数，契约见 8.6 / 12.1）。
//
// 与调用方的关系：路由层把它映射成 upstream.Config（避免本包反向依赖 upstream 包）。
// APIKey 是上游「安全中心 → API」生成的密钥，只进数据库与内存，对外一律掩码。
type Upstream struct {
	// BaseURL 上游地址（含 http(s)://，不带结尾斜杠），如 https://lyew.com。
	BaseURL string `json:"base_url"`
	// Username 上游账号（**必须是注册手机号**：上游对 username 有 4-20 字符校验，见 8.5）。
	Username string `json:"username"`
	// APIKey 上游 API 密钥（12 位随机串，不是登录密码）。
	APIKey string `json:"api_key"`
	// TimeoutSeconds 单次上游请求超时（秒），缺省 5，取值 1-120。
	TimeoutSeconds int `json:"timeout_seconds"`
}

// UpstreamUpdate 是 PUT /api/v1/admin/settings/upstream 的更新意图：nil 表示不修改。
// APIKey 三态语义与支付 key 一致：省略 = 保持、新值 = 替换、空串 = 清空。
type UpstreamUpdate struct {
	BaseURL        *string
	Username       *string
	APIKey         *string
	TimeoutSeconds *int
}

// Missing 返回已开始配置但缺失的字段名（三项全空 = 完全未配置，返回 nil）。
func (u Upstream) Missing() []string {
	if u.BaseURL == "" && u.Username == "" && u.APIKey == "" {
		return nil
	}
	var missing []string
	if u.BaseURL == "" {
		missing = append(missing, "base_url")
	}
	if u.Username == "" {
		missing = append(missing, "username")
	}
	if u.APIKey == "" {
		missing = append(missing, "api_key")
	}
	return missing
}

// Configured 判断上游是否配置齐全（地址 + 账号 + 密钥）。
func (u Upstream) Configured() bool {
	return u.BaseURL != "" && u.Username != "" && u.APIKey != ""
}

// TimeoutSecondsOr 返回生效的超时秒数（<=0 时取缺省值）。
func (u Upstream) TimeoutSecondsOr() int {
	if u.TimeoutSeconds <= 0 {
		return DefaultUpstreamTimeoutSeconds
	}
	return u.TimeoutSeconds
}

// Apply 把更新意图合并到当前值并整体校验（上游允许「配一半」，便于管理员分步填写；
// 未配置齐全时上游功能按未配置处理，其余功能不受影响）。
func (u Upstream) Apply(upd UpstreamUpdate) (Upstream, error) {
	merged := u
	if upd.BaseURL != nil {
		merged.BaseURL = strings.TrimSpace(*upd.BaseURL)
	}
	if upd.Username != nil {
		merged.Username = strings.TrimSpace(*upd.Username)
	}
	if upd.APIKey != nil {
		merged.APIKey = strings.TrimSpace(*upd.APIKey)
	}
	if upd.TimeoutSeconds != nil {
		merged.TimeoutSeconds = *upd.TimeoutSeconds
	}
	if err := merged.Validate(); err != nil {
		return Upstream{}, err
	}
	return merged, nil
}

// Validate 校验取值：base_url 必须是合法 http(s)（留空合法）；超时必须在 1-120 之间。
func (u Upstream) Validate() error {
	if err := validateHTTPURL("base_url", u.BaseURL); err != nil {
		return err
	}
	if u.TimeoutSeconds < MinUpstreamTimeoutSeconds || u.TimeoutSeconds > MaxUpstreamTimeoutSeconds {
		return fmt.Errorf("%w: timeout_seconds 需为 %d-%d 之间的整数，收到 %d",
			ErrRule, MinUpstreamTimeoutSeconds, MaxUpstreamTimeoutSeconds, u.TimeoutSeconds)
	}
	return nil
}

// Encode 返回落库用的 JSON 文本。
func (u Upstream) Encode() (string, error) { return encode(u) }

// decodeUpstream 解析库内 JSON；空值按未配置处理（超时取缺省）。
func decodeUpstream(raw string) (Upstream, error) {
	value := Upstream{TimeoutSeconds: DefaultUpstreamTimeoutSeconds}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "null" {
		return value, nil
	}
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return Upstream{}, fmt.Errorf("%w: %s 的值不是合法 JSON：%v", ErrCorrupt, KeyUpstream, err)
	}
	value.BaseURL = strings.TrimSpace(value.BaseURL)
	value.Username = strings.TrimSpace(value.Username)
	value.APIKey = strings.TrimSpace(value.APIKey)
	value.TimeoutSeconds = value.TimeoutSecondsOr()
	return value, nil
}
