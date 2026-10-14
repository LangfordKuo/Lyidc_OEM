package router

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/buildinfo"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/updates"
)

// stubReleaseFetcher 是路由层测试用的假 GitHub：计数出网次数，并记录最近一次使用的 Token。
type stubReleaseFetcher struct {
	release   updates.Release
	calls     int
	lastToken string
}

func (s *stubReleaseFetcher) Latest(_ context.Context, _, token string) (updates.Release, error) {
	s.calls++
	s.lastToken = token
	return s.release, nil
}

// TestUpdatesAdminEndpoints 覆盖系统更新接口（R13-A，契约 19.3–19.6）：角色闸门、缓存与实时检查、
// Token 三态与掩码、修改仓库时清空缓存。GitHub 用假实现注入，测试不出网。
func TestUpdatesAdminEndpoints(t *testing.T) {
	previousVersion := buildinfo.Version
	buildinfo.Version = "v0.9.0"
	t.Cleanup(func() { buildinfo.Version = previousVersion })

	gdb := testDatabase(t)
	fetcher := &stubReleaseFetcher{release: updates.Release{
		TagName:     "v0.10.0",
		Name:        "v0.10.0",
		PublishedAt: "2026-10-10T12:00:00Z",
		Notes:       "说明",
	}}
	engine := New(Options{
		CacheDir:      t.TempDir(),
		Logger:        silentLogger(),
		DB:            gdb,
		JWT:           config.JWTConfig{Secret: testJWTSecret, ExpireHours: 168},
		UpdateFetcher: fetcher,
	})
	seedAdmin(t, gdb, "root", "admin123456", model.RoleAdmin, model.StatusActive)
	seedAdmin(t, gdb, "fin", "admin123456", model.RoleFinance, model.StatusActive)
	seedAdmin(t, gdb, "sup", "admin123456", model.RoleSupport, model.StatusActive)
	adminToken, _ := loginAdmin(t, engine, "root", "admin123456")
	financeToken, _ := loginAdmin(t, engine, "fin", "admin123456")
	supportToken, _ := loginAdmin(t, engine, "sup", "admin123456")

	// 角色闸门：未登录 401；finance / support 403（仅 admin）。
	rec, _ := doAPI(t, engine, http.MethodGet, "/api/v1/admin/updates", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录读取更新缓存 = HTTP %d，期望 401", rec.Code)
	}
	forbidden := []struct {
		method, path, token string
		body                any
	}{
		{http.MethodGet, "/api/v1/admin/updates", financeToken, nil},
		{http.MethodGet, "/api/v1/admin/updates/check", supportToken, nil},
		{http.MethodGet, "/api/v1/admin/settings/updates", financeToken, nil},
		{http.MethodPut, "/api/v1/admin/settings/updates", supportToken, map[string]string{"repo": "a/b"}},
	}
	for _, c := range forbidden {
		rec, _ = doAPI(t, engine, c.method, c.path, c.token, c.body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s = HTTP %d，期望 403", c.method, c.path, rec.Code)
		}
	}
	if fetcher.calls != 0 {
		t.Fatalf("无权限请求不得出网，实际 %d 次", fetcher.calls)
	}

	// 从未检查：读缓存返回 not_checked，且不出网。
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/updates", adminToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("读取更新缓存 = HTTP %d", rec.Code)
	}
	view := decodeData[updates.View](t, envelope)
	if view.Status != updates.StatusNotChecked || view.HasUpdate || fetcher.calls != 0 {
		t.Fatalf("从未检查时应为 not_checked 且不出网，实际 %+v（calls=%d）", view, fetcher.calls)
	}

	// 实时检查：强制出网，结论按当前运行版本（v0.9.0）计算出有新版本。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/updates/check", adminToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("检查更新 = HTTP %d", rec.Code)
	}
	view = decodeData[updates.View](t, envelope)
	if view.Status != updates.StatusOK || !view.HasUpdate || view.LatestVersion != "v0.10.0" ||
		view.CurrentVersion != "v0.9.0" || view.Repo != "LangfordKuo/Lyidc_OEM" {
		t.Fatalf("检查结果不符: %+v", view)
	}
	if fetcher.calls != 1 {
		t.Fatalf("检查应恰好出网 1 次，实际 %d", fetcher.calls)
	}

	// 读缓存复用上次结果，不再出网。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/updates", adminToken, nil)
	view = decodeData[updates.View](t, envelope)
	if rec.Code != http.StatusOK || view.LatestVersion != "v0.10.0" || fetcher.calls != 1 {
		t.Fatalf("读缓存应复用结果且不出网，实际 HTTP %d view=%+v calls=%d", rec.Code, view, fetcher.calls)
	}

	// 更新源设置：缺省仓库、Token 未配置。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/settings/updates", adminToken, nil)
	settingsView := decodeData[map[string]any](t, envelope)
	if rec.Code != http.StatusOK || settingsView["repo"] != "LangfordKuo/Lyidc_OEM" ||
		settingsView["github_token_configured"] != false {
		t.Fatalf("更新源设置缺省视图不符: HTTP %d %+v", rec.Code, settingsView)
	}

	// 非法仓库 → 40001；空请求 → 40001。
	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/updates", adminToken,
		map[string]string{"repo": "not a repo"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Errorf("非法仓库 = HTTP %d code %d，期望 400 / 40001", rec.Code, envelope.Code)
	}
	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/updates", adminToken, map[string]string{})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Errorf("空请求 = HTTP %d code %d，期望 400 / 40001", rec.Code, envelope.Code)
	}

	// 设置 Token：响应只给掩码，明文不得出现在响应体中；之后的检查使用新 Token。
	const secret = "ghp_secret_value_123"
	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/updates", adminToken,
		map[string]string{"github_token": secret})
	settingsView = decodeData[map[string]any](t, envelope)
	if rec.Code != http.StatusOK || settingsView["github_token_configured"] != true ||
		settingsView["github_token_masked"] != "ghp_****" {
		t.Fatalf("设置 Token 视图不符: HTTP %d %+v", rec.Code, settingsView)
	}
	if strings.Contains(rec.Body.String(), "secret_value") {
		t.Fatal("响应体不得包含 Token 明文")
	}
	rec, _ = doAPI(t, engine, http.MethodGet, "/api/v1/admin/updates/check", adminToken, nil)
	if fetcher.lastToken != secret {
		t.Errorf("检查应使用新 Token，实际 %q", fetcher.lastToken)
	}

	// 修改仓库：同一请求内清空检查缓存，旧仓库结果不冒充新仓库。
	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/updates", adminToken,
		map[string]string{"repo": "someone/else"})
	if rec.Code != http.StatusOK {
		t.Fatalf("修改仓库 = HTTP %d", rec.Code)
	}
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/updates", adminToken, nil)
	view = decodeData[updates.View](t, envelope)
	if view.Repo != "someone/else" || view.Status != updates.StatusNotChecked || view.LatestVersion != "" {
		t.Errorf("修改仓库后缓存应被清空，实际 %+v", view)
	}
}
