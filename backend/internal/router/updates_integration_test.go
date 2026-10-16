package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/buildinfo"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/updates"
)

// stubReleaseFetcher 是路由层测试用的假 GitHub：计数出网次数，并记录最近一次使用的 Token。
type stubReleaseFetcher struct {
	release   updates.Release
	calls     int
	lastToken string
}

// fakeGitHubAPI 是一个**真实可访问**的极简 GitHub Releases 端点：返回固定 tag 并计数请求。
//
// 与 stubReleaseFetcher 的分工：桩替换的是「访问能力」，无法证明请求打到了哪个地址；
// 这里用真 HTTP 服务端，才能证明「生效源就是构建期注入的那个地址、库内残留值没能改道」
// ——这正是 P2 的核心承诺（契约 19.5）。
type fakeGitHubAPI struct {
	server *httptest.Server
	tag    string
	calls  int32
}

func newFakeGitHubAPI(t *testing.T, tag string) *fakeGitHubAPI {
	t.Helper()
	fake := &fakeGitHubAPI{tag: tag}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&fake.calls, 1)
		if !strings.HasSuffix(r.URL.Path, "/releases/latest") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"` + tag + `","name":"` + tag +
			`","published_at":"2026-10-10T12:00:00Z","body":"说明","html_url":"https://example.test/r","assets":[]}`))
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeGitHubAPI) callCount() int { return int(atomic.LoadInt32(&f.calls)) }

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

// TestUpdatesSourceIsImmutable 验证「更新源构建期固定」（P2 核心承诺，契约 19.5）：
//   - GET 只读回显**生效值**并带 fixed=true（未注入时即官方缺省）；
//   - PUT 带 api_base / download_base（值 / 空串 / null 三种形态）一律 40002 且不落库，
//     同请求里的 repo / github_token 也不得被改动（拒绝发生在合并与写库之前）；
//   - 库内残留旧值被忽略：检查仍打构建期注入的源，攻击者主机收不到任何请求。
func TestUpdatesSourceIsImmutable(t *testing.T) {
	gdb := testDatabase(t)
	injected := newFakeGitHubAPI(t, "v0.10.0")
	evil := newFakeGitHubAPI(t, "v9.9.9")

	previousVersion := buildinfo.Version
	buildinfo.Version = "v0.9.0"
	t.Cleanup(func() { buildinfo.Version = previousVersion })

	engine := New(Options{
		CacheDir: t.TempDir(),
		Logger:   silentLogger(),
		DB:       gdb,
		JWT:      config.JWTConfig{Secret: testJWTSecret, ExpireHours: 168},
		// 构建期注入的更新源（生产由 cmd/server 用 buildinfo 填充；测试从构造参数注入）。
		UpdatesAPIBase: injected.server.URL,
	})
	seedAdmin(t, gdb, "root", "admin123456", model.RoleAdmin, model.StatusActive)
	adminToken, _ := loginAdmin(t, engine, "root", "admin123456")

	// GET：两个地址都是**生效值**，且恒带 fixed=true 供界面渲染成只读。
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/settings/updates", adminToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("读取更新源设置 = HTTP %d", rec.Code)
	}
	view := decodeData[map[string]any](t, envelope)
	if view["api_base"] != injected.server.URL || view["download_base"] != "" || view["fixed"] != true {
		t.Fatalf("只读回显不符：%+v", view)
	}

	// 模拟测试站现状：库里残留一条指向攻击者主机的旧值。读取路径不读它。
	residual := `{"repo":"LangfordKuo/Lyidc_OEM","api_base":"` + evil.server.URL +
		`","download_base":"` + evil.server.URL + `"}`
	if err := gdb.Create(&model.Setting{Key: settings.KeyUpdates, Value: residual}).Error; err != nil {
		t.Fatalf("写入残留设置失败：%v", err)
	}

	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/settings/updates", adminToken, nil)
	view = decodeData[map[string]any](t, envelope)
	if rec.Code != http.StatusOK || view["api_base"] != injected.server.URL {
		t.Fatalf("残留旧值不得改变回显的生效源：HTTP %d %+v", rec.Code, view)
	}

	// 检查更新仍打构建期注入的源，攻击者主机一次都收不到。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/updates/check", adminToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("检查更新 = HTTP %d", rec.Code)
	}
	checked := decodeData[updates.View](t, envelope)
	if checked.LatestVersion != "v0.10.0" {
		t.Fatalf("检查结果应来自注入的源，实际 %+v", checked)
	}
	if injected.callCount() != 1 {
		t.Errorf("注入的源应被访问 1 次，实际 %d", injected.callCount())
	}
	if evil.callCount() != 0 {
		t.Fatalf("库内残留的攻击者主机不得收到任何请求，实际 %d 次", evil.callCount())
	}

	// PUT：三种形态的 api_base / download_base 一律 40002，且请求里同带的 repo 也不得落库。
	cases := []struct {
		name string
		body map[string]any
	}{
		{"api_base 有值", map[string]any{"api_base": "https://evil.example/v3"}},
		{"api_base 空串", map[string]any{"api_base": ""}},
		{"api_base null", map[string]any{"api_base": nil}},
		{"download_base 有值", map[string]any{"download_base": "https://evil.example/dl"}},
		{"download_base 空串", map[string]any{"download_base": ""}},
		{"download_base null", map[string]any{"download_base": nil}},
		{"与 repo 同请求", map[string]any{"repo": "octo-org/other", "api_base": "https://evil.example/v3"}},
	}
	for _, testCase := range cases {
		rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/updates", adminToken, testCase.body)
		if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
			t.Errorf("%s = HTTP %d code %d，期望 400 / 40002", testCase.name, rec.Code, envelope.Code)
		}
		if !strings.Contains(envelope.Message, "不可修改") {
			t.Errorf("%s 的拒绝原因应说明地址不可修改，实际 %q", testCase.name, envelope.Message)
		}
	}

	// 拒绝必须发生在写库之前：repo 仍是缺省值（不是 octo-org/other），库内残留值逐字节未变。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/settings/updates", adminToken, nil)
	view = decodeData[map[string]any](t, envelope)
	if rec.Code != http.StatusOK || view["repo"] != "LangfordKuo/Lyidc_OEM" {
		t.Fatalf("被拒请求不得改动任何字段：HTTP %d %+v", rec.Code, view)
	}
	stored, err := store.New(gdb).Setting(context.Background(), settings.KeyUpdates)
	if err != nil {
		t.Fatalf("读取库内设置失败：%v", err)
	}
	if stored.Value != residual {
		t.Fatalf("被拒请求不得落库，库内值应逐字节保持原样：%s", stored.Value)
	}

	// 未注入时回退官方缺省：另一套装配（不传 UpdatesAPIBase）读到的就是官方地址。
	plainEngine := New(Options{
		CacheDir: t.TempDir(),
		Logger:   silentLogger(),
		DB:       gdb,
		JWT:      config.JWTConfig{Secret: testJWTSecret, ExpireHours: 168},
	})
	rec, envelope = doAPI(t, plainEngine, http.MethodGet, "/api/v1/admin/settings/updates", adminToken, nil)
	view = decodeData[map[string]any](t, envelope)
	if rec.Code != http.StatusOK || view["api_base"] != updates.OfficialAPIBase || view["fixed"] != true {
		t.Fatalf("未注入时应回退官方缺省并保持只读：HTTP %d %+v", rec.Code, view)
	}
}
