package dbbackup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	gomysql "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/LangfordKuo/Lyidc_OEM/backend/migrations"
)

// migrateTimeout 是恢复后追迁移的整体超时（与安装向导的 MigrationTimeout 同量级）。
const migrateTimeout = 10 * time.Minute

// latestMigrationVersion 返回内嵌迁移文件里的最大版本号（当前代码所需的库结构版本）。
func latestMigrationVersion() (uint, error) {
	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		return 0, fmt.Errorf("读取内嵌迁移文件失败: %w", err)
	}
	var latest uint
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		version, err := strconv.Atoi(strings.SplitN(strings.TrimSuffix(name, ".up.sql"), "_", 2)[0])
		if err != nil || version <= 0 {
			return 0, fmt.Errorf("迁移文件名 %q 不以版本号开头", name)
		}
		if uint(version) > latest {
			latest = uint(version)
		}
	}
	if latest == 0 {
		return 0, errors.New("内嵌迁移文件为空，构建产物可能不完整")
	}
	return latest, nil
}

// migrateUp 把库追到内嵌迁移的最新版本，返回执行前与执行后的版本（契约 21.5）。
//
// 与安装向导、cmd/migrate **使用同一套 golang-migrate + 内嵌迁移文件**（不复制任何迁移 SQL）。
// 这里刻意不 import internal/install：那套实现的返回值是向导第 3 步的视图对象，且向导状态机与
// 恢复链路无关；两边共用的是同一个 `migrations.FS` 与同一套版本表语义，不会产生口径漂移。
//
// dsn 非空时用它新开一条连接（迁移文件多为多语句，需要 DSN 上的 multiStatements=true，
// 而应用连接池的 DSN 未必带该参数）；dsn 为空时退回应用连接池。
func migrateUp(ctx context.Context, fallback *sql.DB, dsn string) (uint, uint, error) {
	target := fallback
	if strings.TrimSpace(dsn) != "" {
		opened, err := sql.Open("mysql", multiStatementDSN(dsn))
		if err != nil {
			return 0, 0, fmt.Errorf("打开迁移连接失败: %w", err)
		}
		defer func() { _ = opened.Close() }()
		if err := opened.PingContext(ctx); err != nil {
			return 0, 0, fmt.Errorf("连接数据库失败: %w", err)
		}
		target = opened
	}

	execCtx, cancel := context.WithTimeout(ctx, migrateTimeout)
	defer cancel()

	driver, err := migratemysql.WithInstance(target, &migratemysql.Config{})
	if err != nil {
		return 0, 0, fmt.Errorf("初始化迁移驱动失败: %w", err)
	}
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return 0, 0, fmt.Errorf("加载内嵌迁移文件失败: %w", err)
	}
	migrator, err := migrate.NewWithInstance("iofs", source, "mysql", driver)
	if err != nil {
		return 0, 0, fmt.Errorf("初始化迁移器失败: %w", err)
	}
	defer func() { _, _ = migrator.Close() }()

	from, dirty, err := migratorVersion(migrator)
	if err != nil {
		return 0, 0, err
	}
	if dirty {
		return from, from, fmt.Errorf(
			"迁移表处于 dirty 状态（版本 %d 的上一次迁移失败）：请手工修正数据库后用 lyidc-migrate up 追平", from)
	}
	if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return from, from, fmt.Errorf("执行迁移失败: %w", err)
	}
	to, dirtyAfter, err := migratorVersion(migrator)
	if err != nil {
		return from, from, err
	}
	if dirtyAfter {
		return from, to, fmt.Errorf("迁移执行后处于 dirty 状态（版本 %d）：请检查数据库状态", to)
	}
	_ = execCtx
	return from, to, nil
}

// migratorVersion 读取当前迁移版本（未执行任何迁移时为 0）。
func migratorVersion(migrator *migrate.Migrate) (uint, bool, error) {
	version, dirty, err := migrator.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("读取迁移版本失败: %w", err)
	}
	return version, dirty, nil
}

// multiStatementDSN 确保迁移连接允许一次发送多条语句（迁移 SQL 文件里含多语句）。
func multiStatementDSN(dsn string) string {
	parsed, err := gomysql.ParseDSN(strings.TrimSpace(dsn))
	if err != nil {
		return dsn
	}
	parsed.MultiStatements = true
	return parsed.FormatDSN()
}
