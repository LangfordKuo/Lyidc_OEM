package install

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	gomysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// 安装向导连接测试与建库的超时。
const (
	// ConnectTimeout 测试连接的单次超时。
	ConnectTimeout = 8 * time.Second
	// CreateDatabaseTimeout 自动建库的超时。
	CreateDatabaseTimeout = 15 * time.Second
	// databaseNamePattern 允许的库名（限制为字母数字下划线，避免拼接建库语句时出现注入面）。
	databaseNamePattern = `^[A-Za-z0-9_]{1,64}$`
)

// MySQL 错误码（用于把失败原因翻译成人话，契约 13.3）。
const (
	mysqlErrAccessDenied    = 1045
	mysqlErrDBAccesDenied   = 1044
	mysqlErrUnknownDatabase = 1049
	mysqlErrHostNotAllowed  = 1130
)

// databaseRequest 是安装向导第 2 步（数据库配置）的请求体。
type databaseRequest struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	Database       string `json:"database"`
	CreateDatabase bool   `json:"create_database"`
}

// databaseView 是测试连接/保存参数的结果视图。
//
// 密钥口径（契约 13.7）：**不含密码，也不回显完整 DSN**，只回连接目标与探测结果。
type databaseView struct {
	Connected       bool   `json:"connected"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Username        string `json:"username"`
	Database        string `json:"database"`
	ServerVersion   string `json:"server_version"`
	DatabaseCreated bool   `json:"database_created"`
	TableCount      int64  `json:"table_count"`
}

// connectFailure 描述数据库连接失败的原因与处置建议（文本中不含密码）。
type connectFailure struct {
	// Message 面向用户的失败原因。
	Message string
	// Advice 处置建议。
	Advice string
	// NeedCreateDatabase 表示「库不存在且未选择自动建库」。
	NeedCreateDatabase bool
}

// Error 实现 error。
func (f *connectFailure) Error() string { return f.Message }

// normalize 规范化请求字段（去空格、端口缺省）。
func (req *databaseRequest) normalize() {
	req.Host = strings.TrimSpace(req.Host)
	req.Username = strings.TrimSpace(req.Username)
	req.Database = strings.TrimSpace(req.Database)
	if req.Port == 0 {
		req.Port = 3306
	}
}

// validate 校验表单字段；返回的 error 只含字段名与规则，不含密码。
func (req databaseRequest) validate() error {
	switch {
	case req.Host == "":
		return errors.New("host 不能为空")
	case strings.ContainsAny(req.Host, "/\\ \t"):
		return fmt.Errorf("host %q 不是合法的主机名或 IP", req.Host)
	case req.Port < 1 || req.Port > 65535:
		return fmt.Errorf("port 需为 1-65535 之间的整数，收到 %d", req.Port)
	case req.Username == "":
		return errors.New("username 不能为空")
	case req.Database == "":
		return errors.New("database 不能为空")
	}
	if !validDatabaseName(req.Database) {
		return fmt.Errorf("database %q 非法（只允许字母、数字、下划线，最长 64 字符）", req.Database)
	}
	return nil
}

// validDatabaseName 校验库名。
func validDatabaseName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, char := range name {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z',
			char >= '0' && char <= '9', char == '_':
		default:
			return false
		}
	}
	return true
}

// mysqlConfig 构造连接配置（与 config.DatabaseConfig.DSN 的书写风格一致：
// charset=utf8mb4 + parseTime=true + loc=Local + multiStatements=true）。
func (req databaseRequest) mysqlConfig() *gomysql.Config {
	cfg := gomysql.NewConfig()
	cfg.User = req.Username
	cfg.Passwd = req.Password
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(req.Host, strconv.Itoa(req.Port))
	cfg.DBName = req.Database
	cfg.ParseTime = true
	cfg.Loc = time.Local
	cfg.MultiStatements = true
	cfg.Params = map[string]string{"charset": "utf8mb4"}
	return cfg
}

// dsn 返回写回配置文件的连接串。
func (req databaseRequest) dsn() string { return req.mysqlConfig().FormatDSN() }

// serverDSN 返回不带库名的连接串（用于自动建库）。
func (req databaseRequest) serverDSN() string {
	cfg := req.mysqlConfig()
	cfg.DBName = ""
	cfg.MultiStatements = false
	return cfg.FormatDSN()
}

// testDatabaseConnection 测试连接（必要时自动建库），返回探测结果。
//
// 语义（契约 13.3）：
//   - 库不存在（MySQL 1049）且 create_database=true → 先建库再连（建库结果如实回传）；
//   - 库不存在且未勾选自动建库 → 返回 NeedCreateDatabase 的失败提示；
//   - 其余失败按 MySQL 错误码给出具体处置建议。
func testDatabaseConnection(ctx context.Context, req databaseRequest) (databaseView, error) {
	view := databaseView{
		Host:     req.Host,
		Port:     req.Port,
		Username: req.Username,
		Database: req.Database,
	}

	sqlDB, err := openSQL(ctx, req.dsn())
	if err != nil {
		if isUnknownDatabase(err) && req.CreateDatabase {
			if createErr := createDatabase(ctx, req); createErr != nil {
				return databaseView{}, createErr
			}
			view.DatabaseCreated = true
			sqlDB, err = openSQL(ctx, req.dsn())
		}
		if err != nil {
			return databaseView{}, describeConnectError(err, req)
		}
	}
	defer func() { _ = sqlDB.Close() }()

	view.Connected = true
	if err := sqlDB.QueryRowContext(ctx, "SELECT VERSION()").Scan(&view.ServerVersion); err != nil {
		return databaseView{}, &connectFailure{
			Message: "连接成功但查询版本失败：" + err.Error(),
			Advice:  "请确认账号具备基本查询权限，且 MySQL 版本为 5.7+。",
		}
	}
	if err := sqlDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ?",
		req.Database).Scan(&view.TableCount); err != nil {
		return databaseView{}, &connectFailure{
			Message: "连接成功但统计表数量失败：" + err.Error(),
			Advice:  "请确认账号可读取 information_schema。",
		}
	}
	return view, nil
}

// openSQL 建立并探测一个 database/sql 连接池（带超时）。
func openSQL(ctx context.Context, dsn string) (*sql.DB, error) {
	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, ConnectTimeout)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return sqlDB, nil
}

// createDatabase 用不带库名的连接创建数据库（utf8mb4 / utf8mb4_general_ci，与迁移一致）。
func createDatabase(ctx context.Context, req databaseRequest) error {
	sqlDB, err := openSQL(ctx, req.serverDSN())
	if err != nil {
		return describeConnectError(err, req)
	}
	defer func() { _ = sqlDB.Close() }()

	execCtx, cancel := context.WithTimeout(ctx, CreateDatabaseTimeout)
	defer cancel()
	// 库名已按 ^[A-Za-z0-9_]{1,64}$ 校验，此处再加反引号包裹，双重保证不出现注入面。
	statement := "CREATE DATABASE IF NOT EXISTS `" + req.Database + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci"
	if _, err := sqlDB.ExecContext(execCtx, statement); err != nil {
		if isAccessDeniedForDatabase(err) {
			return &connectFailure{
				Message: fmt.Sprintf("数据库 %q 不存在，且当前账号没有建库权限", req.Database),
				Advice:  "请用有权限的账号手动创建该数据库（utf8mb4 / utf8mb4_general_ci），或改用有建库权限的账号。",
			}
		}
		return &connectFailure{
			Message: "自动建库失败：" + err.Error(),
			Advice:  "可手动创建数据库（字符集 utf8mb4 / 排序规则 utf8mb4_general_ci）后重试，或换用有建库权限的账号。",
		}
	}
	return nil
}

// describeConnectError 把连接错误翻译成带处置建议的失败信息（不含密码）。
func describeConnectError(err error, req databaseRequest) error {
	target := net.JoinHostPort(req.Host, strconv.Itoa(req.Port))

	if isUnknownDatabase(err) {
		return &connectFailure{
			Message: fmt.Sprintf("数据库 %q 不存在", req.Database),
			Advice: "勾选「数据库不存在时自动建库」后重试，或先用有权限的账号手动创建该数据库" +
				"（字符集 utf8mb4 / 排序规则 utf8mb4_general_ci）。",
			NeedCreateDatabase: true,
		}
	}

	var mysqlErr *gomysql.MySQLError
	if errors.As(err, &mysqlErr) {
		switch mysqlErr.Number {
		case mysqlErrAccessDenied:
			return &connectFailure{
				Message: fmt.Sprintf("账号或密码错误（MySQL %d）", mysqlErr.Number),
				Advice:  "请核对用户名与密码（密码区分大小写）；若确认无误，检查该账号是否允许从本机 IP 登录。",
			}
		case mysqlErrHostNotAllowed:
			return &connectFailure{
				Message: fmt.Sprintf("MySQL 拒绝本机 IP 连接（MySQL %d）", mysqlErr.Number),
				Advice:  "请在 MySQL 上为该账号授权来自本机 IP 的访问（GRANT ... TO 'user'@'%'）。",
			}
		case mysqlErrDBAccesDenied:
			return &connectFailure{
				Message: fmt.Sprintf("账号对数据库 %q 没有权限（MySQL %d）", req.Database, mysqlErr.Number),
				Advice:  "请为该账号授予该库的读写权限，或换用有权限的账号。",
			}
		}
		return &connectFailure{
			Message: fmt.Sprintf("MySQL 报错（%d）：%s", mysqlErr.Number, mysqlErr.Message),
			Advice:  "请根据 MySQL 报错定位（通常是账号权限、库不存在或字符集问题）。",
		}
	}

	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return &connectFailure{
			Message: fmt.Sprintf("无法连接到 %s（网络不通、端口错误或 MySQL 未启动）", target),
			Advice:  "请确认 MySQL 已启动、端口正确、防火墙放行，并且 host 从本机可达。",
		}
	}

	return &connectFailure{
		Message: "连接失败：" + err.Error(),
		Advice:  "请检查主机、端口、账号与密码是否正确。",
	}
}

// isUnknownDatabase 判断是否为「数据库不存在」（MySQL 1049）。
func isUnknownDatabase(err error) bool {
	var mysqlErr *gomysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == mysqlErrUnknownDatabase
	}
	return false
}

// isAccessDeniedForDatabase 判断是否为「对该库无权限」（MySQL 1044）。
func isAccessDeniedForDatabase(err error) bool {
	var mysqlErr *gomysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == mysqlErrDBAccesDenied
	}
	return false
}

// databaseTableCount 统计目标库的表数量（复用已有句柄，安装状态展示用）。
func databaseTableCount(ctx context.Context, gdb *gorm.DB, database string) (int64, error) {
	var count int64
	err := gdb.WithContext(ctx).Raw(
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ?", database,
	).Scan(&count).Error
	return count, err
}
