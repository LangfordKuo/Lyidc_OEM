package install

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
)

// installedConfigYAML 是一份「已安装实例」的真实配置文件样本（键齐全 + 注释 + 默认弱密钥）。
const installedConfigYAML = `# Lyidc_OEM 后端配置（部署机上的真实文件，含密码与密钥）。
server:
  # HTTP 服务监听地址 host:port
  addr: "0.0.0.0:8080"
  mode: "release"

database:
  # MySQL 5.7 连接串
  dsn: "root:lyidc123@tcp(127.0.0.1:3306)/lyidc?charset=utf8mb4&parseTime=true&loc=Local&multiStatements=true"
  max_open_conns: 25
  max_idle_conns: 5
  conn_max_lifetime: "1h"

jwt:
  # 签名密钥：泄露即可伪造任意账号的 token
  secret: "lyidc-dev-secret-change-me"
  expire_hours: 168

log:
  # 日志级别：debug | info | warn | error
  level: "info"
  format: "text"
`

// jwtCapturingSupervisor 构造一个把 BuildEngine 收到的 JWT 配置记下来的 Supervisor；
// 日志写进返回的缓冲区，供断言「日志说明已轮换、但绝不出现密钥」。
func jwtCapturingSupervisor(t *testing.T, cfg config.Config, configPath string) (*Supervisor, *bytes.Buffer, *config.JWTConfig) {
	t.Helper()
	logs := &bytes.Buffer{}
	captured := &config.JWTConfig{}
	supervisor, err := NewSupervisor(Options{
		Logger:     slog.New(slog.NewTextHandler(logs, nil)),
		Config:     cfg,
		ConfigPath: configPath,
		BuildEngine: func(_ *gorm.DB, jwt config.JWTConfig) http.Handler {
			*captured = jwt
			return stubEngine("normal-engine")
		},
		ProbeTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("构造 Supervisor 失败: %v", err)
	}
	t.Cleanup(supervisor.Shutdown)
	return supervisor, logs, captured
}

// enterInstalled 白盒触发「进入已安装态」（等价于探测到 installed 标记后的那一步）。
func enterInstalled(supervisor *Supervisor) {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	supervisor.enterInstalledLocked(settings.Installed{
		At: "2026-10-16T00:00:00Z", Version: settings.MarkerVersion, Source: settings.SourceAuto,
	})
}

// writeInstalledConfigFile 写一份「已安装实例」配置文件并返回路径。
func writeInstalledConfigFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写入测试配置文件失败: %v", err)
	}
	return path
}

// readConfigFile 读取配置文件内容。
func readConfigFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	return string(raw)
}

// assertNoSecretLeak 断言日志里不含给定密钥本身、也不含它的任何 6 字符片段。
func assertNoSecretLeak(t *testing.T, logs, secret string) {
	t.Helper()
	if secret == "" {
		t.Fatal("密钥为空，无法做日志泄露断言")
	}
	if strings.Contains(logs, secret) {
		t.Fatal("日志里出现了完整密钥")
	}
	for i := 0; i+6 <= len(secret); i++ {
		if strings.Contains(logs, secret[i:i+6]) {
			t.Fatalf("日志里出现了密钥的片段 %q", secret[i:i+6])
		}
	}
}

// assertNoWeakSecretEchoed 断言日志里没有回显开发默认弱密钥本身。
func assertNoWeakSecretEchoed(t *testing.T, logs string) {
	t.Helper()
	if strings.Contains(logs, config.DefaultJWTSecret) {
		t.Error("日志里回显了开发默认密钥")
	}
}

// TestEnterInstalledRotatesWeakSecretBeforeEngine 覆盖「已安装 + 弱密钥 → 自动轮换并写回」，
// 且轮换发生在构造（快照 JWT 的）令牌服务之前（契约 25.1）。
func TestEnterInstalledRotatesWeakSecretBeforeEngine(t *testing.T) {
	path := writeInstalledConfigFile(t, installedConfigYAML)
	cfg := config.Default()
	cfg.DatabaseConfigured = true
	cfg.SourcePath = path

	supervisor, logs, captured := jwtCapturingSupervisor(t, cfg, path)
	enterInstalled(supervisor)

	if !supervisor.IsInstalled() {
		t.Fatal("enterInstalledLocked 后应处于已安装态")
	}
	if supervisor.engine == nil {
		t.Fatal("已安装态必须构建正常模式引擎")
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("写回后的配置无法加载: %v", err)
	}
	if loaded.JWT.Secret == config.DefaultJWTSecret {
		t.Fatal("配置文件里仍是开发默认密钥")
	}
	if loaded.JWT.IsWeakSecret() {
		t.Fatalf("写回后的密钥仍是弱值: %q", loaded.JWT.Secret)
	}
	if len([]rune(loaded.JWT.Secret)) != 43 {
		t.Errorf("轮换后的密钥长度 = %d, 期望 43（与安装向导入同一实现）", len([]rune(loaded.JWT.Secret)))
	}

	// 关键顺序断言：引擎拿到的必须是轮换后的密钥（证明轮换发生在构造引擎之前）。
	if captured.Secret != loaded.JWT.Secret {
		t.Fatalf("引擎收到的密钥与落盘密钥不一致（轮换可能发生在构造引擎之后）: %q vs %q",
			captured.Secret, loaded.JWT.Secret)
	}

	// 只改 jwt.secret：其它配置项一律与原文一致。
	if loaded.Server.Addr != "0.0.0.0:8080" || loaded.Server.Mode != "release" ||
		loaded.Database.DSN != "root:lyidc123@tcp(127.0.0.1:3306)/lyidc?charset=utf8mb4&parseTime=true&loc=Local&multiStatements=true" ||
		loaded.Database.MaxOpenConns != 25 || loaded.Database.MaxIdleConns != 5 ||
		loaded.JWT.ExpireHours != 168 || loaded.Log.Level != "info" || loaded.Log.Format != "text" {
		t.Errorf("轮换破坏了其它配置项: %+v", loaded)
	}

	// 日志：说明已轮换，且绝不含密钥本身或片段。
	logText := logs.String()
	if !strings.Contains(logText, "已自动轮换为随机密钥并写回配置文件") {
		t.Errorf("缺少轮换说明日志:\n%s", logText)
	}
	assertNoSecretLeak(t, logText, loaded.JWT.Secret)
	assertNoWeakSecretEchoed(t, logText)
}

// TestSecondStartupDoesNotRotateAgain 覆盖幂等：密钥已是强值时，第二次启动不再轮换、不碰文件。
func TestSecondStartupDoesNotRotateAgain(t *testing.T) {
	path := writeInstalledConfigFile(t, installedConfigYAML)
	cfg := config.Default()
	cfg.DatabaseConfigured = true
	cfg.SourcePath = path

	first, _, _ := jwtCapturingSupervisor(t, cfg, path)
	enterInstalled(first)
	rotated := readConfigFile(t, path)

	// 第二次启动：新进程读同一份（已轮换的）配置文件。
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("加载已轮换的配置失败: %v", err)
	}
	cfg2 := config.Default()
	cfg2.DatabaseConfigured = true
	cfg2.SourcePath = path
	cfg2.JWT = loaded.JWT

	second, logs, captured := jwtCapturingSupervisor(t, cfg2, path)
	enterInstalled(second)

	if readConfigFile(t, path) != rotated {
		t.Error("第二次启动改动了配置文件（幂等被破坏）")
	}
	if captured.Secret != loaded.JWT.Secret {
		t.Errorf("第二次启动后引擎的密钥被改动了: %q", captured.Secret)
	}
	if strings.Contains(logs.String(), "已自动轮换") {
		t.Errorf("第二次启动不应再触发轮换:\n%s", logs.String())
	}
}

// TestEnterInstalledFallsBackToMemoryWhenConfigUnwritable 覆盖降级路径：
// 写回失败时不阻止启动，改用仅本次进程有效的随机密钥，并在日志里给出人工处置指引。
func TestEnterInstalledFallsBackToMemoryWhenConfigUnwritable(t *testing.T) {
	// 用一个普通文件当目录，制造「父目录不可写」（Windows / Linux 表现一致）。
	blocked := filepath.Join(t.TempDir(), "not-a-dir.yaml")
	if err := os.WriteFile(blocked, []byte("server: {}\n"), 0o600); err != nil {
		t.Fatalf("准备阻塞文件失败: %v", err)
	}
	path := filepath.Join(blocked, "config.yaml")

	cfg := config.Default()
	cfg.DatabaseConfigured = true
	supervisor, logs, captured := jwtCapturingSupervisor(t, cfg, path)
	enterInstalled(supervisor)

	if !supervisor.IsInstalled() || supervisor.engine == nil {
		t.Fatal("写回失败不得阻止启动：状态与引擎都应就绪")
	}
	if (config.JWTConfig{Secret: captured.Secret}).IsWeakSecret() {
		t.Fatalf("内存兜底的密钥仍是弱值: %q", captured.Secret)
	}
	if len([]rune(captured.Secret)) != 43 {
		t.Errorf("内存兜底密钥长度 = %d, 期望 43", len([]rune(captured.Secret)))
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("不可写路径不应被创建: %v", err)
	}

	logText := logs.String()
	if !strings.Contains(logText, "仅本次进程有效的随机密钥") {
		t.Errorf("缺少降级说明日志:\n%s", logText)
	}
	if !strings.Contains(logText, "手工把配置文件里的 jwt.secret 改成 32 个字符以上的随机值") {
		t.Errorf("缺少人工处置指引:\n%s", logText)
	}
	assertNoSecretLeak(t, logText, captured.Secret)
	assertNoWeakSecretEchoed(t, logText)
}

// TestNotInstalledKeepsWeakSecretUntouched 覆盖「未安装 + 弱密钥 → 行为保持不变」：
// 不轮换、不写文件、不构造引擎（因此也不可能拒绝启动或改监听地址）。
func TestNotInstalledKeepsWeakSecretUntouched(t *testing.T) {
	path := writeInstalledConfigFile(t, installedConfigYAML)
	cfg := config.Default()
	cfg.DatabaseConfigured = false // 未配置数据库 → 未安装（安装向导模式）
	cfg.SourcePath = path

	supervisor, logs, captured := jwtCapturingSupervisor(t, cfg, path)
	supervisor.Init(context.Background())

	if supervisor.IsInstalled() {
		t.Fatal("未配置数据库时不应判为已安装")
	}
	if *captured != (config.JWTConfig{}) {
		t.Errorf("未安装时不应构建正常模式引擎: %+v", *captured)
	}
	if got := readConfigFile(t, path); got != installedConfigYAML {
		t.Error("未安装时不应改动配置文件")
	}
	if strings.Contains(logs.String(), "已自动轮换") {
		t.Errorf("未安装时不应轮换:\n%s", logs.String())
	}
	// 安装向导照常可用（未被弱密钥判定拒绝）。
	if rec, _ := doRequest(t, supervisor, http.MethodGet, PathStatus, nil, nil); rec.Code != http.StatusOK {
		t.Errorf("未安装时安装 API 应照常可用，实际 %d", rec.Code)
	}
}
