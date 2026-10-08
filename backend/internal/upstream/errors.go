package upstream

import (
	"errors"
	"fmt"
)

// 上游客户端的错误分类。调用方统一用 errors.Is 判定，不依赖错误文案：
//
//	upstream.ErrNotConfigured —— 未配置上游地址/账号/密钥
//	upstream.ErrNetwork       —— 连接失败、DNS 失败、响应体不可读
//	upstream.ErrTimeout       —— 请求超时（含 context 超时）
//	upstream.ErrAuth          —— 上游鉴权失败（/zjmf_api_login 被拒）
//	upstream.ErrNotLoggedIn   —— 已登录但被上游判定登录态失效（status=405）
//	upstream.ErrBusiness      —— 上游以业务状态码拒绝（status 非 200/1001）
//	upstream.ErrHostNotFound  —— 上游账号下不存在该主机（已删除或不属于该账号；阶段 5c）
//	upstream.ErrUpstream      —— 上游返回异常（HTTP 非 200、非 JSON、未知状态码）
//	upstream.ErrDecode        —— 响应 JSON 解析失败
var (
	ErrNotConfigured = errors.New("upstream: 上游未配置（需要 base_url / username / api_key）")
	ErrNetwork       = errors.New("upstream: 网络错误")
	ErrTimeout       = errors.New("upstream: 请求超时")
	ErrAuth          = errors.New("upstream: 鉴权失败")
	ErrNotLoggedIn   = errors.New("upstream: 登录态失效")
	ErrBusiness      = errors.New("upstream: 业务失败")
	ErrHostNotFound  = errors.New("upstream: 主机不存在")
	ErrUpstream      = errors.New("upstream: 上游返回异常")
	ErrDecode        = errors.New("upstream: 响应解析失败")
)

// HostNotFoundError 表示按主机 ID 回读时上游账号下没有该主机：主机已被删除（终止完成）
// 或不属于当前账号。**同时命中 ErrHostNotFound 与 ErrBusiness**（后者是历史口径，
// 保持既有调用方与日志文案兼容），调用方需要区分「主机已消失」与其它业务失败时用
// errors.Is(err, ErrHostNotFound)（契约 15.8.3 的收敛判定）。
type HostNotFoundError struct {
	// HostID 是被查询的上游主机 ID。
	HostID int
}

func (e *HostNotFoundError) Error() string {
	return fmt.Sprintf("上游业务失败: 上游未返回主机 %d（可能已删除或不属于该账号）", e.HostID)
}

// Unwrap 返回错误树：既命中 ErrHostNotFound（精确判定），也命中 ErrBusiness（兼容既有口径）。
func (e *HostNotFoundError) Unwrap() []error { return []error{ErrHostNotFound, ErrBusiness} }

// HTTPError 表示上游返回了非 200 的 HTTP 状态码。
type HTTPError struct {
	API        string
	HTTPStatus int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("上游 HTTP 状态异常: %d (api=%s)", e.HTTPStatus, e.API)
}

// Unwrap 让 HTTPError 可被 errors.Is(err, ErrUpstream) 命中。
func (e *HTTPError) Unwrap() error { return ErrUpstream }

// AuthError 表示上游鉴权被拒（换取 JWT 失败）。
//
// 上游对「账号不存在」「API 密钥错误」「username 长度不合法（4-20 字符）」「API 功能未开启」
// 都会返回同一条 status=400 + msg="鉴权失败"，无法从文案区分，因此这里只保留原文案。
type AuthError struct {
	API string
	Msg string
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("上游鉴权失败: %s (api=%s)", e.Msg, e.API)
}

// Unwrap 让 AuthError 可被 errors.Is(err, ErrAuth) 命中。
func (e *AuthError) Unwrap() error { return ErrAuth }

// BusinessError 表示上游以业务状态码拒绝了请求（status 既不是 200 也不是 1001）。
//
// Status 是上游业务状态码（400 / 405 / 406 等），Msg 是上游中文提示，原样保留便于排障。
type BusinessError struct {
	API    string
	Status int
	Msg    string
}

func (e *BusinessError) Error() string {
	return fmt.Sprintf("上游业务失败: %s (status=%d, api=%s)", e.Msg, e.Status, e.API)
}

// Unwrap 让 BusinessError 可被 errors.Is(err, ErrBusiness) 命中。
func (e *BusinessError) Unwrap() error { return ErrBusiness }

// NotLoggedInError 表示上游连续两次判定登录态失效（已重新登录仍失败）。
type NotLoggedInError struct {
	API string
	Msg string
}

func (e *NotLoggedInError) Error() string {
	return fmt.Sprintf("上游登录态失效且重新登录后仍被拒: %s (api=%s)", e.Msg, e.API)
}

// Unwrap 让 NotLoggedInError 可被 errors.Is(err, ErrNotLoggedIn) 命中。
func (e *NotLoggedInError) Unwrap() error { return ErrNotLoggedIn }

// MaskAPIKey 把 API 密钥脱敏成「首 4 位****后 3 位」，用于日志与探活响应。
// 长度不足 8 位时全部打码，避免短密钥被还原。
func MaskAPIKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) < 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-3:]
}
