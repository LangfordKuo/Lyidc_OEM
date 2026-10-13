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

// ---------------------------------------------------------------------------
// 备份目录的解析与校验报告（R15，契约 21.10）：设置接口读取/保存共用的口径。
// ---------------------------------------------------------------------------

// TestInspectDirDefault 覆盖读取口径：缺省目录（配置留空）解析为 <安装目录>/backups，
// 目录不存在时只提示、**不创建**。
func TestInspectDirDefault(t *testing.T) {
	install := t.TempDir()
	service := newDirService(install)

	report := service.InspectDir("")
	want := filepath.Join(install, DefaultDirName)
	if report.Dir != want || report.DefaultDir != want {
		t.Fatalf("缺省目录解析 = %q / %q，期望 %q", report.Dir, report.DefaultDir, want)
	}
	if !report.Default || !report.OutsideWebRoot {
		t.Fatalf("缺省目录应判定为缺省且在 Web 根外：%+v", report)
	}
	if report.Exists || report.Writable {
		t.Fatalf("目录尚不存在时不应标为存在/可写：%+v", report)
	}
	if !strings.Contains(report.Message, "不存在") {
		t.Fatalf("目录不存在时应给出提示：%+v", report)
	}
	if _, err := os.Lstat(want); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("InspectDir 绝不能创建目录（契约 21.10：读取不建目录）")
	}

	// 建出来之后：exists + 写入探针通过，message 清空。
	if err := os.MkdirAll(want, 0o750); err != nil {
		t.Fatalf("建立目录失败: %v", err)
	}
	report = service.InspectDir("")
	if !report.Exists || !report.Writable || report.Message != "" {
		t.Fatalf("已存在的可写目录应报告可用：%+v", report)
	}
	// 探针文件用完即删：目录里不留任何东西。
	entries, err := os.ReadDir(want)
	if err != nil || len(entries) != 0 {
		t.Fatalf("写入探针不得留下残留文件：%v / %v", entries, err)
	}
}

// TestInspectDirRelativeAndWebRoot 覆盖解析口径（相对路径按安装目录解析）与最硬的一条边界
// （契约 21.4 / 21.10）：解析结果落在 Web 根内一律判定为不可用并给出原因。
func TestInspectDirRelativeAndWebRoot(t *testing.T) {
	install := t.TempDir()
	service := newDirService(install)

	report := service.InspectDir("dumps/backups")
	if report.Default {
		t.Fatalf("配置了目录时不应标为缺省：%+v", report)
	}
	if report.Dir != filepath.Join(install, "dumps", "backups") {
		t.Fatalf("相对路径应相对安装目录解析，实际 %s", report.Dir)
	}
	if !report.OutsideWebRoot {
		t.Fatalf("相对路径解析后仍在 Web 根外：%+v", report)
	}

	for _, dir := range []string{WebRootName, filepath.Join(WebRootName, "backups")} {
		report = service.InspectDir(dir)
		if report.OutsideWebRoot || !strings.Contains(report.Message, "Web 根") {
			t.Fatalf("落在 Web 根内的目录必须被拒绝并说明原因：%+v", report)
		}
		if _, err := os.Lstat(report.Dir); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("被拒绝的目录不得被创建：%s", report.Dir)
		}
	}
}

// TestPrepareDirCreatesAndProbes 覆盖保存口径（契约 21.10 第 2 条）：
// 目录不存在则按 0750 创建（含中间目录）并做写入探针。
func TestPrepareDirCreatesAndProbes(t *testing.T) {
	install := t.TempDir()
	service := newDirService(install)

	report, err := service.PrepareDir("bk2/nested")
	if err != nil {
		t.Fatalf("合法目录应保存通过：%v", err)
	}
	want := filepath.Join(install, "bk2", "nested")
	if report.Dir != want || !report.Exists || !report.Writable || report.Message != "" {
		t.Fatalf("保存结果口径不正确：%+v", report)
	}
	info, err := os.Stat(want)
	if err != nil || !info.IsDir() {
		t.Fatalf("保存时应创建目录：%v", err)
	}
	if runtime.GOOS != "windows" {
		if mode := info.Mode().Perm(); mode != 0o750 {
			t.Fatalf("目录权限 = %o，期望 0750", mode)
		}
	}
	entries, err := os.ReadDir(want)
	if err != nil || len(entries) != 0 {
		t.Fatalf("写入探针不得留下残留文件：%v / %v", entries, err)
	}

	// 空串 = 恢复缺省目录：同样完成创建与探针。
	report, err = service.PrepareDir("   ")
	if err != nil || !report.Default || report.Dir != service.DefaultDir() || !report.Writable {
		t.Fatalf("留空应回到缺省目录并创建：%+v（err=%v）", report, err)
	}
}

// TestPrepareDirRejectsWebRootWithoutCreating 覆盖保存时的 Web 根拒绝（契约 21.10）：
// 拒绝且**不得**创建目录。
func TestPrepareDirRejectsWebRootWithoutCreating(t *testing.T) {
	install := t.TempDir()
	webRoot := filepath.Join(install, WebRootName)
	if err := os.MkdirAll(webRoot, 0o755); err != nil {
		t.Fatalf("建立 Web 根失败: %v", err)
	}
	service := newDirService(install)

	if _, err := service.PrepareDir(filepath.Join(WebRootName, "secret")); !errors.Is(err, ErrDirUnusable) {
		t.Fatalf("Web 根内的目录必须被拒绝为 ErrDirUnusable，实际 %v", err)
	}
	if _, err := os.Lstat(filepath.Join(webRoot, "secret")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("被拒绝的目录不得被创建")
	}

	// 已存在的非目录对象（文件）同样拒绝。
	file := filepath.Join(install, "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o640); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}
	if _, err := service.PrepareDir("not-a-dir"); !errors.Is(err, ErrDirUnusable) {
		t.Fatalf("指向文件的路径必须被拒绝为 ErrDirUnusable，实际 %v", err)
	}
}

// TestPrepareDirRejectsSymlink 覆盖符号链接拒绝（与运行时同一套 checkDir 口径）。
func TestPrepareDirRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 下创建符号链接需要额外权限，跳过")
	}
	install := t.TempDir()
	target := t.TempDir()
	link := filepath.Join(install, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("创建符号链接失败（跳过）：%v", err)
	}
	service := newDirService(install)
	if _, err := service.PrepareDir("link"); !errors.Is(err, ErrDirUnusable) {
		t.Fatalf("符号链接必须被拒绝，实际 %v", err)
	}
}

// TestProbeWritableRejectsReadOnlyDir 覆盖写入探针：目录存在但不可写时必须报不可用
// （只读挂载、权限不足这类环境问题在保存时就暴露，而不是等第一次备份才失败）。
func TestProbeWritableRejectsReadOnlyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 下 chmod 不改变访问控制，跳过")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Skipf("调整目录权限失败（跳过）：%v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o750) })

	if err := probeWritable(dir); err == nil {
		t.Skip("目录仍可写（多半以 root 运行），跳过")
	}
	report := newDirService(filepath.Dir(dir)).InspectDir(dir)
	if report.Writable || !strings.Contains(report.Message, "不可写") {
		t.Fatalf("只读目录应报告不可写：%+v", report)
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
