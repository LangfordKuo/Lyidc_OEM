package install

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/securityheaders"
)

// P5 安全整改 finding#14 / #18（契约 1.3）：安装页、安装 API 与**监督器自己写出的响应**
// （未安装时的 302 跳转、503 包）同样要带齐 6 个安全响应头。
//
// 后两条尤其重要：它们不经过任何 gin 引擎，覆盖它们的是 Supervisor.ServeHTTP 入口处的那一次 Apply。

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

// newUninstalledSupervisor 构造一个「未配置数据库」的监督器（无需真实数据库）。
func newUninstalledSupervisor(t *testing.T) *Supervisor {
	t.Helper()
	cfg := config.Default()
	cfg.DatabaseConfigured = false
	supervisor := newTestSupervisor(t, cfg, filepath.Join(t.TempDir(), "config.yaml"))
	supervisor.Init(context.Background())
	if supervisor.IsInstalled() {
		t.Fatal("未配置数据库时不应判定为已安装")
	}
	return supervisor
}

func TestInstallSecurityHeaders(t *testing.T) {
	supervisor := newUninstalledSupervisor(t)
	htmlHeaders := map[string]string{"Accept": "text/html"}

	cases := []struct {
		name    string
		method  string
		path    string
		headers map[string]string
		status  int
	}{
		{"安装页", http.MethodGet, PathPage, htmlHeaders, http.StatusOK},
		{"安装 API", http.MethodGet, PathPage + "/api/status", nil, http.StatusOK},
		{"安装页未注册路径", http.MethodGet, PathPage + "/api/nope", nil, http.StatusNotFound},
		{"监督器 302 跳转", http.MethodGet, "/", htmlHeaders, http.StatusFound},
		{"监督器 503 未安装", http.MethodGet, "/api/v1/products", nil, http.StatusServiceUnavailable},
		{"安装模式健康检查", http.MethodGet, "/api/v1/health", nil, http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, _ := doRequest(t, supervisor, tc.method, tc.path, nil, tc.headers)
			if rec.Code != tc.status {
				t.Fatalf("%s %s = %d，期望 %d（body=%s）", tc.method, tc.path, rec.Code, tc.status, rec.Body.String())
			}
			assertSecurityHeaders(t, rec.Header(), tc.name)
		})
	}
}

// TestInstallSecurityHeadersDoNotChangeBusinessPayload 回归：安装态响应体逐字不变。
func TestInstallSecurityHeadersDoNotChangeBusinessPayload(t *testing.T) {
	supervisor := newUninstalledSupervisor(t)

	t.Run("未安装的 503 包", func(t *testing.T) {
		rec, envelope := doRequest(t, supervisor, http.MethodGet, "/api/v1/products", nil, nil)
		if rec.Code != http.StatusServiceUnavailable || envelope.Code != 50301 {
			t.Fatalf("状态码 = %d code = %d，期望 503 / 50301", rec.Code, envelope.Code)
		}
	})

	t.Run("302 跳转目标不变", func(t *testing.T) {
		rec, _ := doRequest(t, supervisor, http.MethodGet, "/", nil, map[string]string{"Accept": "text/html"})
		if location := rec.Header().Get("Location"); location != PathPage {
			t.Fatalf("Location = %q，期望 %q", location, PathPage)
		}
	})

	t.Run("安装页仍是 HTML", func(t *testing.T) {
		rec, _ := doRequest(t, supervisor, http.MethodGet, PathPage, nil, map[string]string{"Accept": "text/html"})
		if contentType := rec.Header().Get("Content-Type"); contentType != "text/html; charset=utf-8" {
			t.Fatalf("Content-Type = %q，期望 text/html; charset=utf-8", contentType)
		}
	})
}
