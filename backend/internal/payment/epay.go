package payment

import (
	"context"
	"crypto/md5"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// 易支付（彩虹标准协议）渠道路径与固定取值。
const (
	// ProviderEpay 是易支付渠道标识（配置段 payment.epay、接口 channel=epay）。
	ProviderEpay = "epay"
	// PayTypeAlipay 支付宝。
	PayTypeAlipay = "alipay"
	// PayTypeWxpay 微信支付。
	PayTypeWxpay = "wxpay"

	// epayCreatePath 是统一下单接口路径（相对 gateway）。
	epayCreatePath = "/mapi.php"
	// epaySignType 是签名算法标识。
	epaySignType = "MD5"
	// epayTradeSuccess 是「支付成功」状态原文。
	epayTradeSuccess = "TRADE_SUCCESS"

	// AckSuccess 是回调处理成功的应答正文（易支付协议要求纯文本）。
	AckSuccess = "success"
	// AckFail 是回调处理失败的应答正文（渠道会按自己的策略重试）。
	AckFail = "fail"

	// epayMaxResponseBytes 是渠道响应读取上限（防异常大响应）。
	epayMaxResponseBytes = 64 << 10
)

// EpayConfig 是易支付渠道配置（由路由层从后台设置 payment.epay 键映射而来，
// 避免本包反向依赖 settings 包；每次取用渠道时按当前设置重新构造）。
type EpayConfig struct {
	// Gateway 渠道网关地址（如 https://pay.example.com，不带结尾斜杠）。
	Gateway string
	// PID 商户 ID。
	PID string
	// Key 商户密钥：只存在于设置表与本进程内存，禁止进日志/响应/入库文件。
	Key string
	// NotifyURL 异步回调完整地址（本服务的 /api/v1/payments/epay/notify）。
	NotifyURL string
	// ReturnURL 同步跳转的目标地址（前端页面）；可空，空时本服务输出默认提示页。
	ReturnURL string
}

// MaskedKey 返回脱敏后的商户密钥，用于日志。
func (c EpayConfig) MaskedKey() string {
	if len(c.Key) <= 4 {
		return "****"
	}
	return c.Key[:2] + "****" + c.Key[len(c.Key)-2:]
}

// Epay 是易支付渠道实现（彩虹易支付标准协议）。
//
// 协议要点（契约见 docs/api-contract.md 12.2）：
//   - 下单：POST {gateway}/mapi.php，form 参数 pid/type/out_trade_no/notify_url/return_url/name/money/sign/sign_type；
//     成功返回 JSON {"code":1,"trade_no":"…","payurl":"…"}；
//   - 签名：非空业务参数按参数名 ASCII 升序拼成 a=1&b=2，末尾拼接商户 KEY，MD5 取小写（下单与回调共用 Sign）；
//   - 回调：GET/POST 均可；校验 pid、sign_type、验签；处理成功应答纯文本 success，否则 fail。
type Epay struct {
	cfg    EpayConfig
	client *http.Client
	logger *slog.Logger
}

// NewEpay 构造易支付渠道；client 为 nil 时使用 http.DefaultClient。
func NewEpay(cfg EpayConfig, client *http.Client, logger *slog.Logger) *Epay {
	if client == nil {
		client = http.DefaultClient
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Epay{cfg: cfg, client: client, logger: logger}
}

// Name 实现 Provider。
func (p *Epay) Name() string { return ProviderEpay }

// PayTypes 实现 Provider：易支付以 type 参数选择支付方式，本批支持支付宝与微信。
func (p *Epay) PayTypes() []string { return []string{PayTypeAlipay, PayTypeWxpay} }

// Ack 实现 Provider：易支付协议要求应答纯文本 success / fail。
func (p *Epay) Ack(ok bool) string {
	if ok {
		return AckSuccess
	}
	return AckFail
}

// CreateOrder 实现 Provider：调用 mapi.php 下单并解析 payurl。
func (p *Epay) CreateOrder(ctx context.Context, req CreateRequest) (*CreateResult, error) {
	payType := strings.ToLower(strings.TrimSpace(req.PayType))
	if payType == "" {
		payType = PayTypeAlipay
	}
	if !p.supportsPayType(payType) {
		return nil, fmt.Errorf("%w: %s 支持 %s", ErrPayTypeUnsupported, ProviderEpay, strings.Join(p.PayTypes(), " / "))
	}
	if strings.TrimSpace(req.OutTradeNo) == "" || strings.TrimSpace(req.Amount) == "" {
		return nil, fmt.Errorf("%w: 缺少 out_trade_no 或 money", ErrGatewayRejected)
	}

	// 签名覆盖的参数集（sign / sign_type 不参与签名）。
	params := map[string]string{
		"pid":          p.cfg.PID,
		"type":         payType,
		"out_trade_no": req.OutTradeNo,
		"notify_url":   p.cfg.NotifyURL,
		"return_url":   p.returnURL(),
		"name":         req.Subject,
		"money":        req.Amount,
	}
	params["sign"] = Sign(params, p.cfg.Key)
	params["sign_type"] = epaySignType

	form := url.Values{}
	for key, value := range params {
		if value != "" {
			form.Set(key, value)
		}
	}

	endpoint := strings.TrimRight(strings.TrimSpace(p.cfg.Gateway), "/") + epayCreatePath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("构造渠道下单请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("渠道下单请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, epayMaxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("读取渠道响应失败: %w", err)
	}
	result, err := parseEpayCreateResponse(body)
	if err != nil {
		// 响应原文可能很长，只记前 200 字节便于排查。
		p.logger.Warn("易支付下单响应异常", "status", resp.StatusCode, "body", truncateForLog(body))
		return nil, err
	}
	result.PayType = payType
	return result, nil
}

// ParseNotify 实现 Provider：解析回调参数并验签。
//
// 校验顺序：必要参数齐全 → pid 与本地配置一致 → sign_type 为 MD5（可省略）→ 验签。
// trade_status 是否成功不在本方法判定（由 Notification.Paid 交调用方处理，便于统一告警）。
func (p *Epay) ParseNotify(r *http.Request) (*Notification, error) {
	if err := r.ParseForm(); err != nil {
		return nil, fmt.Errorf("回调参数解析失败: %w", err)
	}
	params := make(map[string]string, len(r.Form))
	for key := range r.Form {
		params[key] = r.Form.Get(key)
	}

	for _, key := range []string{"pid", "out_trade_no", "trade_no", "money", "trade_status", "sign"} {
		if strings.TrimSpace(params[key]) == "" {
			return nil, fmt.Errorf("回调缺少必要参数 %s", key)
		}
	}
	if params["pid"] != p.cfg.PID {
		return nil, errors.New("回调 pid 与本地配置不一致")
	}
	if signType := strings.ToLower(strings.TrimSpace(params["sign_type"])); signType != "" && signType != "md5" {
		return nil, fmt.Errorf("不支持的签名类型 %q", params["sign_type"])
	}
	if !VerifySign(params, p.cfg.Key) {
		return nil, errors.New("回调验签失败")
	}

	return &Notification{
		OutTradeNo:  params["out_trade_no"],
		TradeNo:     params["trade_no"],
		Amount:      params["money"],
		PayType:     params["type"],
		Subject:     params["name"],
		TradeStatus: params["trade_status"],
		Paid:        params["trade_status"] == epayTradeSuccess,
	}, nil
}

// Sign 计算易支付签名（下单与回调验签共用的单一实现）。
//
// 算法（彩虹易支付标准协议）：
//  1. 取全部业务参数（剔除 sign、sign_type），并跳过值为空的参数；
//  2. 按参数名 ASCII 升序排序，拼成 a=1&b=2 形式（原值直接拼接，不做 URL 编码）；
//  3. 末尾直接拼接商户 KEY（不加密钥名），MD5 后取小写十六进制。
func Sign(params map[string]string, key string) string {
	names := make([]string, 0, len(params))
	for name, value := range params {
		if name == "sign" || name == "sign_type" || value == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	var builder strings.Builder
	for index, name := range names {
		if index > 0 {
			builder.WriteByte('&')
		}
		builder.WriteString(name)
		builder.WriteByte('=')
		builder.WriteString(params[name])
	}
	builder.WriteString(key)

	sum := md5.Sum([]byte(builder.String()))
	return hex.EncodeToString(sum[:])
}

// VerifySign 校验回调签名；比较使用常量时间，大小写不敏感（签名统一输出小写）。
func VerifySign(params map[string]string, key string) bool {
	claimed := strings.ToLower(strings.TrimSpace(params["sign"]))
	if claimed == "" {
		return false
	}
	expected := Sign(params, key)
	return subtle.ConstantTimeCompare([]byte(claimed), []byte(expected)) == 1
}

// supportsPayType 判断支付方式是否受支持。
func (p *Epay) supportsPayType(payType string) bool {
	for _, item := range p.PayTypes() {
		if item == payType {
			return true
		}
	}
	return false
}

// returnURL 返回传给渠道的同步跳转地址（本服务的 return 端点）：
// 从 notify_url 推导——路径以 /notify 结尾时换成 /return，否则取 notify_url 的 scheme+host + 标准 return 路径；
// notify_url 为空时返回空串（不传 return_url，用户支付后停留在渠道页）。
func (p *Epay) returnURL() string {
	notify := strings.TrimSpace(p.cfg.NotifyURL)
	if notify == "" {
		return ""
	}
	parsed, err := url.Parse(notify)
	if err != nil || parsed.Host == "" {
		return ""
	}
	path := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(path, "/notify") {
		parsed.Path = strings.TrimSuffix(path, "/notify") + "/return"
	} else {
		parsed.Path = "/api/v1/payments/epay/return"
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

// epayCreateResponse 是 mapi.php 的响应体；code 在部分实现里是字符串，故用宽松类型。
type epayCreateResponse struct {
	Code      flexibleString `json:"code"`
	Msg       string         `json:"msg"`
	TradeNo   string         `json:"trade_no"`
	PayURL    string         `json:"payurl"`
	QRCode    string         `json:"qrcode"`
	URLScheme string         `json:"urlscheme"`
}

// flexibleString 兼容 JSON 里数字与字符串两种写法（如 code: 1 与 code: "1"）。
type flexibleString string

// UnmarshalJSON 实现 json.Unmarshaler。
func (f *flexibleString) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "null" {
		*f = ""
		return nil
	}
	if strings.HasPrefix(trimmed, `"`) {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		*f = flexibleString(text)
		return nil
	}
	number, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return fmt.Errorf("无法解析字段值 %s", trimmed)
	}
	*f = flexibleString(strconv.FormatFloat(number, 'f', -1, 64))
	return nil
}

// parseEpayCreateResponse 解析下单响应：code=1 且带 payurl 才算成功。
func parseEpayCreateResponse(body []byte) (*CreateResult, error) {
	var parsed epayCreateResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("%w: 响应不是合法 JSON", ErrGatewayResponse)
	}
	if string(parsed.Code) != "1" {
		message := strings.TrimSpace(parsed.Msg)
		if message == "" {
			message = "渠道未返回错误信息"
		}
		return nil, fmt.Errorf("%w: %s", ErrGatewayRejected, message)
	}
	if strings.TrimSpace(parsed.PayURL) == "" {
		return nil, fmt.Errorf("%w: 渠道未返回支付链接（payurl）", ErrGatewayResponse)
	}

	extra := make(map[string]string, 2)
	if parsed.QRCode != "" {
		extra["qrcode"] = parsed.QRCode
	}
	if parsed.URLScheme != "" {
		extra["urlscheme"] = parsed.URLScheme
	}
	return &CreateResult{
		TradeNo: parsed.TradeNo,
		PayURL:  parsed.PayURL,
		Extra:   extra,
	}, nil
}

// truncateForLog 截断响应正文用于日志。
func truncateForLog(body []byte) string {
	const limit = 200
	if len(body) <= limit {
		return string(body)
	}
	return string(body[:limit]) + "…"
}
