// Package upstream 封装对上游「魔方财务系统」（智简魔方）开放 API 的调用。
//
// 鉴权与响应格式（实测结论见 docs/api-contract.md 第 8 节）：
//
//	第一步  POST {base_url}/zjmf_api_login 表单 {username, password=API 密钥} → {status:200, jwt}
//	第二步  后续请求带 Authorization: Bearer <jwt>，上游以服务端缓存二次校验 JWT
//	失效时  上游返回 {"status":405,"msg":"请登陆后再试"} → 本包自动重新登录并原样重放一次
//
// 设计约束：
//  1. 上游即便业务失败也返回 HTTP 200，成败一律以 body 的 status 判定；
//  2. 只有幂等读接口（GET）会自动重试网络类错误，写操作绝不自动重试；
//  3. 自动重试仅覆盖网络/超时/HTTP 异常，业务失败（400/406）直接返回给调用方；
//  4. 密钥只以脱敏形式进入日志。
package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// 缺省参数。
const (
	// DefaultTimeout 是单次上游请求的超时（含连接、读响应）。
	DefaultTimeout = 5 * time.Second
	// DefaultMaxRetries 是幂等读接口在网络类错误下的额外重试次数。
	DefaultMaxRetries = 2
	// DefaultRetryBackoff 是重试的基础退避间隔（第 n 次重试 = n * 该值）。
	DefaultRetryBackoff = 200 * time.Millisecond
	// maxResponseBytes 限制单次响应体大小，避免上游异常时把内存吃满（商品接口实测约 180KB）。
	maxResponseBytes = 16 << 20
	// userAgent 标识调用方，便于上游排查。
	userAgent = "Lyidc_OEM/1.0 (+upstream-client)"

	// 上游业务状态码。
	statusOK         = 200
	statusPaid       = 1001 // /apply_credit 余额支付成功
	statusNotLogged  = 405  // 未登录或 JWT 失效
	pathLogin        = "/zjmf_api_login"
	pathCartClear    = "/cart/clear"
	pathCartAddShop  = "/cart/add_to_shop"
	pathCartSettle   = "/cart/settle"
	pathApplyCredit  = "/apply_credit"
	pathHostRenew    = "/host/renew"
	pathProvisionDef = "/provision/default"
	pathProvisionBtn = "/provision/button"
)

// Config 是上游客户端配置，字段与 config.yaml 的 upstream 段落对应。
type Config struct {
	// BaseURL 上游地址，如 https://lyew.com（缺省从配置读取，代码里不做任何硬编码）。
	BaseURL string
	// Username 上游账号（手机号或邮箱；上游对该字段有 4-20 字符长度校验）。
	Username string
	// APIKey 上游「安全中心 → API」生成的密钥，仅在换取 JWT 时使用一次。
	APIKey string
	// Timeout 单次请求超时，<=0 时取 DefaultTimeout。
	Timeout time.Duration
	// MaxRetries 幂等读接口的重试次数，<0 视为 0，0 表示使用 DefaultMaxRetries。
	MaxRetries int
	// RetryBackoff 重试退避间隔，<=0 时取 DefaultRetryBackoff。
	RetryBackoff time.Duration
	// Logger 缺省使用 slog.Default()。
	Logger *slog.Logger
}

// Client 是上游 API 客户端，内部缓存 JWT，可安全并发使用。
type Client struct {
	cfg    Config
	http   *http.Client
	logger *slog.Logger

	mu  sync.Mutex
	jwt string
}

// New 创建客户端。BaseURL 非法（缺 scheme/host）时返回错误；
// 账号或密钥留空不报错，但调用业务方法会返回 ErrNotConfigured（便于「未配置上游」时仍能启动服务）。
func New(cfg Config) (*Client, error) {
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.Username = strings.TrimSpace(cfg.Username)
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)

	if cfg.BaseURL != "" {
		parsed, err := url.Parse(cfg.BaseURL)
		if err != nil {
			return nil, fmt.Errorf("upstream: base_url 非法: %w", err)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, fmt.Errorf("upstream: base_url 必须以 http:// 或 https:// 开头: %q", cfg.BaseURL)
		}
		if parsed.Host == "" {
			return nil, fmt.Errorf("upstream: base_url 缺少主机名: %q", cfg.BaseURL)
		}
	}

	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = DefaultRetryBackoff
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = DefaultMaxRetries
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &Client{
		cfg:    cfg,
		http:   &http.Client{Timeout: cfg.Timeout},
		logger: cfg.Logger,
	}, nil
}

// BaseURL 返回上游地址（非密，可直接展示）。
func (c *Client) BaseURL() string { return c.cfg.BaseURL }

// MaskedAPIKey 返回脱敏后的 API 密钥，用于日志与探活响应。
func (c *Client) MaskedAPIKey() string { return MaskAPIKey(c.cfg.APIKey) }

// Enabled 判断上游是否已配置齐全（地址 + 账号 + 密钥）。
func (c *Client) Enabled() bool {
	return c.cfg.BaseURL != "" && c.cfg.Username != "" && c.cfg.APIKey != ""
}

// Login 向上游换取 JWT 并缓存。成功返回 nil；鉴权被拒返回 AuthError（errors.Is(err, ErrAuth)）。
func (c *Client) Login(ctx context.Context) (*Response, error) {
	if !c.Enabled() {
		return nil, ErrNotConfigured
	}

	form := url.Values{}
	form.Set("username", c.cfg.Username)
	form.Set("password", c.cfg.APIKey)

	resp, err := c.request(ctx, http.MethodPost, pathLogin, form, "")
	if err != nil {
		return nil, err
	}
	if resp.Status != statusOK {
		return nil, &AuthError{API: pathLogin, Msg: resp.Msg}
	}
	if strings.TrimSpace(resp.JWT) == "" {
		return nil, fmt.Errorf("%w: 登录成功但未返回 jwt (api=%s)", ErrUpstream, pathLogin)
	}

	c.mu.Lock()
	c.jwt = resp.JWT
	c.mu.Unlock()

	c.logger.Debug("上游登录成功", "base_url", c.cfg.BaseURL, "username", c.cfg.Username)
	return resp, nil
}

// token 返回当前缓存的 JWT。
func (c *Client) token() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.jwt
}

// ensureJWT 保证已有可用 JWT，没有则登录。
func (c *Client) ensureJWT(ctx context.Context) (string, error) {
	if token := c.token(); token != "" {
		return token, nil
	}
	if _, err := c.Login(ctx); err != nil {
		return "", err
	}
	return c.token(), nil
}

// Probe 执行一次只读探活：登录 + 查询余额。供管理端 /api/v1/admin/upstream/health 使用。
// 只做读操作，不会在上游产生任何写副作用。
func (c *Client) Probe(ctx context.Context) error {
	if !c.Enabled() {
		return ErrNotConfigured
	}
	if _, err := c.Login(ctx); err != nil {
		return err
	}
	_, err := c.Credit(ctx)
	return err
}

// Get 调用只读上游接口（幂等，网络类错误会自动重试）。out 为 nil 时只校验业务状态。
func (c *Client) Get(ctx context.Context, path string, params url.Values, out any) (*Response, error) {
	return c.do(ctx, http.MethodGet, path, params, true, out)
}

// Post 调用写操作上游接口（绝不自动重试）。out 为 nil 时只校验业务状态。
func (c *Client) Post(ctx context.Context, path string, params url.Values, out any) (*Response, error) {
	return c.do(ctx, http.MethodPost, path, params, false, out)
}

// do 是业务方法的公共入口：保证登录 → 调用 → 解析 data。
func (c *Client) do(ctx context.Context, method, path string, params url.Values, idempotent bool, out any) (*Response, error) {
	resp, err := c.call(ctx, method, path, params, idempotent)
	if err != nil {
		return nil, err
	}
	if out != nil && len(resp.Data) > 0 && string(resp.Data) != "null" {
		if err := json.Unmarshal(resp.Data, out); err != nil {
			return nil, fmt.Errorf("%w: %v (api=%s)", ErrDecode, err, path)
		}
	}
	return resp, nil
}

// call 执行一次带鉴权的请求，并按需重新登录 / 重试。
func (c *Client) call(ctx context.Context, method, path string, params url.Values, idempotent bool) (*Response, error) {
	if !c.Enabled() {
		return nil, ErrNotConfigured
	}
	if _, err := c.ensureJWT(ctx); err != nil {
		return nil, err
	}

	retryBudget := 0
	if idempotent {
		retryBudget = c.cfg.MaxRetries
	}
	relogged := false

	for {
		start := time.Now()
		resp, err := c.request(ctx, method, path, params, c.token())
		if err != nil {
			if retryBudget > 0 && retryable(err) {
				retryBudget--
				if sleepErr := sleepContext(ctx, c.cfg.RetryBackoff); sleepErr != nil {
					return nil, err
				}
				continue
			}
			c.logger.Warn("上游请求失败", "method", method, "api", path,
				"latency_ms", time.Since(start).Milliseconds(), "error", err)
			return nil, err
		}

		switch resp.Status {
		case statusOK, statusPaid:
			c.logger.Debug("上游调用成功", "method", method, "api", path,
				"status", resp.Status, "latency_ms", time.Since(start).Milliseconds())
			return resp, nil
		case statusNotLogged:
			// 405 表示请求未被上游执行，因此写操作也可以安全重放一次。
			if relogged {
				return nil, &NotLoggedInError{API: path, Msg: resp.Msg}
			}
			relogged = true
			c.logger.Warn("上游登录态失效，重新登录后重放", "api", path)
			if _, err := c.Login(ctx); err != nil {
				return nil, err
			}
			continue
		default:
			c.logger.Warn("上游返回业务失败", "method", method, "api", path,
				"status", resp.Status, "msg", resp.Msg)
			return nil, &BusinessError{API: path, Status: resp.Status, Msg: resp.Msg}
		}
	}
}

// request 发送单次 HTTP 请求并把响应解析为 Response。
func (c *Client) request(ctx context.Context, method, path string, params url.Values, jwt string) (*Response, error) {
	endpoint := c.cfg.BaseURL + "/" + strings.TrimLeft(path, "/")

	var body io.Reader
	if method == http.MethodGet {
		if len(params) > 0 {
			endpoint += "?" + params.Encode()
		}
	} else if params != nil {
		// 上游是 PHP，只认 application/x-www-form-urlencoded 的 $_POST。
		body = strings.NewReader(params.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("%w: 构造请求失败: %v (api=%s)", ErrNetwork, err, path)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if jwt != "" {
		req.Header.Set("Authorization", "Bearer "+jwt)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, wrapNetworkError(path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, wrapNetworkError(path, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPError{API: path, HTTPStatus: resp.StatusCode}
	}

	var envelope Response
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("%w: %v (api=%s)", ErrDecode, err, path)
	}
	return &envelope, nil
}

// wrapNetworkError 把底层错误归类为 ErrTimeout / ErrNetwork。
func wrapNetworkError(path string, err error) error {
	var netErr net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return fmt.Errorf("%w: %v (api=%s)", ErrTimeout, err, path)
	default:
		return fmt.Errorf("%w: %v (api=%s)", ErrNetwork, err, path)
	}
}

// retryable 判断错误是否值得对幂等接口重试：只重试网络/超时/上游 HTTP 异常。
func retryable(err error) bool {
	return errors.Is(err, ErrNetwork) || errors.Is(err, ErrTimeout) || errors.Is(err, ErrUpstream)
}

// sleepContext 在退避期间响应 context 取消。
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
