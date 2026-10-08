package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Envelope 是所有接口的统一响应包。
//
//	{
//	  "code": 0,
//	  "message": "ok",
//	  "data": {...}
//	}
type Envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

// New 构造响应包（data 可为 nil，序列化为 null）。
func New(code int, message string, data any) Envelope {
	return Envelope{Code: code, Message: message, Data: data}
}

// Success 返回成功响应（code=0，message="ok"）。
func Success(c *gin.Context, data any) {
	JSON(c, http.StatusOK, New(CodeSuccess, Message(CodeSuccess), data))
}

// SuccessMessage 返回成功响应并自定义提示文案。
func SuccessMessage(c *gin.Context, message string, data any) {
	JSON(c, http.StatusOK, New(CodeSuccess, message, data))
}

// Fail 返回失败响应，HTTP 状态码由 HTTPStatus(code) 推导。
func Fail(c *gin.Context, code int, message string) {
	FailWithData(c, HTTPStatus(code), code, message, nil)
}

// FailCode 返回失败响应，提示文案取错误码缺省值。
func FailCode(c *gin.Context, code int) {
	Fail(c, code, Message(code))
}

// FailWithData 返回携带 data 的失败响应（如健康检查降级时附带状态数据）。
func FailWithData(c *gin.Context, httpStatus, code int, message string, data any) {
	JSON(c, httpStatus, New(code, message, data))
}

// JSON 以指定 HTTP 状态码写出响应包。
func JSON(c *gin.Context, httpStatus int, envelope Envelope) {
	c.JSON(httpStatus, envelope)
}

// Abort 写出失败响应并终止后续处理链。
func Abort(c *gin.Context, code int, message string) {
	JSON(c, HTTPStatus(code), New(code, message, nil))
	c.Abort()
}
