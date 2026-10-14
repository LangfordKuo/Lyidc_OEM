package dbbackup

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	gomysql "github.com/go-sql-driver/mysql"
)

// loadOptions 是一次恢复的参数。
type loadOptions struct {
	// Path 是备份文件路径（由白名单解析得到，绝不来自请求参数）。
	Path string
}

// loadStats 是恢复结果统计。
type loadStats struct {
	// TableCount 是恢复的表数（备份里的 CREATE TABLE 数量）。
	TableCount int
	// SchemaVersion 是备份里的 schema 版本（恢复后由库内迁移表读出）。
	SchemaVersion int64
	// DroppedTables 是「备份里没有、恢复时被删掉」的表——证明恢复是覆盖而非合并（契约 21.5）。
	DroppedTables []string
	// Statements 是执行的语句数（日志用）。
	Statements int
}

// loadDatabase 用备份文件覆盖当前库（契约 21.5）。
//
// 顺序：逐表 `DROP TABLE IF EXISTS` + `CREATE TABLE` + 分批 `INSERT`（备份文件自身的顺序），
// 然后删除**当前库有、备份里没有**的表，最后读出恢复后的 schema 版本交给调用方决定是否追迁移。
// 全程 `FOREIGN_KEY_CHECKS=0`（本项目多数表刻意不建外键，但 DROP 次序仍可能受约束影响），
// 并在退出前恢复成 1——连接会回到连接池，会话变量必须还原。
//
// 失败即中止：**不吞错、不续跑**，错误信息只带语句种类与表名（绝不回显数据内容）。
func loadDatabase(ctx context.Context, sqlDB *sql.DB, opts loadOptions) (loadStats, error) {
	file, err := os.Open(opts.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return loadStats{}, ErrNotFound
		}
		return loadStats{}, fmt.Errorf("打开备份文件失败: %w", err)
	}
	defer func() { _ = file.Close() }()

	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return loadStats{}, fmt.Errorf("获取数据库连接失败: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if err := ensureBackslashEscapes(ctx, conn); err != nil {
		return loadStats{}, err
	}
	if _, err := conn.ExecContext(ctx, "SET SESSION FOREIGN_KEY_CHECKS=0"); err != nil {
		return loadStats{}, fmt.Errorf("关闭外键检查失败: %w", err)
	}
	defer func() {
		// 会话变量随连接回到连接池，必须还原（否则后续业务写入会跳过外键校验）。
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), "SET SESSION FOREIGN_KEY_CHECKS=1")
	}()

	stats := loadStats{}
	backupTables := map[string]bool{}
	err = splitStatements(file, func(statement string) error {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("执行第 %d 条语句（%s）失败: %w",
				stats.Statements+1, describeStatement(statement), err)
		}
		stats.Statements++
		// 表名同时来自 `DROP TABLE IF EXISTS` 与 `CREATE TABLE`（前者先到）：两个都登记，
		// 但只对 CREATE 计数，否则表数会漏成 0。
		if table, ok := statementTable(statement); ok {
			backupTables[table] = true
			if isCreateTable(statement) {
				stats.TableCount++
			}
		}
		return nil
	})
	if err != nil {
		return stats, err
	}

	// 覆盖式：备份里没有的表一律删除（它是备份点之后才出现的，不能留下）。
	current, err := listBaseTables(ctx, conn)
	if err != nil {
		return stats, err
	}
	for _, table := range current {
		if backupTables[table] {
			continue
		}
		if _, err := conn.ExecContext(ctx, "DROP TABLE IF EXISTS "+quoteIdent(table)); err != nil {
			return stats, fmt.Errorf("删除备份中不存在的表 %s 失败: %w", quoteIdent(table), err)
		}
		stats.DroppedTables = append(stats.DroppedTables, table)
	}

	version, err := schemaVersionOf(ctx, conn)
	if err != nil {
		return stats, err
	}
	stats.SchemaVersion = version
	return stats, nil
}

// ensureBackslashEscapes 确认会话的 sql_mode 不含 NO_BACKSLASH_ESCAPES。
//
// 备份文件用反斜杠转义（`\'`、`\\`、`\n`）书写字符串字面量：在 NO_BACKSLASH_ESCAPES 下这些
// 转义不会被解释，字符串会被**静默改写**（数据损坏）。这里选择直接拒绝并给出可读原因，
// 而不是偷偷改会话 sql_mode（后者会顺带关掉严格模式，可能把错误数据静默吞下）。
func ensureBackslashEscapes(ctx context.Context, conn *sql.Conn) error {
	var mode string
	if err := conn.QueryRowContext(ctx, "SELECT @@SESSION.sql_mode").Scan(&mode); err != nil {
		return fmt.Errorf("读取 sql_mode 失败: %w", err)
	}
	if strings.Contains(strings.ToUpper(mode), "NO_BACKSLASH_ESCAPES") {
		return fmt.Errorf("%w: 当前数据库连接启用了 NO_BACKSLASH_ESCAPES，本备份格式（反斜杠转义）无法安全重放；"+
			"请移除该 sql_mode 后重试恢复", ErrIncompatible)
	}
	return nil
}

// schemaVersionOf 读取迁移表的当前版本；迁移表不存在（全新库）时返回 0。
func schemaVersionOf(ctx context.Context, queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}) (int64, error) {
	var version int64
	err := queryer.QueryRowContext(ctx, "SELECT version FROM schema_migrations LIMIT 1").Scan(&version)
	if err != nil {
		if missingTable(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("读取迁移版本失败: %w", err)
	}
	return version, nil
}

// missingTable 判断错误是否为 MySQL 的「表不存在」（1146）。
func missingTable(err error) bool {
	var mysqlErr *gomysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1146
}

// statementTable 提取语句操作的表名（仅识别本包生成的三种形态；
// 识别不出来时返回 false，不影响执行）。
func statementTable(statement string) (string, bool) {
	trimmed := strings.TrimSpace(statement)
	upper := strings.ToUpper(trimmed)
	for _, prefix := range []string{"DROP TABLE IF EXISTS ", "CREATE TABLE ", "INSERT INTO "} {
		if !strings.HasPrefix(upper, prefix) {
			continue
		}
		rest := strings.TrimSpace(trimmed[len(prefix):])
		if strings.HasPrefix(strings.ToUpper(rest), "IF NOT EXISTS ") {
			rest = strings.TrimSpace(rest[len("IF NOT EXISTS "):])
		}
		name, ok := readIdentifier(rest)
		return name, ok
	}
	return "", false
}

// isCreateTable 判断语句是否为 CREATE TABLE。
func isCreateTable(statement string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(statement)), "CREATE TABLE ")
}

// readIdentifier 读出一个标识符（反引号包裹或裸标识符）。
func readIdentifier(value string) (string, bool) {
	if value == "" {
		return "", false
	}
	if value[0] == '`' {
		if end := strings.Index(value[1:], "`"); end >= 0 {
			return value[1 : 1+end], true
		}
		return "", false
	}
	end := 0
	for end < len(value) && !unicode.IsSpace(rune(value[end])) && value[end] != '(' {
		end++
	}
	if end == 0 {
		return "", false
	}
	return value[:end], true
}

// describeStatement 生成语句的安全描述（供错误信息使用）。
//
// **绝不回显语句正文**：备份内容含管理员密码哈希等敏感数据，错误信息会进日志与接口响应。
func describeStatement(statement string) string {
	trimmed := strings.TrimSpace(statement)
	field := strings.Fields(trimmed)
	kind := ""
	if len(field) > 0 {
		kind = strings.ToUpper(field[0])
	}
	switch kind {
	case "INSERT":
		return "INSERT（数据行，内容不回显）"
	case "CREATE", "DROP", "ALTER":
		if table, ok := statementTable(trimmed); ok {
			return kind + " TABLE " + quoteIdent(table)
		}
		return kind + " 语句"
	case "SET", "START", "COMMIT", "ROLLBACK":
		return kind + " 会话语句"
	case "":
		return "空语句"
	default:
		return kind + " 语句"
	}
}

// 备份文件的词法状态。
const (
	stateNormal = iota
	stateSingleQuote
	stateDoubleQuote
	stateBacktick
	stateLineComment
	stateBlockComment
)

// splitStatements 流式切分备份文件：以顶层分号分隔语句，正确跳过注释与字符串/标识符内部的分号。
//
// 只把**当前一条语句**留在内存里（因此整库备份可以超过内存），并对单条语句设上限防御
// 被手工替换的畸形文件。EOF 时若仍有非空内容（未以分号结束）或仍处在字符串里，按文件被截断拒绝。
func splitStatements(source io.Reader, emit func(statement string) error) error {
	const bufferSize = 256 << 10
	reader := bufio.NewReaderSize(source, bufferSize)
	statement := make([]byte, 0, bufferSize)
	state := stateNormal

	// peek 返回下一个未消费的字节；没有更多输入时返回 false。
	peek := func() (byte, bool) {
		next, err := reader.Peek(1)
		if err != nil || len(next) == 0 {
			return 0, false
		}
		return next[0], true
	}
	// pair 判断后续是否紧跟指定字节（用于 `--` 与 `/*`）。
	pair := func(c byte) bool {
		next, ok := peek()
		return ok && next == c
	}

	for {
		c, err := reader.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("读取备份文件失败: %w", err)
		}

		switch state {
		case stateLineComment:
			statement = append(statement, c)
			if c == '\n' {
				state = stateNormal
			}
			continue
		case stateBlockComment:
			statement = append(statement, c)
			if c == '*' && pair('/') {
				next, _ := reader.ReadByte()
				statement = append(statement, next)
				state = stateNormal
			}
			continue
		case stateSingleQuote, stateDoubleQuote:
			statement = append(statement, c)
			if c == '\\' {
				// 反斜杠转义：连同下一个字符一起吞掉（`\'` 不是字符串结束）。
				if _, ok := peek(); ok {
					consumed, _ := reader.ReadByte()
					statement = append(statement, consumed)
				}
				continue
			}
			if (state == stateSingleQuote && c == '\'') || (state == stateDoubleQuote && c == '"') {
				state = stateNormal
			}
			continue
		case stateBacktick:
			statement = append(statement, c)
			if c == '`' {
				state = stateNormal
			}
			continue
		}

		// stateNormal
		switch {
		case c == '\'':
			state = stateSingleQuote
			statement = append(statement, c)
		case c == '"':
			state = stateDoubleQuote
			statement = append(statement, c)
		case c == '`':
			state = stateBacktick
			statement = append(statement, c)
		case c == '-' && pair('-'):
			consumed, _ := reader.ReadByte()
			state = stateLineComment
			statement = append(statement, c, consumed)
		case c == '#':
			state = stateLineComment
			statement = append(statement, c)
		case c == '/' && pair('*'):
			consumed, _ := reader.ReadByte()
			state = stateBlockComment
			statement = append(statement, c, consumed)
		case c == ';':
			// 剥掉语句前的注释（文件里的 `-- Table: x` 之类）：执行不需要它，
			// 而 statementTable 依赖「语句以 CREATE TABLE / DROP TABLE 开头」来收集表名。
			trimmed := strings.TrimSpace(stripComments(string(statement)))
			if trimmed != "" {
				if err := emit(trimmed); err != nil {
					return err
				}
			}
			statement = statement[:0]
		default:
			statement = append(statement, c)
		}
		if len(statement) > maxStatementBytes {
			return fmt.Errorf("%w: 单条语句超过 %d MiB，备份文件可能已损坏",
				ErrIncompatible, maxStatementBytes>>20)
		}
	}

	if state == stateSingleQuote || state == stateDoubleQuote || state == stateBacktick || state == stateBlockComment {
		return fmt.Errorf("%w: 备份文件被截断（语句或字符串未闭合）", ErrIncompatible)
	}
	if rest := strings.TrimSpace(stripComments(string(statement))); rest != "" {
		return fmt.Errorf("%w: 备份文件不完整（最后一条语句未以分号结束）", ErrIncompatible)
	}
	return nil
}

// stripComments 去掉 SQL 文本里的注释（`--` 行注释、`#` 行注释与 `/* */` 块注释），
// 保留字符串字面量与反引号标识符内部的原样内容（其中的 `--` 不是注释）。
//
// **例外（R18 修正）**：MySQL 的**可执行注释**（`/*! ... */`、`/*!50100 ... */`）不是注释而是语句内容，
// 必须原样保留——`SHOW CREATE TABLE` 会把 FULLTEXT 的 ngram 解析器写成
// `/*!50100 WITH PARSER `ngram` */`，其他版本化选项（分区、行格式等）同样如此；
// 剥掉它们会让恢复出来的表结构**静默降级**（R18 的中文关键字检索就依赖 ngram 解析器）。
func stripComments(text string) string {
	var builder strings.Builder
	builder.Grow(len(text))
	for i := 0; i < len(text); {
		c := text[i]
		switch {
		case c == '\'' || c == '"' || c == '`':
			quote := c
			builder.WriteByte(c)
			i++
			for i < len(text) {
				builder.WriteByte(text[i])
				if quote != '`' && text[i] == '\\' && i+1 < len(text) {
					i++
					builder.WriteByte(text[i])
					i++
					continue
				}
				if text[i] == quote {
					i++
					break
				}
				i++
			}
		case c == '-' && i+1 < len(text) && text[i+1] == '-':
			for i < len(text) && text[i] != '\n' {
				i++
			}
		case c == '#':
			for i < len(text) && text[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(text) && text[i+1] == '*':
			if i+2 < len(text) && text[i+2] == '!' {
				// 可执行注释：整段原样保留（含两侧定界符）。
				if end := strings.Index(text[i+2:], "*/"); end >= 0 {
					stop := i + 2 + end + 2
					builder.WriteString(text[i:stop])
					i = stop
					continue
				}
				// 未闭合：按原样写到结尾（与下方块注释的容错一致）。
				builder.WriteString(text[i:])
				i = len(text)
				continue
			}
			i += 2
			for i+1 < len(text) && !(text[i] == '*' && text[i+1] == '/') {
				i++
			}
			i += 2
			if i > len(text) {
				i = len(text)
			}
		default:
			builder.WriteByte(c)
			i++
		}
	}
	return builder.String()
}
