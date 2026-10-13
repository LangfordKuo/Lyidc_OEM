package updates

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/buildinfo"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// fakeStore 是内存版设置存储，满足 Store 与 settings.Source。
type fakeStore struct {
	mu     sync.Mutex
	values map[string]string
	writes int
}

func newFakeStore() *fakeStore {
	return &fakeStore{values: map[string]string{}}
}

func (f *fakeStore) Setting(_ context.Context, key string) (*model.Setting, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	value, ok := f.values[key]
	if !ok {
		return nil, store.ErrNotFound
	}
	return &model.Setting{Key: key, Value: value}, nil
}

func (f *fakeStore) UpsertSettingBy(_ context.Context, key, value string, updatedBy *uint64) (*model.Setting, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.values[key] = value
	f.writes++
	return &model.Setting{Key: key, Value: value, UpdatedBy: updatedBy}, nil
}

func (f *fakeStore) writeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writes
}

// fakeFetcher 是可编排结果的假 GitHub，并记录调用次数与参数。
type fakeFetcher struct {
	mu      sync.Mutex
	release Release
	err     error
	calls   int
	repo    string
	token   string
}

func (f *fakeFetcher) Latest(_ context.Context, repo, token string) (Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.repo = repo
	f.token = token
	return f.release, f.err
}

func (f *fakeFetcher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeFetcher) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

const fixedNowText = "2026-10-14T10:00:00Z"

var fixedNow = time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)

// setBuildVersion 临时替换注入的版本号（用例结束自动还原）。
func setBuildVersion(t *testing.T, version string) {
	t.Helper()
	previous := buildinfo.Version
	buildinfo.Version = version
	t.Cleanup(func() { buildinfo.Version = previous })
}

func newTestService(st *fakeStore, fetcher Fetcher, now time.Time) *Service {
	return NewService(Options{
		Store:    st,
		Settings: settings.NewReader(st),
		Fetcher:  fetcher,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:      func() time.Time { return now },
	})
}

// platformAsset 返回与当前运行平台匹配的产物名。
func platformAsset(version string) string {
	return "lyidc-oem-" + version + "-" + runtime.GOOS + "-" + runtime.GOARCH + ".tar.gz"
}

func latestRelease(tag string) Release {
	return Release{
		TagName:     tag,
		Name:        tag,
		PublishedAt: "2026-10-10T12:00:00Z",
		Notes:       "说明",
		HTMLURL:     "https://github.com/LangfordKuo/Lyidc_OEM/releases/tag/" + tag,
		Assets: []Asset{
			{Name: platformAsset(tag), Size: 10, DownloadURL: "https://example.test/match"},
			{Name: "lyidc-oem-" + tag + "-plan9-386.zip", Size: 11, DownloadURL: "https://example.test/other"},
		},
	}
}

func TestCheckStoresReleaseAndDerivesView(t *testing.T) {
	setBuildVersion(t, "v0.9.0")
	st := newFakeStore()
	fetcher := &fakeFetcher{release: latestRelease("v0.10.0")}
	svc := newTestService(st, fetcher, fixedNow)

	view, err := svc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check 失败: %v", err)
	}
	if view.Status != StatusOK || view.Message != "" {
		t.Errorf("status/message = %q/%q，期望 ok/空", view.Status, view.Message)
	}
	if !view.HasUpdate || view.Compare != compareOK || view.LatestVersion != "v0.10.0" {
		t.Errorf("比较结论不符: has_update=%t compare=%s latest=%s", view.HasUpdate, view.Compare, view.LatestVersion)
	}
	if view.CurrentVersion != "v0.9.0" || view.Repo != settings.DefaultUpdatesRepo || view.Source != "github" {
		t.Errorf("版本/仓库/来源不符: %+v", view)
	}
	if view.CheckedAt != fixedNowText {
		t.Errorf("checked_at = %q，期望 %q", view.CheckedAt, fixedNowText)
	}
	if view.Platform != runtime.GOOS+"-"+runtime.GOARCH {
		t.Errorf("platform = %q", view.Platform)
	}
	if len(view.Assets) != 2 || !view.Assets[0].PlatformMatch || view.Assets[1].PlatformMatch {
		t.Errorf("平台高亮不符: %+v", view.Assets)
	}
	if fetcher.repo != settings.DefaultUpdatesRepo || fetcher.token != "" {
		t.Errorf("出网参数不符: repo=%q token=%q", fetcher.repo, fetcher.token)
	}
	if !strings.Contains(st.values[KeyCheck], `"repo":"`+settings.DefaultUpdatesRepo+`"`) {
		t.Errorf("缓存必须记录仓库，实际 %s", st.values[KeyCheck])
	}
}

func TestCheckFailureKeepsLastSuccessfulRelease(t *testing.T) {
	setBuildVersion(t, "v0.9.0")
	st := newFakeStore()
	fetcher := &fakeFetcher{release: latestRelease("v0.10.0")}
	svc := newTestService(st, fetcher, fixedNow)
	if _, err := svc.Check(context.Background()); err != nil {
		t.Fatalf("首次检查失败: %v", err)
	}

	// 之后一次检查被限流：状态与说明更新，但上一次成功的结论保留（红点不误灭）。
	later := fixedNow.Add(time.Hour)
	svc.now = func() time.Time { return later }
	fetcher.setErr(&Failure{Status: StatusRateLimited, Message: messageRateLimited, HTTPStatus: 403})
	view, err := svc.Check(context.Background())
	if err != nil {
		t.Fatalf("失败的检查不应返回 error: %v", err)
	}
	if view.Status != StatusRateLimited || view.Message != messageRateLimited {
		t.Errorf("status/message = %q/%q", view.Status, view.Message)
	}
	if view.LatestVersion != "v0.10.0" || !view.HasUpdate {
		t.Errorf("失败后应保留上次成功结论，实际 latest=%q has_update=%t", view.LatestVersion, view.HasUpdate)
	}
	if view.CheckedAt != fixedNowText {
		t.Errorf("checked_at 只随成功更新，实际 %q", view.CheckedAt)
	}
}

func TestCachedNeverCallsGitHub(t *testing.T) {
	setBuildVersion(t, "v0.9.0")
	st := newFakeStore()
	fetcher := &fakeFetcher{release: latestRelease("v0.10.0")}
	svc := newTestService(st, fetcher, fixedNow)

	view, err := svc.Cached(context.Background())
	if err != nil {
		t.Fatalf("Cached 失败: %v", err)
	}
	if view.Status != StatusNotChecked || view.Message != messageNotChecked || view.HasUpdate {
		t.Errorf("从未检查时应为 not_checked，实际 %+v", view)
	}
	if fetcher.callCount() != 0 {
		t.Fatalf("读缓存不得访问 GitHub，实际调用 %d 次", fetcher.callCount())
	}

	if _, err := svc.Check(context.Background()); err != nil {
		t.Fatalf("Check 失败: %v", err)
	}
	cached, err := svc.Cached(context.Background())
	if err != nil {
		t.Fatalf("Cached 失败: %v", err)
	}
	if !cached.HasUpdate || cached.LatestVersion != "v0.10.0" {
		t.Errorf("缓存应反映上次检查结果: %+v", cached)
	}
	if fetcher.callCount() != 1 {
		t.Errorf("读缓存后调用次数应仍为 1，实际 %d", fetcher.callCount())
	}
}

func TestDevBuildNeverOffersUpdate(t *testing.T) {
	setBuildVersion(t, "dev")
	st := newFakeStore()
	svc := newTestService(st, &fakeFetcher{release: latestRelease("v0.10.0")}, fixedNow)

	view, err := svc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check 失败: %v", err)
	}
	if view.Compare != compareDevBuild || view.HasUpdate {
		t.Errorf("开发构建必须 compare=dev_build 且 has_update=false，实际 %+v", view)
	}
	if view.Message != messageDevBuild {
		t.Errorf("开发构建应给出说明，实际 %q", view.Message)
	}
	if view.LatestVersion != "v0.10.0" {
		t.Errorf("开发构建仍应展示最新版本信息，实际 %q", view.LatestVersion)
	}
}

func TestRepoChangeHidesOldRecord(t *testing.T) {
	setBuildVersion(t, "v0.9.0")
	st := newFakeStore()
	svc := newTestService(st, &fakeFetcher{release: latestRelease("v0.10.0")}, fixedNow)
	if _, err := svc.Check(context.Background()); err != nil {
		t.Fatalf("Check 失败: %v", err)
	}

	// 仓库被改成别的仓库：旧仓库的结果不得冒充新仓库（缓存按仓库隔离）。
	st.values[settings.KeyUpdates] = `{"repo":"someone/else"}`
	view, err := svc.Cached(context.Background())
	if err != nil {
		t.Fatalf("Cached 失败: %v", err)
	}
	if view.Repo != "someone/else" || view.Status != StatusNotChecked || view.LatestVersion != "" {
		t.Errorf("换仓库后应视为未检查，实际 %+v", view)
	}
}

func TestCanceledCheckDoesNotRecordFailure(t *testing.T) {
	setBuildVersion(t, "v0.9.0")
	st := newFakeStore()
	fetcher := &fakeFetcher{err: &Failure{Status: StatusUnreachable, Message: messageUnreachable}}
	svc := newTestService(st, fetcher, fixedNow)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := svc.Check(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("调用方已取消时应返回取消错误，实际 %v", err)
	}
	if st.writeCount() != 0 {
		t.Errorf("取消不得写入失败状态，实际写入 %d 次", st.writeCount())
	}
}

func TestSchedulerRunsInitialCheck(t *testing.T) {
	setBuildVersion(t, "v0.9.0")
	st := newFakeStore()
	fetcher := &fakeFetcher{release: latestRelease("v0.10.0")}
	svc := NewService(Options{
		Store:        st,
		Settings:     settings.NewReader(st),
		Fetcher:      fetcher,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		InitialDelay: 5 * time.Millisecond,
		Interval:     time.Hour,
	})
	svc.Start()
	defer svc.Stop()

	// 出网与写缓存是两步：轮询到缓存出现为止（读取走 fakeStore 的锁）。
	deadline := time.Now().Add(3 * time.Second)
	var cached string
	for time.Now().Before(deadline) {
		if setting, err := st.Setting(context.Background(), KeyCheck); err == nil {
			cached = setting.Value
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if fetcher.callCount() < 1 {
		t.Fatal("启动延迟后应执行一次定时检查")
	}
	if !strings.Contains(cached, "v0.10.0") {
		t.Errorf("定时检查结果应写入缓存，实际 %q", cached)
	}
}

// TestStartupCheckRespectsInterval 覆盖启动后首次检查的节流（契约 19.4）：缓存新鲜时不出网，过期或缺失才出网。
func TestStartupCheckRespectsInterval(t *testing.T) {
	setBuildVersion(t, "v0.9.0")
	st := newFakeStore()
	fetcher := &fakeFetcher{release: latestRelease("v0.10.0")}
	svc := newTestService(st, fetcher, fixedNow)

	// 缓存新鲜（距上次成功不足一个间隔）：不出网。
	if _, err := svc.Check(context.Background()); err != nil {
		t.Fatalf("Check 失败: %v", err)
	}
	svc.runIfDue()
	if fetcher.callCount() != 1 {
		t.Fatalf("缓存新鲜时启动检查不得出网，实际调用 %d 次", fetcher.callCount())
	}

	// 距上次成功已超过一个间隔：出网。
	svc.now = func() time.Time { return fixedNow.Add(DefaultInterval + time.Minute) }
	svc.runIfDue()
	if fetcher.callCount() != 2 {
		t.Fatalf("缓存过期时启动检查应出网，实际调用 %d 次", fetcher.callCount())
	}
}

func TestMatchesPlatform(t *testing.T) {
	cases := map[string]bool{
		"lyidc-oem-v1.0.0-linux-amd64.tar.gz":        true,
		"lyidc-oem-v1.0.0-linux-amd64.zip":           true,
		"lyidc-oem-v1.0.0-linux-amd64.tar.gz.sha256": false,
		"lyidc-oem-v1.0.0-linux-arm64.tar.gz":        false,
		"lyidc-oem-v1.0.0-windows-amd64.zip":         false,
	}
	for name, want := range cases {
		if got := matchesPlatform(name, "linux-amd64"); got != want {
			t.Errorf("matchesPlatform(%q, linux-amd64) = %t，期望 %t", name, got, want)
		}
	}
}
