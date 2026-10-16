package dbbackup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// newDirServiceWithRoot 构造指定 Web 根的目录判定服务（用于覆盖 Windows 等价写法的包含判定）。
func newDirServiceWithRoot(installDir, webRoot string) *Service {
	return &Service{installDir: installDir, webRoot: webRoot, now: testClock(), logger: discardLogger()}
}

// TestNormalizeDirPathEquivalences 覆盖「同一个目录的多种写法必须归一成同一个值」（P3 finding#10）。
// 只要有一侧没归一化，两侧形态就会不同，包含判定随之失效。
func TestNormalizeDirPathEquivalences(t *testing.T) {
	sep := string(filepath.Separator)
	base := filepath.Join("a", "b", "c")
	want, err := normalizeDirPath(base)
	if err != nil {
		t.Fatalf("普通相对路径应被接受：%v", err)
	}
	forms := []string{
		base + sep,                              // 结尾分隔符
		base + sep + sep,                        // 结尾重复分隔符
		"a" + sep + "." + sep + "b" + sep + "c", // 中间的当前目录段
		"a" + sep + "x" + sep + ".." + sep + "b" + sep + "c", // 中间的可消解相对段
	}
	for _, form := range forms {
		got, err := normalizeDirPath(form)
		if err != nil {
			t.Fatalf("等价写法 %q 应被接受：%v", form, err)
		}
		if got != want {
			t.Fatalf("等价写法 %q 归一为 %q，期望 %q", form, got, want)
		}
	}

	// 扩展长度路径：去掉 `\\?\` 前缀后与普通写法归一成同一个值。
	drive := `D:\data\backups`
	plain, err := normalizeDirPath(drive)
	if err != nil {
		t.Fatalf("盘符绝对路径应被接受：%v", err)
	}
	extended, err := normalizeDirPath(`\\?\` + drive)
	if err != nil {
		t.Fatalf("扩展长度路径应被接受（去掉前缀后按普通绝对路径处理）：%v", err)
	}
	if extended != plain {
		t.Fatalf("`\\\\?\\%s` 归一为 %q，与普通写法 %q 不一致", drive, extended, plain)
	}
	if extended != filepath.Clean(drive) {
		t.Fatalf("扩展长度路径应归一到 %q，实际 %q", filepath.Clean(drive), extended)
	}

	// UNC：`\\?\UNC\...` 必须还原成 `\\server\share\...`。
	unc := `\\server\share\backups`
	plainUNC, err := normalizeDirPath(unc)
	if err != nil {
		t.Fatalf("UNC 路径应被接受：%v", err)
	}
	for _, form := range []string{`\\?\UNC\server\share\backups`, `\\?\unc\server\share\backups`} {
		got, err := normalizeDirPath(form)
		if err != nil {
			t.Fatalf("%q 应被接受：%v", form, err)
		}
		if got != plainUNC {
			t.Fatalf("%q 归一为 %q，期望 %q", form, got, plainUNC)
		}
	}

	// 根路径与结尾分隔符：`C:\`、`/opt/` 合法（不算空段）。
	for _, root := range []string{`C:\`, `C:` + sep, sep, `C:\a\`} {
		if _, err := normalizeDirPath(root); err != nil {
			t.Fatalf("根路径/结尾分隔符写法 %q 应被接受：%v", root, err)
		}
	}
}

// TestNormalizeDirPathRejectsUnsupportedForms 覆盖「无法可靠判定的形态一律拒绝且原因可读」。
func TestNormalizeDirPathRejectsUnsupportedForms(t *testing.T) {
	sep := string(filepath.Separator)
	cases := []struct {
		in           string
		wantContains string
	}{
		// 设备命名空间：`\\.\<设备>` 一律拒绝。
		{`\\.\C:`, "设备路径不受支持"},
		{`\\.\PhysicalDrive0`, "设备路径不受支持"},
		{`\\.\pipe\lyidc`, "设备路径不受支持"},
		// 卷路径与 GLOBALROOT。
		{`\\?\Volume{6b1e1f3a-0000-0000-0000-100000000000}\backups`, "卷路径不受支持"},
		{`\\?\GLOBALROOT\Device\HarddiskVolume1\dist`, "GLOBALROOT"},
		// 其它无法识别的扩展形态（盘符相对写法）。
		{`\\?\C:foo`, "不支持的扩展长度路径形态"},
		{`\\?\UNC\server`, "UNC 路径必须包含服务器与共享名"},
		{`\\\server\share`, "UNC 路径必须以"},
		// 空段、段内尾随空格与点、控制字符。
		{"a" + sep + sep + "b", "空段"},
		{"a" + sep + "b ", "空格或点结尾"},
		{"a" + sep + "b.", "空格或点结尾"},
		{"a" + sep + "b" + sep + "c. ", "空格或点结尾"},
		{"a\x00b", "控制字符"},
		{"a" + sep + "b\x1fc", "控制字符"},
		{"a" + sep + "b\x7fc", "控制字符"},
		{"", "为空"},
	}
	for _, c := range cases {
		got, err := normalizeDirPath(c.in)
		if err == nil {
			t.Fatalf("形态 %q 必须被拒绝，实际归一为 %q", c.in, got)
		}
		if !strings.Contains(err.Error(), c.wantContains) {
			t.Fatalf("形态 %q 的拒绝原因 %q 应包含 %q", c.in, err.Error(), c.wantContains)
		}
	}
}

// TestNormalizeDirPathRejectsDriveRelativeOnWindows 覆盖盘符相对写法（`C:`、`C:foo`）：
// 它表示「C 盘的当前目录」，语义随进程工作目录变化，必须拒绝。
func TestNormalizeDirPathRejectsDriveRelativeOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("盘符判定只在 Windows 上生效")
	}
	for _, in := range []string{`C:`, `C:foo`, `d:backups`} {
		if got, err := normalizeDirPath(in); err == nil {
			t.Fatalf("盘符相对写法 %q 必须被拒绝，实际归一为 %q", in, got)
		} else if !strings.Contains(err.Error(), "盘符相对路径不受支持") {
			t.Fatalf("拒绝原因应说明盘符相对写法：%v", err)
		}
	}
}

// TestCheckDirRejectsWebRootEquivalentForms 是本次修复的**核心断言**（P3 finding#10）：
// 解析结果落在 Web 根内时，无论写成普通形态还是 Windows 等价形态（`\\?\` 扩展长度路径），
// 都必须被拒绝——渗透实测里 `\\?\<Web 根>` 曾被判为「在 Web 根外」并接受。
func TestCheckDirRejectsWebRootEquivalentForms(t *testing.T) {
	install := t.TempDir()
	webRoot := filepath.Join(install, WebRootName)
	if err := os.MkdirAll(webRoot, 0o755); err != nil {
		t.Fatalf("建立 Web 根失败: %v", err)
	}
	service := newDirService(install)

	// 普通形态（既有行为，不得退化）。
	for _, dir := range []string{
		webRoot,
		filepath.Join(webRoot, "static"),
		filepath.Join(webRoot, "backups"),
		filepath.Join(webRoot, "..", WebRootName, "nested"),
	} {
		if err := service.checkDir(dir); err == nil {
			t.Fatalf("Web 根内的 %q 必须被拒绝", dir)
		} else if !strings.Contains(err.Error(), "Web 根") {
			t.Fatalf("拒绝原因应说明 Web 根：%v", err)
		}
	}

	// 扩展长度路径形态（本次修复的核心）：`\\?\` 前缀不得让 Web 根判定失效。
	if err := service.checkDir(`\\?\` + webRoot); err == nil {
		t.Fatalf("`\\\\?\\%s` 指向 Web 根，必须被拒绝（扩展长度路径前缀不得绕过 Web 根判定）", webRoot)
	} else if runtime.GOOS == "windows" && !strings.Contains(err.Error(), "Web 根") {
		// Windows 上必须是因为「确实落在 Web 根内」被拒，而不是因为形态不被支持。
		t.Fatalf("`\\\\?\\` 写法应被判为落在 Web 根内：%v", err)
	}
	if err := service.checkDir(`\\?\` + filepath.Join(webRoot, "backups")); err == nil {
		t.Fatalf("`\\\\?\\` 前缀 + Web 根子目录必须被拒绝")
	}

	// 大小写不同的同一路径（Windows 不区分大小写）。
	if runtime.GOOS == "windows" {
		if err := service.checkDir(strings.ToUpper(webRoot)); err == nil {
			t.Fatalf("大小写不同的 Web 根路径必须被拒绝")
		}
	}

	// UNC 形态：Web 根与备份目录都以 UNC 书写时同样要判出来。
	uncRoot := `\\?\UNC\srv\share\dist`
	uncService := newDirServiceWithRoot(install, uncRoot)
	for _, dir := range []string{`\\srv\share\dist`, `\\?\UNC\srv\share\dist`, `\\srv\share\dist\backups`} {
		if err := uncService.checkDir(dir); err == nil {
			t.Fatalf("UNC 形态的 Web 根内路径 %q 必须被拒绝", dir)
		} else if !strings.Contains(err.Error(), "Web 根") {
			t.Fatalf("UNC 形态的拒绝原因应说明 Web 根：%v", err)
		}
	}
}

// TestCheckDirAcceptsOutsideForms 覆盖必须继续放行的反例：
// 普通的 Web 根外绝对路径、相对路径（按安装目录解析后仍在 Web 根外）、已存在的可写目录、
// 大小写不同与扩展长度路径写法的同一路径。
func TestCheckDirAcceptsOutsideForms(t *testing.T) {
	install := t.TempDir()
	webRoot := filepath.Join(install, WebRootName)
	if err := os.MkdirAll(webRoot, 0o755); err != nil {
		t.Fatalf("建立 Web 根失败: %v", err)
	}
	service := newDirService(install)

	// 已存在的可写目录（缺省目录）。
	defaultDir := filepath.Join(install, DefaultDirName)
	if err := os.MkdirAll(defaultDir, 0o750); err != nil {
		t.Fatalf("建立缺省目录失败: %v", err)
	}
	outside := []string{
		defaultDir,
		filepath.Join(install, "dumps", "backups"),
		filepath.Join(install, "dist-backup"),
		filepath.Join(install, "dist", "..", "backups"),
		filepath.Join(webRoot, "..", "backups"),
		filepath.Join(install, "中文目录", "备份"),
		`\\?\` + defaultDir,
	}
	if runtime.GOOS == "windows" {
		outside = append(outside, strings.ToUpper(defaultDir))
	}
	for _, dir := range outside {
		if err := service.checkDir(dir); err != nil {
			t.Fatalf("Web 根外的 %q 必须放行：%v", dir, err)
		}
	}
}

// TestCheckDirRejectsWhenWebRootFormIsUnsupported 覆盖「Web 根形态不受支持时拒绝写入」：
// 包含关系无从判定时只能拒绝，绝不放行。
func TestCheckDirRejectsWhenWebRootFormIsUnsupported(t *testing.T) {
	install := t.TempDir()
	service := newDirServiceWithRoot(install, `\\.\C:`)
	if err := service.checkDir(filepath.Join(install, DefaultDirName)); !errors.Is(err, ErrDirUnusable) {
		t.Fatalf("Web 根形态非法时必须拒绝写入，实际 %v", err)
	}
}

// TestPrepareDirRejectsExtendedLengthWebRootWithoutCreating 覆盖保存口径（契约 21.10 第 2 条）：
// 被拒绝时**不得创建目录**——扩展长度路径写法同样如此。
func TestPrepareDirRejectsExtendedLengthWebRootWithoutCreating(t *testing.T) {
	install := t.TempDir()
	webRoot := filepath.Join(install, WebRootName)
	if err := os.MkdirAll(webRoot, 0o755); err != nil {
		t.Fatalf("建立 Web 根失败: %v", err)
	}
	service := newDirService(install)

	for _, dir := range []string{
		filepath.Join(WebRootName, "secret"),
		`\\?\` + filepath.Join(webRoot, "secret"),
	} {
		if _, err := service.PrepareDir(dir); !errors.Is(err, ErrDirUnusable) {
			t.Fatalf("Web 根内的 %q 必须被拒绝为 ErrDirUnusable，实际 %v", dir, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(webRoot, "secret")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("被拒绝的目录不得被创建")
	}
	entries, err := os.ReadDir(webRoot)
	if err != nil {
		t.Fatalf("读取 Web 根失败: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("被拒绝的保存不得在 Web 根里留下任何文件：%v", entries)
	}
}

// TestInspectDirReportsExtendedLengthWebRoot 覆盖设置页读取口径（契约 21.10）：
// 扩展长度路径写法同样判为不可用并给出原因，且**不创建目录**。
func TestInspectDirReportsExtendedLengthWebRoot(t *testing.T) {
	install := t.TempDir()
	webRoot := filepath.Join(install, WebRootName)
	if err := os.MkdirAll(webRoot, 0o755); err != nil {
		t.Fatalf("建立 Web 根失败: %v", err)
	}
	service := newDirService(install)

	report := service.InspectDir(`\\?\` + filepath.Join(webRoot, "backups"))
	if report.OutsideWebRoot || report.Message == "" {
		t.Fatalf("扩展长度路径写法指向 Web 根时必须报告不可用：%+v", report)
	}
	if _, err := os.Lstat(filepath.Join(webRoot, "backups")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("InspectDir 绝不能创建目录")
	}
}

// TestBackupDirReportsUnsupportedConfiguredForm 覆盖设置项被写成不受支持形态时的整体判定。
func TestBackupDirReportsUnsupportedConfiguredForm(t *testing.T) {
	install := t.TempDir()
	service := newDirService(install)
	service.settings = staticSettings{dir: `\\.\C:`}

	state := service.BackupDir(context.Background())
	if state.OutsideWebRoot || state.Message == "" {
		t.Fatalf("不受支持的目录形态必须判为不可用并给出原因：%+v", state)
	}
	if !strings.Contains(state.Message, "设备路径不受支持") {
		t.Fatalf("拒绝原因应说明设备路径不受支持：%s", state.Message)
	}
}
