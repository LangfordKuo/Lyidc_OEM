// Package payment 定义支付渠道抽象与注册表：业务代码只依赖 Provider 接口，
// 新增渠道 = 新增一个 Provider 实现 + 在注册表登记工厂 + 在后台设置里新增配置键
// （见 docs/api-contract.md 12.1「渠道可插拔」）。
//
// 本批实现唯一渠道：易支付（彩虹标准协议，见 epay.go）。渠道参数来自后台设置
// （settings 表 payment.epay 键），因此注册表登记的是**工厂**而不是固定实例：
// 每次取用都按当前设置构造，后台改完立即生效。
//
// 密钥安全：任何日志、接口响应与错误信息都不得包含渠道 KEY（错误里只带渠道返回的 msg 摘要）。
package payment

import (
	"context"
	"errors"
	"net/http"
	"sort"
)

// 渠道级哨兵错误，router 据此映射业务错误码与提示文案。
var (
	// ErrUnknownChannel 表示渠道未注册（本服务不支持该渠道）。
	ErrUnknownChannel = errors.New("支付渠道不存在")
	// ErrNotConfigured 表示渠道已注册但未启用或配置不完整（后台设置里没填全）。
	ErrNotConfigured = errors.New("支付渠道未启用或配置不完整")
	// ErrPayTypeUnsupported 表示渠道不支持请求的支付方式（pay_type）。
	ErrPayTypeUnsupported = errors.New("支付方式不受支持")
	// ErrGatewayRejected 表示渠道拒绝了本次下单（如签名错误、商户不存在）。
	ErrGatewayRejected = errors.New("支付渠道拒绝了本次下单")
	// ErrGatewayResponse 表示渠道响应无法解析或缺少必要字段。
	ErrGatewayResponse = errors.New("支付渠道响应异常")
)

// CreateRequest 是发起支付所需的最小业务参数（渠道地址、回调地址等由 Provider 自持的配置补齐）。
type CreateRequest struct {
	// OutTradeNo 是商户单号（本地订单号 / 充值单号，唯一）。
	OutTradeNo string
	// Amount 是定点金额字符串（如 "10.00"），两位小数。
	Amount string
	// Subject 是支付标题（商品名或「余额充值」）。
	Subject string
	// PayType 是渠道内支付方式（如 alipay / wxpay）；空串表示使用渠道默认值。
	PayType string
}

// CreateResult 是渠道下单结果。
type CreateResult struct {
	// TradeNo 是渠道单号。
	TradeNo string
	// PayURL 是支付跳转链接（渠道返回的 payurl）。
	PayURL string
	// PayType 是实际使用的支付方式。
	PayType string
	// Extra 是渠道返回的其他非空字段（如 qrcode / urlscheme），原样透传给调用方展示。
	Extra map[string]string
}

// Notification 是渠道异步回调的解析结果（签名已校验通过）。
type Notification struct {
	// OutTradeNo 是商户单号（本地订单号 / 充值单号）。
	OutTradeNo string
	// TradeNo 是渠道单号。
	TradeNo string
	// Amount 是渠道回传的支付金额（定点小数字符串），调用方必须与本地单金额比对。
	Amount string
	// PayType 是实际支付方式。
	PayType string
	// Subject 是渠道回传的支付标题。
	Subject string
	// TradeStatus 是渠道回传的状态原文（易支付：TRADE_SUCCESS / WAIT_BUYER_PAY 等），用于日志。
	TradeStatus string
	// Paid 表示渠道状态是否为「支付成功」（易支付：trade_status=TRADE_SUCCESS）。
	Paid bool
}

// Provider 是一个支付渠道实现。
type Provider interface {
	// Name 返回渠道标识（如 "epay"），与配置段名、接口里的 channel 参数一致。
	Name() string
	// PayTypes 返回渠道支持的支付方式；下单校验 pay_type 时使用。
	PayTypes() []string
	// CreateOrder 向渠道发起下单，返回支付链接。
	CreateOrder(ctx context.Context, req CreateRequest) (*CreateResult, error)
	// ParseNotify 解析并验签异步回调；验签失败或缺少必要参数时返回错误。
	ParseNotify(r *http.Request) (*Notification, error)
	// Ack 返回回调应答正文（易支付协议为纯文本 success / fail）。
	Ack(ok bool) string
}

// Factory 按**当前设置**构造渠道实例（如读取 settings 表的 payment.epay 键）。
// 未启用或配置不完整时返回 ErrNotConfigured。
type Factory func(ctx context.Context) (Provider, error)

// Static 把固定实例包装成工厂（测试与不支持动态设置的嵌入场景）。
func Static(provider Provider) Factory {
	return func(context.Context) (Provider, error) { return provider, nil }
}

// Registry 是渠道注册表。零值不可用，请用 NewRegistry 构造。
// 未注册的渠道返回 ErrUnknownChannel，已注册但设置未启用时由工厂返回
// ErrNotConfigured——两种情况都不影响其余功能（契约见 12.1）。
type Registry struct {
	factories map[string]Factory
}

// NewRegistry 构造空注册表，随后用 Register 登记各渠道工厂。
func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]Factory, 1)}
}

// Register 登记渠道工厂（name 必须与 Provider.Name() 一致）；重复登记以最后一次为准。
func (r *Registry) Register(name string, factory Factory) {
	if r == nil || factory == nil || name == "" {
		return
	}
	if r.factories == nil {
		r.factories = make(map[string]Factory, 1)
	}
	r.factories[name] = factory
}

// Get 按名称取当前设置下的渠道实例。
// 渠道未注册返回 ErrUnknownChannel；工厂返回的错误（如 ErrNotConfigured）原样回传。
func (r *Registry) Get(ctx context.Context, name string) (Provider, error) {
	if r == nil {
		return nil, ErrUnknownChannel
	}
	factory, ok := r.factories[name]
	if !ok {
		return nil, ErrUnknownChannel
	}
	provider, err := factory(ctx)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, ErrNotConfigured
	}
	return provider, nil
}

// Has 判断渠道是否已登记（不论当前设置是否启用）。
func (r *Registry) Has(name string) bool {
	if r == nil {
		return false
	}
	_, ok := r.factories[name]
	return ok
}

// Names 返回已登记的渠道名（升序），便于日志与文档。
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
