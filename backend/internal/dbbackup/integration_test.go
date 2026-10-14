package dbbackup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	gomysql "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/db"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
	"github.com/LangfordKuo/Lyidc_OEM/backend/migrations"
)

// defaultBackupTestDSN 指向独立的临时库（每次运行重建，绝不触碰开发库 lyidc 与共享测试库 lyidc_test）。
const defaultBackupTestDSN = "root:lyidc123@tcp(127.0.0.1:3306)/lyidc_r14_backup_test" +
	"?charset=utf8mb4&parseTime=true&loc=UTC&multiStatements=true"

// errBackupMySQLUnavailable 表示本机没有可用的 MySQL，集成用例整体跳过。
var errBackupMySQLUnavailable = errors.New("MySQL 不可用")

// backupTestDSN 返回集成测试用的 DSN：优先由 LYIDC_TEST_DSN 派生独立库名，否则用缺省 DSN。
func backupTestDSN() string {
	if dsn := strings.TrimSpace(os.Getenv("LYIDC_TEST_DSN")); dsn != "" {
		if parsed, err := gomysql.ParseDSN(dsn); err == nil && parsed.DBName != "" {
			parsed.DBName += "_r14backup"
			return parsed.FormatDSN()
		}
	}
	return defaultBackupTestDSN
}

// prepareBackupDatabase 重建集成测试库并迁移到最新版本，返回句柄与 DSN。
func prepareBackupDatabase(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	dsn := backupTestDSN()
	parsed, err := gomysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("解析测试 DSN 失败: %v", err)
	}
	database := parsed.DBName

	server := *parsed
	server.DBName = ""
	serverDB, err := sql.Open("mysql", server.FormatDSN())
	if err != nil {
		t.Fatalf("连接 MySQL 服务失败: %v", err)
	}
	defer func() { _ = serverDB.Close() }()
	if err := serverDB.Ping(); err != nil {
		t.Skipf("跳过集成测试：%v: %v", errBackupMySQLUnavailable, err)
	}
	for _, statement := range []string{
		"DROP DATABASE IF EXISTS `" + database + "`",
		"CREATE DATABASE `" + database + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci",
	} {
		if _, err := serverDB.Exec(statement); err != nil {
			t.Fatalf("准备测试库失败（%s）: %v", statement, err)
		}
	}

	if _, _, err := migrateUp(context.Background(), nil, dsn); err != nil {
		t.Fatalf("迁移测试库失败: %v", err)
	}
	gdb, err := db.Open(config.DatabaseConfig{DSN: dsn, MaxOpenConns: 5, MaxIdleConns: 2})
	if err != nil {
		t.Fatalf("连接测试库失败: %v", err)
	}
	return gdb, dsn
}

// newTestService 构造一个绑定临时安装目录的备份服务。
func newTestService(t *testing.T, gdb *gorm.DB, dsn string) *Service {
	t.Helper()
	service, err := NewService(Options{
		InstallDir: t.TempDir(),
		DB:         gdb,
		DSN:        dsn,
		Settings:   settings.NewReader(store.New(gdb)),
		Logger:     discardLogger(),
	})
	if err != nil {
		t.Fatalf("构造备份服务失败: %v", err)
	}
	return service
}

// tableCounts 统计每张表的行数（逐表比对用）。
func tableCounts(t *testing.T, gdb *gorm.DB) map[string]int64 {
	t.Helper()
	tables, err := listBaseTables(context.Background(), mustConn(t, gdb))
	if err != nil {
		t.Fatalf("列出表失败: %v", err)
	}
	counts := map[string]int64{}
	for _, table := range tables {
		var count int64
		if err := gdb.Raw("SELECT COUNT(*) FROM " + quoteIdent(table)).Scan(&count).Error; err != nil {
			t.Fatalf("统计表 %s 行数失败: %v", table, err)
		}
		counts[table] = count
	}
	return counts
}

// mustConn 取一条专用连接（listBaseTables 需要 *sql.Conn）。
func mustConn(t *testing.T, gdb *gorm.DB) *sql.Conn {
	t.Helper()
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("获取连接池失败: %v", err)
	}
	conn, err := sqlDB.Conn(context.Background())
	if err != nil {
		t.Fatalf("获取连接失败: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func exec(t *testing.T, gdb *gorm.DB, statement string, args ...any) {
	t.Helper()
	if err := gdb.Exec(statement, args...).Error; err != nil {
		t.Fatalf("执行 SQL 失败（%s）: %v", statement, err)
	}
}

// migrateSteps 执行 n 步迁移（负数回滚），用于构造「低版本备份」场景。
func migrateSteps(t *testing.T, dsn string, steps int) {
	t.Helper()
	sqlDB, err := sql.Open("mysql", multiStatementDSN(dsn))
	if err != nil {
		t.Fatalf("打开迁移连接失败: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	driver, err := migratemysql.WithInstance(sqlDB, &migratemysql.Config{})
	if err != nil {
		t.Fatalf("初始化迁移驱动失败: %v", err)
	}
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		t.Fatalf("加载迁移文件失败: %v", err)
	}
	migrator, err := migrate.NewWithInstance("iofs", source, "mysql", driver)
	if err != nil {
		t.Fatalf("初始化迁移器失败: %v", err)
	}
	defer func() { _, _ = migrator.Close() }()
	if err := migrator.Steps(steps); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("执行 %d 步迁移失败: %v", steps, err)
	}
}

// TestBackupRestoreRoundTrip 覆盖「备份 → 改数据 → 恢复」（契约 21.1 / 21.5）：
// 恢复后备份点的行数与内容完整，备份之后的所有改动被抹掉，新增的表被删掉（覆盖而非合并）。
func TestBackupRestoreRoundTrip(t *testing.T) {
	gdb, dsn := prepareBackupDatabase(t)
	service := newTestService(t, gdb, dsn)
	ctx := context.Background()

	// 中文、引号、反斜杠、换行、emoji、NULL 与空串并存（nickname 列宽 32 字符）。
	trickyNickname := "中文 a'b\\c\nd🚀"
	exec(t, gdb, `INSERT INTO members (username,email,password_hash,nickname,phone,status,balance,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,NOW(),NOW())`,
		"roundtrip", "roundtrip@example.com", "$2a$10$hash", trickyNickname, nil, "active", "12.34")
	exec(t, gdb, `INSERT INTO members (username,email,password_hash,nickname,phone,status,balance,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,NOW(),NOW())`,
		"emptyphone", "empty@example.com", "$2a$10$hash", "空串电话", "", "active", "0.00")
	exec(t, gdb, `INSERT INTO settings (`+"`key`"+`,value,created_at,updated_at) VALUES (?,?,NOW(),NOW())`,
		"r14.roundtrip", trickyNickname)

	before := tableCounts(t, gdb)

	record, err := service.Create(ctx, "admin")
	if err != nil {
		t.Fatalf("备份失败: %v", err)
	}
	if record.RowCount == 0 || record.TableCount < 10 || record.FileSize == 0 {
		t.Fatalf("备份元信息不合理：%+v", record)
	}
	if record.SchemaVersion < 17 {
		t.Fatalf("备份的 schema 版本 = %d，至少应为 17", record.SchemaVersion)
	}
	if record.Operator != "admin" {
		t.Fatalf("备份操作人 = %q，期望 admin", record.Operator)
	}

	// 备份文件必须落在安装目录的 backups/ 下，且**不在 Web 根（dist/）内**（契约 21.4）。
	_, path, err := service.resolve(service.BackupDir(ctx), record.ID)
	if err != nil {
		t.Fatalf("解析备份路径失败: %v", err)
	}
	if !within(path, filepath.Join(service.InstallDir(), DefaultDirName)) {
		t.Fatalf("备份文件不在 backups/ 目录内：%s", path)
	}
	if within(path, filepath.Join(service.InstallDir(), WebRootName)) {
		t.Fatalf("备份文件落在 Web 根内：%s", path)
	}
	if _, err := os.Stat(strings.TrimSuffix(path, FileExt) + MetaExt); err != nil {
		t.Fatalf("元信息 sidecar 缺失: %v", err)
	}

	// 备份内容含中文与转义字符，逐字节可读回（内容只在测试里读，接口绝不下发）。
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取备份文件失败: %v", err)
	}
	text := string(raw)
	for _, want := range []string{
		"CREATE TABLE `members`",
		"SET FOREIGN_KEY_CHECKS=0;",
		"-- schema_version: ",
		"中文",
		"🚀",
		`a\'b\\c\nd`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("备份文件缺少期望内容：%q", want)
		}
	}
	if strings.Contains(text, "\r\n") {
		t.Fatalf("备份文件不应含 CRLF")
	}

	// 改数据：新增会员、改昵称、删设置、新增表（覆盖性证据）。
	exec(t, gdb, `INSERT INTO members (username,email,password_hash,nickname,status,balance,created_at,updated_at)
		VALUES ('after-backup','after@example.com','$2a$10$hash','备份之后','active','0.00',NOW(),NOW())`)
	exec(t, gdb, "UPDATE members SET nickname = '被改坏了' WHERE username = 'roundtrip'")
	exec(t, gdb, "DELETE FROM settings WHERE `key` = 'r14.roundtrip'")
	exec(t, gdb, "CREATE TABLE `r14_extra` (`id` INT PRIMARY KEY) ENGINE=InnoDB")
	exec(t, gdb, "INSERT INTO `r14_extra` VALUES (1)")

	result, err := service.Restore(ctx, record.ID)
	if err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	if result.Migrated {
		t.Fatalf("备份与代码同为最新版本，不应触发迁移：%+v", result)
	}
	if result.FromSchemaVersion != record.SchemaVersion || result.ToSchemaVersion != record.SchemaVersion {
		t.Fatalf("恢复前后的 schema 版本应等于备份版本：%+v", result)
	}
	if len(result.DroppedTables) != 1 || result.DroppedTables[0] != "r14_extra" {
		t.Fatalf("应删掉备份里没有的表 r14_extra（覆盖而非合并），实际 %v", result.DroppedTables)
	}
	// 恢复的表数必须与备份时的表数一致（回归：DROP 先于 CREATE 到达时曾把表数漏成 0）。
	if result.TableCount != record.TableCount || result.TableCount < 10 {
		t.Fatalf("恢复的表数 = %d，备份时为 %d", result.TableCount, record.TableCount)
	}

	after := tableCounts(t, gdb)
	if _, ok := after["r14_extra"]; ok {
		t.Fatalf("备份之后新建的表在恢复后仍然存在：覆盖不彻底")
	}
	delete(after, "r14_extra")
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatalf("恢复后逐表行数应与备份点一致：\n备份前 %v\n恢复后 %v", before, after)
	}

	var nickname, phone string
	var phoneNull bool
	if err := gdb.Raw("SELECT nickname, IFNULL(phone,'<null>') FROM members WHERE username = 'roundtrip'").
		Row().Scan(&nickname, &phone); err != nil {
		t.Fatalf("读回会员失败: %v", err)
	}
	if nickname != trickyNickname {
		t.Fatalf("中文与特殊字符往返不一致：%q != %q", nickname, trickyNickname)
	}
	if phone != "<null>" {
		t.Fatalf("NULL 电话往返后变成了 %q", phone)
	}
	var emptyPhone string
	if err := gdb.Raw("SELECT phone FROM members WHERE username = 'emptyphone'").Row().Scan(&emptyPhone); err != nil {
		t.Fatalf("读回会员失败: %v", err)
	}
	if emptyPhone != "" {
		t.Fatalf("空串电话往返后变成了 %q", emptyPhone)
	}
	_ = phoneNull
	// 备份之后的写入必须消失。
	var count int64
	if err := gdb.Raw("SELECT COUNT(*) FROM members WHERE username = 'after-backup'").Scan(&count).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("备份之后新增的会员在恢复后仍然存在")
	}
}

// TestRestoreDowngradedSchemaMigratesUp 覆盖「备份一个低版本库 → 恢复 → 追到当前版本」（契约 21.5）。
// TestRestoreKeepsFulltextNgramParser（R18）：备份 → 恢复必须保留 MySQL 可执行注释里的表结构选项。
//
// 背景：`SHOW CREATE TABLE` 把 FULLTEXT 的 ngram 解析器写成 `/*!50100 WITH PARSER `ngram` */`；
// 恢复端若把它当成普通块注释剥掉，恢复出来的表会**静默丢失解析器**，
// 中文关键字检索（契约 23.5）随即失效——默认解析器会把整句当成一个词。
func TestRestoreKeepsFulltextNgramParser(t *testing.T) {
	gdb, dsn := prepareBackupDatabase(t)
	service := newTestService(t, gdb, dsn)
	ctx := context.Background()

	exec(t, gdb, "INSERT INTO operation_logs "+
		"(channel,actor_id,actor_name,action,method,path,http_status,status,ip,duration_ms,summary,created_at) "+
		"VALUES ('admin',1,'admin','修改上游设置','PUT','/api/v1/admin/settings/upstream',200,'success','127.0.0.1',12,?,NOW())",
		"已保存上游凭据设置（账号=13900000000）")

	record, err := service.Create(ctx, "admin")
	if err != nil {
		t.Fatalf("备份失败: %v", err)
	}
	_, path, err := service.resolve(service.BackupDir(ctx), record.ID)
	if err != nil {
		t.Fatalf("解析备份路径失败: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取备份文件失败: %v", err)
	}
	if !strings.Contains(string(raw), "WITH PARSER `ngram`") {
		t.Fatalf("备份文件未包含 ngram 解析器声明（SHOW CREATE TABLE 输出应带可执行注释）")
	}

	// 破坏现场（删表）后恢复：表结构完全来自备份文件。
	exec(t, gdb, "DROP TABLE operation_logs")
	if _, err := service.Restore(ctx, record.ID); err != nil {
		t.Fatalf("恢复失败: %v", err)
	}

	var name, ddl string
	if err := gdb.Raw("SHOW CREATE TABLE operation_logs").Row().Scan(&name, &ddl); err != nil {
		t.Fatalf("读取建表语句失败: %v", err)
	}
	if !strings.Contains(ddl, "WITH PARSER `ngram`") {
		t.Fatalf("恢复后 FULLTEXT 丢失 ngram 解析器（可执行注释被剥掉）:\n%s", ddl)
	}

	// 端到端：恢复后中文短语检索必须仍然命中那一行。
	var hits int64
	if err := gdb.Raw("SELECT COUNT(*) FROM operation_logs WHERE MATCH(action, summary, actor_name) "+
		"AGAINST(? IN BOOLEAN MODE)", `+"上游凭据"`).Scan(&hits).Error; err != nil {
		t.Fatalf("全文检索失败: %v", err)
	}
	if hits != 1 {
		t.Fatalf("恢复后中文关键字检索命中 = %d，期望 1", hits)
	}
}

func TestRestoreDowngradedSchemaMigratesUp(t *testing.T) {
	gdb, dsn := prepareBackupDatabase(t)
	service := newTestService(t, gdb, dsn)
	ctx := context.Background()

	exec(t, gdb, `INSERT INTO members (username,email,password_hash,nickname,status,balance,created_at,updated_at)
		VALUES ('old-schema','old@example.com','$2a$10$hash','低版本库','active','0.00',NOW(),NOW())`)

	// 回滚两步（0019 down 删掉 operation_logs 表、0018 down 删 downstream_token 列并收起枚举），
	// 再备份 → 低版本备份（缺表也缺列；0017 的 product_group_category_maps 仍在备份里）。
	migrateSteps(t, dsn, -2)
	record, err := service.Create(ctx, "admin")
	if err != nil {
		t.Fatalf("备份低版本库失败: %v", err)
	}
	latest, err := latestMigrationVersion()
	if err != nil {
		t.Fatalf("读取最新版本失败: %v", err)
	}
	if record.SchemaVersion != int64(latest)-2 {
		t.Fatalf("备份版本 = %d，期望 %d", record.SchemaVersion, latest-2)
	}

	// 把库推回最新并制造差异，再恢复低版本备份。
	migrateSteps(t, dsn, 2)
	exec(t, gdb, `INSERT INTO members (username,email,password_hash,nickname,status,balance,created_at,updated_at)
		VALUES ('new-schema','new@example.com','$2a$10$hash','高版本库','active','0.00',NOW(),NOW())`)
	exec(t, gdb, `INSERT INTO product_group_category_maps (upstream_group_id, category_id, created_at, updated_at)
		VALUES (9001, 1, NOW(), NOW())`)

	result, err := service.Restore(ctx, record.ID)
	if err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	if !result.Migrated {
		t.Fatalf("低版本备份恢复后应执行迁移：%+v", result)
	}
	if result.ToSchemaVersion != int64(latest) {
		t.Fatalf("迁移后版本 = %d，期望 %d", result.ToSchemaVersion, latest)
	}
	if len(result.DroppedTables) != 1 || result.DroppedTables[0] != "operation_logs" {
		t.Fatalf("低版本备份里没有的表应被删掉，实际 %v", result.DroppedTables)
	}

	// 迁移应把 0019 的表与 0018 的列都重建出来，且库版本追平。
	counts := tableCounts(t, gdb)
	if _, ok := counts["operation_logs"]; !ok {
		t.Fatalf("迁移后 operation_logs（0019）应被重建")
	}
	var fulltextDDL string
	if err := gdb.Raw("SHOW CREATE TABLE operation_logs").Row().Scan(new(string), &fulltextDDL); err != nil {
		t.Fatalf("读取 operation_logs 建表语句失败: %v", err)
	}
	if !strings.Contains(fulltextDDL, "WITH PARSER `ngram`") {
		t.Fatalf("迁移重建的 operation_logs 必须带 ngram 全文解析器:\n%s", fulltextDDL)
	}
	var downstreamColumns int64
	if err := gdb.Raw("SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() " +
		"AND table_name = 'instances' AND column_name = 'downstream_token'").Scan(&downstreamColumns).Error; err != nil {
		t.Fatalf("查询 instances 列失败: %v", err)
	}
	if downstreamColumns != 1 {
		t.Fatalf("迁移后 instances.downstream_token 应被重建（实际 %d 列）", downstreamColumns)
	}
	var version int64
	if err := gdb.Raw("SELECT version FROM schema_migrations LIMIT 1").Scan(&version).Error; err != nil {
		t.Fatalf("读取迁移版本失败: %v", err)
	}
	if version != int64(latest) {
		t.Fatalf("恢复后库版本 = %d，期望 %d", version, latest)
	}
	var restored, lost int64
	if err := gdb.Raw("SELECT COUNT(*) FROM members WHERE username = 'old-schema'").Scan(&restored).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if err := gdb.Raw("SELECT COUNT(*) FROM members WHERE username = 'new-schema'").Scan(&lost).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if restored != 1 || lost != 0 {
		t.Fatalf("恢复后低版本数据应回滚到备份点（old=%d, new=%d）", restored, lost)
	}
}

// TestCreateRejectsDirInsideWebRoot 覆盖「解析结果落在 Web 根内则拒绝保存/拒绝该配置」（契约 21.4）。
func TestCreateRejectsDirInsideWebRoot(t *testing.T) {
	gdb, dsn := prepareBackupDatabase(t)
	install := t.TempDir()
	service, err := NewService(Options{
		InstallDir: install,
		DB:         gdb,
		DSN:        dsn,
		Settings:   staticSettings{dir: filepath.Join("dist", "backups")},
		Logger:     discardLogger(),
	})
	if err != nil {
		t.Fatalf("构造服务失败: %v", err)
	}
	if _, err := service.Create(context.Background(), "admin"); !errors.Is(err, ErrDirUnusable) {
		t.Fatalf("备份目录落在 Web 根内时必须拒绝，实际 %v", err)
	}
	view, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if view.BackupDirOutsideWebRoot || view.BackupDirMessage == "" {
		t.Fatalf("列表应如实报告目录不可用：%+v", view)
	}
	if len(view.Backups) != 0 {
		t.Fatalf("目录不可用时不应列出备份")
	}
}

// TestDeleteAndList 覆盖列表与删除的白名单口径（契约 21.3 / 21.4）。
func TestDeleteAndList(t *testing.T) {
	gdb, dsn := prepareBackupDatabase(t)
	service := newTestService(t, gdb, dsn)
	ctx := context.Background()

	first, err := service.Create(ctx, "admin")
	if err != nil {
		t.Fatalf("备份失败: %v", err)
	}
	view, err := service.List(ctx)
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if len(view.Backups) != 1 || view.Backups[0].ID != first.ID {
		t.Fatalf("列表应含刚创建的备份：%+v", view.Backups)
	}
	if view.TotalSizeBytes != first.FileSize {
		t.Fatalf("总占用 = %d，期望 %d", view.TotalSizeBytes, first.FileSize)
	}
	if view.AutoCleanup {
		t.Fatalf("本批不做自动清理：AutoCleanup 必须为 false")
	}
	if view.Database == "" || !view.BackupDirOutsideWebRoot {
		t.Fatalf("列表应带库名与目录状态：%+v", view)
	}

	// 非白名单 ID 与穿越形式一律 404 语义。
	for _, id := range []string{"../../etc/passwd", "..", first.ID + ".sql", "unknown"} {
		if _, err := service.Delete(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("删除 %q 应拒绝，实际 %v", id, err)
		}
		if _, err := service.Restore(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("恢复 %q 应拒绝，实际 %v", id, err)
		}
	}

	if _, err := service.Delete(ctx, first.ID); err != nil {
		t.Fatalf("删除合法备份失败: %v", err)
	}
	view, err = service.List(ctx)
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if len(view.Backups) != 0 || view.TotalSizeBytes != 0 {
		t.Fatalf("删除后列表应为空：%+v", view)
	}
	// 文件已删：再删/再恢复都是 404 语义。
	if _, err := service.Delete(ctx, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除应拒绝，实际 %v", err)
	}
	if _, err := service.Restore(ctx, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("恢复已删除的备份应拒绝，实际 %v", err)
	}
}

// TestRestoreRejectsNoBackslashEscapes 覆盖转义口径的安全检查（契约 21.7）：
// 会话启用了 NO_BACKSLASH_ESCAPES 时拒绝恢复（而不是静默写出被改写的字符串）。
func TestRestoreRejectsNoBackslashEscapes(t *testing.T) {
	gdb, dsn := prepareBackupDatabase(t)
	// 两个服务共用同一个安装目录：备份文件必须在被恢复服务自己的白名单目录里。
	install := t.TempDir()
	newService := func(handle *gorm.DB) *Service {
		service, err := NewService(Options{
			InstallDir: install,
			DB:         handle,
			DSN:        dsn,
			Settings:   settings.NewReader(store.New(handle)),
			Logger:     discardLogger(),
		})
		if err != nil {
			t.Fatalf("构造备份服务失败: %v", err)
		}
		return service
	}

	record, err := newService(gdb).Create(context.Background(), "admin")
	if err != nil {
		t.Fatalf("备份失败: %v", err)
	}

	// 用启用了 NO_BACKSLASH_ESCAPES 的连接恢复：必须拒绝。
	parsed, err := gomysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("解析 DSN 失败: %v", err)
	}
	parsed.Params = map[string]string{"sql_mode": "'NO_BACKSLASH_ESCAPES'"}
	strict, err := db.Open(config.DatabaseConfig{DSN: parsed.FormatDSN(), MaxOpenConns: 3, MaxIdleConns: 1})
	if err != nil {
		t.Fatalf("连接测试库失败: %v", err)
	}
	var mode string
	if err := strict.Raw("SELECT @@SESSION.sql_mode").Scan(&mode).Error; err != nil {
		t.Fatalf("读取 sql_mode 失败: %v", err)
	}
	if !strings.Contains(strings.ToUpper(mode), "NO_BACKSLASH_ESCAPES") {
		t.Skipf("本机 MySQL 无法通过 DSN 设置 sql_mode（实际 %q），跳过", mode)
	}

	if _, err := newService(strict).Restore(context.Background(), record.ID); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("NO_BACKSLASH_ESCAPES 下应拒绝恢复，实际 %v", err)
	}
}

// TestListSortsNewestFirst 覆盖列表排序（最新在前）。
func TestListSortsNewestFirst(t *testing.T) {
	records := []Backup{
		{ID: "b", CreatedAt: "2026-10-14T07:00:00Z"},
		{ID: "c", CreatedAt: "2026-10-14T09:00:00Z"},
		{ID: "a", CreatedAt: "2026-10-14T07:00:00Z"},
	}
	sortBackups(records)
	got := []string{records[0].ID, records[1].ID, records[2].ID}
	if !sort.StringsAreSorted([]string{}) {
		t.Fatal("unreachable")
	}
	if got[0] != "c" || got[1] != "b" || got[2] != "a" {
		t.Fatalf("排序结果 = %v，期望 [c b a]（时间倒序，同时间按文件名倒序）", got)
	}
}
