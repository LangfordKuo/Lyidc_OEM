package router

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	gomysql "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/auth"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/db"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/migrations"
)

// defaultTestDSN 指向独立的测试库 lyidc_test（不会污染开发库 lyidc）。
// CI 通过环境变量 LYIDC_TEST_DSN 覆盖。
const defaultTestDSN = "root:lyidc123@tcp(127.0.0.1:3306)/lyidc_test?charset=utf8mb4&parseTime=true&loc=UTC&multiStatements=true"

// testJWTSecret 是集成测试使用的固定密钥（与开发默认密钥区分开）。
const testJWTSecret = "integration-test-secret"

// errTestMySQLUnavailable 表示当前环境没有可用的 MySQL，集成测试整体跳过。
var errTestMySQLUnavailable = errors.New("MySQL 测试库不可用")

var (
	testDBOnce sync.Once
	testDB     *gorm.DB
	testDBErr  error
)

// testDSN 返回集成测试使用的 MySQL DSN。
func testDSN() string {
	if dsn := strings.TrimSpace(os.Getenv("LYIDC_TEST_DSN")); dsn != "" {
		return dsn
	}
	return defaultTestDSN
}

// testDatabase 返回已完成迁移的测试库句柄，并在每个用例前清空业务表。
// 本机/CI 没有 MySQL 时以 t.Skip 跳过（不误报为失败）。
func testDatabase(t *testing.T) *gorm.DB {
	t.Helper()

	testDBOnce.Do(func() { testDBErr = prepareTestDatabase(testDSN()) })
	if testDBErr != nil {
		if errors.Is(testDBErr, errTestMySQLUnavailable) {
			t.Skipf("跳过集成测试：%v", testDBErr)
		}
		t.Fatalf("准备测试数据库失败: %v", testDBErr)
	}

	resetBusinessTables(t, testDB)
	return testDB
}

// prepareTestDatabase 建库（如不存在）→ 执行全部迁移 → 返回 GORM 句柄。
func prepareTestDatabase(dsn string) error {
	parsed, err := gomysql.ParseDSN(dsn)
	if err != nil {
		return fmt.Errorf("解析测试 DSN 失败: %w", err)
	}
	database := parsed.DBName
	if database == "" {
		return errors.New("测试 DSN 必须包含数据库名")
	}

	serverDSN := *parsed
	serverDSN.DBName = ""
	serverDB, err := sql.Open("mysql", serverDSN.FormatDSN())
	if err != nil {
		return fmt.Errorf("连接 MySQL 服务失败: %w", err)
	}
	defer func() { _ = serverDB.Close() }()
	if err := serverDB.Ping(); err != nil {
		return fmt.Errorf("%w: %v", errTestMySQLUnavailable, err)
	}
	if _, err := serverDB.Exec("CREATE DATABASE IF NOT EXISTS `" + database +
		"` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci"); err != nil {
		return fmt.Errorf("创建测试数据库失败: %w", err)
	}

	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("打开测试数据库失败: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	driver, err := migratemysql.WithInstance(sqlDB, &migratemysql.Config{})
	if err != nil {
		return fmt.Errorf("初始化迁移驱动失败: %w", err)
	}
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("加载迁移文件失败: %w", err)
	}
	migrator, err := migrate.NewWithInstance("iofs", source, "mysql", driver)
	if err != nil {
		return fmt.Errorf("初始化迁移器失败: %w", err)
	}
	defer func() { _, _ = migrator.Close() }()
	if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("执行迁移失败: %w", err)
	}

	gdb, err := db.Open(config.DatabaseConfig{DSN: dsn, MaxOpenConns: 5, MaxIdleConns: 2})
	if err != nil {
		return fmt.Errorf("连接测试数据库失败: %w", err)
	}
	testDB = gdb.Session(&gorm.Session{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	return nil
}

// resetBusinessTables 清空业务表（账号 + 商品目录），保证用例之间互不影响。
func resetBusinessTables(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	statements := []string{
		"DELETE FROM members",
		"DELETE FROM admins",
		"DELETE FROM products",
		"DELETE FROM product_groups",
		"DELETE FROM coupons",
		"ALTER TABLE members AUTO_INCREMENT = 1",
		"ALTER TABLE admins AUTO_INCREMENT = 1",
		"ALTER TABLE products AUTO_INCREMENT = 1",
		"ALTER TABLE product_groups AUTO_INCREMENT = 1",
		"ALTER TABLE coupons AUTO_INCREMENT = 1",
	}
	for _, statement := range statements {
		if err := gdb.Exec(statement).Error; err != nil {
			t.Fatalf("重置测试表失败（%s）: %v", statement, err)
		}
	}
}

// newAccountEngine 构造带真实数据库的 gin 引擎（httptest 集成测试）。
func newAccountEngine(t *testing.T, gdb *gorm.DB) *gin.Engine {
	t.Helper()
	return New(Options{
		Logger: silentLogger(),
		DB:     gdb,
		JWT:    config.JWTConfig{Secret: testJWTSecret, ExpireHours: 168},
	})
}

// silentLogger 返回丢弃全部日志的 logger，避免测试输出噪音。
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// itoa 把 ID 转成路径参数。
func itoa(id uint64) string {
	return strconv.FormatUint(id, 10)
}

// apiEnvelope 是统一响应包的测试视图。
type apiEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// doAPI 发起一次 HTTP 请求（body 为 nil 时无请求体），返回记录器与响应包。
func doAPI(t *testing.T, engine http.Handler, method, path, token string, body any) (*httptest.ResponseRecorder, apiEnvelope) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("序列化请求体失败: %v", err)
		}
		reader = bytes.NewReader(raw)
	}

	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	var envelope apiEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("响应不是合法 JSON: %v (body=%s)", err, rec.Body.String())
	}
	return rec, envelope
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

// toJSON 把任意值序列化为字符串（用于断言响应体不含敏感字段）。
func toJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	return string(raw)
}

// listMembersRequest 请求 GET /admin/members 并解码分页结果（query 需自带 "?" 或为空）。
func listMembersRequest(t *testing.T, engine http.Handler, query, token string) memberListView {
	t.Helper()

	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/members"+query, token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("会员列表请求失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[memberListView](t, envelope)
}

// seedAdmin 直接写库创建管理员（阶段 1 没有管理员创建接口）。
func seedAdmin(t *testing.T, gdb *gorm.DB, username, password, role, status string) *model.Admin {
	t.Helper()

	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("生成管理员密码哈希失败: %v", err)
	}
	admin := &model.Admin{
		Username:     username,
		PasswordHash: hash,
		Nickname:     username,
		Role:         role,
		Status:       status,
	}
	if err := gdb.Create(admin).Error; err != nil {
		t.Fatalf("写入管理员失败: %v", err)
	}
	return admin
}

// registerMember 调用注册接口并返回会员视图。
func registerMember(t *testing.T, engine http.Handler, username, email, password string) memberView {
	t.Helper()

	rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/auth/register", "", map[string]string{
		"username": username,
		"email":    email,
		"password": password,
	})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("注册 %s 失败: HTTP %d, code=%d, message=%s", username, rec.Code, envelope.Code, envelope.Message)
	}
	return decodeData[memberView](t, envelope)
}

// loginMember 调用会员登录接口并返回 token 与会员视图。
func loginMember(t *testing.T, engine http.Handler, username, password string) (string, memberView) {
	t.Helper()

	rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"username": username,
		"password": password,
	})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("会员登录 %s 失败: HTTP %d, code=%d, message=%s", username, rec.Code, envelope.Code, envelope.Message)
	}
	login := decodeData[memberLoginView](t, envelope)
	return login.Token, login.Member
}

// loginAdmin 调用管理员登录接口并返回 token 与管理员视图。
func loginAdmin(t *testing.T, engine http.Handler, username, password string) (string, adminView) {
	t.Helper()

	rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/admin/auth/login", "", map[string]string{
		"username": username,
		"password": password,
	})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("管理员登录 %s 失败: HTTP %d, code=%d, message=%s", username, rec.Code, envelope.Code, envelope.Message)
	}
	login := decodeData[adminLoginView](t, envelope)
	return login.Token, login.Admin
}
