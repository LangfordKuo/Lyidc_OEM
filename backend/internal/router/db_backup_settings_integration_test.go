package router

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/dbbackup"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// dbBackupSettingsDataView 是备份目录设置视图（与契约 21.10 字段一一对应；
// 独立定义而非复用 handler 类型，json 标签写错才会被这些用例发现）。
type dbBackupSettingsDataView struct {
	Dir            string  `json:"dir"`
	Default        bool    `json:"default"`
	DefaultDir     string  `json:"default_dir"`
	ResolvedDir    string  `json:"resolved_dir"`
	OutsideWebRoot bool    `json:"outside_web_root"`
	Exists         bool    `json:"exists"`
	Writable       bool    `json:"writable"`
	Message        string  `json:"message"`
	UpdatedBy      *uint64 `json:"updated_by"`
	UpdatedAt      *string `json:"updated_at"`
}

// TestDBBackupSettingsEndpoints 覆盖备份目录设置接口（R15，契约 21.10）：
// 角色闸门（仅 admin，含读取）、读取口径（不建目录）、保存时的环境校验
// （Web 根拒绝且不落库、目录按需创建 0750、写入探针）、留空恢复缺省目录，
// 以及「改目录不搬运已有备份（旧备份不出现在新目录列表里，也不被删除）」。
func TestDBBackupSettingsEndpoints(t *testing.T) {
	gdb := testDatabase(t)
	installDir := t.TempDir()

	service, err := dbbackup.NewService(dbbackup.Options{
		InstallDir: installDir,
		DB:         gdb,
		DSN:        testDSN(),
		Settings:   settings.NewReader(store.New(gdb)),
		Logger:     silentLogger(),
	})
	if err != nil {
		t.Fatalf("构造备份服务失败：%v", err)
	}
	engine := New(Options{
		Logger:      silentLogger(),
		DB:          gdb,
		JWT:         config.JWTConfig{Secret: testJWTSecret, ExpireHours: 168},
		DBBackup:    service,
		DatabaseDSN: testDSN(),
	})

	seedAdmin(t, gdb, "root", "admin123456", model.RoleAdmin, model.StatusActive)
	seedAdmin(t, gdb, "fin", "admin123456", model.RoleFinance, model.StatusActive)
	seedAdmin(t, gdb, "sup", "admin123456", model.RoleSupport, model.StatusActive)
	adminToken, _ := loginAdmin(t, engine, "root", "admin123456")
	financeToken, _ := loginAdmin(t, engine, "fin", "admin123456")
	supportToken, _ := loginAdmin(t, engine, "sup", "admin123456")

	const path = "/api/v1/admin/settings/db_backup"

	// 角色闸门：未登录 401；finance / support 403（读取同样受限，契约 21.8）。
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		body := any(nil)
		if method == http.MethodPut {
			body = map[string]any{"dir": ""}
		}
		rec, envelope := doAPI(t, engine, method, path, "", body)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("未登录 %s %s = HTTP %d，期望 401", method, path, rec.Code)
		}
		for name, token := range map[string]string{"finance": financeToken, "support": supportToken} {
			rec, envelope = doAPI(t, engine, method, path, token, body)
			if rec.Code != http.StatusForbidden || envelope.Code != response.CodeForbidden {
				t.Fatalf("%s %s %s = HTTP %d / code %d，期望 403", name, method, path, rec.Code, envelope.Code)
			}
		}
	}

	// 读取缺省口径：dir 空串 = 缺省目录；目录不存在时**不得**创建（读取不产生副作用）。
	defaultDir := filepath.Join(installDir, dbbackup.DefaultDirName)
	view := getDBBackupSettings(t, engine, adminToken, path)
	if view.Dir != "" || !view.Default || view.DefaultDir != defaultDir || view.ResolvedDir != defaultDir {
		t.Fatalf("缺省读取口径不正确：%+v", view)
	}
	if !view.OutsideWebRoot || view.Exists || view.Writable || view.Message == "" {
		t.Fatalf("目录尚不存在时的读取口径不正确：%+v", view)
	}
	if _, err := os.Lstat(defaultDir); !os.IsNotExist(err) {
		t.Fatalf("GET 不得创建缺省目录")
	}

	// 请求体非法：缺 dir 键 / 类型不符 / 含控制字符 → 40001。
	for name, body := range map[string]any{
		"缺 dir 键":    map[string]any{},
		"dir 为 null": map[string]any{"dir": nil},
		"dir 类型不符":   map[string]any{"dir": 123},
		"dir 含控制字符":  map[string]any{"dir": "back\nups"},
	} {
		rec, envelope := doAPI(t, engine, http.MethodPut, path, adminToken, body)
		if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
			t.Fatalf("%s：HTTP %d / code %d，期望 40001", name, rec.Code, envelope.Code)
		}
	}

	// 保存非法目录（Web 根内）：40002 + 可读原因，且**不落库**、不创建目录。
	insideWebRoot := filepath.Join(dbbackup.WebRootName, "backups")
	rec, envelope := doAPI(t, engine, http.MethodPut, path, adminToken, map[string]any{"dir": insideWebRoot})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("Web 根内的目录应被拒绝为 40002：HTTP %d / code %d / body=%s",
			rec.Code, envelope.Code, rec.Body.String())
	}
	if !strings.Contains(envelope.Message, "Web 根") {
		t.Fatalf("拒绝原因应说明 Web 根：%s", envelope.Message)
	}
	if _, err := os.Lstat(filepath.Join(installDir, dbbackup.WebRootName, "backups")); !os.IsNotExist(err) {
		t.Fatalf("被拒绝的目录不得被创建")
	}
	if got := getDBBackupSettings(t, engine, adminToken, path); got.Dir != "" || !got.Default {
		t.Fatalf("被拒绝的保存不得落库：%+v", got)
	}

	// 保存合法目录：目录不存在 → 自动创建（0750）；返回解析后的绝对路径与可用性。
	rec, envelope = doAPI(t, engine, http.MethodPut, path, adminToken, map[string]any{"dir": "bk2"})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("保存合法目录失败：HTTP %d / body=%s", rec.Code, rec.Body.String())
	}
	view = decodeData[dbBackupSettingsDataView](t, envelope)
	customDir := filepath.Join(installDir, "bk2")
	if view.Dir != "bk2" || view.Default || view.ResolvedDir != customDir {
		t.Fatalf("保存结果口径不正确：%+v", view)
	}
	if !view.OutsideWebRoot || !view.Exists || !view.Writable || view.Message != "" {
		t.Fatalf("保存后目录应报告可用：%+v", view)
	}
	if view.UpdatedBy == nil || *view.UpdatedBy == 0 {
		t.Fatalf("保存后应记录审计人：%+v", view)
	}
	if info, err := os.Stat(customDir); err != nil || !info.IsDir() {
		t.Fatalf("保存时应创建目录：%v", err)
	}

	// 再读一次：配置原值 + 解析路径稳定（改完立即生效，无重启）。
	view = getDBBackupSettings(t, engine, adminToken, path)
	if view.Dir != "bk2" || view.ResolvedDir != customDir || view.Default {
		t.Fatalf("重新读取的口径不正确：%+v", view)
	}

	// 立即备份：新目录立即生效，备份文件落在解析后的绝对路径里。
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/admin/db-backups", adminToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("备份失败：HTTP %d / body=%s", rec.Code, rec.Body.String())
	}
	record := decodeData[dbBackupRecordView](t, envelope)
	if _, err := os.Stat(filepath.Join(customDir, record.FileName)); err != nil {
		t.Fatalf("备份文件应落在新目录：%v", err)
	}
	list := listDBBackups(t, engine, adminToken)
	if list.BackupDir != customDir || len(list.Backups) != 1 {
		t.Fatalf("列表应使用新目录：%+v", list)
	}

	// 留空 = 恢复缺省目录：缺省目录按需创建，且**旧目录里的备份不搬走、也不出现在列表里**
	// （契约 21.10 第 5 条：改目录不搬运已有备份）。
	rec, envelope = doAPI(t, engine, http.MethodPut, path, adminToken, map[string]any{"dir": "   "})
	if rec.Code != http.StatusOK {
		t.Fatalf("留空保存失败：HTTP %d / body=%s", rec.Code, rec.Body.String())
	}
	view = decodeData[dbBackupSettingsDataView](t, envelope)
	if view.Dir != "" || !view.Default || view.ResolvedDir != defaultDir || !view.Writable {
		t.Fatalf("留空应回到缺省目录：%+v", view)
	}
	if _, err := os.Stat(filepath.Join(customDir, record.FileName)); err != nil {
		t.Fatalf("改目录不得删除旧目录里的备份：%v", err)
	}
	list = listDBBackups(t, engine, adminToken)
	if list.BackupDir != defaultDir || len(list.Backups) != 0 || list.TotalSizeBytes != 0 {
		t.Fatalf("旧目录的备份不得出现在新目录列表里：%+v", list)
	}
}

// TestDBBackupSettingsUnavailableWithoutService 覆盖备份服务未接入时的降级口径：
// 两个设置接口明确拒绝（500）、不 panic，与 21.3 的四条接口一致。
func TestDBBackupSettingsUnavailableWithoutService(t *testing.T) {
	gdb := testDatabase(t)

	// 直接构造 nil 服务的 handler（绕开路由装配时的自动构造，确定性断言降级口径）。
	handler := &settingsHandler{
		store: store.New(gdb), reader: settings.NewReader(store.New(gdb)),
		backups: nil, logger: silentLogger(),
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/api/v1/admin/settings/db_backup", handler.getDBBackupSettings)
	engine.PUT("/api/v1/admin/settings/db_backup", handler.updateDBBackupSettings)

	for _, method := range []string{http.MethodGet, http.MethodPut} {
		var body any
		if method == http.MethodPut {
			body = map[string]any{"dir": ""}
		}
		rec, envelope := doAPI(t, engine, method, "/api/v1/admin/settings/db_backup", "", body)
		if rec.Code != http.StatusInternalServerError || envelope.Code != response.CodeInternalError {
			t.Fatalf("%s 服务未接入时应明确拒绝：HTTP %d / code %d", method, rec.Code, envelope.Code)
		}
	}
}

// getDBBackupSettings 读取备份目录设置（必须成功）。
func getDBBackupSettings(t *testing.T, engine http.Handler, token, path string) dbBackupSettingsDataView {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodGet, path, token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("读取备份目录设置失败：HTTP %d / body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[dbBackupSettingsDataView](t, envelope)
}

// listDBBackups 读取备份列表（必须成功）。
func listDBBackups(t *testing.T, engine http.Handler, token string) dbBackupListView {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/db-backups", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("读取备份列表失败：HTTP %d / body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[dbBackupListView](t, envelope)
}
