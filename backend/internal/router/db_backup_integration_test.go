package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// TestDBBackupAdminEndpoints 覆盖数据库备份与恢复接口（R14，契约 21.3 / 21.4 / 21.5）：
// 角色闸门、只输出元信息、备份 ID 白名单、恢复的二次确认与密码校验、覆盖式恢复的结果口径。
func TestDBBackupAdminEndpoints(t *testing.T) {
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

	// 只有四条路由，且**没有任何一条能读到备份文件内容**（契约 21.2：不可下载）。
	assertBackupRoutes(t, engine)

	// 清掉上一次中断运行可能残留的探测表（它必须在备份之前就被清掉，否则会被写进备份）。
	if err := gdb.Exec("DROP TABLE IF EXISTS `r14_api_extra`").Error; err != nil {
		t.Fatalf("清理残留表失败: %v", err)
	}
	t.Cleanup(func() { _ = gdb.Exec("DROP TABLE IF EXISTS `r14_api_extra`").Error })

	// 角色闸门：未登录 401；finance / support 403（全部四个接口，含读取）。
	type endpoint struct {
		method string
		path   string
		body   any
	}
	endpoints := []endpoint{
		{http.MethodGet, "/api/v1/admin/db-backups", nil},
		{http.MethodPost, "/api/v1/admin/db-backups", nil},
		{http.MethodPost, "/api/v1/admin/db-backups/lyidc-backup-20261014T070000Z-abcdef01/restore",
			map[string]any{"confirm": true, "password": "admin123456"}},
		{http.MethodDelete, "/api/v1/admin/db-backups/lyidc-backup-20261014T070000Z-abcdef01", nil},
	}
	for _, e := range endpoints {
		rec, _ := doAPI(t, engine, e.method, e.path, "", e.body)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("未登录 %s %s = HTTP %d，期望 401", e.method, e.path, rec.Code)
		}
		for name, token := range map[string]string{"finance": financeToken, "support": supportToken} {
			rec, envelope := doAPI(t, engine, e.method, e.path, token, e.body)
			if rec.Code != http.StatusForbidden || envelope.Code != response.CodeForbidden {
				t.Fatalf("%s %s %s = HTTP %d / code %d，期望 403", name, e.method, e.path, rec.Code, envelope.Code)
			}
		}
	}

	// 立即备份：返回元信息（**只有元信息**，不含任何文件内容或行数据）。
	rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/admin/db-backups", adminToken, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("备份失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	record := decodeData[dbBackupRecordView](t, envelope)
	if !strings.HasPrefix(record.ID, "lyidc-backup-") || !strings.HasSuffix(record.FileName, ".sql") {
		t.Fatalf("备份标识形状不正确：%+v", record)
	}
	if record.Operator != "root" {
		t.Fatalf("备份操作人 = %q，期望发起人用户名 root", record.Operator)
	}
	if record.FileSize <= 0 || record.TableCount <= 0 || record.SchemaVersion <= 0 {
		t.Fatalf("备份元信息不完整：%+v", record)
	}

	// 列表：目录状态 + 总占用 + 不自动清理；响应体里绝不出现备份内容。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/db-backups", adminToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("列表失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	list := decodeData[dbBackupListView](t, envelope)
	if len(list.Backups) != 1 || list.Backups[0].ID != record.ID {
		t.Fatalf("列表应含刚创建的备份：%+v", list.Backups)
	}
	if list.AutoCleanup || !list.BackupDirOutsideWebRoot || list.TotalSizeBytes != record.FileSize {
		t.Fatalf("列表口径不正确：%+v", list)
	}
	body := rec.Body.String()
	for _, forbidden := range []string{"INSERT INTO", "CREATE TABLE", "password_hash", "SET FOREIGN_KEY_CHECKS"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("列表响应出现了备份内容片段 %q：%s", forbidden, body)
		}
	}

	// 备份文件必须落在 <安装目录>/backups 且不在 Web 根（dist/）内。
	if !strings.Contains(list.BackupDir, "backups") || strings.Contains(list.BackupDir, "dist") {
		t.Fatalf("备份目录口径不正确：%s", list.BackupDir)
	}

	// 制造差异：新增会员 + 新增表，恢复后必须消失。
	if err := gdb.Exec(`INSERT INTO members (username,email,password_hash,nickname,status,balance,created_at,updated_at)
		VALUES ('r14-api','r14-api@example.com','$2a$10$hash','接口测试','active','0.00',NOW(),NOW())`).Error; err != nil {
		t.Fatalf("写入会员失败: %v", err)
	}
	if err := gdb.Exec("CREATE TABLE `r14_api_extra` (`id` INT PRIMARY KEY) ENGINE=InnoDB").Error; err != nil {
		t.Fatalf("建表失败: %v", err)
	}

	restorePath := "/api/v1/admin/db-backups/" + record.ID + "/restore"

	// 缺少确认 / 密码错误 / 密码缺失：一律拒绝（40002），且不得动库。
	for name, body := range map[string]any{
		"缺少确认":  map[string]any{"password": "admin123456"},
		"确认非真值": map[string]any{"confirm": false, "password": "admin123456"},
		"缺少密码":  map[string]any{"confirm": true},
		"密码错误":  map[string]any{"confirm": true, "password": "wrong-password"},
	} {
		rec, envelope := doAPI(t, engine, http.MethodPost, restorePath, adminToken, body)
		if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
			t.Fatalf("%s：HTTP %d / code %d，期望 40002", name, rec.Code, envelope.Code)
		}
	}
	// 字段类型不符属于请求体格式问题：40001（契约 21.5）。
	rec, envelope = doAPI(t, engine, http.MethodPost, restorePath, adminToken,
		map[string]any{"confirm": true, "password": 123})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("字段类型错：HTTP %d / code %d，期望 40001", rec.Code, envelope.Code)
	}
	var stillThere int64
	if err := gdb.Raw("SELECT COUNT(*) FROM members WHERE username = 'r14-api'").Scan(&stillThere).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if stillThere != 1 {
		t.Fatalf("被拒绝的恢复不得改动数据库")
	}

	// 防穿越与非白名单 ID：全部 404 语义。
	for _, id := range []string{
		"..",
		"unknown",
		record.ID + ".sql",
		"../../etc/passwd",
		"lyidc-backup-20261014T070000Z-ABCDEF01",
	} {
		rec, envelope := doAPI(t, engine, http.MethodPost,
			"/api/v1/admin/db-backups/"+id+"/restore", adminToken,
			map[string]any{"confirm": true, "password": "admin123456"})
		if rec.Code != http.StatusNotFound || envelope.Code != response.CodeNotFound {
			t.Fatalf("恢复 %q = HTTP %d / code %d，期望 404", id, rec.Code, envelope.Code)
		}
		rec, envelope = doAPI(t, engine, http.MethodDelete,
			"/api/v1/admin/db-backups/"+id, adminToken, nil)
		if rec.Code != http.StatusNotFound || envelope.Code != response.CodeNotFound {
			t.Fatalf("删除 %q = HTTP %d / code %d，期望 404", id, rec.Code, envelope.Code)
		}
	}

	// 正常恢复：覆盖式（新增行与新增表都消失，备份点数据回来）。
	rec, envelope = doAPI(t, engine, http.MethodPost, restorePath, adminToken,
		map[string]any{"confirm": true, "password": "admin123456"})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("恢复失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	result := decodeData[dbBackupRestoreView](t, envelope)
	if result.BackupID != record.ID || result.Migrated {
		t.Fatalf("恢复结果口径不正确：%+v", result)
	}
	if len(result.DroppedTables) != 1 || result.DroppedTables[0] != "r14_api_extra" {
		t.Fatalf("恢复应删掉备份里没有的表（覆盖而非合并），实际 %v（body=%s）", result.DroppedTables, rec.Body.String())
	}
	if err := gdb.Raw("SELECT COUNT(*) FROM members WHERE username = 'r14-api'").Scan(&stillThere).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if stillThere != 0 {
		t.Fatalf("恢复后备份之后的改动必须被抹掉")
	}
	var extraExists int64
	if err := gdb.Raw(`SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = DATABASE() AND table_name = 'r14_api_extra'`).Scan(&extraExists).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if extraExists != 0 {
		t.Fatalf("备份之后新建的表必须被删掉")
	}

	// 删除：只按 ID；文件与 sidecar 一并删除。
	rec, envelope = doAPI(t, engine, http.MethodDelete, "/api/v1/admin/db-backups/"+record.ID, adminToken, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("删除失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/db-backups", adminToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("列表失败: HTTP %d", rec.Code)
	}
	list = decodeData[dbBackupListView](t, envelope)
	if len(list.Backups) != 0 || list.TotalSizeBytes != 0 {
		t.Fatalf("删除后列表应为空：%+v", list.Backups)
	}
	// 已删除的备份：恢复与删除都是 404 语义（不存在与已删除同码）。
	rec, envelope = doAPI(t, engine, http.MethodPost, restorePath, adminToken,
		map[string]any{"confirm": true, "password": "admin123456"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("恢复已删除的备份 = HTTP %d，期望 404", rec.Code)
	}
}

// TestDBBackupUnavailableWithoutService 覆盖服务未接入时的降级口径（数据库句柄不可用时
// 构造不出备份服务）：接口明确拒绝、不 panic，其余功能不受影响。
func TestDBBackupUnavailableWithoutService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	handler := &dbBackupHandler{service: nil, logger: silentLogger()}
	engine.GET("/api/v1/admin/db-backups", handler.listBackups)
	engine.POST("/api/v1/admin/db-backups", handler.createBackup)
	engine.POST("/api/v1/admin/db-backups/:id/restore", handler.restoreBackup)
	engine.DELETE("/api/v1/admin/db-backups/:id", handler.deleteBackup)

	for _, route := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/admin/db-backups"},
		{http.MethodPost, "/api/v1/admin/db-backups"},
		{http.MethodPost, "/api/v1/admin/db-backups/lyidc-backup-20261014T070000Z-abcdef01/restore"},
		{http.MethodDelete, "/api/v1/admin/db-backups/lyidc-backup-20261014T070000Z-abcdef01"},
	} {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(route.method, route.path, nil))
		var envelope apiEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("%s %s 响应不是合法 JSON: %v", route.method, route.path, err)
		}
		if rec.Code != http.StatusInternalServerError || envelope.Code != response.CodeInternalError {
			t.Fatalf("%s %s 服务未接入时应明确拒绝：HTTP %d / code %d",
				route.method, route.path, rec.Code, envelope.Code)
		}
	}
}

// assertBackupRoutes 断言备份相关路由只有约定的四条，且没有任何「读文件内容」的接口（契约 21.2）。
func assertBackupRoutes(t *testing.T, engine *gin.Engine) {
	t.Helper()
	var backupRoutes []string
	for _, route := range engine.Routes() {
		if !strings.Contains(route.Path, "db-backups") {
			continue
		}
		backupRoutes = append(backupRoutes, route.Method+" "+route.Path)
		if strings.Contains(route.Path, "download") {
			t.Fatalf("备份接口不得提供下载：%s", route.Path)
		}
	}
	want := map[string]bool{
		"DELETE /api/v1/admin/db-backups/:id":       true,
		"GET /api/v1/admin/db-backups":              true,
		"POST /api/v1/admin/db-backups":             true,
		"POST /api/v1/admin/db-backups/:id/restore": true,
	}
	if len(backupRoutes) != len(want) {
		t.Fatalf("备份路由应为 %d 条（%v），实际 %v", len(want), want, backupRoutes)
	}
	for _, route := range backupRoutes {
		if !want[route] {
			t.Fatalf("出现预期外的备份路由 %q（备份接口只允许列表/创建/恢复/删除）", route)
		}
	}
}

// dbBackupRecordView 是备份元信息视图（与契约 21.3 字段一一对应）。
type dbBackupRecordView struct {
	ID            string `json:"id"`
	FileName      string `json:"file_name"`
	CreatedAt     string `json:"created_at"`
	Database      string `json:"database"`
	SchemaVersion int64  `json:"schema_version"`
	TableCount    int    `json:"table_count"`
	RowCount      int64  `json:"row_count"`
	FileSize      int64  `json:"file_size"`
	Operator      string `json:"operator"`
	AppVersion    string `json:"app_version"`
	FileExists    bool   `json:"file_exists"`
}

// dbBackupListView 是列表视图。
type dbBackupListView struct {
	Database                string               `json:"database"`
	BackupDir               string               `json:"backup_dir"`
	BackupDirDefault        bool                 `json:"backup_dir_default"`
	BackupDirOutsideWebRoot bool                 `json:"backup_dir_outside_web_root"`
	BackupDirMessage        string               `json:"backup_dir_message"`
	TotalSizeBytes          int64                `json:"total_size_bytes"`
	Backups                 []dbBackupRecordView `json:"backups"`
	AutoCleanup             bool                 `json:"auto_cleanup"`
}

// dbBackupRestoreView 是恢复结果视图。
type dbBackupRestoreView struct {
	BackupID          string   `json:"backup_id"`
	FileName          string   `json:"file_name"`
	Database          string   `json:"database"`
	FromSchemaVersion int64    `json:"from_schema_version"`
	ToSchemaVersion   int64    `json:"to_schema_version"`
	Migrated          bool     `json:"migrated"`
	TableCount        int      `json:"table_count"`
	DroppedTables     []string `json:"dropped_tables"`
	RestoredAt        string   `json:"restored_at"`
	Warning           string   `json:"warning"`
}
