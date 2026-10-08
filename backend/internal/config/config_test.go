package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultIsValidAndComplete(t *testing.T) {
	cfg := Default()

	if cfg.Server.Addr != DefaultAddr {
		t.Errorf("Default() 监听地址 = %q, 期望 %q", cfg.Server.Addr, DefaultAddr)
	}
	if cfg.Server.Mode != DefaultMode {
		t.Errorf("Default() 运行模式 = %q, 期望 %q", cfg.Server.Mode, DefaultMode)
	}
	if cfg.Database.DSN != DefaultDSN {
		t.Errorf("Default() DSN = %q, 期望 %q", cfg.Database.DSN, DefaultDSN)
	}
	if cfg.Database.MaxOpenConns != DefaultMaxOpenConns || cfg.Database.MaxIdleConns != DefaultMaxIdleConns {
		t.Errorf("Default() 连接池 = (%d, %d), 期望 (%d, %d)",
			cfg.Database.MaxOpenConns, cfg.Database.MaxIdleConns, DefaultMaxOpenConns, DefaultMaxIdleConns)
	}
	if cfg.Log.Level != DefaultLogLevel || cfg.Log.Format != DefaultLogFormat {
		t.Errorf("Default() 日志 = (%q, %q), 期望 (%q, %q)",
			cfg.Log.Level, cfg.Log.Format, DefaultLogLevel, DefaultLogFormat)
	}
	if cfg.SourcePath != "" {
		t.Errorf("Default() SourcePath = %q, 期望空字符串", cfg.SourcePath)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Default().Validate() = %v, 期望 nil", err)
	}
}

func TestLoadFallsBackToDefaultsWhenConfigFileMissing(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(EnvConfigPath, "")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\") 返回错误: %v", err)
	}
	if cfg != Default() {
		t.Errorf("Load(\"\") = %+v, 期望缺省配置 %+v", cfg, Default())
	}
}

func TestLoadAppliesOverridesAndKeepsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
server:
  addr: "0.0.0.0:9090"
database:
  dsn: "lyidc:secret@tcp(127.0.0.1:3307)/lyidc_test?charset=utf8mb4"
  max_idle_conns: 3
  conn_max_lifetime: 30m
log:
  level: "debug"
  format: "json"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写入临时配置失败: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(%q) 返回错误: %v", path, err)
	}

	if cfg.SourcePath != path {
		t.Errorf("SourcePath = %q, 期望 %q", cfg.SourcePath, path)
	}
	if cfg.Server.Addr != "0.0.0.0:9090" {
		t.Errorf("server.addr = %q, 期望覆盖为 0.0.0.0:9090", cfg.Server.Addr)
	}
	if cfg.Server.Mode != DefaultMode {
		t.Errorf("server.mode = %q, 期望保留缺省值 %q", cfg.Server.Mode, DefaultMode)
	}
	if cfg.Database.DSN != "lyidc:secret@tcp(127.0.0.1:3307)/lyidc_test?charset=utf8mb4" {
		t.Errorf("database.dsn 未按文件覆盖: %q", cfg.Database.DSN)
	}
	if cfg.Database.MaxOpenConns != DefaultMaxOpenConns {
		t.Errorf("database.max_open_conns = %d, 期望保留缺省值 %d", cfg.Database.MaxOpenConns, DefaultMaxOpenConns)
	}
	if cfg.Database.MaxIdleConns != 3 {
		t.Errorf("database.max_idle_conns = %d, 期望 3", cfg.Database.MaxIdleConns)
	}
	if cfg.Database.ConnMaxLifetime != 30*time.Minute {
		t.Errorf("database.conn_max_lifetime = %s, 期望 30m", cfg.Database.ConnMaxLifetime)
	}
	if cfg.Log.Level != "debug" || cfg.Log.Format != "json" {
		t.Errorf("log = (%q, %q), 期望 (debug, json)", cfg.Log.Level, cfg.Log.Format)
	}
	if got := cfg.Log.SlogLevel(); got != slog.LevelDebug {
		t.Errorf("Log.SlogLevel() = %v, 期望 %v", got, slog.LevelDebug)
	}
}

func TestLoadRejectsUnavailableExplicitPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-exists.yaml")

	if _, err := Load(path); err == nil {
		t.Fatalf("Load(%q) 期望返回错误，实际为 nil", path)
	}
}

func TestLoadRejectsInvalidConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("log:\n  level: \"verbose\"\n"), 0o600); err != nil {
		t.Fatalf("写入临时配置失败: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() 期望返回错误，实际为 nil")
	}
	if !strings.Contains(err.Error(), "log.level") {
		t.Errorf("错误信息未包含字段名: %v", err)
	}
}

func TestResolvePathPrefersEnvThenSearchPaths(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "env.yaml")
	if err := os.WriteFile(envPath, []byte("log:\n  level: \"warn\"\n"), 0o600); err != nil {
		t.Fatalf("写入临时配置失败: %v", err)
	}
	t.Setenv(EnvConfigPath, envPath)

	got, err := ResolvePath("")
	if err != nil {
		t.Fatalf("ResolvePath(\"\") 返回错误: %v", err)
	}
	if got != envPath {
		t.Errorf("ResolvePath(\"\") = %q, 期望环境变量路径 %q", got, envPath)
	}

	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("写入临时配置失败: %v", err)
	}
	t.Setenv(EnvConfigPath, "")
	got, err = ResolvePath("")
	if err != nil {
		t.Fatalf("ResolvePath(\"\") 返回错误: %v", err)
	}
	if got != "config.yaml" {
		t.Errorf("ResolvePath(\"\") = %q, 期望 \"config.yaml\"", got)
	}
}

func TestValidateReportsAllProblems(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{
			name:   "监听地址非法",
			mutate: func(c *Config) { c.Server.Addr = "127.0.0.1" },
			want:   "server.addr",
		},
		{
			name:   "运行模式非法",
			mutate: func(c *Config) { c.Server.Mode = "production" },
			want:   "server.mode",
		},
		{
			name:   "DSN 为空",
			mutate: func(c *Config) { c.Database.DSN = "  " },
			want:   "database.dsn",
		},
		{
			name:   "空闲连接数大于最大连接数",
			mutate: func(c *Config) { c.Database.MaxIdleConns = c.Database.MaxOpenConns + 1 },
			want:   "max_idle_conns",
		},
		{
			name:   "连接存活时间为负",
			mutate: func(c *Config) { c.Database.ConnMaxLifetime = -time.Minute },
			want:   "conn_max_lifetime",
		},
		{
			name:   "日志级别非法",
			mutate: func(c *Config) { c.Log.Level = "trace" },
			want:   "log.level",
		},
		{
			name:   "日志格式非法",
			mutate: func(c *Config) { c.Log.Format = "xml" },
			want:   "log.format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tt.mutate(&cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatalf("Validate() 期望返回错误，实际为 nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Validate() 错误 %v 未包含 %q", err, tt.want)
			}
		})
	}
}

func TestValidateAggregatesMultipleProblems(t *testing.T) {
	cfg := Default()
	cfg.Server.Addr = "bad"
	cfg.Log.Level = "trace"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() 期望返回错误，实际为 nil")
	}
	for _, want := range []string{"server.addr", "log.level"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("聚合错误 %v 未包含 %q", err, want)
		}
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		value   string
		want    slog.Level
		wantErr bool
	}{
		{value: "debug", want: slog.LevelDebug},
		{value: "INFO", want: slog.LevelInfo},
		{value: "warn", want: slog.LevelWarn},
		{value: "warning", want: slog.LevelWarn},
		{value: "error", want: slog.LevelError},
		{value: "verbose", want: slog.LevelInfo, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got, err := ParseLogLevel(tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseLogLevel(%q) 错误 = %v, 期望错误=%t", tt.value, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseLogLevel(%q) = %v, 期望 %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestSlogLevelFallsBackToInfo(t *testing.T) {
	if got := (LogConfig{Level: "nonsense"}).SlogLevel(); got != slog.LevelInfo {
		t.Errorf("非法级别应回退到 info，实际 %v", got)
	}
}

func TestDefaultJWTConfigIsDevelopmentDefault(t *testing.T) {
	cfg := Default()

	if cfg.JWT.Secret != DefaultJWTSecret {
		t.Errorf("jwt.secret 缺省值 = %q, 期望 %q", cfg.JWT.Secret, DefaultJWTSecret)
	}
	if cfg.JWT.ExpireHours != DefaultJWTExpireHours {
		t.Errorf("jwt.expire_hours 缺省值 = %d, 期望 %d", cfg.JWT.ExpireHours, DefaultJWTExpireHours)
	}
	if DefaultJWTExpireHours != 24*7 {
		t.Errorf("缺省有效期 = %d 小时, 期望 168（7 天）", DefaultJWTExpireHours)
	}
	if !cfg.JWT.UsesDefaultSecret() {
		t.Error("UsesDefaultSecret() 对缺省密钥返回 false")
	}
}

func TestLoadAppliesJWTConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
jwt:
  secret: "production-secret-please-change"
  expire_hours: 24
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写入临时配置失败: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(%q) 返回错误: %v", path, err)
	}
	if cfg.JWT.Secret != "production-secret-please-change" {
		t.Errorf("jwt.secret = %q, 期望按文件覆盖", cfg.JWT.Secret)
	}
	if cfg.JWT.ExpireHours != 24 {
		t.Errorf("jwt.expire_hours = %d, 期望 24", cfg.JWT.ExpireHours)
	}
	if cfg.JWT.UsesDefaultSecret() {
		t.Error("自定义密钥被判定为默认密钥")
	}
}

func TestValidateRejectsInvalidJWTConfig(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{
			name:   "密钥为空",
			mutate: func(c *Config) { c.JWT.Secret = "   " },
			want:   "jwt.secret",
		},
		{
			name:   "有效期为 0",
			mutate: func(c *Config) { c.JWT.ExpireHours = 0 },
			want:   "jwt.expire_hours",
		},
		{
			name:   "有效期为负",
			mutate: func(c *Config) { c.JWT.ExpireHours = -1 },
			want:   "jwt.expire_hours",
		},
		{
			name:   "有效期超过上限",
			mutate: func(c *Config) { c.JWT.ExpireHours = MaxJWTExpireHours + 1 },
			want:   "jwt.expire_hours",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tt.mutate(&cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatalf("Validate() 期望返回错误，实际为 nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Validate() 错误 %v 未包含 %q", err, tt.want)
			}
		})
	}
}

func TestMaskDSNHidesPassword(t *testing.T) {
	masked := MaskDSN(DefaultDSN)

	if strings.Contains(masked, "lyidc123") {
		t.Errorf("MaskDSN() 未隐藏密码: %q", masked)
	}
	if !strings.Contains(masked, "@tcp(127.0.0.1:3306)/lyidc") || !strings.HasPrefix(masked, "root") {
		t.Errorf("MaskDSN() 丢失连接信息: %q", masked)
	}
	if got := MaskDSN("not-a-dsn"); got != "<无法解析的 DSN>" {
		t.Errorf("非法 DSN 的掩码结果 = %q", got)
	}
}
