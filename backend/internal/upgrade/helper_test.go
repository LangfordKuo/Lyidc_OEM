package upgrade

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// installFixture 是一套「假部署」：安装目录 + 暂存目录 + 交接文件。
type installFixture struct {
	dir       string
	workDir   string
	stagedDir string
	statePath string
	logPath   string
	handoff   Handoff
}

// newInstallFixture 搭出最小安装目录（两个二进制 + dist）与暂存发布包。
func newInstallFixture(t *testing.T) *installFixture {
	t.Helper()
	dir := t.TempDir()
	fixture := &installFixture{
		dir:       dir,
		workDir:   filepath.Join(dir, WorkDirName, "job-1"),
		stagedDir: filepath.Join(dir, WorkDirName, "job-1", "staged", "pkg"),
		statePath: filepath.Join(dir, StateFile),
		logPath:   filepath.Join(dir, LogFile),
	}
	// 安装目录里的旧版本文件。
	writeFile(t, filepath.Join(dir, binaryName(ServerBinary)), "old-server")
	writeFile(t, filepath.Join(dir, binaryName(MigrateBinary)), "old-migrate")
	writeFile(t, filepath.Join(dir, DistDir, "index.html"), "old-front")
	// 暂存的新版本文件。
	writeFile(t, filepath.Join(fixture.stagedDir, binaryName(ServerBinary)), "new-server")
	writeFile(t, filepath.Join(fixture.stagedDir, binaryName(MigrateBinary)), "new-migrate")
	writeFile(t, filepath.Join(fixture.stagedDir, DistDir, "index.html"), "new-front")

	fixture.handoff = Handoff{
		JobID:       "job-1",
		Dir:         dir,
		StatePath:   fixture.statePath,
		LogPath:     fixture.logPath,
		WorkDir:     fixture.workDir,
		StagedDir:   fixture.stagedDir,
		OldPID:      0,
		HealthURL:   "http://127.0.0.1:18080/api/v1/health",
		ServerArgs:  []string{"--config", "config.yaml"},
		FromVersion: "v1.0.0",
		Target:      "v1.1.0",
	}
	// 主进程落盘的初始状态（handing_off，running）。
	job := &Job{
		ID: "job-1", Status: StatusRunning, Phase: PhaseHandingOff, Progress: phaseProgress[PhaseHandingOff],
		FromVersion: "v1.0.0", TargetVersion: "v1.1.0", StartedAt: "2026-10-14T10:00:00Z",
		UpdatedAt: "2026-10-14T10:00:00Z", InstallDir: dir, HelperPID: os.Getpid(),
	}
	raw, err := encodeJob(job)
	if err != nil {
		t.Fatalf("编码状态失败：%v", err)
	}
	if err := os.WriteFile(fixture.statePath, raw, 0o600); err != nil {
		t.Fatalf("写状态失败：%v", err)
	}
	return fixture
}

// options 返回一套默认成功的注入动作，测试按需覆盖。
func (f *installFixture) options(t *testing.T) HelperOptions {
	return HelperOptions{
		Now:      func() time.Time { return time.Date(2026, 10, 14, 10, 5, 0, 0, time.UTC) },
		WaitExit: func(int, time.Duration) error { return nil },
		Migrate: func(context.Context, string, string, []string, time.Duration, io.Writer) error {
			return nil
		},
		Start: func(string, string, []string, io.Writer) (int, error) { return 9001, nil },
		Health: func(context.Context, string, time.Duration) error {
			return nil
		},
		Kill:   func(int) error { return nil },
		Settle: 0,
	}
}

// readJobFile 读出状态文件。
func (f *installFixture) readJobFile(t *testing.T) *Job {
	t.Helper()
	raw, err := os.ReadFile(f.statePath)
	if err != nil {
		t.Fatalf("读状态失败：%v", err)
	}
	job, err := decodeJob(raw)
	if err != nil {
		t.Fatalf("解析状态失败：%v", err)
	}
	return job
}

// assertContent 断言文件内容。
func assertContent(t *testing.T, path, expected string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s 失败：%v", path, err)
	}
	if string(raw) != expected {
		t.Fatalf("%s 内容应为 %q，实际 %q", path, expected, string(raw))
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("建目录失败：%v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}
}

func TestRunHelperHappyPath(t *testing.T) {
	fixture := newInstallFixture(t)
	options := fixture.options(t)

	if code := RunHelper(fixture.handoff, options); code != 0 {
		t.Fatalf("升级成功时退出码应为 0，实际 %d", code)
	}
	job := fixture.readJobFile(t)
	if job.Status != StatusDone || job.Phase != PhaseDone || job.Progress != 100 {
		t.Fatalf("终态不符：%s/%s/%d", job.Status, job.Phase, job.Progress)
	}
	if job.Message != "" || job.RolledBack || !job.DBMigrated {
		t.Fatalf("成功态字段不符：%+v", job)
	}
	assertContent(t, filepath.Join(fixture.dir, binaryName(ServerBinary)), "new-server")
	assertContent(t, filepath.Join(fixture.dir, binaryName(MigrateBinary)), "new-migrate")
	assertContent(t, filepath.Join(fixture.dir, DistDir, "index.html"), "new-front")
	// 成功后备份与工作目录都要清理掉。
	for _, backup := range []string{
		filepath.Join(fixture.dir, binaryName(ServerBinary)) + BackupSuffix,
		filepath.Join(fixture.dir, DistDir) + BackupSuffix,
	} {
		if _, err := os.Stat(backup); err == nil {
			t.Fatalf("备份应被清理：%s", backup)
		}
	}
	if _, err := os.Stat(fixture.workDir); err == nil {
		t.Fatal("工作目录应被清理")
	}
	log := readLog(t, fixture.logPath)
	for _, expected := range []string{"已备份并替换", "执行数据库迁移", "健康检查通过", "升级完成"} {
		if !strings.Contains(log, expected) {
			t.Fatalf("升级日志缺少 %q：%s", expected, log)
		}
	}
}

func TestRunHelperRollsBackOnHealthFailure(t *testing.T) {
	fixture := newInstallFixture(t)
	options := fixture.options(t)
	healthCalls := 0
	options.Health = func(_ context.Context, url string, _ time.Duration) error {
		healthCalls++
		if healthCalls == 1 {
			return errors.New("健康检查超时（1m0s）：服务尚未响应")
		}
		// 回滚后旧版健康检查通过。
		if !strings.HasPrefix(url, "http://127.0.0.1:18080") {
			return errors.New("健康检查地址不符")
		}
		return nil
	}
	killed := 0
	options.Kill = func(pid int) error { killed++; return nil }

	if code := RunHelper(fixture.handoff, options); code != 1 {
		t.Fatalf("健康检查失败时退出码应为 1，实际 %d", code)
	}
	job := fixture.readJobFile(t)
	if job.Status != StatusRolledBack || !job.RolledBack || !job.DBMigrated {
		t.Fatalf("回滚态字段不符：%+v", job)
	}
	if !strings.Contains(job.Message, "健康检查未通过") {
		t.Fatalf("回滚原因不符：%s", job.Message)
	}
	if killed == 0 {
		t.Fatal("回滚前应结束新进程")
	}
	// 旧文件必须原样回来，新文件不能残留在安装目录。
	assertContent(t, filepath.Join(fixture.dir, binaryName(ServerBinary)), "old-server")
	assertContent(t, filepath.Join(fixture.dir, binaryName(MigrateBinary)), "old-migrate")
	assertContent(t, filepath.Join(fixture.dir, DistDir, "index.html"), "old-front")
	if _, err := os.Stat(filepath.Join(fixture.dir, binaryName(ServerBinary)) + BackupSuffix); err == nil {
		t.Fatal("回滚后备份应已改名回原位")
	}
}

func TestRunHelperRollsBackOnMigrateFailure(t *testing.T) {
	fixture := newInstallFixture(t)
	options := fixture.options(t)
	starts := 0
	options.Start = func(_ string, exe string, _ []string, _ io.Writer) (int, error) {
		starts++
		if strings.Contains(exe, MigrateBinary) {
			t.Fatal("迁移不应通过启动进程执行")
		}
		return 9002, nil
	}
	options.Migrate = func(context.Context, string, string, []string, time.Duration, io.Writer) error {
		return errors.New("up: 迁移 0017 失败")
	}

	if code := RunHelper(fixture.handoff, options); code != 1 {
		t.Fatalf("迁移失败时退出码应为 1，实际 %d", code)
	}
	job := fixture.readJobFile(t)
	if job.Status != StatusRolledBack || job.DBMigrated {
		t.Fatalf("迁移失败不应标记已迁移：%+v", job)
	}
	if !strings.Contains(job.Message, "数据库迁移失败") {
		t.Fatalf("回滚原因不符：%s", job.Message)
	}
	if starts != 1 {
		t.Fatalf("迁移失败时只应重新拉起旧版一次，实际 %d 次", starts)
	}
	assertContent(t, filepath.Join(fixture.dir, binaryName(ServerBinary)), "old-server")
}

func TestRunHelperReportsFailureWhenRollbackAlsoFails(t *testing.T) {
	fixture := newInstallFixture(t)
	options := fixture.options(t)
	options.Health = func(context.Context, string, time.Duration) error { return errors.New("始终不可用") }
	options.Start = func(string, string, []string, io.Writer) (int, error) { return 9003, nil }

	if code := RunHelper(fixture.handoff, options); code != 1 {
		t.Fatalf("退出码应为 1，实际 %d", code)
	}
	job := fixture.readJobFile(t)
	if job.Status != StatusFailed || !job.RolledBack {
		t.Fatalf("回滚也失败时应为 failed 且标记已回滚：%+v", job)
	}
	if !strings.Contains(job.Message, "请人工介入") {
		t.Fatalf("应明确要求人工介入：%s", job.Message)
	}
	// 文件仍应还原成功（失败的是服务拉起，不是文件还原）。
	assertContent(t, filepath.Join(fixture.dir, binaryName(ServerBinary)), "old-server")
}

func TestRunHelperAbortsWhenOldProcessNeverExits(t *testing.T) {
	fixture := newInstallFixture(t)
	fixture.handoff.OldPID = 4242
	options := fixture.options(t)
	waitCalls := 0
	options.WaitExit = func(int, time.Duration) error {
		waitCalls++
		return errors.New("等待进程 123 退出超时（1m0s）")
	}
	starts := 0
	options.Start = func(string, string, []string, io.Writer) (int, error) { starts++; return 9004, nil }

	if code := RunHelper(fixture.handoff, options); code != 1 {
		t.Fatalf("退出码应为 1，实际 %d", code)
	}
	job := fixture.readJobFile(t)
	if job.Status != StatusFailed || !strings.Contains(job.Message, "等待旧进程退出超时") {
		t.Fatalf("失败原因不符：%+v", job)
	}
	if waitCalls != 1 || starts != 0 {
		t.Fatalf("未退出旧进程时不应替换或拉起任何服务：wait=%d start=%d", waitCalls, starts)
	}
	// 未替换任何文件：旧文件还在原位，也没有产生备份。
	assertContent(t, filepath.Join(fixture.dir, binaryName(ServerBinary)), "old-server")
	if _, err := os.Stat(filepath.Join(fixture.dir, binaryName(ServerBinary)) + BackupSuffix); err == nil {
		t.Fatal("中止时不应产生备份文件")
	}
}

func TestRunHelperAbortsWhenInstallDirIncomplete(t *testing.T) {
	fixture := newInstallFixture(t)
	if err := os.RemoveAll(filepath.Join(fixture.dir, DistDir)); err != nil {
		t.Fatalf("移除 dist 失败：%v", err)
	}
	options := fixture.options(t)

	if code := RunHelper(fixture.handoff, options); code != 1 {
		t.Fatalf("退出码应为 1，实际 %d", code)
	}
	job := fixture.readJobFile(t)
	if job.Status != StatusFailed || !strings.Contains(job.Message, "缺少") {
		t.Fatalf("失败原因不符：%+v", job)
	}
	assertContent(t, filepath.Join(fixture.dir, binaryName(ServerBinary)), "old-server")
}

func TestRunHelperAbortsWhenStagedPackageIncomplete(t *testing.T) {
	fixture := newInstallFixture(t)
	if err := os.Remove(filepath.Join(fixture.stagedDir, DistDir, "index.html")); err != nil {
		t.Fatalf("移除暂存文件失败：%v", err)
	}
	options := fixture.options(t)

	if code := RunHelper(fixture.handoff, options); code != 1 {
		t.Fatalf("退出码应为 1，实际 %d", code)
	}
	job := fixture.readJobFile(t)
	if job.Status != StatusFailed || !strings.Contains(job.Message, "发布包缺少") {
		t.Fatalf("失败原因不符：%+v", job)
	}
	assertContent(t, filepath.Join(fixture.dir, binaryName(ServerBinary)), "old-server")
}

func TestRunHelperRebuildsMissingState(t *testing.T) {
	fixture := newInstallFixture(t)
	if err := os.Remove(fixture.statePath); err != nil {
		t.Fatalf("删除状态失败：%v", err)
	}
	options := fixture.options(t)

	if code := RunHelper(fixture.handoff, options); code != 0 {
		t.Fatalf("退出码应为 0，实际 %d", code)
	}
	job := fixture.readJobFile(t)
	if job.Status != StatusDone || job.ID != "job-1" || job.TargetVersion != "v1.1.0" {
		t.Fatalf("应按交接信息重建状态：%+v", job)
	}
}

func TestMigrateArgsPassesConfigPath(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"双横线分离", []string{"--config", "/opt/lyidc/config.yaml"}, []string{"-config", "/opt/lyidc/config.yaml"}},
		{"单横线分离", []string{"-config", "config.yaml"}, []string{"-config", "config.yaml"}},
		{"等号形式", []string{"-config=config.yaml"}, []string{"-config", "config.yaml"}},
		{"无配置参数", []string{"--other", "x"}, nil},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := migrateArgs(testCase.args)
			if len(got) != len(testCase.want) {
				t.Fatalf("参数不符：%v", got)
			}
			for index := range got {
				if got[index] != testCase.want[index] {
					t.Fatalf("参数不符：%v", got)
				}
			}
		})
	}
}

func TestParseProbeVersion(t *testing.T) {
	cases := map[string]string{
		"lyidc-oem server version=v1.2.3 commit=abc built_at=2026-10-14T10:00:00Z": "v1.2.3",
		"lyidc-oem migrate version=dev commit=unknown built_at=unknown":            "dev",
		"完全不是版本输出": "",
	}
	for output, expected := range cases {
		if got := parseProbeVersion(output); got != expected {
			t.Fatalf("%q → %q，期望 %q", output, got, expected)
		}
	}
}

// readLog 读出升级日志。
func readLog(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读日志失败：%v", err)
	}
	return string(raw)
}

func TestRemoveWithRetryHandlesNormalPaths(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "keep.txt")
	tree := filepath.Join(dir, "tree")
	writeFile(t, file, "x")
	writeFile(t, filepath.Join(tree, "nested", "inner.txt"), "y")

	if err := removeWithRetry(file, 100*time.Millisecond); err != nil {
		t.Fatalf("删除文件失败：%v", err)
	}
	if err := removeWithRetry(tree, 100*time.Millisecond); err != nil {
		t.Fatalf("删除目录树失败：%v", err)
	}
	// 不存在也视为成功（删除是幂等的）。
	if err := removeWithRetry(filepath.Join(dir, "absent"), 100*time.Millisecond); err != nil {
		t.Fatalf("删除不存在的路径应成功：%v", err)
	}
}

func TestRemoveWithRetryGivesUpAfterTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("用非法路径触发错误在 Windows 上不可靠（本用例覆盖 Unix 分支）")
	}
	// 非法路径（含 NUL 字节）无法删除：应当在超时后返回错误，而不是无限重试。
	start := time.Now()
	err := removeWithRetry("bad\x00name", 300*time.Millisecond)
	if err == nil {
		t.Fatal("非法路径应当报错")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("重试应受超时约束，实际耗时 %v", elapsed)
	}
}
