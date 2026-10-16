// Package securityheaders 为**所有** HTTP 响应统一设置安全响应头
// （P5 安全整改 finding#14 / #18，契约 1.3）。
//
// 背景：2026-10-17 渗透实测确认全站响应**一处安全头都没有**，外部源可以把 `/console`
// （localStorage 里存着会员 JWT）帧入，并通过点击劫持让受害者照常发出已鉴权请求。
//
// 本包是这套响应头的**唯一实现**，取值只在这里写一份，被三个装配点复用（不复制取值）：
//
//   - `router.New` 的 gin 引擎——正常模式的全部响应（`/api/v1/**`、`/api/host/sync`、
//     未注册路径的 404、方法不匹配的 405、panic 恢复出的 500）；
//   - `install.newInstallEngine` 的 gin 引擎——安装页与 `/install/api/**`；
//   - `install.Supervisor.ServeHTTP`——安装监督器自己写出的响应（未安装时的 302 跳转、
//     503 包、安装模式的健康检查）。前两处是 gin 引擎内部，这一处是引擎之外的兜底，
//     三者叠加后**没有任何一条 Go 侧响应会漏头**。
//
// 取舍（已写进契约 1.3，勿擅自扩项）：
//
//   - CSP **刻意只加** frame-ancestors / base-uri / object-src 三条指令，不加 script-src /
//     style-src：现有 SPA 是 Vite 产物、可能带内联样式，全量 CSP 会把页面整死。本批目标是
//     消灭点击劫持与插件/表单劫持，不是上全量 CSP。
//   - HSTS **不带** includeSubDomains / preload：主域的子域未必属于本系统，带上会误伤；
//     浏览器只在 HTTPS 响应上生效，明文部署下留头无害。
//
// 写入语义：一律 `Header().Set`——每个头只有一个值，绝不重复追加。中间件在 `c.Next()` **之前**
// 写头，因此业务 handler 若显式设置了同名头，以 handler 的值为准（当前全站没有任何 handler 设置它们）。
package securityheaders

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// 安全响应头的名称。
const (
	HeaderContentTypeOptions      = "X-Content-Type-Options"
	HeaderFrameOptions            = "X-Frame-Options"
	HeaderContentSecurityPolicy   = "Content-Security-Policy"
	HeaderReferrerPolicy          = "Referrer-Policy"
	HeaderPermissionsPolicy       = "Permissions-Policy"
	HeaderStrictTransportSecurity = "Strict-Transport-Security"
)

// 安全响应头的取值（契约 1.3 逐字写明，改动必须同步契约与部署侧 Nginx 片段）。
const (
	// ValueContentTypeOptions 禁止浏览器按内容嗅探 MIME 类型。
	ValueContentTypeOptions = "nosniff"
	// ValueFrameOptions 禁止被任何源帧入（点击劫持的主闸门；与 CSP 的 frame-ancestors 双保险）。
	ValueFrameOptions = "DENY"
	// ValueContentSecurityPolicy 只加三条指令：禁帧入、禁改写 base、禁插件与 object。
	ValueContentSecurityPolicy = "frame-ancestors 'none'; base-uri 'self'; object-src 'none'"
	// ValueReferrerPolicy 跨源只带来源（scheme + host），不带路径与查询串。
	ValueReferrerPolicy = "strict-origin-when-cross-origin"
	// ValuePermissionsPolicy 关掉定位、摄像头与麦克风三个强能力。
	ValuePermissionsPolicy = "geolocation=(), camera=(), microphone=()"
	// ValueStrictTransportSecurity 一年内强制 HTTPS；**不带** includeSubDomains / preload。
	ValueStrictTransportSecurity = "max-age=31536000"
)

// Headers 返回本批固定的 6 个安全响应头，顺序固定（便于测试与运维按同一份清单逐条核对）。
//
// 返回的是副本，调用方修改不会影响包内取值。
func Headers() [][2]string {
	return [][2]string{
		{HeaderContentTypeOptions, ValueContentTypeOptions},
		{HeaderFrameOptions, ValueFrameOptions},
		{HeaderContentSecurityPolicy, ValueContentSecurityPolicy},
		{HeaderReferrerPolicy, ValueReferrerPolicy},
		{HeaderPermissionsPolicy, ValuePermissionsPolicy},
		{HeaderStrictTransportSecurity, ValueStrictTransportSecurity},
	}
}

// Apply 把 6 个安全响应头写入给定 Header（`Set` 语义：每个头只会有当前这一个值）。
//
// 调用时机必须早于写出响应体：在 gin 里用 Middleware，在 http.Handler 里在任何写操作之前。
func Apply(h http.Header) {
	if h == nil {
		return
	}
	for _, header := range Headers() {
		h.Set(header[0], header[1])
	}
}

// Middleware 返回 gin 中间件：在后续处理链**之前**写头，因此 2xx / 4xx / 5xx（含 NoRoute 的 404、
// NoMethod 的 405、Recovery 恢复出的 500）都带上这 6 个头。
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		Apply(c.Writer.Header())
		c.Next()
	}
}
