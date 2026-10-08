package install

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	gomysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/auth"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/db"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/router"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// defaultInstallTestDSN 指向独立的安装测试库（与 router 的 lyidc_test 分开，互不影响）。
// CI 可通过环境变量 LYIDC_INSTALL_TEST_DSN 覆盖。
const defaultInstallTestDSN = "root:lyidc123@tcp(127.0.0.1:3306)/lyidc_install_test" +
	"?charset=utf8mb4&parseTime=true&loc=UTC&multiStatements=true"

// 测试用管理员密码（不写死在业务代码里）。
const testInstallerPassword = "0ps-Str0ng-Pass"

// installTestDSN 返回集成测试使用的 DSN。
func installTestDSN() string {
	if dsn := strings.TrimSpace(os.Getenv("LYIDC_INSTALL_TEST_DSN")); dsn != "" {
		return dsn
	}
	return defaultInstallTestDSN
}

// freshInstallDatabase 确保测试库存在并清空全部表（DROP），返回 DSN 与对应的表单参数。
// 本机没有 MySQL 时以 t.Skip 跳过。
func freshInstallDatabase(t *testing.T) (string, databaseRequest) {
	t.Helper()

	dsn := installTestDSN()
	parsed, err := gomysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("解析测试 DSN 失败: %v", err)
	}
	if parsed.DBName == "" {
		t.Fatal("测试 DSN 必须包含数据库名")
	}

	serverDSN := *parsed
	serverDSN.DBName = ""
	serverDB, err := sql.Open("mysql", serverDSN.FormatDSN())
	if err != nil {
		t.Fatalf("连接 MySQL 服务失败: %v", err)
	}
	defer func() { _ = serverDB.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := serverDB.PingContext(ctx); err != nil {
		t.Skipf("跳过集成测试：MySQL 测试库不可用（%v）", err)
	}
	if _, err := serverDB.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS `"+parsed.DBName+
		"` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci"); err != nil {
		t.Fatalf("创建测试库失败: %v", err)
	}

	dropAllTables(t, dsn, parsed.DBName)

	host, portText, err := net.SplitHostPort(parsed.Addr)
	if err != nil {
		t.Fatalf("解析测试库地址失败: %v", err)
	}
	port := 0
	if _, err := fmt.Sscanf(portText, "%d", &port); err != nil {
		t.Fatalf("解析测试库端口失败: %v", err)
	}
	return dsn, databaseRequest{
		Host:           host,
		Port:           port,
		Username:       parsed.User,
		Password:       parsed.Passwd,
		Database:       parsed.DBName,
		CreateDatabase: true,
	}
}

// dropAllTables 删除目标库的全部表。
func dropAllTables(t *testing.T, dsn, database string) {
	t.Helper()

	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	rows, err := sqlDB.Query(
		"SELECT table_name FROM information_schema.tables WHERE table_schema = ?", database)
	if err != nil {
		t.Fatalf("查询测试库表清单失败: %v", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("读取表名失败: %v", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("遍历表清单失败: %v", err)
	}
	_ = rows.Close()

	if _, err := sqlDB.Exec("SET FOREIGN_KEY_CHECKS = 0"); err != nil {
		t.Fatalf("关闭外键检查失败: %v", err)
	}
	for _, name := range tables {
		if _, err := sqlDB.Exec("DROP TABLE IF EXISTS `" + name + "`"); err != nil {
			t.Fatalf("删除表 %s 失败: %v", name, err)
		}
	}
}

// installTestConfig 构造测试用配置。
func installTestConfig(dsn string, configured bool, configPath string) config.Config {
	cfg := config.Default()
	cfg.Server.Mode = "test"
	cfg.Database.DSN = dsn
	cfg.DatabaseConfigured = configured
	cfg.SourcePath = configPath
	return cfg
}

// newInstallSupervisor 构造带真实 router 引擎的 Supervisor（安装完成后登录接口真实可用）。
func newInstallSupervisor(t *testing.T, cfg config.Config, configPath string) *Supervisor {
	t.Helper()

	supervisor, err := NewSupervisor(Options{
		Logger:     silentLogger(),
		Config:     cfg,
		ConfigPath: configPath,
		BuildEngine: func(gdb *gorm.DB, jwt config.JWTConfig) http.Handler {
			return router.New(router.Options{
				Logger: silentLogger(),
				Ping:   db.PingFunc(gdb),
				DB:     gdb,
				JWT:    jwt,
			})
		},
		ProbeTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("构造 Supervisor 失败: %v", err)
	}
	t.Cleanup(supervisor.Shutdown)
	return supervisor
}

// openTestDB 打开测试库的 GORM 句柄（断言库内状态用）。
func openTestDB(t *testing.T, dsn string) *gorm.DB {
	t.Helper()

	gdb, err := db.Open(config.DatabaseConfig{DSN: dsn, MaxOpenConns: 3, MaxIdleConns: 1})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	gdb = gdb.Session(&gorm.Session{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	t.Cleanup(func() {
		if err := db.Close(gdb); err != nil {
			t.Logf("关闭测试库失败: %v", err)
		}
	})
	return gdb
}

// migrateForTest 用安装代码本身执行迁移（准备场景用的库结构）。
func migrateForTest(t *testing.T, dsn string) migrationResult {
	t.Helper()

	result, err := runMigrations(context.Background(), dsn)
	if err != nil {
		t.Fatalf("准备测试库（执行迁移）失败: %v", err)
	}
	return result
}

// expectSuccess 断言响应为成功包并返回 data。
func expectSuccess(t *testing.T, rec *httptest.ResponseRecorder, envelope apiEnvelope, what string) {
	t.Helper()
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("%s 失败: HTTP %d, code=%d, message=%s", what, rec.Code, envelope.Code, envelope.Message)
	}
}

// TestInstallFullFlow 走完整个安装向导（场景 1 → 正常模式），覆盖 A 节的全部关键判定。
func TestInstallFullFlow(t *testing.T) {
	dsn, form := freshInstallDatabase(t)

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	// 预置一个「既有配置文件」：验证合并写入保留既有键与注释。
	existing := "# 既有注释（必须保留）\nserver:\n  addr: \"0.0.0.0:9999\"\n  mode: \"debug\"\n\nlog:\n  level: \"warn\"\n"
	if err := os.WriteFile(configPath, []byte(existing), 0o600); err != nil {
		t.Fatalf("准备既有配置文件失败: %v", err)
	}

	cfg := installTestConfig(config.DefaultDSN, false, configPath)
	supervisor := newInstallSupervisor(t, cfg, configPath)
	supervisor.Init(context.Background())

	if state, _ := supervisor.Snapshot(); state != StateUnconfigured {
		t.Fatalf("初始状态 = %s, 期望 unconfigured（未显式配置 dsn）", state)
	}

	// —— 未安装时的拦截行为 ——
	rec, _ := doRequest(t, supervisor, http.MethodGet, "/", nil, map[string]string{"Accept": "text/html"})
	if rec.Code != http.StatusFound {
		t.Errorf("未安装时 GET / = %d, 期望 302", rec.Code)
	}
	rec, envelope := doRequest(t, supervisor, http.MethodPost, "/api/v1/admin/auth/login",
		map[string]string{"username": "admin", "password": "admin123456"}, nil)
	if rec.Code != http.StatusServiceUnavailable || envelope.Code != response.CodeNotInstalled {
		t.Errorf("未安装时登录接口 = %d/%d, 期望 503/%d", rec.Code, envelope.Code, response.CodeNotInstalled)
	}
	rec, envelope = doRequest(t, supervisor, http.MethodGet, "/api/v1/health", nil, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("未安装时健康检查 = %d, 期望 503", rec.Code)
	}
	rec, _ = doRequest(t, supervisor, http.MethodGet, PathPage, nil, map[string]string{"Accept": "text/html"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "安装向导") {
		t.Fatalf("安装页 = %d, body 片段 = %.120s", rec.Code, rec.Body.String())
	}

	// —— 第 1 步：环境检查 ——
	rec, envelope = doRequest(t, supervisor, http.MethodGet, PathEnvironment, nil, nil)
	env := decodeData[environmentView](t, envelope)
	expectSuccess(t, rec, envelope, "环境检查")
	if !env.OK || len(env.Runtime.Migrations) != 8 {
		t.Fatalf("环境检查结果异常: ok=%t migrations=%d", env.OK, len(env.Runtime.Migrations))
	}
	if env.Runtime.Migrations[7].Name != "0008_create_instance_ops_and_renew" {
		t.Errorf("最后一个迁移 = %s", env.Runtime.Migrations[7].Name)
	}

	// —— 第 2 步：测试连接 + 保存 ——
	rec, envelope = doRequest(t, supervisor, http.MethodPost, PathDatabaseTest, form, nil)
	view := decodeData[databaseView](t, envelope)
	expectSuccess(t, rec, envelope, "测试数据库连接")
	if !view.Connected || !strings.HasPrefix(view.ServerVersion, "5.7") {
		t.Fatalf("连接结果异常: %+v", view)
	}
	if view.TableCount != 0 {
		t.Errorf("新库表数量 = %d, 期望 0", view.TableCount)
	}

	rec, envelope = doRequest(t, supervisor, http.MethodPost, PathDatabaseSave, form, nil)
	saveView := decodeData[databaseSaveView](t, envelope)
	expectSuccess(t, rec, envelope, "保存数据库参数")
	if saveView.State != string(StateTablesMissing) || saveView.FirstStep != 3 {
		t.Fatalf("保存后状态 = %s/%d, 期望 tables_missing/3", saveView.State, saveView.FirstStep)
	}
	if raw := string(envelope.Data); strings.Contains(raw, form.Password) {
		t.Error("保存响应泄露了数据库密码")
	}

	// —— 第 3 步：初始化建表 ——
	rec, envelope = doRequest(t, supervisor, http.MethodPost, PathInitialize, map[string]any{}, nil)
	initResult := decodeData[initView](t, envelope)
	expectSuccess(t, rec, envelope, "初始化建表")
	if initResult.FromVersion != 0 || initResult.ToVersion != 8 || len(initResult.Applied) != 8 {
		t.Fatalf("迁移结果异常: %+v", initResult.migrationResult)
	}
	if initResult.State != string(StateAdminMissing) || initResult.FirstStep != 4 {
		t.Fatalf("建表后状态 = %s/%d, 期望 admin_missing/4", initResult.State, initResult.FirstStep)
	}

	// —— 第 4 步：管理员账号 ——
	rec, envelope = doRequest(t, supervisor, http.MethodPost, PathAdmin, map[string]string{
		"username":         "opsadmin",
		"password":         testInstallerPassword,
		"confirm_password": testInstallerPassword,
	}, nil)
	adminResult := decodeData[adminView](t, envelope)
	expectSuccess(t, rec, envelope, "创建管理员")
	// 全新库：迁移 0003 的默认管理员行被改写为安装者账号（替换而非新建，id 保持 1）
	if !adminResult.ReplacedDefaultAdmin || adminResult.Updated || adminResult.AdminID == 0 {
		t.Fatalf("管理员结果异常: %+v", adminResult)
	}
	// 续装定位：建完管理员 → 第 5 步（等待站点信息）
	if adminResult.State != string(StateSiteMissing) || adminResult.FirstStep != 5 {
		t.Fatalf("建管理员后状态 = %s/%d, 期望 site_missing/5", adminResult.State, adminResult.FirstStep)
	}

	// —— 第 5 步：站点信息 ——
	rec, envelope = doRequest(t, supervisor, http.MethodPost, PathSite, map[string]string{
		"name":        "Lyidc 安装验证站",
		"url":         "https://cloud.example.com/",
		"admin_email": "ops@example.com",
	}, nil)
	siteResult := decodeData[siteView](t, envelope)
	expectSuccess(t, rec, envelope, "保存站点信息")
	if siteResult.URL != "https://cloud.example.com" || siteResult.Name != "Lyidc 安装验证站" {
		t.Fatalf("站点信息异常: %+v", siteResult)
	}
	// 续装定位：存完站点 → 第 6 步（等待完成安装）
	if siteResult.State != string(StatePending) || siteResult.FirstStep != 6 {
		t.Fatalf("存站点后状态 = %s/%d, 期望 pending/6", siteResult.State, siteResult.FirstStep)
	}

	// —— 第 6 步：完成安装 ——
	rec, envelope = doRequest(t, supervisor, http.MethodPost, PathComplete, map[string]any{}, nil)
	body := rec.Body.String()
	completeResult := decodeData[completeView](t, envelope)
	expectSuccess(t, rec, envelope, "完成安装")
	if !completeResult.Installed || !completeResult.ConfigWritten || !completeResult.MarkerWritten ||
		completeResult.RestartRequired {
		t.Fatalf("完成结果异常: %+v", completeResult)
	}
	if completeResult.JWTSecretWritten != true {
		t.Error("jwt_secret_written 应为 true")
	}
	if completeResult.AdminConsole != adminConsolePath {
		t.Errorf("admin_console = %q, 期望 %q", completeResult.AdminConsole, adminConsolePath)
	}
	// 密钥安全：响应不得包含数据库密码或 JWT 密钥明文
	if strings.Contains(body, form.Password) {
		t.Error("完成响应泄露了数据库密码")
	}

	// —— 热切换：无需重启即可用新 JWT 密钥登录 ——
	if !supervisor.IsInstalled() {
		t.Fatal("完成后 IsInstalled() 应为 true")
	}
	rec, envelope = doRequest(t, supervisor, http.MethodPost, "/api/v1/admin/auth/login", map[string]string{
		"username": "opsadmin", "password": testInstallerPassword,
	}, nil)
	loginResult := decodeData[adminLoginView](t, envelope)
	expectSuccess(t, rec, envelope, "安装后管理员登录")
	if loginResult.Token == "" {
		t.Fatal("登录未返回 token")
	}
	if strings.Contains(rec.Body.String(), "jwt") && strings.Contains(rec.Body.String(), `"secret"`) {
		t.Error("登录响应包含密钥字段")
	}

	rec, envelope = doRequest(t, supervisor, http.MethodGet, "/api/v1/admin/profile", nil,
		map[string]string{"Authorization": "Bearer " + loginResult.Token})
	expectSuccess(t, rec, envelope, "安装后读取管理员资料")

	// 默认管理员已不可用（被改写为安装者账号）
	rec, _ = doRequest(t, supervisor, http.MethodPost, "/api/v1/admin/auth/login", map[string]string{
		"username": "admin", "password": "admin123456",
	}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("默认管理员登录 = %d, 期望 401", rec.Code)
	}

	// —— 安装页永久关闭 ——
	rec, _ = doRequest(t, supervisor, http.MethodGet, PathPage, nil, map[string]string{"Accept": "text/html"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>系统已安装 · Lyidc_OEM</title>") {
		t.Fatalf("重访安装页 = %d, 期望「系统已安装」提示页", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `id="step-1"`) {
		t.Error("重访安装页不应出现安装表单")
	}
	for _, path := range []string{PathStatus, PathEnvironment} {
		rec, envelope = doRequest(t, supervisor, http.MethodGet, path, nil, nil)
		if rec.Code != http.StatusServiceUnavailable || envelope.Code != response.CodeInstallClosed {
			t.Errorf("装完后 GET %s = %d/%d, 期望 503/%d", path, rec.Code, envelope.Code, response.CodeInstallClosed)
		}
	}
	rec, envelope = doRequest(t, supervisor, http.MethodPost, PathComplete, map[string]any{}, nil)
	if rec.Code != http.StatusServiceUnavailable || envelope.Code != response.CodeInstallClosed {
		t.Errorf("重复完成 = %d/%d, 期望 503/%d", rec.Code, envelope.Code, response.CodeInstallClosed)
	}

	// —— 正常模式接口恢复 ——
	rec, envelope = doRequest(t, supervisor, http.MethodGet, "/api/v1/health", nil, nil)
	expectSuccess(t, rec, envelope, "安装后健康检查")
	health := decodeData[healthData](t, envelope)
	if health.DB != "up" || health.Status != "ok" {
		t.Errorf("健康检查 = %+v, 期望 db=up", health)
	}
	rec, envelope = doRequest(t, supervisor, http.MethodGet, "/api/v1/products", nil, nil)
	expectSuccess(t, rec, envelope, "安装后读取公开商品目录")

	// —— 库内断言 ——
	gdb := openTestDB(t, dsn)
	st := store.New(gdb)
	marker, err := st.Setting(context.Background(), settings.KeyInstalled)
	if err != nil {
		t.Fatalf("读取 installed 标记失败: %v", err)
	}
	parsedMarker, err := settings.ParseInstalled(marker.Value)
	if err != nil || parsedMarker.Source != settings.SourceWizard || parsedMarker.Version != settings.MarkerVersion {
		t.Fatalf("installed 标记异常: %+v (%v)", parsedMarker, err)
	}

	siteSetting, err := st.Setting(context.Background(), settings.KeySite)
	if err != nil {
		t.Fatalf("读取 site 设置失败: %v", err)
	}
	var decodedSite settings.Site
	if err := json.Unmarshal([]byte(siteSetting.Value), &decodedSite); err != nil {
		t.Fatalf("site 设置不是合法 JSON: %v", err)
	}
	if decodedSite.Name != "Lyidc 安装验证站" || decodedSite.AdminEmail != "ops@example.com" {
		t.Errorf("site 设置 = %+v", decodedSite)
	}

	if _, err := st.Setting(context.Background(), settings.KeyInstallProgress); err == nil {
		t.Error("安装完成后不应残留 install.progress 进度标记")
	}

	var admins []model.Admin
	if err := gdb.Find(&admins).Error; err != nil {
		t.Fatalf("读取管理员失败: %v", err)
	}
	if len(admins) != 1 || admins[0].Username != "opsadmin" {
		t.Fatalf("管理员表 = %+v, 期望只有 opsadmin", admins)
	}
	var defaultRows int64
	if err := gdb.Model(&model.Admin{}).
		Where("password_hash = ?", store.DefaultAdminPasswordHash).Count(&defaultRows).Error; err != nil {
		t.Fatalf("统计默认管理员失败: %v", err)
	}
	if defaultRows != 0 {
		t.Fatalf("库内仍残留未修改的默认管理员 %d 行", defaultRows)
	}
	if !auth.VerifyPassword(admins[0].PasswordHash, testInstallerPassword) {
		t.Error("管理员密码哈希与安装时填写的密码不匹配")
	}

	// —— 配置文件断言：合并写入 + 随机密钥 ——
	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("写出的配置文件无法加载: %v", err)
	}
	if !loaded.DatabaseConfigured {
		t.Error("配置文件应显式包含 database.dsn")
	}
	if loaded.Database.DSN != form.dsn() {
		t.Errorf("配置文件 dsn = %q, 期望 %q", loaded.Database.DSN, form.dsn())
	}
	if loaded.JWT.Secret == config.DefaultJWTSecret || len(loaded.JWT.Secret) < 40 {
		t.Errorf("jwt.secret 未被替换为随机密钥（长度 %d）", len(loaded.JWT.Secret))
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("读取配置文件失败: %v", err)
	}
	for _, want := range []string{"# 既有注释（必须保留）", "0.0.0.0:9999", `level: "warn"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("合并后的配置文件丢失既有内容 %q:\n%s", want, raw)
		}
	}
}

// TestScenario5AutoMarkInstalled 覆盖契约 13.2 的场景 5：
// 库可达、表齐全、无 installed 标记但已有管理员 → 自动补标记并进入正常模式。
func TestScenario5AutoMarkInstalled(t *testing.T) {
	dsn, _ := freshInstallDatabase(t)

	t.Run("只有迁移写入的默认管理员（存量开发库）", func(t *testing.T) {
		migrateForTest(t, dsn)

		configPath := filepath.Join(t.TempDir(), "config.yaml")
		cfg := installTestConfig(dsn, true, configPath)
		supervisor := newInstallSupervisor(t, cfg, configPath)
		supervisor.Init(context.Background())

		if state, detail := supervisor.Snapshot(); state != StateInstalled {
			t.Fatalf("状态 = %s（%s）, 期望 installed", state, detail)
		}

		// /install 关闭、正常接口可用
		rec, _ := doRequest(t, supervisor, http.MethodGet, PathPage, nil, map[string]string{"Accept": "text/html"})
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "系统已安装") {
			t.Errorf("存量库应关闭安装页: %d", rec.Code)
		}
		rec, envelope := doRequest(t, supervisor, http.MethodGet, "/api/v1/products", nil, nil)
		expectSuccess(t, rec, envelope, "存量库正常模式接口")

		// 标记已补写，来源为 auto
		gdb := openTestDB(t, dsn)
		setting, err := store.New(gdb).Setting(context.Background(), settings.KeyInstalled)
		if err != nil {
			t.Fatalf("读取 installed 标记失败: %v", err)
		}
		marker, err := settings.ParseInstalled(setting.Value)
		if err != nil {
			t.Fatalf("解析 installed 标记失败: %v", err)
		}
		if marker.Source != settings.SourceAuto {
			t.Errorf("标记来源 = %q, 期望 auto", marker.Source)
		}
	})

	t.Run("已有安装者管理员", func(t *testing.T) {
		migrateForTest(t, dsn)
		gdb := openTestDB(t, dsn)
		hash, err := auth.HashPassword("An0ther-Pass")
		if err != nil {
			t.Fatalf("生成密码哈希失败: %v", err)
		}
		if _, err := store.New(gdb).EnsureInstallerAdmin(context.Background(), "opsadmin", hash, "站长"); err != nil {
			t.Fatalf("准备安装者管理员失败: %v", err)
		}
		if err := gdb.Exec("DELETE FROM settings WHERE `key` = ?", settings.KeyInstalled).Error; err != nil {
			t.Fatalf("清理安装标记失败: %v", err)
		}

		configPath := filepath.Join(t.TempDir(), "config.yaml")
		supervisor := newInstallSupervisor(t, installTestConfig(dsn, true, configPath), configPath)
		supervisor.Init(context.Background())

		if state, detail := supervisor.Snapshot(); state != StateInstalled {
			t.Fatalf("状态 = %s（%s）, 期望 installed", state, detail)
		}
	})
}

// TestScenario4AdminMissing 覆盖场景 4：表齐全、无标记、admins 为空 → 从管理员步骤起。
func TestScenario4AdminMissing(t *testing.T) {
	dsn, _ := freshInstallDatabase(t)
	migrateForTest(t, dsn)

	gdb := openTestDB(t, dsn)
	if err := gdb.Exec("DELETE FROM admins").Error; err != nil {
		t.Fatalf("清空管理员失败: %v", err)
	}

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	supervisor := newInstallSupervisor(t, installTestConfig(dsn, true, configPath), configPath)
	supervisor.Init(context.Background())

	state, detail := supervisor.Snapshot()
	if state != StateAdminMissing {
		t.Fatalf("状态 = %s（%s）, 期望 admin_missing", state, detail)
	}
	if firstStep := state.FirstStep(); firstStep != 4 {
		t.Errorf("起始步骤 = %d, 期望 4", firstStep)
	}
	if _, err := store.New(gdb).Setting(context.Background(), settings.KeyInstalled); err == nil {
		t.Error("场景 4 不应写入 installed 标记")
	}

	// 未安装时业务接口被拦截，但健康检查显示数据库可达
	rec, envelope := doRequest(t, supervisor, http.MethodGet, "/api/v1/products", nil, nil)
	if rec.Code != http.StatusServiceUnavailable || envelope.Code != response.CodeNotInstalled {
		t.Errorf("业务接口 = %d/%d, 期望 503/%d", rec.Code, envelope.Code, response.CodeNotInstalled)
	}
	rec, _ = doRequest(t, supervisor, http.MethodGet, "/api/v1/health", nil, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("数据库可达时健康检查 = %d, 期望 200", rec.Code)
	}
}

// TestScenario3TablesMissing 覆盖场景 3：库可达但核心表缺失 → 从初始化步骤起。
func TestScenario3TablesMissing(t *testing.T) {
	dsn, _ := freshInstallDatabase(t)

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	supervisor := newInstallSupervisor(t, installTestConfig(dsn, true, configPath), configPath)
	supervisor.Init(context.Background())

	state, detail := supervisor.Snapshot()
	if state != StateTablesMissing {
		t.Fatalf("状态 = %s（%s）, 期望 tables_missing", state, detail)
	}
	if !strings.Contains(detail, "settings") || !strings.Contains(detail, "admins") {
		t.Errorf("判定依据应列出缺失的核心表: %s", detail)
	}

	rec, envelope := doRequest(t, supervisor, http.MethodGet, PathStatus, nil, nil)
	status := decodeData[statusView](t, envelope)
	expectSuccess(t, rec, envelope, "空库状态查询")
	if !status.Database.Reachable || status.Progress.TablesReady || status.Progress.AdminReady {
		t.Errorf("状态视图 = %+v", status)
	}
	if status.FirstStep != 3 {
		t.Errorf("first_step = %d, 期望 3", status.FirstStep)
	}
}

// TestScenario2DBUnreachable 覆盖场景 2：配置存在但库连不上 → 安装/修复模式，health 报 db=down。
func TestScenario2DBUnreachable(t *testing.T) {
	// 取一个确定没人监听的端口
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("获取空闲端口失败: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	host, portText, _ := net.SplitHostPort(addr)
	dsn := fmt.Sprintf("root:wrongpass@tcp(%s)/lyidc?charset=utf8mb4&parseTime=true", addr)

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	supervisor := newInstallSupervisor(t, installTestConfig(dsn, true, configPath), configPath)
	supervisor.Init(context.Background())

	state, detail := supervisor.Snapshot()
	if state != StateDBUnreachable {
		t.Fatalf("状态 = %s（%s）, 期望 db_unreachable", state, detail)
	}
	if strings.Contains(detail, "wrongpass") {
		t.Fatalf("判定依据泄露了密码: %s", detail)
	}

	// 安装页可用（提示连接失败），健康检查 db=down
	rec, _ := doRequest(t, supervisor, http.MethodGet, PathPage, nil, map[string]string{"Accept": "text/html"})
	if rec.Code != http.StatusOK {
		t.Errorf("修复模式下安装页 = %d, 期望 200", rec.Code)
	}
	rec, _ = doRequest(t, supervisor, http.MethodGet, "/api/v1/health", nil, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("健康检查 = %d, 期望 503（db=down）", rec.Code)
	}

	// 第 2 步的测试连接返回 50303 + 处置建议，且不回显密码
	port := 0
	_, _ = fmt.Sscanf(portText, "%d", &port)
	rec, envelope := doRequest(t, supervisor, http.MethodPost, PathDatabaseTest, databaseRequest{
		Host: host, Port: port, Username: "root", Password: "wrongpass", Database: "lyidc",
	}, nil)
	if rec.Code != http.StatusServiceUnavailable || envelope.Code != response.CodeDBConnectFailed {
		t.Fatalf("连接失败响应 = %d/%d, 期望 503/%d", rec.Code, envelope.Code, response.CodeDBConnectFailed)
	}
	if strings.Contains(rec.Body.String(), "wrongpass") {
		t.Error("连接失败响应泄露了密码")
	}
	var failure struct {
		Advice string `json:"advice"`
	}
	if err := json.Unmarshal(envelope.Data, &failure); err != nil || failure.Advice == "" {
		t.Errorf("连接失败响应缺少处置建议: %s", envelope.Data)
	}

	// 数据库不可达时重复「完成」应被拒绝（引导回第 2 步）
	rec, envelope = doRequest(t, supervisor, http.MethodPost, PathComplete, map[string]any{}, nil)
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusServiceUnavailable {
		t.Errorf("未完成前置步骤时完成安装 = %d, 期望 4xx/503", rec.Code)
	}
}

// TestWizardStatusFollowsEachStep 覆盖续装态的逐步推进（契约 13.1 补充规则 3）：
// 每一步之后 `GET /install/api/status` 的 state / state_label / first_step 都必须与实况一致
// （修复前「管理员已建、站点未建」会被错误报成 admin_missing / 第 4 步）。
func TestWizardStatusFollowsEachStep(t *testing.T) {
	_, form := freshInstallDatabase(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	supervisor := newInstallSupervisor(t, installTestConfig(config.DefaultDSN, false, configPath), configPath)
	supervisor.Init(context.Background())

	// 第 2 步：保存数据库参数（场景 1 起步必需）
	rec, envelope := doRequest(t, supervisor, http.MethodPost, PathDatabaseSave, form, nil)
	expectSuccess(t, rec, envelope, "保存数据库参数")

	// 建表完成 → 等待管理员（第 4 步）
	rec, envelope = doRequest(t, supervisor, http.MethodPost, PathInitialize, map[string]any{}, nil)
	initResult := decodeData[initView](t, envelope)
	expectSuccess(t, rec, envelope, "初始化建表")
	if initResult.State != string(StateAdminMissing) || initResult.FirstStep != 4 {
		t.Fatalf("建表后 = %s/%d, 期望 admin_missing/4", initResult.State, initResult.FirstStep)
	}
	// 尚未建管理员、未写站点：摘要字段应为空（界面显示「未创建/未填写」）
	status := assertStatusState(t, supervisor, StateAdminMissing, "等待创建管理员账号", 4)
	assertSummaryFields(t, status, "", "")

	// 建管理员完成 → 等待站点信息（第 5 步）
	rec, envelope = doRequest(t, supervisor, http.MethodPost, PathAdmin, map[string]string{
		"username": "opsadmin", "password": testInstallerPassword, "confirm_password": testInstallerPassword,
	}, nil)
	adminResult := decodeData[adminView](t, envelope)
	expectSuccess(t, rec, envelope, "创建管理员")
	if adminResult.State != string(StateSiteMissing) || adminResult.FirstStep != 5 {
		t.Fatalf("建管理员后 = %s/%d, 期望 site_missing/5", adminResult.State, adminResult.FirstStep)
	}
	// 管理员已建：摘要字段应反映库内实况（站点仍未写）
	status = assertStatusState(t, supervisor, StateSiteMissing, "等待站点信息", 5)
	assertSummaryFields(t, status, "opsadmin", "")

	// 存站点完成 → 等待完成安装（第 6 步）
	rec, envelope = doRequest(t, supervisor, http.MethodPost, PathSite, map[string]string{
		"name": "续装验证站", "url": "https://verify.example.com",
	}, nil)
	siteResult := decodeData[siteView](t, envelope)
	expectSuccess(t, rec, envelope, "保存站点信息")
	if siteResult.State != string(StatePending) || siteResult.FirstStep != 6 {
		t.Fatalf("存站点后 = %s/%d, 期望 pending/6", siteResult.State, siteResult.FirstStep)
	}
	// 管理员与站点都已就绪：摘要字段两项都要有值（完成页据此展示，不依赖表单瞬时值）
	status = assertStatusState(t, supervisor, StatePending, "等待完成安装", 6)
	assertSummaryFields(t, status, "opsadmin", "续装验证站")

	// 未完成前仍是安装模式：安装页是向导页，业务接口继续被拦截
	rec, _ = doRequest(t, supervisor, http.MethodGet, PathPage, nil, map[string]string{"Accept": "text/html"})
	// 页标题是区分两个页面的可靠标志（提示页与向导页的 JS 源码里都含「系统已安装」字样）。
	page := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(page, "<title>Lyidc_OEM 安装向导</title>") ||
		strings.Contains(page, "<title>系统已安装 · Lyidc_OEM</title>") {
		t.Errorf("未完成安装时安装页应为向导页: %d", rec.Code)
	}
	rec, envelope = doRequest(t, supervisor, http.MethodGet, "/api/v1/products", nil, nil)
	if rec.Code != http.StatusServiceUnavailable || envelope.Code != response.CodeNotInstalled {
		t.Errorf("未完成安装时业务接口 = %d/%d, 期望 503/%d", rec.Code, envelope.Code, response.CodeNotInstalled)
	}

	// 完成安装后状态收敛为 installed（第 0 步）
	rec, envelope = doRequest(t, supervisor, http.MethodPost, PathComplete, map[string]any{}, nil)
	expectSuccess(t, rec, envelope, "完成安装")
	if !supervisor.IsInstalled() {
		t.Fatal("完成安装后应为已安装状态")
	}
}

// TestRestartResumesAtCorrectStep 覆盖「进程重启后续装定位」：
// 向导中途重启（库内留有 install.progress）时，启动探测落在正确的续装步骤，
// 既不会被场景 5 误判为存量库，也不会一律回退到第 4 步。
func TestRestartResumesAtCorrectStep(t *testing.T) {
	dsn, _ := freshInstallDatabase(t)
	migrateForTest(t, dsn)

	gdb := openTestDB(t, dsn)
	st := store.New(gdb)
	hash, err := auth.HashPassword(testInstallerPassword)
	if err != nil {
		t.Fatalf("生成密码哈希失败: %v", err)
	}
	if _, err := st.EnsureInstallerAdmin(context.Background(), "opsadmin", hash, "站长"); err != nil {
		t.Fatalf("准备安装者管理员失败: %v", err)
	}

	// 阶段一：管理员已建、站点未建、进度标记 stage=admin → 重启后应落在第 5 步
	writeProgressMarker(t, st, settings.StageAdmin)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	supervisor := newInstallSupervisor(t, installTestConfig(dsn, true, configPath), configPath)
	supervisor.Init(context.Background())

	if state, detail := supervisor.Snapshot(); state != StateSiteMissing {
		t.Fatalf("重启后状态 = %s（%s）, 期望 site_missing", state, detail)
	}
	assertSummaryFields(t, assertStatusState(t, supervisor, StateSiteMissing, "等待站点信息", 5), "opsadmin", "")
	if _, err := st.Setting(context.Background(), settings.KeyInstalled); err == nil {
		t.Error("续装态不应写入 installed 标记")
	}

	// 阶段二：补上站点信息 + 进度标记 stage=site → 再次重启应落在第 6 步
	siteJSON, err := settings.Site{Name: "续装验证站", URL: "https://verify.example.com"}.Encode()
	if err != nil {
		t.Fatalf("序列化站点信息失败: %v", err)
	}
	if _, err := st.UpsertSettingBy(context.Background(), settings.KeySite, siteJSON, nil); err != nil {
		t.Fatalf("写入站点信息失败: %v", err)
	}
	writeProgressMarker(t, st, settings.StageSite)

	restarted := newInstallSupervisor(t, installTestConfig(dsn, true, configPath), configPath)
	restarted.Init(context.Background())
	if state, detail := restarted.Snapshot(); state != StatePending {
		t.Fatalf("重启后状态 = %s（%s）, 期望 pending", state, detail)
	}
	// 重启后表单是空的，摘要必须来自库内实况（本批修复点 1 的回归断言）
	assertSummaryFields(t, assertStatusState(t, restarted, StatePending, "等待完成安装", 6),
		"opsadmin", "续装验证站")
}

// assertStatusState 复核 GET /install/api/status 的 state / state_label / first_step，并返回状态视图。
func assertStatusState(t *testing.T, supervisor *Supervisor, want State, wantLabel string, wantStep int) statusView {
	t.Helper()

	rec, envelope := doRequest(t, supervisor, http.MethodGet, PathStatus, nil, nil)
	status := decodeData[statusView](t, envelope)
	expectSuccess(t, rec, envelope, "状态查询")
	if status.State != string(want) || status.StateLabel != wantLabel || status.FirstStep != wantStep {
		t.Fatalf("状态 = %s/%s/first_step=%d, 期望 %s/%s/%d",
			status.State, status.StateLabel, status.FirstStep, want, wantLabel, wantStep)
	}
	return status
}

// assertSummaryFields 复核 status 里「库内实际已就绪」的展示字段（完成页摘要数据源）。
func assertSummaryFields(t *testing.T, status statusView, wantAdmin, wantSite string) {
	t.Helper()

	if status.AdminUsername != wantAdmin {
		t.Errorf("status.admin_username = %q, 期望 %q", status.AdminUsername, wantAdmin)
	}
	if status.SiteName != wantSite {
		t.Errorf("status.site_name = %q, 期望 %q", status.SiteName, wantSite)
	}
}

// writeProgressMarker 写入「安装进行中」进度标记（模拟向导改过库之后的现场）。
func writeProgressMarker(t *testing.T, st *store.Store, stage string) {
	t.Helper()

	encoded, err := settings.NewInstallProgress(stage).Encode()
	if err != nil {
		t.Fatalf("构造进度标记失败: %v", err)
	}
	if _, err := st.UpsertSettingBy(context.Background(), settings.KeyInstallProgress, encoded, nil); err != nil {
		t.Fatalf("写入进度标记失败: %v", err)
	}
}

// TestRepairModeRestoresNormalOperation 覆盖契约 13.2 的修复模式关键分支：
// 启动时数据库不可达（安装/修复模式）→ 用向导改对参数 → 探测发现该库**已安装**
// → 当场热切换回正常模式（无需重启），安装向导随之关闭。
func TestRepairModeRestoresNormalOperation(t *testing.T) {
	dsn, form := freshInstallDatabase(t)

	// 预置一个「已安装」的库：迁移 + 安装者管理员 + installed 标记
	migrateForTest(t, dsn)
	gdb := openTestDB(t, dsn)
	st := store.New(gdb)
	hash, err := auth.HashPassword(testInstallerPassword)
	if err != nil {
		t.Fatalf("生成密码哈希失败: %v", err)
	}
	if _, err := st.EnsureInstallerAdmin(context.Background(), "opsadmin", hash, "站长"); err != nil {
		t.Fatalf("准备安装者管理员失败: %v", err)
	}
	marker, err := settings.NewInstalled(settings.SourceWizard).Encode()
	if err != nil {
		t.Fatalf("构造安装标记失败: %v", err)
	}
	if _, err := st.InsertSettingIfAbsent(context.Background(), settings.KeyInstalled, marker); err != nil {
		t.Fatalf("写入安装标记失败: %v", err)
	}

	// 用一个连不上的 DSN 启动 → 修复模式
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("获取空闲端口失败: %v", err)
	}
	closedAddr := listener.Addr().String()
	_ = listener.Close()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	brokenDSN := fmt.Sprintf("root:pw@tcp(%s)/lyidc?charset=utf8mb4&parseTime=true", closedAddr)
	supervisor := newInstallSupervisor(t, installTestConfig(brokenDSN, true, configPath), configPath)
	supervisor.Init(context.Background())

	if state, _ := supervisor.Snapshot(); state != StateDBUnreachable {
		t.Fatalf("初始状态 = %s, 期望 db_unreachable", state)
	}

	// 第 2 步保存正确参数 → 服务发现该库已安装 → 直接进入正常模式
	rec, envelope := doRequest(t, supervisor, http.MethodPost, PathDatabaseSave, form, nil)
	saveView := decodeData[databaseSaveView](t, envelope)
	expectSuccess(t, rec, envelope, "修复模式保存数据库参数")
	if !saveView.Installed || saveView.State != string(StateInstalled) {
		t.Fatalf("保存响应 = %+v, 期望 installed=true / state=installed", saveView)
	}
	if !supervisor.IsInstalled() {
		t.Fatal("修复成功后应切换为已安装状态")
	}

	// 正常模式立即可用：健康检查 db=up、安装页关闭、管理员可登录
	rec, envelope = doRequest(t, supervisor, http.MethodGet, "/api/v1/health", nil, nil)
	health := decodeData[healthData](t, envelope)
	expectSuccess(t, rec, envelope, "修复后健康检查")
	if health.DB != "up" {
		t.Errorf("健康检查 = %+v, 期望 db=up", health)
	}
	rec, _ = doRequest(t, supervisor, http.MethodGet, PathPage, nil, map[string]string{"Accept": "text/html"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "系统已安装") {
		t.Errorf("修复后安装页应显示「系统已安装」: %d", rec.Code)
	}
	rec, envelope = doRequest(t, supervisor, http.MethodPost, "/api/v1/admin/auth/login", map[string]string{
		"username": "opsadmin", "password": testInstallerPassword,
	}, nil)
	login := decodeData[adminLoginView](t, envelope)
	expectSuccess(t, rec, envelope, "修复后管理员登录")
	if login.Token == "" {
		t.Error("修复后登录未返回 token")
	}
}

// TestDatabaseTestReportsMissingDatabase 覆盖「库不存在」分支：未勾选自动建库时给出明确提示。
func TestDatabaseTestReportsMissingDatabase(t *testing.T) {
	_, form := freshInstallDatabase(t)
	form.Database = form.Database + "_absent"
	form.CreateDatabase = false

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	supervisor := newInstallSupervisor(t, installTestConfig(config.DefaultDSN, false, configPath), configPath)
	supervisor.Init(context.Background())

	rec, envelope := doRequest(t, supervisor, http.MethodPost, PathDatabaseTest, form, nil)
	if rec.Code != http.StatusServiceUnavailable || envelope.Code != response.CodeDBConnectFailed {
		t.Fatalf("库不存在时测试连接 = %d/%d, 期望 503/%d", rec.Code, envelope.Code, response.CodeDBConnectFailed)
	}
	var failure struct {
		Advice             string `json:"advice"`
		NeedCreateDatabase bool   `json:"need_create_database"`
	}
	if err := json.Unmarshal(envelope.Data, &failure); err != nil {
		t.Fatalf("解析失败响应失败: %v", err)
	}
	if !failure.NeedCreateDatabase || !strings.Contains(failure.Advice, "自动建库") {
		t.Errorf("缺少「自动建库」提示: %+v", failure)
	}

	// 勾选自动建库后应建库成功
	form.CreateDatabase = true
	rec, envelope = doRequest(t, supervisor, http.MethodPost, PathDatabaseTest, form, nil)
	view := decodeData[databaseView](t, envelope)
	expectSuccess(t, rec, envelope, "自动建库后测试连接")
	if !view.Connected || !view.DatabaseCreated {
		t.Fatalf("自动建库结果 = %+v", view)
	}

	// 清理：删除临时库，避免残留
	if parsed, parseErr := gomysql.ParseDSN(installTestDSN()); parseErr == nil {
		serverDSN := *parsed
		serverDSN.DBName = ""
		if sqlDB, openErr := sql.Open("mysql", serverDSN.FormatDSN()); openErr == nil {
			_, _ = sqlDB.Exec("DROP DATABASE IF EXISTS `" + form.Database + "`")
			_ = sqlDB.Close()
		}
	}
}

// TestRestartMidWizardKeepsWizardOpen 覆盖安装进度标记：向导改过库之后重启进程，
// 库内只有默认管理员也不应被判为「存量库」而自动补标记。
func TestRestartMidWizardKeepsWizardOpen(t *testing.T) {
	dsn, _ := freshInstallDatabase(t)
	migrateForTest(t, dsn)

	gdb := openTestDB(t, dsn)
	progress, err := settings.NewInstallProgress(settings.StageInitialized).Encode()
	if err != nil {
		t.Fatalf("构造进度标记失败: %v", err)
	}
	if _, err := store.New(gdb).UpsertSettingBy(context.Background(),
		settings.KeyInstallProgress, progress, nil); err != nil {
		t.Fatalf("写入进度标记失败: %v", err)
	}

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	supervisor := newInstallSupervisor(t, installTestConfig(dsn, true, configPath), configPath)
	supervisor.Init(context.Background())

	state, detail := supervisor.Snapshot()
	if state != StateAdminMissing {
		t.Fatalf("状态 = %s（%s）, 期望 admin_missing（安装进行中）", state, detail)
	}
	if _, err := store.New(gdb).Setting(context.Background(), settings.KeyInstalled); err == nil {
		t.Error("安装进行中不应写入 installed 标记")
	}
}

// TestConcurrentInstallIsSerialized 覆盖并发安装保护：并发的「完成安装」只有一个成功。
func TestConcurrentInstallIsSerialized(t *testing.T) {
	dsn, _ := freshInstallDatabase(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")

	cfg := installTestConfig(dsn, true, configPath)
	supervisor := newInstallSupervisor(t, cfg, configPath)
	supervisor.Init(context.Background())
	if state, _ := supervisor.Snapshot(); state != StateTablesMissing {
		t.Fatalf("初始状态 = %s, 期望 tables_missing", state)
	}

	// 准备到「待完成」：建表 + 管理员 + 站点信息
	if rec, envelope := doRequest(t, supervisor, http.MethodPost, PathInitialize, map[string]any{}, nil); rec.Code != http.StatusOK {
		t.Fatalf("初始化失败: %d %s", rec.Code, envelope.Message)
	}
	rec, envelope := doRequest(t, supervisor, http.MethodPost, PathAdmin, map[string]string{
		"username": "opsadmin", "password": testInstallerPassword, "confirm_password": testInstallerPassword,
	}, nil)
	expectSuccess(t, rec, envelope, "创建管理员")
	rec, envelope = doRequest(t, supervisor, http.MethodPost, PathSite,
		map[string]string{"name": "并发测试站"}, nil)
	expectSuccess(t, rec, envelope, "保存站点信息")

	// 并发发起 6 次「完成安装」
	const workers = 6
	var (
		wait      sync.WaitGroup
		mu        sync.Mutex
		successes int
		closed    int
		others    []string
	)
	wait.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wait.Done()
			rec, envelope := doRequest(t, supervisor, http.MethodPost, PathComplete, map[string]any{}, nil)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case rec.Code == http.StatusOK && envelope.Code == response.CodeSuccess:
				successes++
			case envelope.Code == response.CodeInstallClosed:
				closed++
			default:
				others = append(others, fmt.Sprintf("%d/%d", rec.Code, envelope.Code))
			}
		}()
	}
	wait.Wait()

	if successes != 1 {
		t.Fatalf("完成安装成功次数 = %d, 期望恰好 1（其余被互斥挡住）；其余结果: %v", successes, others)
	}
	if closed != workers-1 {
		t.Errorf("被关闭拒绝次数 = %d, 期望 %d", closed, workers-1)
	}

	gdb := openTestDB(t, dsn)
	if _, err := store.New(gdb).Setting(context.Background(), settings.KeyInstalled); err != nil {
		t.Fatalf("并发完成后 installed 标记缺失: %v", err)
	}
	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("并发完成后配置文件不可加载: %v", err)
	}
	if !loaded.DatabaseConfigured || loaded.JWT.Secret == config.DefaultJWTSecret {
		t.Errorf("并发完成后配置文件内容异常: dsn_configured=%t secret_len=%d",
			loaded.DatabaseConfigured, len(loaded.JWT.Secret))
	}
}

// TestInsertSettingIfAbsentIsOneShot 覆盖跨进程一次性保护（installed 标记的条件插入）。
func TestInsertSettingIfAbsentIsOneShot(t *testing.T) {
	dsn, _ := freshInstallDatabase(t)
	migrateForTest(t, dsn)

	gdb := openTestDB(t, dsn)
	st := store.New(gdb)

	first, err := st.InsertSettingIfAbsent(context.Background(), "installed", `{"at":"2026-10-08T00:00:00Z","version":1,"source":"wizard"}`)
	if err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	if !first {
		t.Fatal("首次写入应返回 true")
	}
	second, err := st.InsertSettingIfAbsent(context.Background(), "installed", `{"at":"2026-10-08T00:01:00Z","version":1,"source":"wizard"}`)
	if err != nil {
		t.Fatalf("第二次写入失败: %v", err)
	}
	if second {
		t.Fatal("已存在的键不应被再次写入")
	}
}

// adminLoginView 是管理员登录响应的测试视图。
type adminLoginView struct {
	Token string `json:"token"`
}
