package migrations_test

import (
	"database/sql"
	"strings"
	"testing"

	gomysql "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/LangfordKuo/Lyidc_OEM/backend/migrations"
)

// 迁移 up/down 往返测试（R10-B 起硬性约束：最新迁移必须 up/down 成对；R12 扩到 0017；R16 扩到 0018）。
//
// 在独立的临时库 lyidc_mig_check 上跑：全量 up → 断言最新迁移（0018）落表/落列且版本 18 →
// down 一步 → 断言 0018 的改动全部撤销（列消失、host_id 回到无符号、枚举收起、占位行被清）且版本 17 →
// 再 up 一次确认可重复应用。
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

	countMaps := func() int64 {
		t.Helper()
		var n int64
		if err := sqlDB.QueryRow("SELECT COUNT(*) FROM information_schema.tables " +
			"WHERE table_schema = 'lyidc_mig_check' AND table_name = 'product_group_category_maps'").Scan(&n); err != nil {
			t.Fatalf("查询表存在性失败: %v", err)
		}
		return n
	}

	// columnType 返回 instances 某列的 COLUMN_TYPE（列不存在时返回空串）。
	columnType := func(column string) string {
		t.Helper()
		var columnType string
		err := sqlDB.QueryRow("SELECT COLUMN_TYPE FROM information_schema.columns "+
			"WHERE table_schema = 'lyidc_mig_check' AND table_name = 'instances' AND column_name = ?", column).Scan(&columnType)
		if err == sql.ErrNoRows {
			return ""
		}
		if err != nil {
			t.Fatalf("查询列 %s 失败: %v", column, err)
		}
		return columnType
	}

	// isInt 判断列类型是不是 int（MySQL 5.7 会带显示宽度，如 int(11) / int(10) unsigned）。
	isInt := func(columnType string, unsigned bool) bool {
		if !strings.HasPrefix(columnType, "int") {
			return false
		}
		return strings.Contains(columnType, "unsigned") == unsigned
	}

	// 1) 全量 up：0017 建表仍在，0018 的三项改动落库，版本停在 18。
	if err := migrator.Up(); err != nil {
		t.Fatalf("全量 up 失败: %v", err)
	}
	if n := countMaps(); n != 1 {
		t.Fatalf("up 后 product_group_category_maps 存在性 = %d，期望 1", n)
	}
	if got := columnType("downstream_token"); !strings.HasPrefix(got, "varchar(32)") {
		t.Fatalf("up 后 downstream_token 列类型 = %q，期望 varchar(32)", got)
	}
	if got := columnType("host_id"); !isInt(got, false) {
		t.Fatalf("up 后 host_id 列类型 = %q，期望带符号 int（负数占位）", got)
	}
	if got := columnType("status"); !strings.Contains(got, "'provisioning'") {
		t.Fatalf("up 后 status 枚举 = %q，期望含 provisioning", got)
	}
	version, dirty, err := migrator.Version()
	if err != nil || version != 18 || dirty {
		t.Fatalf("up 后版本 = %d/dirty=%v err=%v，期望 18/clean", version, dirty, err)
	}

	// 占位行（status=provisioning、host_id=-订单号）与已交付行各插一条：
	// down 必须清掉占位行（负数在 INT UNSIGNED 上无表示）并保留已交付行。
	insertInstance := func(orderID uint64, hostID int, status string) {
		t.Helper()
		if _, err := sqlDB.Exec("INSERT INTO instances "+
			"(member_id, order_id, host_id, product_id, product_name, name, billing_cycle, status, "+
			"downstream_token, created_at, updated_at) VALUES (1, ?, ?, 1, 'p', 'oem-x', 'monthly', ?, 'tok', "+
			"UTC_TIMESTAMP(), UTC_TIMESTAMP())", orderID, hostID, status); err != nil {
			t.Fatalf("插入实例行（order=%d status=%s）失败: %v", orderID, status, err)
		}
	}
	insertInstance(7, -7, "provisioning")
	insertInstance(8, 90001, "active")

	// 2) down 一步：0018 撤销（列消失、host_id 回到无符号、枚举收起、占位行被清），版本退回 17。
	if err := migrator.Steps(-1); err != nil {
		t.Fatalf("down 一步失败: %v", err)
	}
	if n := countMaps(); n != 1 {
		t.Fatalf("down 0018 不应影响 0017 的表，存在性 = %d，期望 1", n)
	}
	if got := columnType("downstream_token"); got != "" {
		t.Fatalf("down 后 downstream_token 仍存在: %q", got)
	}
	if got := columnType("host_id"); !isInt(got, true) {
		t.Fatalf("down 后 host_id 列类型 = %q，期望 int unsigned", got)
	}
	if got := columnType("status"); strings.Contains(got, "'provisioning'") {
		t.Fatalf("down 后 status 枚举 = %q，不应含 provisioning", got)
	}
	var placeholder, delivered int64
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM instances WHERE status = 'provisioning'").Scan(&placeholder); err != nil {
		t.Fatalf("统计占位行失败: %v", err)
	}
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM instances WHERE host_id = 90001").Scan(&delivered); err != nil {
		t.Fatalf("统计已交付行失败: %v", err)
	}
	if placeholder != 0 || delivered != 1 {
		t.Fatalf("down 后占位行 = %d（期望 0）、已交付行 = %d（期望 1）", placeholder, delivered)
	}
	version, dirty, err = migrator.Version()
	if err != nil || version != 17 || dirty {
		t.Fatalf("down 后版本 = %d/dirty=%v err=%v，期望 17/clean", version, dirty, err)
	}

	// 3) 再 up 一次：0018 可重复应用（up 幂等由版本号保证）。
	if err := migrator.Up(); err != nil {
		t.Fatalf("二次 up 失败: %v", err)
	}
	if n := countMaps(); n != 1 {
		t.Fatalf("二次 up 后 product_group_category_maps 存在性 = %d，期望 1", n)
	}
	if got := columnType("downstream_token"); !strings.HasPrefix(got, "varchar(32)") {
		t.Fatalf("二次 up 后 downstream_token 列类型 = %q，期望 varchar(32)", got)
	}
	if got := columnType("host_id"); !isInt(got, false) {
		t.Fatalf("二次 up 后 host_id 列类型 = %q，期望带符号 int", got)
	}
}
