package upstream

import (
	"context"
	"log/slog"
	"sync"
)

// Loader 返回**当前设置**下的客户端配置与设置指纹（指纹一般为库内设置 JSON 原文）。
// 指纹变化即视为设置被管理员修改：Manager 会重建客户端，旧客户端的 JWT 缓存随之失效。
// 上游未配置时返回零值配置（BaseURL 等为空）即可，由 Manager 统一转成 enabled=false。
type Loader func(ctx context.Context) (Config, string, error)

// Provider 提供当前设置下的上游客户端（契约 8.6 / 12.1：后台改完设置，下一次调用即用新参数）。
// 路由层只依赖本接口，测试可用 StaticProvider 或自定义实现替代。
type Provider interface {
	// Current 返回当前设置下的客户端与「配置是否齐全」。
	// 客户端一定非 nil（设置读取/构造失败时返回错误）；enabled=false 表示未配置齐全，
	// 此时调用方不得发起业务调用，只能取 BaseURL / MaskedAPIKey 用于展示。
	Current(ctx context.Context) (*Client, bool, error)
}

// StaticProvider 提供固定客户端（测试与不支持动态设置的嵌入场景）。
type StaticProvider struct {
	C *Client
}

// Current 实现 Provider：C 为 nil 时返回 (nil, false, ErrNotConfigured)。
func (p StaticProvider) Current(context.Context) (*Client, bool, error) {
	if p.C == nil {
		return nil, false, ErrNotConfigured
	}
	return p.C, p.C.Enabled(), nil
}

// Manager 按设置动态提供上游客户端：
//
//   - 每次取用都调用 Loader 读一次设置（settings 表主键查询，开销极小）；
//   - 指纹与上次一致时复用现有客户端（**保留其 JWT 缓存**，避免每次调用都重新登录）；
//   - 指纹变化时丢弃旧客户端并重建（**旧 JWT 缓存一并失效**，保证下一次调用用新参数）；
//   - 未配置齐全时返回 enabled=false，其余功能不受影响。
//
// 因此不存在「启动时读一次」的陈旧配置，也没有短缓存窗口：后台改完立即生效。
type Manager struct {
	loader Loader
	logger *slog.Logger

	mu          sync.Mutex
	client      *Client
	fingerprint string
}

// NewManager 构造动态管理器；logger 为 nil 时使用 slog.Default()。
func NewManager(loader Loader, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{loader: loader, logger: logger}
}

// Current 实现 Provider。
func (m *Manager) Current(ctx context.Context) (*Client, bool, error) {
	if m == nil || m.loader == nil {
		return nil, false, ErrNotConfigured
	}
	cfg, fingerprint, err := m.loader(ctx)
	if err != nil {
		return nil, false, err
	}
	if cfg.Logger == nil {
		cfg.Logger = m.logger
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client != nil && m.fingerprint == fingerprint {
		return m.client, m.client.Enabled(), nil
	}

	client, err := New(cfg)
	if err != nil {
		return nil, false, err
	}
	if m.client != nil {
		m.logger.Info("上游设置已变更，重建客户端（旧登录态已失效）",
			"base_url", client.BaseURL(), "api_key", client.MaskedAPIKey())
	}
	m.client = client
	m.fingerprint = fingerprint
	return client, client.Enabled(), nil
}
