package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// writeTempConfig 在临时目录写一个配置文件并返回其路径。
func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写入测试配置文件失败: %v", err)
	}
	return path
}

func TestSaveMergedCreatesNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")

	written, err := SaveMerged(path, SaveInput{
		DSN:       "root:pa&ss@tcp(127.0.0.1:3306)/demo?charset=utf8mb4&parseTime=true&multiStatements=true",
		JWTSecret: "install-generated-secret",
	})
	if err != nil {
		t.Fatalf("SaveMerged 失败: %v", err)
	}
	if filepath.Clean(written) != filepath.Clean(path) {
		t.Fatalf("写入路径 = %q, 期望 %q", written, path)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("写出的配置文件无法加载: %v", err)
	}
	if cfg.Database.DSN != "root:pa&ss@tcp(127.0.0.1:3306)/demo?charset=utf8mb4&parseTime=true&multiStatements=true" {
		t.Errorf("database.dsn = %q 未正确写入", cfg.Database.DSN)
	}
	if cfg.JWT.Secret != "install-generated-secret" {
		t.Errorf("jwt.secret = %q 未正确写入", cfg.JWT.Secret)
	}
	if !cfg.DatabaseConfigured {
		t.Error("DatabaseConfigured = false，期望 true（已显式写入 dsn）")
	}
	if cfg.Server.Addr != DefaultAddr || cfg.Log.Level != DefaultLogLevel {
		t.Errorf("缺省节未补齐: addr=%q level=%q", cfg.Server.Addr, cfg.Log.Level)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取写出的配置文件失败: %v", err)
	}
	if !strings.Contains(string(raw), "# Lyidc_OEM 后端配置") {
		t.Errorf("新建文件缺少文件头注释:\n%s", raw)
	}
	for _, section := range []string{"server:", "database:", "jwt:", "log:"} {
		if !strings.Contains(string(raw), section) {
			t.Errorf("新建文件缺少节 %s:\n%s", section, raw)
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat 失败: %v", err)
	}
	// Windows 只有只读位可表达，权限断言仅在有 POSIX 语义的平台上有意义。
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("文件权限 = %o, 期望 600", info.Mode().Perm())
	}
}

func TestSaveMergedPreservesExistingKeysAndComments(t *testing.T) {
	path := writeTempConfig(t, `# 本地开发配置（保留这段注释）。
server:
  addr: "0.0.0.0:9000"   # 监听全部网卡
  mode: "debug"

database:
  dsn: "old:old@tcp(127.0.0.1:3306)/olddb"
  max_open_conns: 10

jwt:
  secret: "lyidc-dev-secret-change-me"
  expire_hours: 24
`)

	if _, err := SaveMerged(path, SaveInput{
		DSN:       "new:new@tcp(127.0.0.1:3307)/newdb",
		JWTSecret: "freshly-generated",
	}); err != nil {
		t.Fatalf("SaveMerged 失败: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取配置文件失败: %v", err)
	}
	content := string(raw)

	for _, want := range []string{
		"# 本地开发配置（保留这段注释）。",
		"# 监听全部网卡",
		`max_open_conns: 10`,
		`new:new@tcp(127.0.0.1:3307)/newdb`,
		"freshly-generated",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("合并后的文件缺少 %q:\n%s", want, content)
		}
	}
	for _, unwanted := range []string{"olddb", "lyidc-dev-secret-change-me"} {
		if strings.Contains(content, unwanted) {
			t.Errorf("合并后的文件仍包含旧值 %q:\n%s", unwanted, content)
		}
	}

	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("合并后的文件不是合法 YAML: %v", err)
	}
	server, ok := doc["server"].(map[string]any)
	if !ok {
		t.Fatalf("server 节不是映射: %#v", doc["server"])
	}
	if server["addr"] != "0.0.0.0:9000" || server["mode"] != "debug" {
		t.Errorf("server 节既有取值被覆盖: %#v", server)
	}
	if _, ok := doc["log"]; !ok {
		t.Error("缺失的 log 节未被补齐")
	}
}

func TestSaveMergedDropsNothingWhenOnlySecretChanges(t *testing.T) {
	path := writeTempConfig(t, "database:\n  dsn: \"root:pw@tcp(127.0.0.1:3306)/db\"\n")

	if _, err := SaveMerged(path, SaveInput{JWTSecret: "only-secret"}); err != nil {
		t.Fatalf("SaveMerged 失败: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("重新加载失败: %v", err)
	}
	if cfg.Database.DSN != "root:pw@tcp(127.0.0.1:3306)/db" {
		t.Errorf("只改密钥时 dsn 被改动: %q", cfg.Database.DSN)
	}
	if cfg.JWT.Secret != "only-secret" {
		t.Errorf("jwt.secret = %q, 期望 only-secret", cfg.JWT.Secret)
	}
}

func TestSaveMergedRejectsEmptyInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := SaveMerged(path, SaveInput{}); err == nil {
		t.Fatal("空变更应返回错误")
	}
}

func TestSaveMergedRejectsBrokenYAML(t *testing.T) {
	path := writeTempConfig(t, "server: [未闭合\n")

	if _, err := SaveMerged(path, SaveInput{DSN: "x"}); err == nil {
		t.Fatal("既有文件语法错误时应返回错误（不覆盖用户文件）")
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "未闭合") {
		t.Error("语法错误的既有文件被覆盖")
	}
}

func TestResolveWritePathDefaultsToRunDir(t *testing.T) {
	abs, err := ResolveWritePath("")
	if err != nil {
		t.Fatalf("ResolveWritePath 失败: %v", err)
	}
	if !filepath.IsAbs(abs) || filepath.Base(abs) != "config.yaml" {
		t.Errorf("ResolveWritePath(\"\") = %q", abs)
	}
}

func TestCheckWritable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := CheckWritable(path); err != nil {
		t.Fatalf("新文件路径应可写: %v", err)
	}

	if err := os.WriteFile(path, []byte("server: {}\n"), 0o600); err != nil {
		t.Fatalf("准备测试文件失败: %v", err)
	}
	if err := CheckWritable(path); err != nil {
		t.Fatalf("既有文件应可写: %v", err)
	}

	if err := CheckWritable(filepath.Join(path, "子路径.yaml")); err == nil {
		t.Error("以文件为目录的路径应判定不可写")
	}
}
