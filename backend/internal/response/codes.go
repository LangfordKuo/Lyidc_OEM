// Package response 定义统一 JSON 响应包与错误码表。
package response

import "net/http"

// 业务错误码表。0 表示成功；40xxx 表示参数/校验类错误；
// 401/403/404/409/500 为语义化错误码；50001 起为服务端具体故障。
const (
	// CodeSuccess 成功。
	CodeSuccess = 0

	// CodeInvalidParam 参数错误（格式/类型不合法）。
	CodeInvalidParam = 40001
	// CodeValidationFailed 参数校验失败（业务规则不满足）。
	CodeValidationFailed = 40002
	// CodeMissingParam 缺少必要参数。
	CodeMissingParam = 40003

	// CodeUnauthorized 未认证或凭证无效。
	CodeUnauthorized = 401
	// CodeForbidden 已认证但无权访问。
	CodeForbidden = 403
	// CodeNotFound 资源不存在。
	CodeNotFound = 404
	// CodeConflict 资源冲突（重复创建等）。
	CodeConflict = 409

	// CodeInternalError 服务器内部错误。
	CodeInternalError = 500
	// CodeDatabaseError 数据库故障。
	CodeDatabaseError = 50001
	// CodePaymentGateway 支付渠道调用失败（渠道不可达、渠道拒绝下单、响应异常）。
	// 渠道返回的提示文案已脱敏，不含商户密钥（契约 12.2）。
	CodePaymentGateway = 50002

	// CodeNotInstalled 系统尚未完成安装：除安装页、安装 API 与健康检查外的请求一律拒绝
	// （浏览器页面请求改为 302 跳转到 /install），见契约 13.1。
	CodeNotInstalled = 50301
	// CodeInstallClosed 安装向导已关闭：系统已安装完成，向导不可重入（契约 13.5）。
	CodeInstallClosed = 50302
	// CodeDBConnectFailed 数据库连接失败（安装向导的测试连接/保存参数场景），契约 13.3。
	CodeDBConnectFailed = 50303
)

// messages 是错误码到缺省提示的映射。
var messages = map[int]string{
	CodeSuccess:          "ok",
	CodeInvalidParam:     "参数错误",
	CodeValidationFailed: "参数校验失败",
	CodeMissingParam:     "缺少必要参数",
	CodeUnauthorized:     "未认证或凭证无效",
	CodeForbidden:        "无权访问",
	CodeNotFound:         "资源不存在",
	CodeConflict:         "资源冲突",
	CodeInternalError:    "服务器内部错误",
	CodeDatabaseError:    "数据库错误",
	CodePaymentGateway:   "支付渠道调用失败",
	CodeNotInstalled:     "系统尚未安装",
	CodeInstallClosed:    "系统已安装，安装向导已关闭",
	CodeDBConnectFailed:  "数据库连接失败",
}

// Message 返回错误码的缺省提示；未登记的代码返回通用提示。
func Message(code int) string {
	if message, ok := messages[code]; ok {
		return message
	}
	if code >= 40000 && code < 40100 {
		return "请求参数不合法"
	}
	if code >= 50000 {
		return "服务器内部错误"
	}
	return "未知错误"
}

// HTTPStatus 由业务错误码推导 HTTP 状态码。
func HTTPStatus(code int) int {
	switch {
	case code == CodeSuccess:
		return http.StatusOK
	case code == CodeUnauthorized:
		return http.StatusUnauthorized
	case code == CodeForbidden:
		return http.StatusForbidden
	case code == CodeNotFound:
		return http.StatusNotFound
	case code == CodeConflict:
		return http.StatusConflict
	case code == CodeNotInstalled, code == CodeInstallClosed, code == CodeDBConnectFailed:
		return http.StatusServiceUnavailable
	case code >= 40000 && code < 40100:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
