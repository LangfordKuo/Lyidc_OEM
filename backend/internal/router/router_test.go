package router

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	gin.DefaultWriter = io.Discard
	gin.DefaultErrorWriter = io.Discard
	os.Exit(m.Run())
}

// healthEnvelope 是健康检查响应的强类型视图。
type healthEnvelope struct {
	Code    int        `json:"code"`
	Message string     `json:"message"`
	Data    healthData `json:"data"`
}

func newTestEngine(opts Options) *gin.Engine {
	opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(opts)
}

func doRequest(t *testing.T, engine http.Handler, method, target string) (*httptest.ResponseRecorder, healthEnvelope) {
	t.Helper()

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(method, target, nil))

	var envelope healthEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("响应不是合法 JSON: %v (body=%s)", err, rec.Body.String())
	}
	return rec, envelope
}

func TestHealthReturnsOKWhenDatabaseUp(t *testing.T) {
	engine := newTestEngine(Options{
		Ping: func(context.Context) error { return nil },
	})

	rec, envelope := doRequest(t, engine, http.MethodGet, "/api/v1/health")

	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP 状态码 = %d, 期望 %d", rec.Code, http.StatusOK)
	}
	if envelope.Code != response.CodeSuccess {
		t.Errorf("code = %d, 期望 0", envelope.Code)
	}
	if envelope.Message != "ok" {
		t.Errorf("message = %q, 期望 \"ok\"", envelope.Message)
	}
	if envelope.Data.Status != "ok" {
		t.Errorf("data.status = %q, 期望 \"ok\"", envelope.Data.Status)
	}
	if envelope.Data.DB != "up" {
		t.Errorf("data.db = %q, 期望 \"up\"", envelope.Data.DB)
	}
	if _, err := time.Parse(time.RFC3339, envelope.Data.Time); err != nil {
		t.Errorf("data.time = %q 不是 RFC3339: %v", envelope.Data.Time, err)
	}
}

func TestHealthReturns503WhenDatabaseDown(t *testing.T) {
	engine := newTestEngine(Options{
		Ping: func(context.Context) error { return errors.New("connection refused") },
	})

	rec, envelope := doRequest(t, engine, http.MethodGet, "/api/v1/health")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("HTTP 状态码 = %d, 期望 %d", rec.Code, http.StatusServiceUnavailable)
	}
	if envelope.Code != response.CodeInternalError {
		t.Errorf("code = %d, 期望 %d", envelope.Code, response.CodeInternalError)
	}
	if envelope.Data.DB != "down" {
		t.Errorf("data.db = %q, 期望 \"down\"", envelope.Data.DB)
	}
	if envelope.Data.Status != "degraded" {
		t.Errorf("data.status = %q, 期望 \"degraded\"", envelope.Data.Status)
	}
}

func TestHealthReturns503WhenDatabaseNotConfigured(t *testing.T) {
	engine := newTestEngine(Options{})

	rec, envelope := doRequest(t, engine, http.MethodGet, "/api/v1/health")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("HTTP 状态码 = %d, 期望 %d", rec.Code, http.StatusServiceUnavailable)
	}
	if envelope.Data.DB != "down" {
		t.Errorf("data.db = %q, 期望 \"down\"", envelope.Data.DB)
	}
}

func TestHealthTimeoutIsApplied(t *testing.T) {
	engine := newTestEngine(Options{
		HealthTimeout: time.Millisecond,
		Ping: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	})

	rec, envelope := doRequest(t, engine, http.MethodGet, "/api/v1/health")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("HTTP 状态码 = %d, 期望 %d", rec.Code, http.StatusServiceUnavailable)
	}
	if envelope.Data.DB != "down" {
		t.Errorf("data.db = %q, 期望 \"down\"", envelope.Data.DB)
	}
}

func TestUnknownRouteReturnsEnvelope(t *testing.T) {
	engine := newTestEngine(Options{})

	rec, envelope := doRequest(t, engine, http.MethodGet, "/api/v1/unknown")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("HTTP 状态码 = %d, 期望 %d", rec.Code, http.StatusNotFound)
	}
	if envelope.Code != response.CodeNotFound {
		t.Errorf("code = %d, 期望 %d", envelope.Code, response.CodeNotFound)
	}
	if envelope.Message != "接口不存在" {
		t.Errorf("message = %q, 期望 \"接口不存在\"", envelope.Message)
	}
}

func TestMethodNotAllowedReturnsEnvelope(t *testing.T) {
	engine := newTestEngine(Options{Ping: func(context.Context) error { return nil }})

	rec, envelope := doRequest(t, engine, http.MethodPost, "/api/v1/health")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("HTTP 状态码 = %d, 期望 %d", rec.Code, http.StatusNotFound)
	}
	if envelope.Code != response.CodeNotFound {
		t.Errorf("code = %d, 期望 %d", envelope.Code, response.CodeNotFound)
	}
}

func TestRoutesAreRegisteredUnderAPIV1(t *testing.T) {
	engine := newTestEngine(Options{Ping: func(context.Context) error { return nil }})

	found := false
	for _, route := range engine.Routes() {
		if route.Method == http.MethodGet && route.Path == "/api/v1/health" {
			found = true
		}
	}
	if !found {
		t.Errorf("未注册 GET /api/v1/health，已注册路由: %+v", engine.Routes())
	}
}

func TestLevelForStatus(t *testing.T) {
	tests := []struct {
		status int
		want   slog.Level
	}{
		{status: http.StatusOK, want: slog.LevelInfo},
		{status: http.StatusNotFound, want: slog.LevelWarn},
		{status: http.StatusServiceUnavailable, want: slog.LevelError},
	}

	for _, tt := range tests {
		if got := levelForStatus(tt.status); got != tt.want {
			t.Errorf("levelForStatus(%d) = %v, 期望 %v", tt.status, got, tt.want)
		}
	}
}
