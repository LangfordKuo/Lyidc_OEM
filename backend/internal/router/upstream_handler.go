package router

import (
	"context"
	"errors"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upstream"
)

// 上游探活失败时的固定文案（未配置上游不会发起请求）。
const (
	upstreamErrorNotConfigured = "上游未配置"
)

// upstreamHealthData 是上游探活接口的数据体（契约见 docs/api-contract.md 第 9 节）。
type upstreamHealthData struct {
	Connected    bool   `json:"connected"`
	BaseURL      string `json:"base_url"`
	LatencyMS    int64  `json:"latency_ms"`
	APIKeyMasked string `json:"api_key_masked"`
	CheckedAt    string `json:"checked_at"`
	// Error 仅在 connected=false 时出现，内容已脱敏（不含密钥、JWT）。
	Error string `json:"error,omitempty"`
}

// upstreamHealthHandler 处理 GET /api/v1/admin/upstream/health：
// 按**当前后台设置**取上游客户端，发起一次只读调用（登录 + 查余额）验证连通性。
//
// 探活失败属于业务结果而非接口错误，因此始终返回 HTTP 200 + code=0，
// data.connected=false 并附带 error 说明；管理员 token 无效仍由中间件返回 401。
func upstreamHealthHandler(provider upstream.Provider, timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		data := upstreamHealthData{
			CheckedAt: time.Now().UTC().Format(time.RFC3339),
		}

		if provider == nil {
			data.Error = upstreamErrorNotConfigured
			response.Success(c, data)
			return
		}
		client, enabled, err := provider.Current(c.Request.Context())
		if err != nil {
			// 设置读取/构造失败（含未配置）：不发起请求，按未配置口径答复。
			data.Error = upstreamProbeError(err)
			response.Success(c, data)
			return
		}
		if client != nil {
			data.BaseURL = client.BaseURL()
			data.APIKeyMasked = client.MaskedAPIKey()
		}
		if !enabled {
			data.Error = upstreamErrorNotConfigured
			response.Success(c, data)
			return
		}

		if timeout <= 0 {
			timeout = defaultUpstreamProbeTimeout
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()

		start := time.Now()
		probeErr := client.Probe(ctx)
		data.LatencyMS = time.Since(start).Milliseconds()

		if probeErr != nil {
			data.Connected = false
			data.Error = upstreamProbeError(probeErr)
			response.Success(c, data)
			return
		}

		data.Connected = true
		response.Success(c, data)
	}
}

// upstreamProbeError 把上游客户端错误转成可展示文案。
// 上游提示（BusinessError/AuthError）由上游给出，不含密钥；其余按错误分类给固定文案。
func upstreamProbeError(err error) string {
	switch {
	case errors.Is(err, upstream.ErrNotConfigured):
		return upstreamErrorNotConfigured
	case errors.Is(err, upstream.ErrTimeout):
		return "上游请求超时"
	case errors.Is(err, upstream.ErrAuth):
		return upErrText(err, "上游鉴权失败")
	case errors.Is(err, upstream.ErrNotLoggedIn):
		return "上游登录态失效，重新登录后仍被拒绝"
	case errors.Is(err, upstream.ErrBusiness):
		return upErrText(err, "上游业务失败")
	case errors.Is(err, upstream.ErrDecode):
		return "上游返回内容无法解析"
	case errors.Is(err, upstream.ErrNetwork):
		return "无法连接上游"
	default:
		return upErrText(err, "上游返回异常")
	}
}

// upErrText 优先使用上游/客户端的原始错误文案，为空时回退到固定文案。
func upErrText(err error, fallback string) string {
	if err == nil || err.Error() == "" {
		return fallback
	}
	return err.Error()
}
