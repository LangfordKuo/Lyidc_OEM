package router

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upstream"
)

// upstreamHealthView 是探活接口 data 的测试视图。
type upstreamHealthView struct {
	Connected    bool   `json:"connected"`
	BaseURL      string `json:"base_url"`
	LatencyMS    int64  `json:"latency_ms"`
	APIKeyMasked string `json:"api_key_masked"`
	CheckedAt    string `json:"checked_at"`
	Error        string `json:"error"`
}

// upstreamTestKey 是测试用假密钥（非真实密钥）。
const upstreamTestKey = "TestKey1234567"

// newUpstreamEngine 构造带假上游（或 nil）的 gin 引擎。
func newUpstreamEngine(t *testing.T, gdb *gorm.DB, client *upstream.Client) *gin.Engine {
	t.Helper()
	return New(Options{
		Logger:          silentLogger(),
		DB:              gdb,
		JWT:             config.JWTConfig{Secret: testJWTSecret, ExpireHours: 168},
		Upstream:        client,
		UpstreamTimeout: 2 * time.Second,
	})
}

// fakeUpstreamClient 启动一个假上游并返回指向它的客户端。
// handler 负责处理除 /zjmf_api_login 之外的请求（登录固定成功）。
func fakeUpstreamClient(t *testing.T, handler http.HandlerFunc) *upstream.Client {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if r.URL.Path == "/zjmf_api_login" {
			_, _ = io.WriteString(w, `{"status":200,"msg":"鉴权成功","jwt":"fake-jwt"}`)
			return
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)

	client, err := upstream.New(upstream.Config{
		BaseURL:  server.URL,
		Username: "13800000000",
		APIKey:   upstreamTestKey,
		Timeout:  2 * time.Second,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("构造上游客户端失败: %v", err)
	}
	return client
}

func TestUpstreamHealthRequiresAdminToken(t *testing.T) {
	gdb := testDatabase(t)
	resetAccountTables(t, gdb)
	engine := newUpstreamEngine(t, gdb, nil)

	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/upstream/health", "", nil)
	if rec.Code != http.StatusUnauthorized || envelope.Code != response.CodeUnauthorized {
		t.Fatalf("无 token 访问探活接口 = (%d, code=%d), 期望 (401, %d)",
			rec.Code, envelope.Code, response.CodeUnauthorized)
	}
}

func TestUpstreamHealthWhenNotConfigured(t *testing.T) {
	gdb := testDatabase(t)
	resetAccountTables(t, gdb)
	seedAdmin(t, gdb, "admin", "admin123456", model.RoleAdmin, model.StatusActive)
	engine := newUpstreamEngine(t, gdb, nil)

	token, _ := loginAdmin(t, engine, "admin", "admin123456")
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/upstream/health", token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("未配置上游时探活 = (%d, code=%d), 期望 (200, 0)（探活失败不是接口错误）",
			rec.Code, envelope.Code)
	}

	data := decodeData[upstreamHealthView](t, envelope)
	if data.Connected {
		t.Errorf("connected = true, 期望 false")
	}
	if data.Error != upstreamErrorNotConfigured {
		t.Errorf("error = %q, 期望 %q", data.Error, upstreamErrorNotConfigured)
	}
	if data.CheckedAt == "" {
		t.Errorf("checked_at 不应为空")
	}
}

func TestUpstreamHealthConnected(t *testing.T) {
	gdb := testDatabase(t)
	resetAccountTables(t, gdb)
	seedAdmin(t, gdb, "admin", "admin123456", model.RoleAdmin, model.StatusActive)

	client := fakeUpstreamClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cart/credit" {
			t.Errorf("探活只应调用只读接口，实际路径 = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fake-jwt" {
			t.Errorf("Authorization = %q, 期望 Bearer fake-jwt", got)
		}
		_, _ = io.WriteString(w, `{"status":200,"msg":"请求成功","data":{"credit":"100.00","currency":{"id":1,"code":"CNY"}}}`)
	})

	engine := newUpstreamEngine(t, gdb, client)
	token, _ := loginAdmin(t, engine, "admin", "admin123456")
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/upstream/health", token, nil)

	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("探活成功时 = (%d, code=%d), 期望 (200, 0)", rec.Code, envelope.Code)
	}

	data := decodeData[upstreamHealthView](t, envelope)
	if !data.Connected {
		t.Fatalf("connected = false（error=%q），期望 true", data.Error)
	}
	if data.Error != "" {
		t.Errorf("error = %q, 期望为空", data.Error)
	}
	if data.LatencyMS < 0 {
		t.Errorf("latency_ms = %d, 期望非负", data.LatencyMS)
	}
	if data.APIKeyMasked != "Test****567" {
		t.Errorf("api_key_masked = %q, 期望 Test****567（不得出现完整密钥）", data.APIKeyMasked)
	}
	if data.BaseURL != client.BaseURL() {
		t.Errorf("base_url = %q, 期望 %q", data.BaseURL, client.BaseURL())
	}
}

func TestUpstreamHealthReportsUpstreamBusinessFailure(t *testing.T) {
	gdb := testDatabase(t)
	resetAccountTables(t, gdb)
	seedAdmin(t, gdb, "admin", "admin123456", model.RoleAdmin, model.StatusActive)

	client := fakeUpstreamClient(t, func(w http.ResponseWriter, _ *http.Request) {
		// 上游未开通 API 时的真实返回（HTTP 200 + 业务 400）。
		_, _ = io.WriteString(w, `{"status":400,"msg":"暂未开通API功能","is_aff":"1"}`)
	})

	engine := newUpstreamEngine(t, gdb, client)
	token, _ := loginAdmin(t, engine, "admin", "admin123456")
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/upstream/health", token, nil)

	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("上游业务失败时探活 = (%d, code=%d), 期望 (200, 0)", rec.Code, envelope.Code)
	}

	data := decodeData[upstreamHealthView](t, envelope)
	if data.Connected {
		t.Errorf("connected = true, 期望 false")
	}
	if data.Error == "" {
		t.Errorf("error 应说明上游失败原因")
	}
	if data.CheckedAt == "" {
		t.Errorf("checked_at 不应为空")
	}
}
