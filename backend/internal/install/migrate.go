package install

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	gomysql "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/LangfordKuo/Lyidc_OEM/backend/migrations"
)

// MigrationTimeout 是执行迁移的整体超时。
const MigrationTimeout = 60 * time.Second

// migrationStep 是一个迁移版本的展示信息。
type migrationStep struct {
	Version uint   `json:"version"`
	Name    string `json:"name"`
}

// migrationResult 是「初始化建表」的结果视图。
type migrationResult struct {
	// FromVersion 执行前的版本（0 表示尚未执行任何迁移）。
	FromVersion uint `json:"from_version"`
	// ToVersion 执行后的版本。
	ToVersion uint `json:"to_version"`
	// Applied 本次实际应用的迁移版本。
	Applied []migrationStep `json:"applied"`
	// NoChange 表示执行时已是最新版本（重复执行第 3 步的幂等结果）。
	NoChange bool `json:"no_change"`
	// TableCount 建表后库内表数量（含 schema_migrations）。
	TableCount int64 `json:"table_count"`
}

// runMigrations 用内嵌迁移文件（0001 起全部）建表，返回实际应用的版本。
//
// 复用与 cmd/migrate 完全相同的迁移执行方式（golang-migrate + migrations.FS），
// 不复制任何迁移 SQL（契约 13.3）。
func runMigrations(ctx context.Context, dsn string) (migrationResult, error) {
	available, err := availableMigrations()
	if err != nil {
		return migrationResult{}, err
	}

	sqlDB, err := sql.Open("mysql", multiStatementDSN(dsn))
	if err != nil {
		return migrationResult{}, fmt.Errorf("打开数据库连接失败: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	execCtx, cancel := context.WithTimeout(ctx, MigrationTimeout)
	defer cancel()
	if err := sqlDB.PingContext(execCtx); err != nil {
		return migrationResult{}, fmt.Errorf("连接数据库失败: %w", err)
	}

	driver, err := migratemysql.WithInstance(sqlDB, &migratemysql.Config{})
	if err != nil {
		return migrationResult{}, fmt.Errorf("初始化迁移驱动失败: %w", err)
	}
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return migrationResult{}, fmt.Errorf("加载内嵌迁移文件失败: %w", err)
	}
	migrator, err := migrate.NewWithInstance("iofs", source, "mysql", driver)
	if err != nil {
		return migrationResult{}, fmt.Errorf("初始化迁移器失败: %w", err)
	}
	defer func() { _, _ = migrator.Close() }()

	from, dirty, err := currentVersion(migrator)
	if err != nil {
		return migrationResult{}, err
	}
	if dirty {
		return migrationResult{}, fmt.Errorf(
			"迁移表处于 dirty 状态（版本 %d 的上一次迁移失败）：请按迁移文件手工修正数据库后重试", from)
	}

	noChange := false
	if err := migrator.Up(); err != nil {
		if !errors.Is(err, migrate.ErrNoChange) {
			return migrationResult{}, fmt.Errorf("执行迁移失败: %w", err)
		}
		noChange = true
	}

	to, dirtyAfter, err := currentVersion(migrator)
	if err != nil {
		return migrationResult{}, err
	}
	if dirtyAfter {
		return migrationResult{}, fmt.Errorf("迁移执行后处于 dirty 状态（版本 %d）：请检查数据库状态", to)
	}

	result := migrationResult{
		FromVersion: from,
		ToVersion:   to,
		NoChange:    noChange,
		Applied:     migrationsBetween(available, from, to),
	}
	if err := sqlDB.QueryRowContext(execCtx,
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()").
		Scan(&result.TableCount); err != nil {
		return result, fmt.Errorf("统计表数量失败: %w", err)
	}
	return result, nil
}

// availableMigrations 返回内嵌迁移文件中的版本清单（升序）。
func availableMigrations() ([]migrationStep, error) {
	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("读取内嵌迁移文件失败: %w", err)
	}

	var steps []migrationStep
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		base := strings.TrimSuffix(name, ".up.sql")
		parts := strings.SplitN(base, "_", 2)
		version, err := strconv.Atoi(parts[0])
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("迁移文件名 %q 不以版本号开头", name)
		}
		steps = append(steps, migrationStep{Version: uint(version), Name: base})
	}
	if len(steps) == 0 {
		return nil, errors.New("内嵌迁移文件为空，构建产物可能不完整")
	}
	sort.Slice(steps, func(i, j int) bool { return steps[i].Version < steps[j].Version })
	return steps, nil
}

// migrationsBetween 返回版本区间 (from, to] 内的迁移（用于展示「本次应用了哪些版本」）。
func migrationsBetween(available []migrationStep, from, to uint) []migrationStep {
	applied := make([]migrationStep, 0, len(available))
	for _, step := range available {
		if step.Version > from && step.Version <= to {
			applied = append(applied, step)
		}
	}
	return applied
}

// currentVersion 返回当前迁移版本（未执行任何迁移时返回 0）。
func currentVersion(migrator *migrate.Migrate) (uint, bool, error) {
	version, dirty, err := migrator.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("读取迁移版本失败: %w", err)
	}
	return version, dirty, nil
}

// multiStatementDSN 确保迁移连接允许一次发送多条语句（迁移 SQL 文件）。
func multiStatementDSN(dsn string) string {
	parsed, err := gomysql.ParseDSN(strings.TrimSpace(dsn))
	if err != nil {
		return dsn
	}
	parsed.MultiStatements = true
	return parsed.FormatDSN()
}
