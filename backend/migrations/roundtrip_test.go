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

// 迁移 up/down 往返测试（R10-B 起硬性约束：最新迁移必须 up/down 成对；
// R12 扩到 0017；R16 扩到 0018；R18 扩到 0019；P4 扩到 0020）。
//
// 在独立的临时库 lyidc_mig_check 上跑：全量 up → 断言最新迁移（0020）给 orders 加出
// coupon_counted 并把 coupons.used_count 的注释改成 P4 口径、0019 的 operation_logs 仍在，
// 且版本 20 → down 一步 → 断言 0020 的列被删、注释还原、0019 的表仍在且版本退回 19 →
// 再 down 两步确认 0019/0018 的 down 不受影响 → 再全量 up 一次确认可重复应用。
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
		return columnTypeOf(t, sqlDB, "instances", column)
	}

	// isInt 判断列类型是不是 int（MySQL 5.7 会带显示宽度，如 int(11) / int(10) unsigned）。
	isInt := func(columnType string, unsigned bool) bool {
		if !strings.HasPrefix(columnType, "int") {
			return false
		}
		return strings.Contains(columnType, "unsigned") == unsigned
	}

	// countTable 返回指定表在临时库里是否存在（1/0）。
	countTable := func(table string) int64 {
		t.Helper()
		var n int64
		if err := sqlDB.QueryRow("SELECT COUNT(*) FROM information_schema.tables "+
			"WHERE table_schema = 'lyidc_mig_check' AND table_name = ?", table).Scan(&n); err != nil {
			t.Fatalf("查询表 %s 存在性失败: %v", table, err)
		}
		return n
	}

	// countIndex 返回指定索引在临时库里是否存在（1/0），用于断言 0019 的三条普通索引与 FULLTEXT。
	countIndex := func(table, index string) int64 {
		t.Helper()
		var n int64
		if err := sqlDB.QueryRow("SELECT COUNT(DISTINCT index_name) FROM information_schema.statistics "+
			"WHERE table_schema = 'lyidc_mig_check' AND table_name = ? AND index_name = ?", table, index).Scan(&n); err != nil {
			t.Fatalf("查询索引 %s.%s 失败: %v", table, index, err)
		}
		return n
	}

	// 1) 全量 up：0017 建表、0018 三项改动、0019 的 operation_logs 与 0020 的占用列都在，版本停在 20。
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
	// 0019（R18 操作日志）：表 + 三条普通索引 + ngram FULLTEXT 都要建成。
	if n := countTable("operation_logs"); n != 1 {
		t.Fatalf("up 后 operation_logs 存在性 = %d，期望 1", n)
	}
	for _, index := range []string{
		"idx_operation_logs_channel_created",
		"idx_operation_logs_actor_created",
		"idx_operation_logs_created",
		"ft_operation_logs_search",
	} {
		if n := countIndex("operation_logs", index); n != 1 {
			t.Fatalf("up 后 operation_logs 索引 %s 存在性 = %d，期望 1", index, n)
		}
	}
	// FULLTEXT 必须真的用上 ngram 解析器（否则中文关键字检索会静默失效——默认解析器把整句当一个词）。
	var fulltextName, fulltextDDL string
	if err := sqlDB.QueryRow("SHOW CREATE TABLE operation_logs").Scan(&fulltextName, &fulltextDDL); err != nil {
		t.Fatalf("读取 operation_logs 建表语句失败: %v", err)
	}
	if !strings.Contains(fulltextDDL, "WITH PARSER `ngram`") {
		t.Fatalf("operation_logs 的 FULLTEXT 未使用 ngram 解析器:\n%s", fulltextDDL)
	}
	// 0020（P4 优惠码「下单即占用」）：orders 加出 coupon_counted（TINYINT(1) NOT NULL DEFAULT 0），
	// 并把 coupons.used_count 的列注释改成 P4 口径（只改注释，不改类型与默认值）。
	if got := columnTypeOf(t, sqlDB, "orders", "coupon_counted"); !strings.HasPrefix(got, "tinyint(1)") {
		t.Fatalf("up 后 orders.coupon_counted 列类型 = %q，期望 tinyint(1)", got)
	}
	var couponCountedDefault string
	if err := sqlDB.QueryRow("SELECT COLUMN_DEFAULT FROM information_schema.columns " +
		"WHERE table_schema = 'lyidc_mig_check' AND table_name = 'orders' AND column_name = 'coupon_counted'").
		Scan(&couponCountedDefault); err != nil {
		t.Fatalf("查询 orders.coupon_counted 默认值失败: %v", err)
	}
	if couponCountedDefault != "0" {
		t.Fatalf("orders.coupon_counted 默认值 = %q，期望 \"0\"（历史行必须默认未占用）", couponCountedDefault)
	}
	if got := columnCommentOf(t, sqlDB, "coupons", "used_count"); !strings.Contains(got, "已占用次数") {
		t.Fatalf("up 后 coupons.used_count 列注释 = %q，期望含「已占用次数」", got)
	}
	version, dirty, err := migrator.Version()
	if err != nil || version != 20 || dirty {
		t.Fatalf("up 后版本 = %d/dirty=%v err=%v，期望 20/clean", version, dirty, err)
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

	// 2) down 一步：只撤销 0020（占用列被删、used_count 注释还原），0019 的 operation_logs
	//    与 0018 的改动原样保留，版本退回 19。
	if err := migrator.Steps(-1); err != nil {
		t.Fatalf("down 一步失败: %v", err)
	}
	if got := columnTypeOf(t, sqlDB, "orders", "coupon_counted"); got != "" {
		t.Fatalf("down 0020 后 orders.coupon_counted 仍存在: %q", got)
	}
	if got := columnCommentOf(t, sqlDB, "coupons", "used_count"); got != "已使用次数（本阶段只读不增）" {
		t.Fatalf("down 0020 后 coupons.used_count 列注释 = %q，期望还原为 0005 原文", got)
	}
	if n := countTable("operation_logs"); n != 1 {
		t.Fatalf("down 0020 不应影响 0019 的表（存在性 = %d），期望 1", n)
	}
	version, dirty, err = migrator.Version()
	if err != nil || version != 19 || dirty {
		t.Fatalf("down 0020 后版本 = %d/dirty=%v err=%v，期望 19/clean", version, dirty, err)
	}

	// 3) 再 down 一步：撤销 0019（operation_logs 被删），0018 的改动原样保留，版本退回 18。
	if err := migrator.Steps(-1); err != nil {
		t.Fatalf("down 第二步失败: %v", err)
	}
	if n := countTable("operation_logs"); n != 0 {
		t.Fatalf("down 0019 后 operation_logs 仍存在（存在性 = %d），期望 0", n)
	}
	if got := columnType("downstream_token"); !strings.HasPrefix(got, "varchar(32)") {
		t.Fatalf("down 0019 不应影响 0018 的列，downstream_token = %q", got)
	}
	version, dirty, err = migrator.Version()
	if err != nil || version != 18 || dirty {
		t.Fatalf("down 0019 后版本 = %d/dirty=%v err=%v，期望 18/clean", version, dirty, err)
	}

	// 4) 再 down 一步：0018 撤销（列消失、host_id 回到无符号、枚举收起、占位行被清），版本退回 17。
	if err := migrator.Steps(-1); err != nil {
		t.Fatalf("down 第三步失败: %v", err)
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

	// 5) 再全量 up：0018 / 0019 / 0020 都可重复应用（up 幂等由版本号保证）。
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
	if n := countTable("operation_logs"); n != 1 {
		t.Fatalf("二次 up 后 operation_logs 存在性 = %d，期望 1", n)
	}
	if n := countIndex("operation_logs", "ft_operation_logs_search"); n != 1 {
		t.Fatalf("二次 up 后 FULLTEXT 索引存在性 = %d，期望 1", n)
	}
	if got := columnTypeOf(t, sqlDB, "orders", "coupon_counted"); !strings.HasPrefix(got, "tinyint(1)") {
		t.Fatalf("二次 up 后 orders.coupon_counted 列类型 = %q，期望 tinyint(1)", got)
	}
	version, dirty, err = migrator.Version()
	if err != nil || version != 20 || dirty {
		t.Fatalf("二次 up 后版本 = %d/dirty=%v err=%v，期望 20/clean", version, dirty, err)
	}
}

// columnTypeOf 返回指定表某列的 COLUMN_TYPE（表或列不存在时返回空串）。
func columnTypeOf(t *testing.T, db *sql.DB, table, column string) string {
	t.Helper()
	var columnType string
	err := db.QueryRow("SELECT COLUMN_TYPE FROM information_schema.columns "+
		"WHERE table_schema = 'lyidc_mig_check' AND table_name = ? AND column_name = ?", table, column).Scan(&columnType)
	if err == sql.ErrNoRows {
		return ""
	}
	if err != nil {
		t.Fatalf("查询列 %s.%s 失败: %v", table, column, err)
	}
	return columnType
}

// columnCommentOf 返回指定表某列的 COLUMN_COMMENT（列不存在时返回空串）。
func columnCommentOf(t *testing.T, db *sql.DB, table, column string) string {
	t.Helper()
	var comment string
	err := db.QueryRow("SELECT COLUMN_COMMENT FROM information_schema.columns "+
		"WHERE table_schema = 'lyidc_mig_check' AND table_name = ? AND column_name = ?", table, column).Scan(&comment)
	if err == sql.ErrNoRows {
		return ""
	}
	if err != nil {
		t.Fatalf("查询列注释 %s.%s 失败: %v", table, column, err)
	}
	return comment
}
