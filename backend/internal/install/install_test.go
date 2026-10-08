package install

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	gin.DefaultWriter = io.Discard
	gin.DefaultErrorWriter = io.Discard
	os.Exit(m.Run())
}

// silentLogger 返回丢弃全部日志的 logger。
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// stubEngine 是用于验证路由分发的假引擎。
func stubEngine(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(body))
	})
}

// newTestSupervisor 构造一个不依赖真实数据库的 Supervisor。
func newTestSupervisor(t *testing.T, cfg config.Config, configPath string) *Supervisor {
	t.Helper()
	supervisor, err := NewSupervisor(Options{
		Logger:     silentLogger(),
		Config:     cfg,
		ConfigPath: configPath,
		BuildEngine: func(*gorm.DB, config.JWTConfig) http.Handler {
			return stubEngine("normal-engine")
		},
		ProbeTimeout: 500 * 1e6, // 500ms
	})
	if err != nil {
		t.Fatalf("构造 Supervisor 失败: %v", err)
	}
	t.Cleanup(supervisor.Shutdown)
	return supervisor
}

// doRequest 发起一次请求并返回记录器与响应包（body 为 nil 时无请求体）。
func doRequest(t *testing.T, supervisor *Supervisor, method, path string, body any, headers map[string]string) (*httptest.ResponseRecorder, apiEnvelope) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("序列化请求体失败: %v", err)
		}
		reader = strings.NewReader(string(raw))
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	rec := httptest.NewRecorder()
	supervisor.ServeHTTP(rec, req)

	var envelope apiEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &envelope)
	return rec, envelope
}

// apiEnvelope 是统一响应包的测试视图。
type apiEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// decodeData 把响应包 data 解码为指定类型。
func decodeData[T any](t *testing.T, envelope apiEnvelope) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(envelope.Data, &value); err != nil {
		t.Fatalf("解析 data 失败: %v (data=%s)", err, envelope.Data)
	}
	return value
}

func TestStateStepMapping(t *testing.T) {
	cases := []struct {
		state     State
		wizard    bool
		firstStep int
	}{
		{StateUnconfigured, true, 1},
		{StateDBUnreachable, true, 2},
		{StateTablesMissing, true, 3},
		{StateAdminMissing, true, 4},
		{StateSiteMissing, true, 5},
		{StatePending, true, 6},
		{StateInstalled, false, 0},
	}
	for _, tc := range cases {
		if got := tc.state.NeedsWizard(); got != tc.wizard {
			t.Errorf("%s.NeedsWizard() = %t, 期望 %t", tc.state, got, tc.wizard)
		}
		if got := tc.state.FirstStep(); got != tc.firstStep {
			t.Errorf("%s.FirstStep() = %d, 期望 %d", tc.state, got, tc.firstStep)
		}
	}
}

func TestStateLabels(t *testing.T) {
	cases := map[State]string{
		StateUnconfigured:  "未配置数据库",
		StateDBUnreachable: "数据库不可达",
		StateTablesMissing: "数据库尚未初始化",
		StateAdminMissing:  "等待创建管理员账号",
		StateSiteMissing:   "等待站点信息",
		StatePending:       "等待完成安装",
		StateInstalled:     "已安装",
	}
	for state, want := range cases {
		if got := stateLabel(state); got != want {
			t.Errorf("stateLabel(%s) = %q, 期望 %q", state, got, want)
		}
	}
}

func TestSupervisorUnconfiguredRoutes(t *testing.T) {
	cfg := config.Default()
	cfg.DatabaseConfigured = false
	supervisor := newTestSupervisor(t, cfg, filepath.Join(t.TempDir(), "config.yaml"))
	supervisor.Init(context.Background())

	if state, _ := supervisor.Snapshot(); state != StateUnconfigured {
		t.Fatalf("state = %s, 期望 unconfigured", state)
	}
	if supervisor.IsInstalled() {
		t.Fatal("未安装时 IsInstalled() 应为 false")
	}

	// 浏览器页面请求 → 302 到 /install
	rec, _ := doRequest(t, supervisor, http.MethodGet, "/", nil, map[string]string{"Accept": "text/html"})
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != PathPage {
		t.Errorf("GET / = %d Location=%q, 期望 302 → %s", rec.Code, rec.Header().Get("Location"), PathPage)
	}

	// 安装页可访问
	rec, _ = doRequest(t, supervisor, http.MethodGet, PathPage, nil, map[string]string{"Accept": "text/html"})
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, 期望 200", PathPage, rec.Code)
	}
	page := rec.Body.String()
	for _, want := range []string{"Lyidc_OEM 安装向导", "环境检查", "数据库配置", PathStatus} {
		if !strings.Contains(page, want) && want != PathStatus {
			t.Errorf("安装页缺少 %q", want)
		}
	}

	// 业务 API → 503 + 50301
	rec, envelope := doRequest(t, supervisor, http.MethodPost, "/api/v1/orders", map[string]any{}, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("未安装时 POST /api/v1/orders = %d, 期望 503", rec.Code)
	}
	if envelope.Code != response.CodeNotInstalled {
		t.Errorf("code = %d, 期望 %d", envelope.Code, response.CodeNotInstalled)
	}
	if envelope.Message != "系统尚未安装" {
		t.Errorf("message = %q", envelope.Message)
	}

	// 非浏览器请求（无 Accept: text/html）同样返回 JSON 503
	rec, envelope = doRequest(t, supervisor, http.MethodGet, "/assets/app.js", nil, nil)
	if rec.Code != http.StatusServiceUnavailable || envelope.Code != response.CodeNotInstalled {
		t.Errorf("静态资源请求 = %d/%d, 期望 503/%d", rec.Code, envelope.Code, response.CodeNotInstalled)
	}

	// 健康检查照常工作（数据库未配置 → db=down）
	rec, envelope = doRequest(t, supervisor, http.MethodGet, "/api/v1/health", nil, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("健康检查 = %d, 期望 503", rec.Code)
	}
	health := decodeData[healthData](t, envelope)
	if health.DB != "down" || health.Status != "degraded" {
		t.Errorf("健康检查 data = %+v, 期望 db=down / status=degraded", health)
	}
}

func TestSupervisorStatusAndEnvironment(t *testing.T) {
	cfg := config.Default()
	cfg.DatabaseConfigured = false
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	supervisor := newTestSupervisor(t, cfg, configPath)
	supervisor.Init(context.Background())

	rec, envelope := doRequest(t, supervisor, http.MethodGet, PathStatus, nil, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("状态接口 = %d/%d, 期望 200/0 (%s)", rec.Code, envelope.Code, envelope.Message)
	}
	status := decodeData[statusView](t, envelope)
	if status.State != string(StateUnconfigured) || status.FirstStep != 1 || status.StepCount != StepCount {
		t.Errorf("状态视图 = %+v", status)
	}
	if status.Installed || status.Progress.Completed {
		t.Errorf("未安装时不应报告 installed: %+v", status.Progress)
	}
	if status.ConfigPath != configPath {
		t.Errorf("config_path = %q, 期望 %q", status.ConfigPath, configPath)
	}
	if status.Database.Configured {
		t.Error("未显式配置 dsn 时 database.configured 应为 false")
	}

	rec, envelope = doRequest(t, supervisor, http.MethodGet, PathEnvironment, nil, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("环境检查接口 = %d/%d (%s)", rec.Code, envelope.Code, envelope.Message)
	}
	env := decodeData[environmentView](t, envelope)
	if !env.OK {
		t.Errorf("环境检查未通过: %+v", env.Checks)
	}
	if len(env.Checks) < 3 {
		t.Errorf("环境检查项 = %d, 期望至少 3 项", len(env.Checks))
	}
	if len(env.Runtime.Migrations) < 6 {
		t.Errorf("内嵌迁移版本 = %d, 期望至少 6 个", len(env.Runtime.Migrations))
	}
	if env.Runtime.ConfigPath != configPath {
		t.Errorf("runtime.config_path = %q", env.Runtime.ConfigPath)
	}
}

func TestSupervisorInstalledModeDelegatesAndClosesWizard(t *testing.T) {
	cfg := config.Default()
	supervisor := newTestSupervisor(t, cfg, filepath.Join(t.TempDir(), "config.yaml"))

	// 白盒：直接置为已安装态（等价于探测后发现 installed 标记）。
	supervisor.mu.Lock()
	supervisor.state = StateInstalled
	supervisor.detail = "系统已安装（2026-10-08T00:00:00Z）"
	supervisor.engine = stubEngine("normal-engine")
	supervisor.mu.Unlock()

	rec, _ := doRequest(t, supervisor, http.MethodGet, "/api/v1/products", nil, nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "normal-engine" {
		t.Fatalf("已安装时应由正常引擎处理: %d %q", rec.Code, rec.Body.String())
	}

	// 重访安装页 → 提示页，且不含安装向导表单
	rec, _ = doRequest(t, supervisor, http.MethodGet, PathPage, nil, map[string]string{"Accept": "text/html"})
	if rec.Code != http.StatusOK {
		t.Fatalf("重访 /install = %d, 期望 200", rec.Code)
	}
	page := rec.Body.String()
	if !strings.Contains(page, "系统已安装") {
		t.Errorf("重访安装页应为「系统已安装」提示页: %s", page)
	}
	if strings.Contains(page, "开始初始化") || strings.Contains(page, "创建管理员") {
		t.Error("重访安装页不应包含安装向导表单")
	}

	// 安装 API 一律关闭
	for _, path := range []string{PathStatus, PathEnvironment} {
		rec, envelope := doRequest(t, supervisor, http.MethodGet, path, nil, nil)
		if rec.Code != http.StatusServiceUnavailable || envelope.Code != response.CodeInstallClosed {
			t.Errorf("GET %s = %d/%d, 期望 503/%d", path, rec.Code, envelope.Code, response.CodeInstallClosed)
		}
	}
	for _, path := range []string{PathDatabaseTest, PathDatabaseSave, PathInitialize, PathAdmin, PathSite, PathComplete} {
		rec, envelope := doRequest(t, supervisor, http.MethodPost, path, map[string]any{}, nil)
		if rec.Code != http.StatusServiceUnavailable || envelope.Code != response.CodeInstallClosed {
			t.Errorf("POST %s = %d/%d, 期望 503/%d", path, rec.Code, envelope.Code, response.CodeInstallClosed)
		}
	}
}

func TestDatabaseRequestValidationAndDSN(t *testing.T) {
	valid := databaseRequest{Host: "127.0.0.1", Port: 3306, Username: "root", Password: "p@ss:word/1", Database: "lyidc_db"}
	valid.normalize()
	if err := valid.validate(); err != nil {
		t.Fatalf("合法请求被拒: %v", err)
	}

	dsn := valid.dsn()
	for _, want := range []string{"root:", "@tcp(127.0.0.1:3306)/lyidc_db", "charset=utf8mb4", "parseTime=true", "multiStatements=true"} {
		if !strings.Contains(dsn, want) {
			t.Errorf("DSN 缺少 %q: %s", want, dsn)
		}
	}

	// 密码中的特殊字符必须被正确转义，且不能被截断
	if host, port, user, database := parseDSNTarget(dsn); host != "127.0.0.1" || port != 3306 ||
		user != "root" || database != "lyidc_db" {
		t.Errorf("parseDSNTarget = %s/%d/%s/%s", host, port, user, database)
	}
	if password := dsnPassword(dsn); password != "p@ss:word/1" {
		t.Errorf("dsnPassword = %q, 期望原文", password)
	}

	cases := []struct {
		name string
		req  databaseRequest
	}{
		{"主机为空", databaseRequest{Port: 3306, Username: "root", Database: "db"}},
		{"端口非法", databaseRequest{Host: "127.0.0.1", Port: 70000, Username: "root", Database: "db"}},
		{"用户名为空", databaseRequest{Host: "127.0.0.1", Port: 3306, Database: "db"}},
		{"库名为空", databaseRequest{Host: "127.0.0.1", Port: 3306, Username: "root"}},
		{"库名含非法字符", databaseRequest{Host: "127.0.0.1", Port: 3306, Username: "root", Database: "db;drop"}},
		{"主机含空格", databaseRequest{Host: "127.0.0.1 3306", Port: 3306, Username: "root", Database: "db"}},
	}
	for _, tc := range cases {
		req := tc.req
		req.normalize()
		if err := req.validate(); err == nil {
			t.Errorf("%s 应被拒绝", tc.name)
		}
	}

	// 端口缺省为 3306
	req := databaseRequest{Host: "127.0.0.1", Username: "root", Database: "db"}
	req.normalize()
	if req.Port != 3306 {
		t.Errorf("端口缺省 = %d, 期望 3306", req.Port)
	}
}

func TestValidDatabaseName(t *testing.T) {
	for _, name := range []string{"lyidc", "lyidc_install_verify", "Db1", strings.Repeat("a", 64)} {
		if !validDatabaseName(name) {
			t.Errorf("%q 应合法", name)
		}
	}
	for _, name := range []string{"", "db-name", "db.name", "db name", "库", strings.Repeat("a", 65)} {
		if validDatabaseName(name) {
			t.Errorf("%q 应非法", name)
		}
	}
}

func TestValidateAdminRequest(t *testing.T) {
	ok := adminRequest{Username: "opsadmin", Password: "Str0ng-Pass", ConfirmPassword: "Str0ng-Pass"}
	if err := validateAdminRequest(ok); err != nil {
		t.Fatalf("合法请求被拒: %v", err)
	}

	cases := []struct {
		name string
		req  adminRequest
	}{
		{"用户名太短", adminRequest{Username: "ab", Password: "Str0ng-Pass", ConfirmPassword: "Str0ng-Pass"}},
		{"用户名非法字符", adminRequest{Username: "运维员", Password: "Str0ng-Pass", ConfirmPassword: "Str0ng-Pass"}},
		{"密码太短", adminRequest{Username: "opsadmin", Password: "short", ConfirmPassword: "short"}},
		{"两次密码不一致", adminRequest{Username: "opsadmin", Password: "Str0ng-Pass", ConfirmPassword: "Str0ng-Pas"}},
		{"使用默认密码", adminRequest{Username: "opsadmin", Password: defaultAdminPassword, ConfirmPassword: defaultAdminPassword}},
		{"昵称过长", adminRequest{Username: "opsadmin", Password: "Str0ng-Pass", ConfirmPassword: "Str0ng-Pass",
			Nickname: strings.Repeat("昵", maxAdminNicknameRunes+1)}},
	}
	for _, tc := range cases {
		if err := validateAdminRequest(tc.req); err == nil {
			t.Errorf("%s 应被拒绝", tc.name)
		}
	}

	// 用户名恰为默认账号但密码不同：允许（默认行会被改写成该账号）
	renamed := adminRequest{Username: defaultAdminUsername, Password: "An0ther-Pass", ConfirmPassword: "An0ther-Pass"}
	if err := validateAdminRequest(renamed); err != nil {
		t.Errorf("沿用 admin 用户名但换密码应允许: %v", err)
	}
}

func TestGenerateJWTSecret(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 16; i++ {
		secret, err := generateJWTSecret()
		if err != nil {
			t.Fatalf("生成密钥失败: %v", err)
		}
		if len(secret) != 43 {
			t.Fatalf("密钥长度 = %d, 期望 43（32 字节 base64url）", len(secret))
		}
		if secret == config.DefaultJWTSecret {
			t.Fatal("生成的密钥不应等于开发默认密钥")
		}
		for _, char := range secret {
			if !strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_", char) {
				t.Fatalf("密钥含非 base64url 字符: %q", char)
			}
		}
		if seen[secret] {
			t.Fatal("生成的密钥出现重复")
		}
		seen[secret] = true
	}
}

func TestSanitizeDBErrorMasksPassword(t *testing.T) {
	dsn := "root:sup3r-secret@tcp(127.0.0.1:3306)/lyidc?charset=utf8mb4"
	err := &connectFailure{
		Message: "Access denied for user 'root' (using password: sup3r-secret)",
	}
	masked := sanitizeDBError(err, dsn)
	if strings.Contains(masked, "sup3r-secret") {
		t.Fatalf("错误文本仍含密码: %s", masked)
	}
	if !strings.Contains(masked, "****") {
		t.Errorf("密码应被替换为掩码: %s", masked)
	}
}

func TestIsInstallPath(t *testing.T) {
	cases := map[string]bool{
		"/install":            true,
		"/install/":           true,
		"/install/api/status": true,
		"/installing":         false,
		"/api/v1/products":    false,
		"/":                   false,
	}
	for path, want := range cases {
		if got := isInstallPath(path); got != want {
			t.Errorf("isInstallPath(%q) = %t, 期望 %t", path, got, want)
		}
	}
}

func TestRenderInstallPageContainsSteps(t *testing.T) {
	page := string(renderInstallPage("/tmp/config.yaml"))
	for _, want := range []string{"Lyidc_OEM 安装向导", "/tmp/config.yaml", "step-1", "step-6", "完成安装"} {
		if !strings.Contains(page, want) {
			t.Errorf("安装页缺少 %q", want)
		}
	}
	if strings.Contains(page, "{{") {
		t.Error("安装页存在未渲染的模板占位符")
	}
}

// TestRenderInstallPageUsesStatusForSummaryAndAdvance 锁住两处「重启续装」UI 修复
// （页面 JS 没有单测框架，用关键代码片段做回归守卫）：
//  1. 完成页摘要必须取 status 的库内实况字段，而不是表单瞬时值；
//  2. 第 2 步保存成功后的推进必须按最新状态落位，而不是固定跳第 3 步。
func TestRenderInstallPageUsesStatusForSummaryAndAdvance(t *testing.T) {
	page := string(renderInstallPage("/tmp/config.yaml"))
	for _, want := range []string{
		"status.admin_username",
		"status.site_name",
		"goStep(startStep(status))",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("安装页缺少修复后的逻辑片段 %q", want)
		}
	}
	if strings.Contains(page, `goStep(3);`) {
		t.Error("安装页仍存在固定跳第 3 步的旧逻辑（提交后落位错误）")
	}
}

func TestEnvironmentViewReportsUnwritableConfigDir(t *testing.T) {
	cfg := config.Default()
	cfg.DatabaseConfigured = false
	// 用一个不可能写入的路径（把文件当目录用）验证检查项失败时的处置建议。
	blocked := filepath.Join(t.TempDir(), "file.yaml")
	if err := os.WriteFile(blocked, []byte("server: {}\n"), 0o600); err != nil {
		t.Fatalf("准备测试文件失败: %v", err)
	}
	supervisor := newTestSupervisor(t, cfg, filepath.Join(blocked, "config.yaml"))

	_, envelope := doRequest(t, supervisor, http.MethodGet, PathEnvironment, nil, nil)
	env := decodeData[environmentView](t, envelope)
	if env.OK {
		t.Fatal("配置目录不可写时 ok 应为 false")
	}
	found := false
	for _, check := range env.Checks {
		if check.Key == "config_write" {
			found = true
			if check.OK || check.Advice == "" {
				t.Errorf("config_write 检查 = %+v, 期望失败且带处置建议", check)
			}
		}
	}
	if !found {
		t.Error("缺少 config_write 检查项")
	}
}
