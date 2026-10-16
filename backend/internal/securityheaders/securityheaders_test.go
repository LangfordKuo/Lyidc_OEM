package securityheaders

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

// wantHeaders 是本批固定的 6 个安全响应头（契约 1.3 的清单，逐字对照）。
var wantHeaders = [][2]string{
	{"X-Content-Type-Options", "nosniff"},
	{"X-Frame-Options", "DENY"},
	{"Content-Security-Policy", "frame-ancestors 'none'; base-uri 'self'; object-src 'none'"},
	{"Referrer-Policy", "strict-origin-when-cross-origin"},
	{"Permissions-Policy", "geolocation=(), camera=(), microphone=()"},
	{"Strict-Transport-Security", "max-age=31536000"},
}

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	// 5xx 用例是**故意** panic 的，Recovery 会把堆栈写到 gin 的错误输出，测试里丢掉它。
	gin.DefaultWriter = io.Discard
	gin.DefaultErrorWriter = io.Discard
	os.Exit(m.Run())
}

// assertHeaders 断言 6 个头齐全、取值逐字相等、且每个头只有一个值（Set 语义，不重复追加）。
func assertHeaders(t *testing.T, header http.Header, context string) {
	t.Helper()
	for _, want := range wantHeaders {
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

func TestHeadersMatchesContract(t *testing.T) {
	headers := Headers()
	if len(headers) != len(wantHeaders) {
		t.Fatalf("Headers() 返回 %d 条，期望 %d 条", len(headers), len(wantHeaders))
	}
	for index, want := range wantHeaders {
		if headers[index][0] != want[0] || headers[index][1] != want[1] {
			t.Errorf("Headers()[%d] = %q，期望 %q", index, headers[index], want)
		}
	}

	// 返回的是副本：调用方改写不影响包内取值。
	headers[0][1] = "被改坏"
	if again := Headers(); again[0][1] != "nosniff" {
		t.Errorf("Headers() 返回的不是副本：%q", again[0][1])
	}
}

func TestApplySetsAllHeadersOnce(t *testing.T) {
	header := http.Header{}
	// 预置一个同名头：Apply 用 Set 语义，结果只应留下本批取值（不会出现两个值）。
	header.Set(HeaderFrameOptions, "SAMEORIGIN")
	Apply(header)
	assertHeaders(t, header, "Apply")

	// 幂等：重复 Apply 不会追加出第二个值。
	Apply(header)
	assertHeaders(t, header, "Apply 两次")
}

func TestApplyNilHeaderIsSafe(t *testing.T) {
	Apply(nil) // 不应 panic
}

func TestMiddlewareCoversAllStatuses(t *testing.T) {
	engine := gin.New()
	engine.HandleMethodNotAllowed = true
	// 与生产同一顺序：安全头在最外层（先写头），Recovery 在其内层接 panic。
	engine.Use(Middleware(), gin.Recovery())
	engine.GET("/ok", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"code": 0}) })
	engine.GET("/boom", func(*gin.Context) { panic("模拟业务 panic") })
	engine.NoRoute(func(c *gin.Context) { c.JSON(http.StatusNotFound, gin.H{"code": 404}) })
	engine.NoMethod(func(c *gin.Context) { c.JSON(http.StatusMethodNotAllowed, gin.H{"code": 405}) })

	cases := []struct {
		name   string
		method string
		path   string
		status int
	}{
		{"2xx", http.MethodGet, "/ok", http.StatusOK},
		{"4xx 未注册路径", http.MethodGet, "/nope", http.StatusNotFound},
		{"4xx 方法不匹配", http.MethodPost, "/ok", http.StatusMethodNotAllowed},
		{"5xx panic 恢复", http.MethodGet, "/boom", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
			if rec.Code != tc.status {
				t.Fatalf("状态码 = %d，期望 %d", rec.Code, tc.status)
			}
			assertHeaders(t, rec.Header(), tc.name)
		})
	}
}

// TestMiddlewareDoesNotClobberHandlerHeader 锁定写入语义：中间件在 c.Next() **之前**写头，
// 因此业务 handler 显式设置的同类头以 handler 为准（契约 1.3 第 2 条）。
func TestMiddlewareDoesNotClobberHandlerHeader(t *testing.T) {
	engine := gin.New()
	engine.Use(Middleware())
	engine.GET("/custom", func(c *gin.Context) {
		c.Header(HeaderFrameOptions, "SAMEORIGIN")
		c.Status(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/custom", nil))

	if got := rec.Header().Get(HeaderFrameOptions); got != "SAMEORIGIN" {
		t.Errorf("handler 显式设置的同名头被中间件覆盖：%q", got)
	}
	if values := rec.Header().Values(HeaderFrameOptions); len(values) != 1 {
		t.Errorf("同名头出现了 %d 个值：%q", len(values), values)
	}
	// 其余 5 个头照常由中间件补齐。
	for _, want := range wantHeaders {
		if want[0] == HeaderFrameOptions {
			continue
		}
		if got := rec.Header().Get(want[0]); got != want[1] {
			t.Errorf("%s = %q，期望 %q", want[0], got, want[1])
		}
	}
}
