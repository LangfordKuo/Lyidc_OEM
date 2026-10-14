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
//
// baseURL 是**构建期注入**的上游地址（R17，由 cmd/server 从 buildinfo 传入）：每次构造客户端
// 都用它，不从 settings 读、也不接受任何请求入参；设置指纹只覆盖 username / api_key / timeout。
// 未注入（空串）时客户端恒为 enabled=false，上游相关功能按「未配置」处理。
func newUpstreamProvider(reader *settings.Reader, baseURL string, logger *slog.Logger) upstream.Provider {
	if baseURL == "" {
		logger.Warn("未注入上游地址（构建期变量 UpstreamBaseURL 为空），上游相关功能将不可用",
			"setting", "buildinfo.UpstreamBaseURL")
	} else {
		logger.Info("上游地址由构建期注入（不可通过任何界面或接口修改）", "base_url", baseURL)
	}
	return upstream.NewManager(func(ctx context.Context) (upstream.Config, string, error) {
		state, err := reader.Upstream(ctx)
		if err != nil {
			return upstream.Config{}, "", err
		}
		return upstream.Config{
			BaseURL:  baseURL,
			Username: state.Value.Username,
			APIKey:   state.Value.APIKey,
			Timeout:  time.Duration(state.Value.TimeoutSecondsOr()) * time.Second,
			Logger:   logger,
		}, state.Fingerprint, nil
	}, logger)
}
