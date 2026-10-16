package upgrade

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/buildinfo"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/updates"
)

// fakeChecker 提供固定的缓存检查结果（不发出网请求）。
type fakeChecker struct {
	release updates.Release
	ok      bool
}

func (f fakeChecker) CachedRelease(context.Context) (updates.Release, string, bool, error) {
	return f.release, "owner/repo", f.ok, nil
}

// testSource 把假的更新源（httptest 服务器的 URL）当成**构建期注入**的更新源：
// 地址是 https，host 由注入的 download_base 进入白名单（契约 20.7 第 8 条）。
// 生产里这两个值来自 buildinfo，测试是从进程内构造参数注入的——没有任何 HTTP 入参能改它。
func testSource(server *httptest.Server) updates.Source {
	return updates.ResolveSource("", server.URL)
}

// testHarness 组装一个指向临时安装目录的升级服务。
type testHarness struct {
	service   *Service
	dir       string
	exited    chan int
	handoffs  []Handoff
	helperPID int
	target    string
}

// newHarness 构造服务；assetName 为服务器平台产物名，archiveBody 为发布包内容。
func newHarness(t *testing.T, current, target, assetName, archiveBody, checksumBody string) *testHarness {
	t.Helper()
	dir := t.TempDir()
	harness := &testHarness{dir: dir, exited: make(chan int, 1), helperPID: 4242, target: target}

	// 用 TLS 假源：下载地址必须 https（契约 20.7 第 8 条），明文 http 的 mock 源应当被拒。
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch filepath.Base(r.URL.Path) {
		case assetName:
			_, _ = w.Write([]byte(archiveBody))
		case assetName + ".sha256":
			_, _ = w.Write([]byte(checksumBody))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	release := updates.Release{
		TagName: target,
		Assets: []updates.Asset{
			{Name: assetName, Size: int64(len(archiveBody)), DownloadURL: server.URL + "/dl/" + assetName},
			{Name: assetName + ".sha256", Size: int64(len(checksumBody)), DownloadURL: server.URL + "/dl/" + assetName + ".sha256"},
		},
	}
	previousBuildVersion := buildinfo.Version
	buildinfo.Version = current
	t.Cleanup(func() { buildinfo.Version = previousBuildVersion })

	service, err := NewService(Options{
		Dir:     dir,
		Addr:    "127.0.0.1:18080",
		Args:    []string{"--config", "config.yaml"},
		Checker: fakeChecker{release: release, ok: true},
		Source:  testSource(server),
		Client:  server.Client(),
		Now:     testNow,
		Exit:    func(code int) { harness.exited <- code },
		Spawn: func(_ context.Context, h Handoff) (int, error) {
			harness.handoffs = append(harness.handoffs, h)
			return harness.helperPID, nil
		},
	})
	if err != nil {
		t.Fatalf("构造升级服务失败：%v", err)
	}
	harness.service = service
	// 暂存二进制无法在单测里真正执行（临时文件不是可执行程序），这里注入版本探针的结果。
	previousVerify := verifyStagedBinary
	verifyStagedBinary = func(context.Context, string, string) error { return nil }
	t.Cleanup(func() { verifyStagedBinary = previousVerify })
	return harness
}

// waitHandoff 等待后台协程完成交接。
func (h *testHarness) waitHandoff(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if len(h.handoffs) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("未在超时时间内完成交接")
}

// testNow 是测试用的固定时钟（与服务构造时注入的时钟一致）。
func testNow() time.Time { return time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC) }

// buildPackageArchive 打包一个最小发布包（两个二进制 + dist/index.html）。
func buildPackageArchive(t *testing.T, inner string) ([]byte, string) {
	t.Helper()
	dir := t.TempDir()
	archive := filepath.Join(dir, "pkg.tar.gz")
	writeTarGz(t, archive, map[string]string{
		inner + "/" + serverNameForTest():       "server",
		inner + "/" + binaryName(MigrateBinary): "migrate",
		inner + "/dist/index.html":              "<html>front</html>",
	})
	body, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("读取归档失败：%v", err)
	}
	digest := sha256.Sum256(body)
	return body, hex.EncodeToString(digest[:])
}

// waitForTerminal 等待任务进入终态（后台协程推进）。
func waitForTerminal(t *testing.T, service *Service) *JobView {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		view, err := service.Job(context.Background())
		if err != nil {
			t.Fatalf("读取任务失败：%v", err)
		}
		if view != nil && view.Done() {
			return view
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("任务未在超时时间内进入终态")
	return nil
}

// waitForPhase 等待任务推进到指定阶段（后台协程推进）。
func waitForPhase(t *testing.T, service *Service, phase string) *JobView {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var last *JobView
	for time.Now().Before(deadline) {
		view, err := service.Job(context.Background())
		if err != nil {
			t.Fatalf("读取任务失败：%v", err)
		}
		last = view
		if view != nil && view.Phase == phase {
			return view
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("任务未在超时时间内到达阶段 %s：%+v", phase, last)
	return nil
}

func TestApplyRejectsInvalidRequests(t *testing.T) {
	platform := updates.Platform()
	assetName := "lyidc-oem-v1.1.0-" + platform + ".tar.gz"
	archive, digest := buildPackageArchive(t, "lyidc-oem-v1.1.0-"+platform)

	cases := []struct {
		name     string
		request  ApplyRequest
		kind     RejectKind
		contains string
	}{
		{"缺少二次确认", ApplyRequest{TargetVersion: "v1.1.0"}, RejectInvalidParam, "confirm"},
		{"目标版本为空", ApplyRequest{Confirm: true}, RejectInvalidParam, "target_version"},
		{"目标版本非法", ApplyRequest{Confirm: true, TargetVersion: "1.x"}, RejectInvalidParam, "不是合法的版本号"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			harness := newHarness(t, "v1.0.0", "v1.1.0", assetName, string(archive),
				digest+"  "+assetName+"\n")
			_, err := harness.service.Apply(context.Background(), testCase.request)
			assertReject(t, err, testCase.kind, testCase.contains)
		})
	}
}

func TestApplyRejectsDevBuild(t *testing.T) {
	platform := updates.Platform()
	assetName := "lyidc-oem-v1.1.0-" + platform + ".tar.gz"
	archive, digest := buildPackageArchive(t, "lyidc-oem-v1.1.0-"+platform)
	harness := newHarness(t, "dev-a1b2c3d", "v1.1.0", assetName, string(archive), digest+"  "+assetName+"\n")

	_, err := harness.service.Apply(context.Background(), ApplyRequest{Confirm: true, TargetVersion: "v1.1.0"})
	assertReject(t, err, RejectRule, "开发构建")
}

func TestApplyRejectsWhenNoCachedRelease(t *testing.T) {
	dir := t.TempDir()
	service, err := NewService(Options{
		Dir: dir, Addr: "127.0.0.1:18080",
		Checker: fakeChecker{ok: false},
	})
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	previousBuildVersion := buildinfo.Version
	buildinfo.Version = "v1.0.0"
	t.Cleanup(func() { buildinfo.Version = previousBuildVersion })

	_, err = service.Apply(context.Background(), ApplyRequest{Confirm: true, TargetVersion: "v1.1.0"})
	assertReject(t, err, RejectRule, "尚未检查更新")
}

func TestApplyRejectsVersionMismatchAndDowngrade(t *testing.T) {
	platform := updates.Platform()
	assetName := "lyidc-oem-v1.1.0-" + platform + ".tar.gz"
	archive, digest := buildPackageArchive(t, "lyidc-oem-v1.1.0-"+platform)

	harness := newHarness(t, "v1.0.0", "v1.1.0", assetName, string(archive), digest+"  "+assetName+"\n")
	_, err := harness.service.Apply(context.Background(), ApplyRequest{Confirm: true, TargetVersion: "v1.2.0"})
	assertReject(t, err, RejectRule, "不一致")

	// 目标版本等于当前版本：缓存里的最新版更高时同样拒绝（防同版本重装）。
	harness = newHarness(t, "v1.1.0", "v1.1.0", assetName, string(archive), digest+"  "+assetName+"\n")
	_, err = harness.service.Apply(context.Background(), ApplyRequest{Confirm: true, TargetVersion: "v1.1.0"})
	assertReject(t, err, RejectRule, "不高于当前版本")
}

func TestApplyRejectsMissingAssets(t *testing.T) {
	platform := updates.Platform()
	assetName := "lyidc-oem-v1.1.0-" + platform + ".tar.gz"
	archive, digest := buildPackageArchive(t, "lyidc-oem-v1.1.0-"+platform)

	// 缺少本平台产物：换一个平台名。
	other := "linux-ppc64"
	if platform == other {
		other = "windows-386"
	}
	harness := newHarness(t, "v1.0.0", "v1.1.0", "lyidc-oem-v1.1.0-"+other+".tar.gz",
		string(archive), digest+"\n")
	_, err := harness.service.Apply(context.Background(), ApplyRequest{Confirm: true, TargetVersion: "v1.1.0"})
	assertReject(t, err, RejectRule, "缺少本服务器平台")

	// 缺少同名 .sha256：只保留发布包资产。
	dir := t.TempDir()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(archive))
	}))
	defer server.Close()
	service, err := NewService(Options{
		Dir: dir, Addr: "127.0.0.1:18080",
		Checker: fakeChecker{ok: true, release: updates.Release{TagName: "v1.1.0", Assets: []updates.Asset{
			{Name: assetName, Size: int64(len(archive)), DownloadURL: server.URL + "/" + assetName},
		}}},
		Source: testSource(server),
		Client: server.Client(),
	})
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	previousBuildVersion := buildinfo.Version
	buildinfo.Version = "v1.0.0"
	t.Cleanup(func() { buildinfo.Version = previousBuildVersion })
	_, err = service.Apply(context.Background(), ApplyRequest{Confirm: true, TargetVersion: "v1.1.0"})
	assertReject(t, err, RejectRule, "缺少同名 .sha256")
}

func TestApplyRejectsChecksumFileUnreachable(t *testing.T) {
	platform := updates.Platform()
	assetName := "lyidc-oem-v1.1.0-" + platform + ".tar.gz"
	archive, _ := buildPackageArchive(t, "lyidc-oem-v1.1.0-"+platform)

	dir := t.TempDir()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	service, err := NewService(Options{
		Dir: dir, Addr: "127.0.0.1:18080",
		Checker: fakeChecker{ok: true, release: updates.Release{TagName: "v1.1.0", Assets: []updates.Asset{
			{Name: assetName, Size: int64(len(archive)), DownloadURL: server.URL + "/" + assetName},
			{Name: assetName + ".sha256", Size: 100, DownloadURL: server.URL + "/" + assetName + ".sha256"},
		}}},
		Source: testSource(server),
		Client: server.Client(),
	})
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	previousBuildVersion := buildinfo.Version
	buildinfo.Version = "v1.0.0"
	t.Cleanup(func() { buildinfo.Version = previousBuildVersion })
	_, err = service.Apply(context.Background(), ApplyRequest{Confirm: true, TargetVersion: "v1.1.0"})
	assertReject(t, err, RejectRule, "无法连接更新源")
}

func TestApplyRejectsWhenJobRunning(t *testing.T) {
	platform := updates.Platform()
	assetName := "lyidc-oem-v1.1.0-" + platform + ".tar.gz"
	archive, digest := buildPackageArchive(t, "lyidc-oem-v1.1.0-"+platform)
	harness := newHarness(t, "v1.0.0", "v1.1.0", assetName, string(archive), digest+"  "+assetName+"\n")

	running := &Job{ID: "20261014T100000Z-aaaa", Status: StatusRunning, Phase: PhaseDownloading,
		UpdatedAt: testNow().UTC().Format(time.RFC3339)}
	raw, err := encodeJob(running)
	if err != nil {
		t.Fatalf("编码失败：%v", err)
	}
	if err := os.WriteFile(harness.service.StatePath(), raw, 0o600); err != nil {
		t.Fatalf("写状态失败：%v", err)
	}
	_, err = harness.service.Apply(context.Background(), ApplyRequest{Confirm: true, TargetVersion: "v1.1.0"})
	assertReject(t, err, RejectConflict, "已有升级任务进行中")
}

func TestApplySuccessWritesHandoffState(t *testing.T) {
	platform := updates.Platform()
	assetName := "lyidc-oem-v1.1.0-" + platform + ".tar.gz"
	archive, digest := buildPackageArchive(t, "lyidc-oem-v1.1.0-"+platform)
	harness := newHarness(t, "v1.0.0", "v1.1.0", assetName, string(archive), digest+"  "+assetName+"\n")

	view, err := harness.service.Apply(context.Background(), ApplyRequest{Confirm: true, TargetVersion: "v1.1.0"})
	if err != nil {
		t.Fatalf("发起升级失败：%v", err)
	}
	if view == nil || view.Status != StatusRunning {
		t.Fatalf("返回的任务视图不符：%+v", view)
	}

	final := waitForPhase(t, harness.service, PhaseHandingOff)
	harness.waitHandoff(t)
	if len(harness.handoffs) != 1 {
		t.Fatalf("应交接一次，实际 %d 次", len(harness.handoffs))
	}
	handoff := harness.handoffs[0]
	if handoff.Target != "v1.1.0" || handoff.FromVersion != "v1.0.0" {
		t.Fatalf("交接版本不符：%+v", handoff)
	}
	if len(handoff.ServerArgs) != 2 || handoff.ServerArgs[0] != "--config" {
		t.Fatalf("交接应沿用旧进程参数：%v", handoff.ServerArgs)
	}
	if handoff.HealthURL != "http://127.0.0.1:18080/api/v1/health" {
		t.Fatalf("健康检查地址不符：%s", handoff.HealthURL)
	}
	// helper 副本与交接文件必须落在安装目录内、且不在替换范围里。
	if _, statErr := os.Stat(filepath.Join(harness.dir, WorkDirName, final.ID, "handoff.json")); statErr != nil {
		t.Fatalf("交接文件缺失：%v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(harness.dir, WorkDirName, final.ID, "helper", binaryName(ServerBinary))); statErr != nil {
		t.Fatalf("helper 副本缺失：%v", statErr)
	}
	// 解包结果留在暂存目录，等 helper 取用。
	if _, statErr := os.Stat(filepath.Join(harness.dir, WorkDirName, final.ID, "staged")); statErr != nil {
		t.Fatalf("暂存目录缺失：%v", statErr)
	}
	select {
	case <-harness.exited:
	case <-time.After(3 * time.Second):
		t.Fatal("主进程未按契约请求退出")
	}
	// 读取接口应报出 handing_off 阶段与日志尾部。
	read, err := harness.service.Job(context.Background())
	if err != nil {
		t.Fatalf("读取任务失败：%v", err)
	}
	if read.PhaseLabel == "" || len(read.LogTail) == 0 {
		t.Fatalf("任务视图缺少阶段名或日志：%+v", read)
	}
	if read.HelperPID != harness.helperPID {
		t.Fatalf("helper pid 应落盘：%d", read.HelperPID)
	}
}

func TestApplyRejectsChecksumMismatchDuringDownload(t *testing.T) {
	platform := updates.Platform()
	assetName := "lyidc-oem-v1.1.0-" + platform + ".tar.gz"
	archive, _ := buildPackageArchive(t, "lyidc-oem-v1.1.0-"+platform)
	wrong := strings.Repeat("a", 64)
	harness := newHarness(t, "v1.0.0", "v1.1.0", assetName, string(archive), wrong+"  "+assetName+"\n")

	if _, err := harness.service.Apply(context.Background(), ApplyRequest{Confirm: true, TargetVersion: "v1.1.0"}); err != nil {
		t.Fatalf("发起升级失败：%v", err)
	}
	final := waitForTerminal(t, harness.service)
	if final.Status != StatusFailed || final.Phase != PhaseFailed {
		t.Fatalf("sha256 不匹配应失败：%s/%s", final.Status, final.Phase)
	}
	if !strings.Contains(final.Message, "sha256 校验失败") {
		t.Fatalf("失败原因不符：%s", final.Message)
	}
	if len(harness.handoffs) != 0 {
		t.Fatal("校验失败绝不应交接给 helper")
	}
	select {
	case <-harness.exited:
		t.Fatal("校验失败不应退出主进程")
	default:
	}
	// 校验失败要清掉工作目录，且绝不留下任何待替换文件。
	if _, statErr := os.Stat(filepath.Join(harness.dir, WorkDirName, final.ID)); statErr == nil {
		t.Fatal("校验失败后工作目录应被清理")
	}
}

func TestApplyRejectsUnwritableInstallDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上目录权限位不生效，本用例在 Linux 上覆盖")
	}
	if os.Geteuid() == 0 {
		t.Skip("root 下目录权限不生效")
	}
	platform := updates.Platform()
	assetName := "lyidc-oem-v1.1.0-" + platform + ".tar.gz"
	archive, digest := buildPackageArchive(t, "lyidc-oem-v1.1.0-"+platform)
	harness := newHarness(t, "v1.0.0", "v1.1.0", assetName, string(archive), digest+"  "+assetName+"\n")

	if err := os.Chmod(harness.dir, 0o500); err != nil {
		t.Skipf("无法收回目录权限：%v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(harness.dir, 0o700) })

	_, err := harness.service.Apply(context.Background(), ApplyRequest{Confirm: true, TargetVersion: "v1.1.0"})
	assertReject(t, err, RejectRule, "安装目录不可写")
}

func TestJobViewLogTailIsTrimmed(t *testing.T) {
	dir := t.TempDir()
	service, err := NewService(Options{Dir: dir, Addr: "127.0.0.1:18080"})
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	var builder strings.Builder
	for index := 0; index < logTailLines+20; index++ {
		builder.WriteString(fmt.Sprintf("line-%d\n", index))
	}
	builder.WriteString(strings.Repeat("x", logLineMax+50) + "\n")
	if err := os.WriteFile(service.LogPath(), []byte(builder.String()), 0o600); err != nil {
		t.Fatalf("写日志失败：%v", err)
	}
	job := &Job{ID: "job-1", Status: StatusDone, Phase: PhaseDone, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := service.writeJob(job); err != nil {
		t.Fatalf("写状态失败：%v", err)
	}
	view, err := service.Job(context.Background())
	if err != nil || view == nil {
		t.Fatalf("读取任务失败：%v", err)
	}
	if len(view.LogTail) != logTailLines {
		t.Fatalf("日志尾行数应为 %d，实际 %d", logTailLines, len(view.LogTail))
	}
	if !view.LogTruncated {
		t.Fatal("日志被截断时应标记 log_truncated")
	}
	last := view.LogTail[len(view.LogTail)-1]
	if len([]rune(last)) != logLineMax+1 || !strings.HasSuffix(last, "…") {
		t.Fatalf("超长行应被截断并加省略号：%d", len([]rune(last)))
	}
}

func TestJobMarksStaleRunningAsFailed(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	service, err := NewService(Options{Dir: dir, Addr: "127.0.0.1:18080", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	job := &Job{
		ID: "job-stale", Status: StatusRunning, Phase: PhaseMigrating,
		UpdatedAt: now.Add(-staleRunningAfter - time.Minute).UTC().Format(time.RFC3339),
	}
	if err := service.writeJob(job); err != nil {
		t.Fatalf("写状态失败：%v", err)
	}
	if view, err := service.Job(context.Background()); err != nil || view == nil || view.Status != StatusRunning {
		// 写入时 UpdatedAt 会被刷新为 now，所以此刻仍应视为运行中。
		t.Fatalf("刚写入的任务不应判为陈旧：%+v %v", view, err)
	}

	// 手工把 updated_at 改回过去，模拟进程被强杀后的残留状态。
	stale := &Job{
		ID: "job-stale", Status: StatusRunning, Phase: PhaseMigrating,
		UpdatedAt: now.Add(-staleRunningAfter - time.Minute).UTC().Format(time.RFC3339),
	}
	raw, _ := json.Marshal(stale)
	if err := os.WriteFile(service.StatePath(), raw, 0o600); err != nil {
		t.Fatalf("写状态失败：%v", err)
	}
	view, err := service.Job(context.Background())
	if err != nil || view == nil {
		t.Fatalf("读取任务失败：%v", err)
	}
	if view.Status != StatusFailed || !strings.Contains(view.Message, "任务中断") {
		t.Fatalf("陈旧任务应被落盘为失败：%s / %s", view.Status, view.Message)
	}
}

func TestJobReturnsNilWithoutState(t *testing.T) {
	dir := t.TempDir()
	service, err := NewService(Options{Dir: dir, Addr: "127.0.0.1:18080"})
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	view, err := service.Job(context.Background())
	if err != nil || view != nil {
		t.Fatalf("从未升级时应返回 nil：%+v %v", view, err)
	}
}

func TestNewServiceUsesExecutableDir(t *testing.T) {
	service, err := NewService(Options{Addr: "127.0.0.1:18080"})
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("取可执行文件失败：%v", err)
	}
	if service.Dir() != filepath.Dir(executable) {
		t.Fatalf("安装目录应为可执行文件所在目录：%s", service.Dir())
	}
}

func TestHealthURLDerivation(t *testing.T) {
	cases := map[string]string{
		"0.0.0.0:8080":  "http://127.0.0.1:8080/api/v1/health",
		"127.0.0.1:80":  "http://127.0.0.1:80/api/v1/health",
		"[::]:9000":     "http://127.0.0.1:9000/api/v1/health",
		"10.0.0.5:8080": "http://10.0.0.5:8080/api/v1/health",
		"":              "",
	}
	for addr, expected := range cases {
		if actual := healthURL(addr); actual != expected {
			t.Fatalf("%s → %s，期望 %s", addr, actual, expected)
		}
	}
}

func TestApplyRejectsWhenHealthURLCannotBeDerived(t *testing.T) {
	platform := updates.Platform()
	assetName := "lyidc-oem-v1.1.0-" + platform + ".tar.gz"
	archive, digest := buildPackageArchive(t, "lyidc-oem-v1.1.0-"+platform)

	dir := t.TempDir()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			_, _ = w.Write([]byte(digest + "  " + assetName + "\n"))
			return
		}
		_, _ = w.Write([]byte(archive))
	}))
	defer server.Close()
	service, err := NewService(Options{
		Dir: dir, Addr: "",
		Checker: fakeChecker{ok: true, release: updates.Release{TagName: "v1.1.0", Assets: []updates.Asset{
			{Name: assetName, Size: int64(len(archive)), DownloadURL: server.URL + "/" + assetName},
			{Name: assetName + ".sha256", Size: 100, DownloadURL: server.URL + "/" + assetName + ".sha256"},
		}}},
		Source: testSource(server),
		Client: server.Client(),
		Spawn:  func(context.Context, Handoff) (int, error) { return 0, nil },
	})
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	previousBuildVersion := buildinfo.Version
	buildinfo.Version = "v1.0.0"
	t.Cleanup(func() { buildinfo.Version = previousBuildVersion })

	_, err = service.Apply(context.Background(), ApplyRequest{Confirm: true, TargetVersion: "v1.1.0"})
	assertReject(t, err, RejectRule, "健康检查地址")
}

// TestApplyRejectsNonAllowlistedDownloadURL 覆盖 apply 前的下载地址二次校验（契约 20.7 第 8 条）：
// 明文 http 的本地 mock 源、以及不在白名单内的主机，一律 40002 拒绝、记 WARN，
// 且**任何出网动作都不发生**（拒绝在同步前置检查里，任务状态也不落盘）。
func TestApplyRejectsNonAllowlistedDownloadURL(t *testing.T) {
	platform := updates.Platform()
	assetName := "lyidc-oem-v1.1.0-" + platform + ".tar.gz"
	archive, digest := buildPackageArchive(t, "lyidc-oem-v1.1.0-"+platform)

	// 明文 http 的 mock 源：正是渗透链里「自己搭一个下游源」的形态。
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			_, _ = w.Write([]byte(digest + "  " + assetName + "\n"))
			return
		}
		_, _ = w.Write([]byte(archive))
	}))
	defer plain.Close()
	// 一个 https 但**未被注入**的主机：它不在白名单里。
	tlsOnly := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			_, _ = w.Write([]byte(digest + "  " + assetName + "\n"))
			return
		}
		_, _ = w.Write([]byte(archive))
	}))
	defer tlsOnly.Close()

	previousBuildVersion := buildinfo.Version
	buildinfo.Version = "v1.0.0"
	t.Cleanup(func() { buildinfo.Version = previousBuildVersion })

	cases := []struct {
		name     string
		source   updates.Source
		baseURL  string
		client   *http.Client
		contains string
	}{
		{"明文 http 的 mock 源", updates.ResolveSource("", ""), plain.URL, plain.Client(), "必须使用 https"},
		{"https 但未被注入的主机", updates.ResolveSource("", ""), tlsOnly.URL, tlsOnly.Client(), "不在允许范围内"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var logs bytes.Buffer
			service, err := NewService(Options{
				Dir: t.TempDir(), Addr: "127.0.0.1:18080",
				Logger: slog.New(slog.NewTextHandler(&logs, nil)),
				Checker: fakeChecker{ok: true, release: updates.Release{TagName: "v1.1.0", Assets: []updates.Asset{
					{Name: assetName, Size: int64(len(archive)), DownloadURL: testCase.baseURL + "/" + assetName},
					{Name: assetName + ".sha256", Size: 100, DownloadURL: testCase.baseURL + "/" + assetName + ".sha256"},
				}}},
				Source: testCase.source,
				Client: testCase.client,
				Exit:   func(int) {},
			})
			if err != nil {
				t.Fatalf("构造失败：%v", err)
			}
			_, err = service.Apply(context.Background(), ApplyRequest{Confirm: true, TargetVersion: "v1.1.0"})
			assertReject(t, err, RejectRule, testCase.contains)
			if !strings.Contains(logs.String(), "拒绝从非白名单更新源下载") {
				t.Errorf("拒绝时应记 WARN 日志，实际日志：%s", logs.String())
			}
			// 拒绝发生在唯一的前置出网动作之前，也不得落盘任务状态。
			if view, jobErr := service.Job(context.Background()); jobErr != nil || view != nil {
				t.Fatalf("被拒的发起不得落盘任务状态：%+v %v", view, jobErr)
			}
		})
	}
}

func assertReject(t *testing.T, err error, kind RejectKind, contains string) {
	t.Helper()
	if err == nil {
		t.Fatal("应被拒绝但没有报错")
	}
	var rejected *RejectError
	if !errors.As(err, &rejected) {
		t.Fatalf("应返回 *RejectError，实际 %T：%v", err, err)
	}
	if rejected.Kind != kind {
		t.Fatalf("拒绝分类不符：%v（%s）", rejected.Kind, rejected.Message)
	}
	if contains != "" && !strings.Contains(rejected.Message, contains) {
		t.Fatalf("拒绝原因应包含 %q：%s", contains, rejected.Message)
	}
}

func TestWriteJobSurvivesConcurrentReads(t *testing.T) {
	dir := t.TempDir()
	service, err := NewService(Options{Dir: dir, Addr: "127.0.0.1:18080"})
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	job := &Job{ID: "job-race", Status: StatusRunning, Phase: PhaseDownloading, Progress: 0}
	if err := service.writeJob(job); err != nil {
		t.Fatalf("写入初始状态失败：%v", err)
	}

	// 读接口（前端轮询 / 页面刷新）与写入（阶段推进）会并发：Windows 上目标文件被读取时
	// rename 可能短暂失败，必须重试而不是让升级因「写状态失败」中断。
	stop := make(chan struct{})
	var wait sync.WaitGroup
	for index := 0; index < 4; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, readErr := service.Job(context.Background()); readErr != nil {
					t.Errorf("并发读取失败：%v", readErr)
					return
				}
			}
		}()
	}
	for index := 0; index < 200; index++ {
		job.Progress = index % 100
		if writeErr := service.writeJob(job); writeErr != nil {
			close(stop)
			wait.Wait()
			t.Fatalf("第 %d 次写入状态失败：%v", index, writeErr)
		}
	}
	close(stop)
	wait.Wait()

	view, err := service.Job(context.Background())
	if err != nil || view == nil || view.ID != "job-race" {
		t.Fatalf("并发读写后状态应可读：%+v %v", view, err)
	}
	// 临时文件不应残留（每次写入都完成 rename）。
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读目录失败：%v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("临时状态文件残留：%s", entry.Name())
		}
	}
}
