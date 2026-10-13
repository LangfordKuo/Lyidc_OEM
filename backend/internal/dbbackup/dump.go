package dbbackup

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// 分块与缓冲上限（契约 21.1）。
const (
	// insertMaxRows 是单条 INSERT 的行数上限。
	insertMaxRows = 200
	// insertMaxBytes 是单条 INSERT 的字节上限：MySQL 5.7 的 max_allowed_packet 缺省 4 MiB，
	// 这里按 512 KiB 切分，给语句前缀与转义留足余量（绝不拼一条巨型 INSERT）。
	insertMaxBytes = 512 << 10
	// writeBufferBytes 是写盘缓冲（流式写盘，不在内存里堆整库内容）。
	writeBufferBytes = 1 << 20
	// maxStatementBytes 是恢复时单条语句的字节上限（防御被手工替换/损坏的备份文件）。
	maxStatementBytes = 64 << 20
)

// dumpOptions 是一次导出的参数。
type dumpOptions struct {
	// Path 是临时文件路径（导出完成后由调用方 rename 成最终备份名）。
	Path string
	// Database 是库名（写进文件头与元信息）。
	Database string
	// SchemaVersion 是 schema_migrations 的当前版本。
	SchemaVersion int64
	// AppVersion / Operator 是备份时的应用版本与操作人（写进文件头与元信息）。
	AppVersion string
	Operator   string
	Now        time.Time
}

// dumpStats 是导出结果统计。
type dumpStats struct {
	TableCount int
	RowCount   int64
	FileSize   int64
}

// dumpDatabase 把整个库导出成可重放的 SQL 文本（契约 21.1）。
//
// **一致性口径**：整库导出在一个事务里完成（`REPEATABLE READ` +
// `START TRANSACTION WITH CONSISTENT SNAPSHOT`），全部表读的是**同一个一致性快照**——
// 备份点即开始导出的那一刻，表之间不会出现「A 表是新数据、B 表是旧数据」的错位。
// 取舍：InnoDB 会为长事务保留 undo（导出期间库的写负载越高占用越多）；本项目数据量与
// 单机部署形态下可接受，换来的是「恢复出来的一定是一个自洽的库」。
//
// 每张表按 `DROP TABLE IF EXISTS` → `SHOW CREATE TABLE` 的建表语句 → 分批 `INSERT` 的顺序写盘；
// 空表只有建表语句，没有 INSERT。导出期间**不把整库内容堆在内存里**（只有一条 INSERT 的缓冲）。
func dumpDatabase(ctx context.Context, sqlDB *sql.DB, opts dumpOptions) (dumpStats, error) {
	file, err := os.OpenFile(opts.Path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return dumpStats{}, fmt.Errorf("创建备份文件失败: %w", err)
	}
	defer func() { _ = file.Close() }()
	writer := bufio.NewWriterSize(file, writeBufferBytes)

	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return dumpStats{}, fmt.Errorf("获取数据库连接失败: %w", err)
	}
	defer func() { _ = conn.Close() }()

	// 一致性快照：读视图在 START TRANSACTION 时就建立，之后所有表的读取都落在同一快照上。
	if _, err := conn.ExecContext(ctx, "SET SESSION TRANSACTION ISOLATION LEVEL REPEATABLE READ"); err != nil {
		return dumpStats{}, fmt.Errorf("设置隔离级别失败: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "START TRANSACTION WITH CONSISTENT SNAPSHOT"); err != nil {
		return dumpStats{}, fmt.Errorf("开启一致性快照失败: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		}
	}()

	tables, err := listBaseTables(ctx, conn)
	if err != nil {
		return dumpStats{}, err
	}

	if err := writeDumpHeader(writer, opts, len(tables)); err != nil {
		return dumpStats{}, err
	}

	stats := dumpStats{TableCount: len(tables)}
	for _, table := range tables {
		rows, err := dumpTable(ctx, conn, writer, table)
		if err != nil {
			return dumpStats{}, err
		}
		stats.RowCount += rows
	}

	if _, err := fmt.Fprintf(writer,
		"-- end of backup: %d tables, %d rows\n", stats.TableCount, stats.RowCount); err != nil {
		return dumpStats{}, fmt.Errorf("写入备份结尾失败: %w", err)
	}
	if err := writer.Flush(); err != nil {
		return dumpStats{}, fmt.Errorf("写入备份文件失败: %w", err)
	}
	if err := file.Sync(); err != nil {
		return dumpStats{}, fmt.Errorf("刷新备份文件失败: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		return dumpStats{}, fmt.Errorf("读取备份文件大小失败: %w", err)
	}
	stats.FileSize = info.Size()

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return dumpStats{}, fmt.Errorf("提交导出事务失败: %w", err)
	}
	committed = true
	return stats, nil
}

// writeDumpHeader 写文件头（注释 + 会话前置语句）。
//
// `SET FOREIGN_KEY_CHECKS=0` 与 `SET NAMES utf8mb4` 随文件一起走：恢复端即使忘了设置，
// 重放这个文件也能得到正确结果（本项目多数表刻意不建外键，但 DROP 次序仍可能受约束影响）。
func writeDumpHeader(writer *bufio.Writer, opts dumpOptions, tables int) error {
	header := []string{
		"-- Lyidc_OEM database backup",
		"-- format: lyidc-backup/v1 (可重放 SQL；本文件含敏感数据，不得对外提供下载)",
		"-- generated_at: " + opts.Now.UTC().Format(time.RFC3339),
		"-- database: " + opts.Database,
		"-- schema_version: " + strconv.FormatInt(opts.SchemaVersion, 10),
		"-- app_version: " + opts.AppVersion,
		"-- operator: " + opts.Operator,
		"-- tables: " + strconv.Itoa(tables),
		"SET FOREIGN_KEY_CHECKS=0;",
		"SET NAMES utf8mb4;",
		"",
	}
	for _, line := range header {
		if _, err := writer.WriteString(line + "\n"); err != nil {
			return fmt.Errorf("写入备份文件头失败: %w", err)
		}
	}
	return nil
}

// listBaseTables 列出库内全部基表（不含视图），按名字排序保证导出顺序稳定。
func listBaseTables(ctx context.Context, conn *sql.Conn) ([]string, error) {
	rows, err := conn.QueryContext(ctx, "SHOW FULL TABLES WHERE Table_type = 'BASE TABLE'")
	if err != nil {
		return nil, fmt.Errorf("列出数据表失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tables []string
	for rows.Next() {
		var name, kind string
		if err := rows.Scan(&name, &kind); err != nil {
			return nil, fmt.Errorf("读取表名失败: %w", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历数据表失败: %w", err)
	}
	sort.Strings(tables)
	return tables, nil
}

// dumpTable 导出一张表，返回行数。
func dumpTable(ctx context.Context, conn *sql.Conn, writer *bufio.Writer, table string) (int64, error) {
	var name, ddl string
	if err := conn.QueryRowContext(ctx, "SHOW CREATE TABLE "+quoteIdent(table)).Scan(&name, &ddl); err != nil {
		return 0, fmt.Errorf("读取表 %s 的建表语句失败: %w", quoteIdent(table), err)
	}
	if _, err := fmt.Fprintf(writer,
		"\n-- Table: %s\nDROP TABLE IF EXISTS %s;\n%s;\n", quoteIdent(table), quoteIdent(table), ddl); err != nil {
		return 0, fmt.Errorf("写入表 %s 的建表语句失败: %w", quoteIdent(table), err)
	}

	rows, err := conn.QueryContext(ctx, "SELECT * FROM "+quoteIdent(table))
	if err != nil {
		return 0, fmt.Errorf("读取表 %s 的数据失败: %w", quoteIdent(table), err)
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return 0, fmt.Errorf("读取表 %s 的列名失败: %w", quoteIdent(table), err)
	}
	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return 0, fmt.Errorf("读取表 %s 的列类型失败: %w", quoteIdent(table), err)
	}
	binaries := make([]bool, len(columnTypes))
	for i, columnType := range columnTypes {
		binaries[i] = isBinaryType(columnType.DatabaseTypeName())
	}

	prefix := insertPrefix(table, columns)
	scanned := make([]any, len(columns))
	pointers := make([]any, len(columns))
	for i := range scanned {
		pointers[i] = &scanned[i]
	}

	var (
		statement   []byte
		rowValues   []byte
		rowsInChunk int
		total       int64
	)
	flush := func() error {
		if rowsInChunk == 0 {
			return nil
		}
		statement = append(statement, ";\n"...)
		if _, err := writer.Write(statement); err != nil {
			return fmt.Errorf("写入表 %s 的数据失败: %w", quoteIdent(table), err)
		}
		statement = statement[:0]
		rowsInChunk = 0
		return nil
	}

	for rows.Next() {
		if err := rows.Scan(pointers...); err != nil {
			return total, fmt.Errorf("读取表 %s 的行失败: %w", quoteIdent(table), err)
		}
		rowValues = rowValues[:0]
		rowValues = append(rowValues, '(')
		for i, value := range scanned {
			if i > 0 {
				rowValues = append(rowValues, ',')
			}
			rowValues = appendValueLiteral(rowValues, value, binaries[i])
		}
		rowValues = append(rowValues, ')')

		if rowsInChunk > 0 && (rowsInChunk >= insertMaxRows || len(statement)+len(rowValues)+2 > insertMaxBytes) {
			if err := flush(); err != nil {
				return total, err
			}
		}
		if rowsInChunk == 0 {
			statement = append(statement[:0], prefix...)
		} else {
			statement = append(statement, ',')
		}
		statement = append(statement, rowValues...)
		rowsInChunk++
		total++
	}
	if err := rows.Err(); err != nil {
		return total, fmt.Errorf("遍历表 %s 的数据失败: %w", quoteIdent(table), err)
	}
	if err := flush(); err != nil {
		return total, err
	}
	return total, nil
}

// insertPrefix 拼出 `INSERT INTO `t` (`c1`,`c2`) VALUES ` 前缀。
func insertPrefix(table string, columns []string) string {
	var builder strings.Builder
	builder.WriteString("INSERT INTO ")
	builder.WriteString(quoteIdent(table))
	builder.WriteString(" (")
	for i, column := range columns {
		if i > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(quoteIdent(column))
	}
	builder.WriteString(") VALUES ")
	return builder.String()
}

// quoteIdent 用反引号包裹标识符（内部反引号按 MySQL 口径双写）。
func quoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// binaryTypeNames 是以二进制串形式写出的列类型（用 X'..' 字面量）。
//
// TEXT / VARCHAR / JSON / DECIMAL / ENUM / SET 等列驱动同样返回 []byte，但它们是**文本**，
// 必须走转义字符串字面量；只有真正的二进制类型才用十六进制，避免 latin1 之外的字节被改写。
var binaryTypeNames = map[string]bool{
	"BINARY": true, "VARBINARY": true,
	"TINYBLOB": true, "BLOB": true, "MEDIUMBLOB": true, "LONGBLOB": true,
	"BIT":      true,
	"GEOMETRY": true, "POINT": true, "LINESTRING": true, "POLYGON": true,
	"MULTIPOINT": true, "MULTILINESTRING": true, "MULTIPOLYGON": true, "GEOMETRYCOLLECTION": true,
}

// isBinaryType 判断列类型是否按二进制处理。
func isBinaryType(databaseType string) bool {
	return binaryTypeNames[strings.ToUpper(databaseType)]
}

// appendValueLiteral 把一行里的一个值写成 SQL 字面量（NULL 与字符串严格区分）。
func appendValueLiteral(dst []byte, value any, binary bool) []byte {
	switch v := value.(type) {
	case nil:
		return append(dst, "NULL"...)
	case time.Time:
		return appendTimeLiteral(dst, v)
	case []byte:
		if binary {
			return appendHexLiteral(dst, v)
		}
		return appendStringLiteral(dst, string(v))
	case string:
		return appendStringLiteral(dst, v)
	case int64:
		return strconv.AppendInt(dst, v, 10)
	case uint64:
		return strconv.AppendUint(dst, v, 10)
	case float64:
		return strconv.AppendFloat(dst, v, 'g', -1, 64)
	case float32:
		return strconv.AppendFloat(dst, float64(v), 'g', -1, 32)
	case bool:
		if v {
			return append(dst, '1')
		}
		return append(dst, '0')
	default:
		// 兜底：未知驱动类型按文本处理，绝不 panic、绝不静默丢弃。
		return appendStringLiteral(dst, fmt.Sprint(v))
	}
}

// appendTimeLiteral 写 DATETIME / TIMESTAMP / DATE 字面量。
//
// 连接的 DSN 强制 parseTime=true & loc=UTC，驱动把时间列还原为 UTC 墙钟值；这里按同样的
// UTC 墙钟值写回，因此往返后与库中存储的字节完全一致（与开发机时区无关）。
// 小数秒只在存在时才写，避免把 DATETIME(0) 的四舍五入带进来。
func appendTimeLiteral(dst []byte, value time.Time) []byte {
	if value.IsZero() {
		// 驱动把 MySQL 的零值日期（'0000-00-00'）还原为 Go 的零值时间。
		return appendStringLiteral(dst, "0000-00-00 00:00:00")
	}
	utc := value.UTC()
	layout := "2006-01-02 15:04:05"
	if utc.Nanosecond() != 0 {
		layout = "2006-01-02 15:04:05.000000"
	}
	return appendStringLiteral(dst, utc.Format(layout))
}

// appendStringLiteral 写单引号字符串字面量：转义单引号、反斜杠与控制字符。
//
// 换行/回车/退格等一律写成转义序列（而不是原样换行），使一条 INSERT 始终是文件里的一行——
// 备份文件因此可以按行阅读与 diff，恢复端的分词器也只需处理一种转义口径。
func appendStringLiteral(dst []byte, value string) []byte {
	dst = append(dst, '\'')
	for i := 0; i < len(value); i++ {
		switch c := value[i]; c {
		case '\'':
			dst = append(dst, '\\', '\'')
		case '\\':
			dst = append(dst, '\\', '\\')
		case '\n':
			dst = append(dst, '\\', 'n')
		case '\r':
			dst = append(dst, '\\', 'r')
		case 0x00:
			dst = append(dst, '\\', '0')
		case 0x1a:
			dst = append(dst, '\\', 'Z')
		default:
			dst = append(dst, c)
		}
	}
	return append(dst, '\'')
}

// appendHexLiteral 写十六进制字面量（二进制列用）。
func appendHexLiteral(dst []byte, value []byte) []byte {
	dst = append(dst, 'X', '\'')
	const digits = "0123456789ABCDEF"
	for _, b := range value {
		dst = append(dst, digits[b>>4], digits[b&0x0f])
	}
	return append(dst, '\'')
}
