package response

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	gin.DefaultWriter = io.Discard
	gin.DefaultErrorWriter = io.Discard
	os.Exit(m.Run())
}

// callHandler 在 gin 测试上下文中执行 handler 并返回响应。
func callHandler(t *testing.T, handler gin.HandlerFunc) (*httptest.ResponseRecorder, Envelope) {
	t.Helper()

	engine := gin.New()
	engine.GET("/test", handler)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/test", nil))

	var envelope Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("响应不是合法 JSON: %v (body=%s)", err, rec.Body.String())
	}
	return rec, envelope
}

func TestSuccessWritesEnvelopeWithCodeZero(t *testing.T) {
	rec, envelope := callHandler(t, func(c *gin.Context) {
		Success(c, gin.H{"status": "ok"})
	})

	if rec.Code != http.StatusOK {
		t.Errorf("HTTP 状态码 = %d, 期望 %d", rec.Code, http.StatusOK)
	}
	if envelope.Code != CodeSuccess {
		t.Errorf("code = %d, 期望 %d", envelope.Code, CodeSuccess)
	}
	if envelope.Message != "ok" {
		t.Errorf("message = %q, 期望 \"ok\"", envelope.Message)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("data 类型 = %T, 期望对象", envelope.Data)
	}
	if data["status"] != "ok" {
		t.Errorf("data.status = %v, 期望 ok", data["status"])
	}
}

func TestSuccessKeepsEnvelopeKeysForNilData(t *testing.T) {
	rec, envelope := callHandler(t, func(c *gin.Context) {
		Success(c, nil)
	})

	if rec.Code != http.StatusOK {
		t.Errorf("HTTP 状态码 = %d, 期望 %d", rec.Code, http.StatusOK)
	}
	if envelope.Data != nil {
		t.Errorf("data = %v, 期望 null", envelope.Data)
	}
	if !jsonHasKeys(rec.Body.Bytes(), "code", "message", "data") {
		t.Errorf("响应包缺少固定字段: %s", rec.Body.String())
	}
}

func TestSuccessMessageOverridesMessage(t *testing.T) {
	_, envelope := callHandler(t, func(c *gin.Context) {
		SuccessMessage(c, "创建成功", nil)
	})

	if envelope.Code != CodeSuccess {
		t.Errorf("code = %d, 期望 0", envelope.Code)
	}
	if envelope.Message != "创建成功" {
		t.Errorf("message = %q, 期望 \"创建成功\"", envelope.Message)
	}
}

func TestFailDerivesHTTPStatusFromCode(t *testing.T) {
	tests := []struct {
		code       int
		wantStatus int
	}{
		{code: CodeInvalidParam, wantStatus: http.StatusBadRequest},
		{code: CodeValidationFailed, wantStatus: http.StatusBadRequest},
		{code: CodeMissingParam, wantStatus: http.StatusBadRequest},
		{code: CodeUnauthorized, wantStatus: http.StatusUnauthorized},
		{code: CodeForbidden, wantStatus: http.StatusForbidden},
		{code: CodeNotFound, wantStatus: http.StatusNotFound},
		{code: CodeConflict, wantStatus: http.StatusConflict},
		{code: CodeInternalError, wantStatus: http.StatusInternalServerError},
		{code: CodeDatabaseError, wantStatus: http.StatusInternalServerError},
		{code: 99999, wantStatus: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(Message(tt.code), func(t *testing.T) {
			rec, envelope := callHandler(t, func(c *gin.Context) {
				Fail(c, tt.code, Message(tt.code))
			})

			if rec.Code != tt.wantStatus {
				t.Errorf("code=%d 的 HTTP 状态码 = %d, 期望 %d", tt.code, rec.Code, tt.wantStatus)
			}
			if envelope.Code != tt.code {
				t.Errorf("业务 code = %d, 期望 %d", envelope.Code, tt.code)
			}
			if envelope.Message == "" {
				t.Error("message 不应为空")
			}
		})
	}
}

func TestFailCodeUsesDefaultMessage(t *testing.T) {
	_, envelope := callHandler(t, func(c *gin.Context) {
		FailCode(c, CodeNotFound)
	})

	if envelope.Message != "资源不存在" {
		t.Errorf("message = %q, 期望 \"资源不存在\"", envelope.Message)
	}
}

func TestFailWithDataKeepsStatusAndData(t *testing.T) {
	rec, envelope := callHandler(t, func(c *gin.Context) {
		FailWithData(c, http.StatusServiceUnavailable, CodeInternalError, "数据库不可用", gin.H{"db": "down"})
	})

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("HTTP 状态码 = %d, 期望 %d", rec.Code, http.StatusServiceUnavailable)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("data 类型 = %T, 期望对象", envelope.Data)
	}
	if data["db"] != "down" {
		t.Errorf("data.db = %v, 期望 down", data["db"])
	}
}

func TestAbortStopsHandlerChain(t *testing.T) {
	secondHandlerRan := false

	engine := gin.New()
	engine.GET("/test",
		func(c *gin.Context) { Abort(c, CodeForbidden, "无权访问") },
		func(c *gin.Context) { secondHandlerRan = true },
	)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/test", nil))

	var envelope Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("响应不是合法 JSON: %v (body=%s)", err, rec.Body.String())
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("HTTP 状态码 = %d, 期望 %d", rec.Code, http.StatusForbidden)
	}
	if envelope.Code != CodeForbidden {
		t.Errorf("业务 code = %d, 期望 %d", envelope.Code, CodeForbidden)
	}
	if secondHandlerRan {
		t.Error("Abort 之后后续 handler 仍被执行")
	}
}

func TestMessageFallbacks(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{code: CodeSuccess, want: "ok"},
		{code: CodeInvalidParam, want: "参数错误"},
		{code: 40099, want: "请求参数不合法"},
		{code: CodeUnauthorized, want: "未认证或凭证无效"},
		{code: CodeForbidden, want: "无权访问"},
		{code: CodeNotFound, want: "资源不存在"},
		{code: CodeConflict, want: "资源冲突"},
		{code: CodeInternalError, want: "服务器内部错误"},
		{code: CodeDatabaseError, want: "数据库错误"},
		{code: 50099, want: "服务器内部错误"},
		{code: 12345, want: "未知错误"},
	}

	for _, tt := range tests {
		if got := Message(tt.code); got != tt.want {
			t.Errorf("Message(%d) = %q, 期望 %q", tt.code, got, tt.want)
		}
	}
}

func TestHTTPStatusForSuccess(t *testing.T) {
	if got := HTTPStatus(CodeSuccess); got != http.StatusOK {
		t.Errorf("HTTPStatus(0) = %d, 期望 %d", got, http.StatusOK)
	}
}

func jsonHasKeys(raw []byte, keys ...string) bool {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false
	}
	for _, key := range keys {
		if _, ok := payload[key]; !ok {
			return false
		}
	}
	return true
}
