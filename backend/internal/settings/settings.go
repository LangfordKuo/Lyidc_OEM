// Package settings 承载「后台管理设置」：settings 表中按 key 存放的 JSON 设置值的
// 读取、解析、校验与更新意图合并（契约见 docs/api-contract.md 12.1）。
//
// 总原则（主控下发）：所有「用户可设置」的内容一律做进后台管理设置（数据库 + 管理端接口），
// 不让用户改任何配置文件；部署级参数（数据库连接、监听端口、JWT 密钥、日志等级）不在此列，
// 继续留在 config.yaml。
//
// 生效方式：**读时校验、按内容失效**——每次使用都读一次 settings 表（主键查询，开销极小），
// 内容与调用方缓存一致时复用（上游客户端据此保留 JWT 缓存），内容变化时立即用新值重建。
// 因此后台改完设置立即生效，既不需要重启，也没有「短缓存窗口」。
//
// 密钥安全：Key / APIKey 只存在于数据库与本进程内存；对外接口一律返回掩码
// （MaskSecret），任何日志、响应与错误信息都不含明文。
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// 设置键。本批承载两个；后续阶段的站点/邮件等设置直接在此追加。
const (
	// KeyPaymentEpay 易支付渠道参数（值结构见 Epay）。
	KeyPaymentEpay = "payment.epay"
	// KeyUpstream 上游「魔方财务系统」对接参数（值结构见 Upstream）。
	KeyUpstream = "upstream"
)

// 校验错误分类（与定价/优惠码的约定一致）：格式类 → 40001，规则类 → 40002。
var (
	// ErrFormat 取值格式不合法（URL 非 http(s)、字段类型不符等）。
	ErrFormat = errors.New("设置项格式非法")
	// ErrRule 取值格式合法但不满足业务规则（启用时缺必填项、超时越界）。
	ErrRule = errors.New("设置项规则不成立")
	// ErrCorrupt 库内设置值无法解析（只可能是绕过接口手工改库导致）。
	ErrCorrupt = errors.New("设置值无法解析")
)

// Source 是设置的读取来源（由 store.Store 实现）。
type Source interface {
	// Setting 按 key 读取一条设置；不存在时返回 store.ErrNotFound。
	Setting(ctx context.Context, key string) (*model.Setting, error)
}

// Meta 是一条设置的审计信息。
type Meta struct {
	// Exists 表示库中已有该键的记录（false 表示从未经接口写入，走缺省值）。
	Exists    bool
	UpdatedBy *uint64
	UpdatedAt *time.Time
}

// Reader 按 key 读取并解析设置。
type Reader struct {
	source Source
}

// NewReader 构造设置读取器。
func NewReader(source Source) *Reader { return &Reader{source: source} }

// EpayState 是 payment.epay 的读取结果。
type EpayState struct {
	Value Epay
	Meta  Meta
	// Fingerprint 是库内 JSON 原文（未配置为空串），供调用方判断「设置是否变化」。
	Fingerprint string
}

// UpstreamState 是 upstream 的读取结果。
type UpstreamState struct {
	Value Upstream
	Meta  Meta
	// Fingerprint 同上。
	Fingerprint string
}

// Epay 读取 payment.epay 设置；未配置时返回缺省值（Enabled=false）与空指纹。
func (r *Reader) Epay(ctx context.Context) (EpayState, error) {
	raw, meta, err := r.raw(ctx, KeyPaymentEpay)
	if err != nil {
		return EpayState{}, err
	}
	value, err := decodeEpay(raw)
	if err != nil {
		return EpayState{}, err
	}
	return EpayState{Value: value, Meta: meta, Fingerprint: raw}, nil
}

// Upstream 读取 upstream 设置；未配置时返回缺省值（超时取缺省 5 秒）与空指纹。
func (r *Reader) Upstream(ctx context.Context) (UpstreamState, error) {
	raw, meta, err := r.raw(ctx, KeyUpstream)
	if err != nil {
		return UpstreamState{}, err
	}
	value, err := decodeUpstream(raw)
	if err != nil {
		return UpstreamState{}, err
	}
	return UpstreamState{Value: value, Meta: meta, Fingerprint: raw}, nil
}

// raw 读取设置原文与审计信息；键不存在不算错误（返回空串 + Exists=false）。
func (r *Reader) raw(ctx context.Context, key string) (string, Meta, error) {
	if r == nil || r.source == nil {
		return "", Meta{}, errors.New("设置读取器未初始化")
	}
	setting, err := r.source.Setting(ctx, key)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return "", Meta{}, nil
	case err != nil:
		return "", Meta{}, err
	}
	return setting.Value, Meta{Exists: true, UpdatedBy: setting.UpdatedBy, UpdatedAt: &setting.UpdatedAt}, nil
}

// MaskSecret 返回密钥的掩码写法：前 4 位 + "****"。
// 密钥为空返回空串；不足 8 位时只返回 "****"（避免过短密钥被整个还原）。
func MaskSecret(secret string) string {
	if secret == "" {
		return ""
	}
	if len(secret) < 8 {
		return "****"
	}
	return secret[:4] + "****"
}

// validateHTTPURL 校验 URL 字段：留空表示未配置（合法）；非空时必须是带主机名的 http(s) 地址。
func validateHTTPURL(field, value string) error {
	if value == "" {
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("%w: %s 不是合法的 URL：%v", ErrFormat, field, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%w: %s 必须以 http:// 或 https:// 开头，收到 %q", ErrFormat, field, value)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%w: %s 缺少主机名，收到 %q", ErrFormat, field, value)
	}
	return nil
}

// encode 把设置值序列化为落库 JSON 文本（字段顺序由结构体固定，内容可比较）。
func encode(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("%w: 设置值序列化失败: %v", ErrCorrupt, err)
	}
	return string(raw), nil
}
