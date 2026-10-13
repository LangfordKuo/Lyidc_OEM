package dbbackup

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
)

// testClock 返回固定时钟（文件名里的时间戳可预期；UTC 与本地时区无关）。
func testClock() func() time.Time {
	return func() time.Time { return time.Date(2026, 10, 14, 7, 15, 0, 0, time.UTC) }
}

// discardLogger 返回丢弃全部日志的 logger。
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newDirService 构造只关心目录判定的服务实例（无需数据库：这些用例只走路径与白名单逻辑）。
func newDirService(installDir string) *Service {
	return &Service{
		installDir: installDir,
		webRoot:    filepath.Join(installDir, WebRootName),
		now:        testClock(),
		logger:     discardLogger(),
	}
}

// TestBackupDirDefaultIsOutsideWebRoot 覆盖缺省目录口径（契约 21.4）：
// 缺省 `<安装目录>/backups` 与 `dist/` 同级，不在 Web 根内。
func TestBackupDirDefaultIsOutsideWebRoot(t *testing.T) {
	install := t.TempDir()
	service := newDirService(install)

	state := service.BackupDir(context.Background())
	want := filepath.Join(install, DefaultDirName)
	if state.Dir != want {
		t.Fatalf("缺省备份目录 = %s，期望 %s", state.Dir, want)
	}
	if !state.Default || !state.OutsideWebRoot || state.Message != "" {
		t.Fatalf("缺省目录应判定为可用：%+v", state)
	}
}

// TestBackupDirRejectsInsideWebRoot 覆盖最硬的一条边界（契约 21.4）：
// 解析结果落在 Web 根（dist/）内必须拒绝——备份含管理员密码哈希，绝不能被 Web 服务器读到。
func TestBackupDirRejectsInsideWebRoot(t *testing.T) {
	install := t.TempDir()
	webRoot := filepath.Join(install, WebRootName)
	if err := os.MkdirAll(webRoot, 0o755); err != nil {
		t.Fatalf("建立 Web 根失败: %v", err)
	}
	service := newDirService(install)

	for _, dir := range []string{
		webRoot,
		filepath.Join(webRoot, "static"),
		filepath.Join(webRoot, "..", WebRootName, "nested"),
	} {
		if err := service.checkDir(dir); err == nil {
			t.Fatalf("目录 %s 落在 Web 根内，必须被拒绝", dir)
		} else if !errors.Is(err, ErrDirUnusable) {
			t.Fatalf("目录 %s 的错误应为 ErrDirUnusable，实际 %v", dir, err)
		}
	}

	// 相邻但不在其内（backups/ 与 dist/ 同级）必须放行。
	if err := service.checkDir(filepath.Join(install, DefaultDirName)); err != nil {
		t.Fatalf("与 dist/ 同级的目录应放行：%v", err)
	}
	// 名字以 dist 开头但同级（dist-backup/）也不在 Web 根内。
	if err := service.checkDir(filepath.Join(install, "dist-backup")); err != nil {
		t.Fatalf("dist-backup 不在 Web 根内，应放行：%v", err)
	}
}

// TestBackupDirRejectsSymlinkIntoWebRoot 覆盖用符号链接绕过 Web 根判定的情形。
func TestBackupDirRejectsSymlinkIntoWebRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 下创建符号链接需要额外权限，跳过")
	}
	install := t.TempDir()
	webRoot := filepath.Join(install, WebRootName)
	if err := os.MkdirAll(webRoot, 0o755); err != nil {
		t.Fatalf("建立 Web 根失败: %v", err)
	}
	link := filepath.Join(install, "sneaky")
	if err := os.Symlink(webRoot, link); err != nil {
		t.Skipf("创建符号链接失败（跳过）：%v", err)
	}
	service := newDirService(install)
	if err := service.checkDir(link); err == nil {
		t.Fatalf("指向 Web 根的符号链接必须被拒绝")
	}
}

// TestBackupDirHonoursSettings 覆盖设置项语义：相对路径按安装目录解析，留空走缺省。
func TestBackupDirHonoursSettings(t *testing.T) {
	install := t.TempDir()
	service := newDirService(install)
	service.settings = staticSettings{dir: "dumps/backups"}

	state := service.BackupDir(context.Background())
	if state.Default {
		t.Fatalf("配置了目录时不应标为缺省")
	}
	if state.Dir != filepath.Join(install, "dumps", "backups") {
		t.Fatalf("相对路径应相对安装目录解析，实际 %s", state.Dir)
	}
	if !state.OutsideWebRoot {
		t.Fatalf("相对路径解析后仍在 Web 根外：%+v", state)
	}

	// 配置成 dist 内 → 拒绝。
	service.settings = staticSettings{dir: filepath.Join(WebRootName, "backups")}
	state = service.BackupDir(context.Background())
	if state.OutsideWebRoot || state.Message == "" {
		t.Fatalf("落在 Web 根内的配置必须被拒绝并给出原因：%+v", state)
	}
	if !strings.Contains(state.Message, "Web 根") {
		t.Fatalf("拒绝原因应说明 Web 根：%s", state.Message)
	}

	// 设置值损坏 → 拒绝且给出可读原因。
	service.settings = staticSettings{corrupt: true}
	state = service.BackupDir(context.Background())
	if state.OutsideWebRoot || !strings.Contains(state.Message, "无法解析") {
		t.Fatalf("损坏的设置值应被拒绝：%+v", state)
	}
}

// TestResolveRejectsNonWhitelistedIDs 覆盖防路径穿越（契约 21.4）：
// 非白名单形状的 ID（含 `../`、绝对路径、盘符、其他前缀）一律按「备份不存在」拒绝。
func TestResolveRejectsNonWhitelistedIDs(t *testing.T) {
	dir := t.TempDir()
	service := newDirService(filepath.Dir(dir))

	valid := "lyidc-backup-20261014T071500Z-1a2b3c4d"
	if err := os.WriteFile(filepath.Join(dir, valid+FileExt), []byte("-- x"), 0o640); err != nil {
		t.Fatalf("写入测试备份文件失败: %v", err)
	}

	invalid := []string{
		"",
		"..",
		"../../etc/passwd",
		"..\\..\\windows\\win.ini",
		"/etc/passwd",
		"C:\\Windows\\win.ini",
		"lyidc-backup-20261014T071500Z-1a2b3c4d/../../secret",
		"lyidc-backup-20261014T071500Z-1a2b3c4d" + FileExt, // 带扩展名不算 ID
		"other-backup-20261014T071500Z-1a2b3c4d",
		"lyidc-backup-20261014T071500Z-1A2B3C4D", // 大写不在白名单
		"lyidc-backup-20261014T071500Z-1a2b3c4",  // 随机串长度不符
		"lyidc-backup-20261314T071500Z-1a2b3c4d", // 月份 13 不在形状内
	}
	for _, id := range invalid {
		if _, _, err := service.resolve(DirState{Dir: dir}, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("ID %q 应被拒绝为 ErrNotFound，实际 %v", id, err)
		}
	}

	if _, path, err := service.resolve(DirState{Dir: dir}, valid); err != nil {
		t.Fatalf("合法 ID 应被放行：%v", err)
	} else if !within(path, dir) {
		t.Fatalf("解析结果必须仍在备份目录内：%s", path)
	}
}

// TestResolveRejectsSymlink 覆盖「解析后必须仍是备份目录内的普通文件」。
func TestResolveRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 下创建符号链接需要额外权限，跳过")
	}
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.sql")
	if err := os.WriteFile(outside, []byte("-- x"), 0o640); err != nil {
		t.Fatalf("写入外部文件失败: %v", err)
	}
	valid := "lyidc-backup-20261014T071500Z-1a2b3c4d"
	if err := os.Symlink(outside, filepath.Join(dir, valid+FileExt)); err != nil {
		t.Skipf("创建符号链接失败（跳过）：%v", err)
	}
	service := newDirService(filepath.Dir(dir))
	if _, _, err := service.resolve(DirState{Dir: dir}, valid); !errors.Is(err, ErrNotFound) {
		t.Fatalf("符号链接必须被拒绝，实际 %v", err)
	}
}

// TestNewIDShapeAndCollision 覆盖文件名不可猜测（契约 21.2）：形状严格、同秒不重名。
func TestNewIDShapeAndCollision(t *testing.T) {
	dir := t.TempDir()
	service := newDirService(filepath.Dir(dir))

	seen := map[string]bool{}
	for i := 0; i < 8; i++ {
		id := service.newID(dir)
		if !backupIDPattern.MatchString(id) {
			t.Fatalf("生成的 ID 不符合形状：%s", id)
		}
		if seen[id] {
			t.Fatalf("同秒内生成重复 ID：%s", id)
		}
		seen[id] = true
	}
}

// TestWithin 覆盖目录包含判定的边界。
func TestWithin(t *testing.T) {
	sep := string(filepath.Separator)
	base := filepath.Join("a", "dist")
	cases := []struct {
		child string
		want  bool
	}{
		{base, true},
		{filepath.Join("a", "dist", "x"), true},
		{filepath.Join("a", "distx"), false},
		{filepath.Join("a", "dis"), false},
		{filepath.Join("a", "dist") + ".." + sep + "other", false},
	}
	for _, c := range cases {
		if got := within(c.child, base); got != c.want {
			t.Fatalf("within(%q, %q) = %v，期望 %v", c.child, base, got, c.want)
		}
	}
}

// TestLatestMigrationVersion 覆盖「当前代码所需版本」的读取。
func TestLatestMigrationVersion(t *testing.T) {
	version, err := latestMigrationVersion()
	if err != nil {
		t.Fatalf("读取内嵌迁移最新版本失败: %v", err)
	}
	if version < 17 {
		t.Fatalf("内嵌迁移最新版本 = %d，至少应包含 R12 的 0017", version)
	}
}

// staticSettings 是固定返回的备份设置读取器（测试用）。
type staticSettings struct {
	dir     string
	corrupt bool
}

func (s staticSettings) DBBackup(context.Context) (settings.DBBackupState, error) {
	if s.corrupt {
		return settings.DBBackupState{}, settings.ErrCorrupt
	}
	return settings.DBBackupState{Value: settings.DBBackup{Dir: s.dir}}, nil
}

// 保证 staticSettings 满足 SettingsReader（编译期断言）。
var _ SettingsReader = staticSettings{}
