// Package config 负责加载与校验 Lyidc_OEM 后端配置。
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	gomysql "github.com/go-sql-driver/mysql"
	"gopkg.in/yaml.v3"
)

// 缺省值。
const (
	DefaultAddr            = "127.0.0.1:8080"
	DefaultMode            = "release"
	DefaultDSN             = "root:lyidc123@tcp(127.0.0.1:3306)/lyidc?charset=utf8mb4&parseTime=true&loc=Local&multiStatements=true"
	DefaultMaxOpenConns    = 25
	DefaultMaxIdleConns    = 5
	DefaultConnMaxLifetime = time.Hour
	DefaultLogLevel        = "info"
	DefaultLogFormat       = "text"

	// DefaultJWTSecret 是开发默认签名密钥。警告：生产环境必须改成足够随机的强密钥，
	// 否则任何人都能伪造 token（服务启动时会打印告警日志）。
	DefaultJWTSecret = "lyidc-dev-secret-change-me"
	// DefaultJWTExpireHours 是 token 缺省有效期：168 小时 = 7 天。
	DefaultJWTExpireHours = 168
	// MaxJWTExpireHours 是 token 有效期上限（8760 小时 = 1 年），防止误配成超长有效期。
	MaxJWTExpireHours = 8760
)

// EnvConfigPath 是显式指定配置文件路径的环境变量名。
const EnvConfigPath = "LYIDC_CONFIG"

// DefaultSearchPaths 是未显式指定配置文件时的查找顺序（相对当前工作目录）。
var DefaultSearchPaths = []string{"config.yaml", filepath.Join("backend", "config.yaml")}

// Config 是后端完整配置。
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	JWT      JWTConfig      `yaml:"jwt"`
	Log      LogConfig      `yaml:"log"`

	// SourcePath 记录实际加载的配置文件路径，为空表示使用缺省值。
	SourcePath string `yaml:"-"`
}

// JWTConfig 是 JWT 签发与校验配置。
type JWTConfig struct {
	// Secret 是 HS256 签名密钥。缺省为开发默认值 DefaultJWTSecret，生产必须修改。
	Secret string `yaml:"secret"`
	// ExpireHours 是 token 有效期（小时），缺省 168（7 天）。
	ExpireHours int `yaml:"expire_hours"`
}

// UsesDefaultSecret 判断当前是否仍在使用开发默认密钥。
func (j JWTConfig) UsesDefaultSecret() bool {
	return j.Secret == DefaultJWTSecret
}

// ServerConfig 是 HTTP 服务配置。
type ServerConfig struct {
	Addr string `yaml:"addr"`
	Mode string `yaml:"mode"`
}

// DatabaseConfig 是 MySQL 连接配置。
type DatabaseConfig struct {
	DSN             string        `yaml:"dsn"`
	MaxOpenConns    int           `yaml:"max_open_conns"`
	MaxIdleConns    int           `yaml:"max_idle_conns"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime"`
}

// LogConfig 是日志配置。
type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// Default 返回带缺省值的配置。
func Default() Config {
	return Config{
		Server: ServerConfig{
			Addr: DefaultAddr,
			Mode: DefaultMode,
		},
		Database: DatabaseConfig{
			DSN:             DefaultDSN,
			MaxOpenConns:    DefaultMaxOpenConns,
			MaxIdleConns:    DefaultMaxIdleConns,
			ConnMaxLifetime: DefaultConnMaxLifetime,
		},
		JWT: JWTConfig{
			Secret:      DefaultJWTSecret,
			ExpireHours: DefaultJWTExpireHours,
		},
		Log: LogConfig{
			Level:  DefaultLogLevel,
			Format: DefaultLogFormat,
		},
	}
}

// Load 加载配置：显式路径 > 环境变量 LYIDC_CONFIG > 默认查找路径 > 缺省值。
// 未找到任何配置文件时返回缺省配置且不报错。
func Load(explicitPath string) (Config, error) {
	cfg := Default()

	path, err := ResolvePath(explicitPath)
	if err != nil {
		return cfg, err
	}
	if path == "" {
		return cfg, nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("读取配置文件 %s: %w", path, err)
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("解析配置文件 %s: %w", path, err)
	}

	cfg.SourcePath = path
	if err := cfg.Validate(); err != nil {
		return cfg, fmt.Errorf("配置校验失败 (%s): %w", path, err)
	}
	return cfg, nil
}

// ResolvePath 返回实际使用的配置文件路径；返回空字符串表示未找到配置文件。
func ResolvePath(explicitPath string) (string, error) {
	if path := strings.TrimSpace(explicitPath); path != "" {
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("指定的配置文件不可用: %w", err)
		}
		return path, nil
	}

	if path := strings.TrimSpace(os.Getenv(EnvConfigPath)); path != "" {
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("环境变量 %s 指向的配置文件不可用: %w", EnvConfigPath, err)
		}
		return path, nil
	}

	for _, path := range DefaultSearchPaths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", nil
}

// Validate 校验配置取值，返回全部问题的聚合错误。
func (c Config) Validate() error {
	var errs []error

	if _, _, err := net.SplitHostPort(c.Server.Addr); err != nil {
		errs = append(errs, fmt.Errorf("server.addr %q 非法（应为 host:port）: %w", c.Server.Addr, err))
	}
	switch strings.ToLower(c.Server.Mode) {
	case "debug", "release", "test":
	default:
		errs = append(errs, fmt.Errorf("server.mode %q 非法（可选 debug/release/test）", c.Server.Mode))
	}

	if strings.TrimSpace(c.Database.DSN) == "" {
		errs = append(errs, errors.New("database.dsn 不能为空"))
	}
	if c.Database.MaxOpenConns < 0 {
		errs = append(errs, fmt.Errorf("database.max_open_conns 不能为负数: %d", c.Database.MaxOpenConns))
	}
	if c.Database.MaxIdleConns < 0 {
		errs = append(errs, fmt.Errorf("database.max_idle_conns 不能为负数: %d", c.Database.MaxIdleConns))
	}
	if c.Database.MaxOpenConns > 0 && c.Database.MaxIdleConns > c.Database.MaxOpenConns {
		errs = append(errs, fmt.Errorf("database.max_idle_conns (%d) 不能大于 max_open_conns (%d)",
			c.Database.MaxIdleConns, c.Database.MaxOpenConns))
	}
	if c.Database.ConnMaxLifetime < 0 {
		errs = append(errs, fmt.Errorf("database.conn_max_lifetime 不能为负数: %s", c.Database.ConnMaxLifetime))
	}

	if strings.TrimSpace(c.JWT.Secret) == "" {
		errs = append(errs, errors.New("jwt.secret 不能为空（缺省应使用开发默认值，留空即视为误配）"))
	}
	if c.JWT.ExpireHours <= 0 || c.JWT.ExpireHours > MaxJWTExpireHours {
		errs = append(errs, fmt.Errorf("jwt.expire_hours %d 非法（应在 1-%d 之间，单位小时）",
			c.JWT.ExpireHours, MaxJWTExpireHours))
	}

	if _, err := ParseLogLevel(c.Log.Level); err != nil {
		errs = append(errs, err)
	}
	switch strings.ToLower(c.Log.Format) {
	case "text", "json":
	default:
		errs = append(errs, fmt.Errorf("log.format %q 非法（可选 text/json）", c.Log.Format))
	}

	return errors.Join(errs...)
}

// SlogLevel 返回日志级别；解析失败时回退到 info。
func (c LogConfig) SlogLevel() slog.Level {
	level, err := ParseLogLevel(c.Level)
	if err != nil {
		return slog.LevelInfo
	}
	return level
}

// ParseLogLevel 解析日志级别文本。
func ParseLogLevel(value string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("log.level %q 非法（可选 debug/info/warn/error）", value)
	}
}

// MaskDSN 解析 DSN 后抹掉密码，便于安全打印日志。
func MaskDSN(dsn string) string {
	parsed, err := gomysql.ParseDSN(dsn)
	if err != nil {
		return "<无法解析的 DSN>"
	}
	parsed.Passwd = ""
	return parsed.FormatDSN()
}
