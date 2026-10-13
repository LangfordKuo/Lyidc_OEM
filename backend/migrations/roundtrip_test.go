package migrations_test

import (
	"database/sql"
	"testing"

	gomysql "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/LangfordKuo/Lyidc_OEM/backend/migrations"
)

// 迁移 up/down 往返测试（R10-B 硬性约束：0016 必须 up/down 成对）。
//
// 在独立的临时库 lyidc_mig_check 上跑：全量 up → 断言 0016 落表且版本 16 →
// down 一步 → 断言表被删且版本 15 → 再 up 一次确认可重复应用。
// 本机/CI 没有 MySQL 时 t.Skip（与集成测试同一口径）。

const roundTripDSN = "root:lyidc123@tcp(127.0.0.1:3306)/lyidc_mig_check?charset=utf8mb4&parseTime=true&loc=UTC&multiStatements=true"

func TestMigrationsUpDownRoundTrip(t *testing.T) {
	parsed, err := gomysql.ParseDSN(roundTripDSN)
	if err != nil {
		t.Fatalf("解析 DSN 失败: %v", err)
	}
	serverDSN := *parsed
	serverDSN.DBName = ""
	serverDB, err := sql.Open("mysql", serverDSN.FormatDSN())
	if err != nil {
		t.Skipf("跳过迁移往返测试（MySQL 不可用）: %v", err)
	}
	defer func() { _ = serverDB.Close() }()
	if err := serverDB.Ping(); err != nil {
		t.Skipf("跳过迁移往返测试（MySQL 不可用）: %v", err)
	}

	if _, err := serverDB.Exec("DROP DATABASE IF EXISTS `lyidc_mig_check`"); err != nil {
		t.Fatalf("清理临时库失败: %v", err)
	}
	if _, err := serverDB.Exec("CREATE DATABASE `lyidc_mig_check` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci"); err != nil {
		t.Fatalf("创建临时库失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = serverDB.Exec("DROP DATABASE IF EXISTS `lyidc_mig_check`")
		_ = serverDB.Close()
	})

	sqlDB, err := sql.Open("mysql", roundTripDSN)
	if err != nil {
		t.Fatalf("连接临时库失败: %v", err)
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

	countPayouts := func() int64 {
		t.Helper()
		var n int64
		if err := sqlDB.QueryRow("SELECT COUNT(*) FROM information_schema.tables " +
			"WHERE table_schema = 'lyidc_mig_check' AND table_name = 'referral_payouts'").Scan(&n); err != nil {
			t.Fatalf("查询表存在性失败: %v", err)
		}
		return n
	}

	// 1) 全量 up：0016 建表，版本停在 16。
	if err := migrator.Up(); err != nil {
		t.Fatalf("全量 up 失败: %v", err)
	}
	if n := countPayouts(); n != 1 {
		t.Fatalf("up 后 referral_payouts 存在性 = %d，期望 1", n)
	}
	version, dirty, err := migrator.Version()
	if err != nil || version != 16 || dirty {
		t.Fatalf("up 后版本 = %d/dirty=%v err=%v，期望 16/clean", version, dirty, err)
	}

	// 2) down 一步：0016 回滚删表，版本退回 15。
	if err := migrator.Steps(-1); err != nil {
		t.Fatalf("down 一步失败: %v", err)
	}
	if n := countPayouts(); n != 0 {
		t.Fatalf("down 后 referral_payouts 存在性 = %d，期望 0", n)
	}
	version, dirty, err = migrator.Version()
	if err != nil || version != 15 || dirty {
		t.Fatalf("down 后版本 = %d/dirty=%v err=%v，期望 15/clean", version, dirty, err)
	}

	// 3) 再 up 一次：0016 可重复应用（up 幂等由版本号保证）。
	if err := migrator.Up(); err != nil {
		t.Fatalf("二次 up 失败: %v", err)
	}
	if n := countPayouts(); n != 1 {
		t.Fatalf("二次 up 后 referral_payouts 存在性 = %d，期望 1", n)
	}
}
