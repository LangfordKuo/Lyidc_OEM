package router

import (
	"context"
	"log/slog"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/payment"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upstream"
)

// newPaymentRegistry 构造支付渠道注册表，并登记本批唯一渠道：易支付。
//
// 登记的是**工厂**而非固定实例：每次取用都按后台设置（settings 表 payment.epay 键）重新解析，
// 因此管理员在后台改完参数立即生效，无需重启（契约 12.1）。
// 未启用或配置不完整时工厂返回 payment.ErrNotConfigured。
func newPaymentRegistry(reader *settings.Reader, logger *slog.Logger) *payment.Registry {
	registry := payment.NewRegistry()
	registry.Register(payment.ProviderEpay, func(ctx context.Context) (payment.Provider, error) {
		state, err := reader.Epay(ctx)
		if err != nil {
			return nil, err
		}
		if !state.Value.Usable() {
			return nil, payment.ErrNotConfigured
		}
		return payment.NewEpay(payment.EpayConfig{
			Gateway:   state.Value.Gateway,
			PID:       state.Value.PID,
			Key:       state.Value.Key,
			NotifyURL: state.Value.NotifyURL,
			ReturnURL: state.Value.ReturnURL,
		}, nil, logger), nil
	})
	return registry
}

// newUpstreamProvider 构造按后台设置动态生效的上游客户端提供者（契约 8.6 / 12.1）：
// 设置指纹变化时重建客户端并丢弃旧 JWT 缓存，保证下一次调用用新参数。
func newUpstreamProvider(reader *settings.Reader, logger *slog.Logger) upstream.Provider {
	return upstream.NewManager(func(ctx context.Context) (upstream.Config, string, error) {
		state, err := reader.Upstream(ctx)
		if err != nil {
			return upstream.Config{}, "", err
		}
		return upstream.Config{
			BaseURL:  state.Value.BaseURL,
			Username: state.Value.Username,
			APIKey:   state.Value.APIKey,
			Timeout:  time.Duration(state.Value.TimeoutSecondsOr()) * time.Second,
			Logger:   logger,
		}, state.Fingerprint, nil
	}, logger)
}
