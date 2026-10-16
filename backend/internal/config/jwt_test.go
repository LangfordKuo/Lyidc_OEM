package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// installedConfigYAML 是一份「已安装实例」的真实配置文件样本：键齐全、带注释、含默认弱密钥。
// 轮换写回后除 jwt.secret 外一切（键、值、顺序、注释）都必须逐字保持不变。
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

// writeInstalledConfig 在临时目录写一份「已安装实例」配置文件并返回路径。
func writeInstalledConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(installedConfigYAML), 0o600); err != nil {
		t.Fatalf("写入测试配置文件失败: %v", err)
	}
	return path
}

// readFileString 读取文件内容（断言用）。
func readFileString(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	return string(raw)
}

// nonEmptyLines 返回去掉空行后的行列表（yaml 往返不保留空行，逐行比较时须排除）。
func nonEmptyLines(raw string) []string {
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// yamlMap 把 YAML 文本解析成通用映射（深比较用）。
func yamlMap(t *testing.T, raw string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("解析 YAML 失败: %v\n%s", err, raw)
	}
	return doc
}

// TestIsWeakSecretMatrix 锁定弱密钥判定矩阵（契约 25.1）。
func TestIsWeakSecretMatrix(t *testing.T) {
	strong := strings.Repeat("a", MinJWTSecretLength)
	strong43 := "Zm9vYmFyYmF6cXV4MTIzNDU2Nzg5MGFiY2RlZmdoaWo" // 43 字符 base64url

	cases := []struct {
		name   string
		secret string
		weak   bool
	}{
		{"空串", "", true},
		{"纯空白", "   ", true},
		{"开发默认值", DefaultJWTSecret, true},
		{"默认值带首尾空白", "  " + DefaultJWTSecret + "  ", true},
		{"31 字符", strings.Repeat("a", MinJWTSecretLength-1), true},
		{"32 字符", strong, false},
		{"32 字符带首尾空白", "  " + strong + "  ", false},
		{"43 字符强密钥（安装向导入口径）", strong43, false},
		{"弱值但含首尾空白的 31 字符", " " + strings.Repeat("x", MinJWTSecretLength-1) + " ", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := (JWTConfig{Secret: tc.secret}).IsWeakSecret(); got != tc.weak {
				t.Errorf("IsWeakSecret(%q, %d 字符) = %t, 期望 %t",
					tc.secret, len([]rune(tc.secret)), got, tc.weak)
			}
		})
	}

	// 长度按「字符数」而非字节数计：11 个汉字 = 33 字节仍判为弱值（口径写进注释，别改成 len）。
	if !(JWTConfig{Secret: strings.Repeat("密", 11)}).IsWeakSecret() {
		t.Error("11 个汉字（33 字节）应按字符数判为弱密钥")
	}
	if (JWTConfig{Secret: strings.Repeat("密", MinJWTSecretLength)}).IsWeakSecret() {
		t.Errorf("%d 个汉字应按字符数判为强密钥", MinJWTSecretLength)
	}

	// 缺省配置必须被判为弱值（生产缺省即高危）。
	if !Default().JWT.IsWeakSecret() {
		t.Error("缺省 jwt.secret 应判为弱密钥")
	}
}

// TestGenerateJWTSecretShape 锁定共用密钥生成器的形状（安装向导与轮换同口径）。
func TestGenerateJWTSecretShape(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 16; i++ {
		secret, err := GenerateJWTSecret()
		if err != nil {
			t.Fatalf("生成密钥失败: %v", err)
		}
		if len(secret) != 43 {
			t.Fatalf("密钥长度 = %d, 期望 43（32 字节 base64url）", len(secret))
		}
		if (JWTConfig{Secret: secret}).IsWeakSecret() {
			t.Fatalf("生成的密钥被判为弱密钥: %q", secret)
		}
		if seen[secret] {
			t.Fatal("生成的密钥出现重复")
		}
		seen[secret] = true
	}
}

// TestRotateWeakJWTSecretWritesOnlySecret 覆盖「已安装 + 弱密钥 → 写回临时 config.yaml」：
// 文件中 secret 已变、除 secret 外解析结果与原文一致、再跑一次不再轮换（幂等）。
func TestRotateWeakJWTSecretWritesOnlySecret(t *testing.T) {
	path := writeInstalledConfig(t)
	before := readFileString(t, path)

	rotation := RotateWeakJWTSecret(JWTConfig{Secret: DefaultJWTSecret, ExpireHours: DefaultJWTExpireHours}, path)
	if !rotation.Rotated || !rotation.Written {
		t.Fatalf("期望轮换并写回，实际 rotated=%t written=%t err=%v",
			rotation.Rotated, rotation.Written, rotation.WriteErr)
	}
	if rotation.Secret == DefaultJWTSecret || (JWTConfig{Secret: rotation.Secret}).IsWeakSecret() {
		t.Fatal("轮换后的密钥仍是弱值")
	}

	after := readFileString(t, path)
	if after == before {
		t.Fatal("配置文件内容未发生变化")
	}
	if strings.Contains(after, DefaultJWTSecret) {
		t.Fatal("写回后文件里仍残留开发默认密钥")
	}

	// 解析后深比较：除 jwt.secret 外必须逐键相等（键、值、顺序、注释风格不变）。
	beforeDoc := yamlMap(t, before)
	afterDoc := yamlMap(t, after)
	beforeJWT := beforeDoc["jwt"].(map[string]any)
	afterJWT := afterDoc["jwt"].(map[string]any)
	beforeJWT["secret"] = "<normalized>"
	afterJWT["secret"] = "<normalized>"
	if !reflect.DeepEqual(beforeDoc, afterDoc) {
		t.Errorf("写回前后除 jwt.secret 外还有其它差异:\nbefore=%#v\nafter=%#v", beforeDoc, afterDoc)
	}

	// 逐行比较（空行除外——yaml.v3 往返本就不保留空行，这是 SaveMerged 既有行为）：
	// 允许且仅允许 secret 那一行不同，其余每一行都逐字节不变。
	beforeLines := nonEmptyLines(before)
	afterLines := nonEmptyLines(after)
	if len(beforeLines) != len(afterLines) {
		t.Fatalf("写回前后非空行数不同: %d → %d", len(beforeLines), len(afterLines))
	}
	diffLines := 0
	for i := range beforeLines {
		if beforeLines[i] != afterLines[i] {
			diffLines++
			if !strings.Contains(beforeLines[i], "secret:") {
				t.Errorf("第 %d 行被改动且不是 secret 行:\n- %s\n+ %s", i+1, beforeLines[i], afterLines[i])
			}
		}
	}
	if diffLines != 1 {
		t.Errorf("改动行数 = %d, 期望恰好 1 行（只有 jwt.secret）", diffLines)
	}

	// 原文里的每一条注释都必须原样保留。
	for _, line := range beforeLines {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if !strings.Contains(after, line) {
			t.Errorf("注释丢失: %q", line)
		}
	}

	// 写回的内容能被正常加载，且拿到的是新密钥。
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("写回后的配置无法加载: %v", err)
	}
	if loaded.JWT.Secret != rotation.Secret {
		t.Errorf("落盘密钥 = %q, 期望本次生成的新密钥", loaded.JWT.Secret)
	}
	if loaded.JWT.ExpireHours != DefaultJWTExpireHours || loaded.Database.DSN == "" {
		t.Errorf("写回后其它配置项被破坏: %+v", loaded)
	}

	// 幂等：第二次启动（密钥已是强值）不再轮换、文件逐字节不变。
	second := RotateWeakJWTSecret(loaded.JWT, path)
	if second.Rotated || second.Written {
		t.Errorf("第二次调用不应再轮换: rotated=%t written=%t", second.Rotated, second.Written)
	}
	if second.Secret != loaded.JWT.Secret {
		t.Errorf("未轮换时应原样返回现有密钥")
	}
	if readFileString(t, path) != after {
		t.Error("第二次调用改动了配置文件（幂等被破坏）")
	}
}

// TestRotateWeakJWTSecretSkipsStrongSecret 覆盖「强密钥 → 完全不碰文件」。
func TestRotateWeakJWTSecretSkipsStrongSecret(t *testing.T) {
	strong := "u7Fq2mZx8KdW1sVb4nPc6tRy9LaJ3hGe5wQz0iXoBkN"
	path := writeInstalledConfig(t)
	doc := strings.Replace(installedConfigYAML, DefaultJWTSecret, strong, 1)
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatalf("写入测试配置文件失败: %v", err)
	}

	rotation := RotateWeakJWTSecret(JWTConfig{Secret: strong, ExpireHours: DefaultJWTExpireHours}, path)
	if rotation.Rotated || rotation.Written || rotation.WriteErr != nil {
		t.Fatalf("强密钥不应触发轮换: %+v", rotation)
	}
	if rotation.Secret != strong {
		t.Errorf("未轮换时应原样返回现有密钥")
	}
	if readFileString(t, path) != doc {
		t.Error("强密钥场景下配置文件被改动")
	}
}

// TestRotateWeakJWTSecretUnwritableFallsBackToMemory 覆盖「写回失败 → 内存兜底且不报错」：
// 父目录不可写（用一个普通文件当目录）时仍返回强密钥供本次进程使用，只是不落盘。
func TestRotateWeakJWTSecretUnwritableFallsBackToMemory(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "not-a-dir.yaml")
	if err := os.WriteFile(blocked, []byte("server: {}\n"), 0o600); err != nil {
		t.Fatalf("准备阻塞文件失败: %v", err)
	}
	path := filepath.Join(blocked, "config.yaml")

	rotation := RotateWeakJWTSecret(JWTConfig{Secret: DefaultJWTSecret, ExpireHours: DefaultJWTExpireHours}, path)
	if !rotation.Rotated {
		t.Fatal("不可写时仍必须生成新密钥（仅本次进程有效）")
	}
	if rotation.Written {
		t.Fatal("父目录不可写时不应报告写回成功")
	}
	if rotation.WriteErr == nil {
		t.Fatal("写回失败必须带回原因（供日志给出人工指引）")
	}
	if (JWTConfig{Secret: rotation.Secret}).IsWeakSecret() {
		t.Fatal("内存兜底的密钥仍是弱值")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("不可写路径不应被创建: %v", err)
	}
}
