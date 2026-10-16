package router

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/securityheaders"
)

// P5 安全整改 finding#14 / #18（契约 1.3）：正常模式引擎的**每一条**响应都要带齐 6 个安全响应头。
//
// 本用例刻意**不依赖数据库**：构造一个不带 DB 的引擎，覆盖 2xx（上游回推接口恒 200）、
// 4xx（未注册路径 404、未携带凭证 401）与 5xx（健康检查数据库不可用 503），
// 并顺带断言业务响应体（统一响应包 / 上游约定包）**没有被安全头影响**。

// newSecurityHeaderEngine 构造一个不连数据库的正常模式引擎。
func newSecurityHeaderEngine(t *testing.T) *gin.Engine {
	t.Helper()
	return New(Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
}

// assertSecurityHeaders 断言 6 个头齐全、取值逐字相等、且每个头只有一个值。
func assertSecurityHeaders(t *testing.T, header http.Header, context string) {
	t.Helper()
	for _, want := range securityheaders.Headers() {
		values := header.Values(want[0])
		if len(values) != 1 {
			t.Errorf("%s: %s 有 %d 个值 %q，期望恰好 1 个", context, want[0], len(values), values)
			continue
		}
		if values[0] != want[1] {
			t.Errorf("%s: %s = %q，期望 %q", context, want[0], values[0], want[1])
		}
	}
}

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	engine := newSecurityHeaderEngine(t)

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		status int
	}{
		// 上游回推接口的 HTTP 状态恒为 200（成败看 body.status），是不连库就能拿到的 2xx。
		{"2xx 上游回推", http.MethodPost, "/api/host/sync", "", http.StatusOK},
		{"4xx 未注册路径", http.MethodGet, "/api/v1/nope", "", http.StatusNotFound},
		{"4xx 方法不匹配", http.MethodDelete, "/api/v1/health", "", http.StatusNotFound},
		{"4xx 未携带凭证", http.MethodGet, "/api/v1/members/me", "", http.StatusUnauthorized},
		{"5xx 数据库不可用", http.MethodGet, "/api/v1/health", "", http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var reader io.Reader
			if tc.body != "" {
				reader = strings.NewReader(tc.body)
			}
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, reader))
			if rec.Code != tc.status {
				t.Fatalf("%s %s = %d，期望 %d（body=%s）", tc.method, tc.path, rec.Code, tc.status, rec.Body.String())
			}
			assertSecurityHeaders(t, rec.Header(), tc.name)
		})
	}
}

// TestSecurityHeadersDoNotChangeBusinessPayload 回归：加了头之后，业务响应体逐字不变。
func TestSecurityHeadersDoNotChangeBusinessPayload(t *testing.T) {
	engine := newSecurityHeaderEngine(t)

	t.Run("统一响应包形状不变", func(t *testing.T) {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/members/me", nil))

		var envelope struct {
			Code    int             `json:"code"`
			Message string          `json:"message"`
			Data    json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("响应不是统一响应包：%v（body=%s）", err, rec.Body.String())
		}
		if envelope.Code != 401 || envelope.Message != msgInvalidCredential {
			t.Fatalf("响应包 = %+v，期望 code=401 message=%q", envelope, msgInvalidCredential)
		}
		if string(envelope.Data) != "null" {
			t.Fatalf("data = %s，期望 null", envelope.Data)
		}
		if contentType := rec.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
			t.Fatalf("Content-Type = %q，期望 application/json", contentType)
		}
	})

	t.Run("上游回推包形状不变", func(t *testing.T) {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/host/sync", nil))

		var response struct {
			Status int    `json:"status"`
			Msg    string `json:"msg"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("响应不是上游约定包：%v（body=%s）", err, rec.Body.String())
		}
		if response.Status != hostSyncFail || response.Msg != msgHostSyncIncomplete {
			t.Fatalf("回推应答 = %+v，期望 status=%d msg=%q", response, hostSyncFail, msgHostSyncIncomplete)
		}
	})

	t.Run("安装模式下未注册路径同样带头", func(t *testing.T) {
		// 404 走 NoRoute（引擎顶层），验证头不是只挂在 /api/v1 分组上。
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/完全不存在", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("状态码 = %d，期望 404", rec.Code)
		}
		assertSecurityHeaders(t, rec.Header(), "顶层 404")
	})
}
