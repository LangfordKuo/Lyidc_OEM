package updates

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/buildinfo"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// KeyCheck 是更新检查缓存的设置键（契约 19.4）。它不是用户设置，只由本包读写。
const KeyCheck = "updates.check"

const (
	// DefaultInterval 是定时刷新的间隔（契约 19.4）。
	DefaultInterval = 6 * time.Hour
	// DefaultInitialDelay 是进程启动后首次检查的延迟（契约 19.4），避免启动时集中出网。
	DefaultInitialDelay = 60 * time.Second
	// scheduledCheckTimeout 是一次定时检查（含读写设置与出网）的整体上限。
	scheduledCheckTimeout = 30 * time.Second

	sourceGitHub = "github"
)

// Store 是缓存读写所需的存储能力（生产为 *store.Store）。
type Store interface {
	// Setting 读取设置；不存在时返回 store.ErrNotFound。
	Setting(ctx context.Context, key string) (*model.Setting, error)
	// UpsertSettingBy 写入设置；缓存由系统写入，updatedBy 传 nil。
	UpsertSettingBy(ctx context.Context, key, value string, updatedBy *uint64) (*model.Setting, error)
}

// record 是缓存的持久化结构（契约 19.4）。
//
// Release / CheckedAt 只在成功获取时更新；Status / Message 记录最近一次尝试。
// 失败尝试因此不会抹掉上一次成功的结论（一次限流不误灭红点、不误亮红点）。
type record struct {
	Repo      string   `json:"repo"`
	Release   *Release `json:"release"`
	CheckedAt string   `json:"checked_at"`
	Status    string   `json:"status"`
	Message   string   `json:"message"`
}

// View 是 GET /api/v1/admin/updates 与 GET /api/v1/admin/updates/check 的对外视图（契约 19.3）。
type View struct {
	CurrentVersion string      `json:"current_version"`
	Commit         string      `json:"commit"`
	BuiltAt        string      `json:"built_at"`
	LatestVersion  string      `json:"latest_version"`
	HasUpdate      bool        `json:"has_update"`
	Compare        string      `json:"compare"`
	Status         string      `json:"status"`
	Message        string      `json:"message"`
	Source         string      `json:"source"`
	Repo           string      `json:"repo"`
	ReleaseName    string      `json:"release_name"`
	PublishedAt    string      `json:"published_at"`
	Notes          string      `json:"notes"`
	NotesTruncated bool        `json:"notes_truncated"`
	HTMLURL        string      `json:"html_url"`
	Platform       string      `json:"platform"`
	Assets         []AssetView `json:"assets"`
	CheckedAt      string      `json:"checked_at"`
}

// AssetView 是产物的对外视图；PlatformMatch 标记与服务器运行平台对应的产物。
type AssetView struct {
	Name          string `json:"name"`
	Size          int64  `json:"size"`
	DownloadURL   string `json:"download_url"`
	PlatformMatch bool   `json:"platform_match"`
}

// Options 是 Service 的构造参数。
type Options struct {
	// Store 是缓存所在的存储（生产为 *store.Store）。
	Store Store
	// Settings 读取更新源设置（仓库与可选 Token），每次检查都重读，改完即生效。
	Settings *settings.Reader
	// Source 是生效的更新源地址（P2：构建期固定项，契约 19.5）。**生产必须传**
	// updates.ResolveSource(...) 的结果；零值时按官方 OfficialAPIBase 处理，便于测试。
	//
	// 它取代了旧版从设置里读 api_base 的做法：库内遗留的 api_base / download_base 一律不读，
	// 因此残留旧值（哪怕是攻击者主机）不可能改变本进程的出网目标。
	Source Source
	// Fetcher 访问 GitHub；nil 时使用默认 Client。与 FetcherFor 二选一：
	// 注入 Fetcher 时忽略 API 基地址（测试用固定桩），生产走 FetcherFor（R13-B 的更新源注入，契约 19.5）。
	Fetcher Fetcher
	// FetcherFor 按生效的 API 基地址构造访问器（契约 19.5 / 20.8）；nil 时退化为 Fetcher。
	FetcherFor func(apiBase string, timeout time.Duration) Fetcher
	Logger     *slog.Logger
	// Interval 是定时刷新间隔；零值取 DefaultInterval。
	Interval time.Duration
	// InitialDelay 是首次检查延迟；零值取 DefaultInitialDelay。
	InitialDelay time.Duration
	// Now 注入时钟（测试用）；nil 时取 time.Now。
	Now func() time.Time
}

// Service 负责检查更新、维护缓存与定时刷新（契约 19.3–19.4）。
type Service struct {
	store        Store
	settings     *settings.Reader
	source       Source
	fetcher      Fetcher
	fetcherFor   func(apiBase string, timeout time.Duration) Fetcher
	logger       *slog.Logger
	now          func() time.Time
	interval     time.Duration
	initialDelay time.Duration

	// mu 让手动检查与定时检查互斥：同一时刻只出网一次。
	mu sync.Mutex

	startOnce sync.Once
	stop      chan struct{}
	stopOnce  sync.Once
}

// NewService 构造检查服务。它只负责检查与读取；后台定时任务需要显式调用 Start。
func NewService(opts Options) *Service {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	fetcher := opts.Fetcher
	if fetcher == nil {
		fetcher = NewClient(logger, buildinfo.Version)
	}
	// 注入固定桩时忽略地址注入：桩决定一切（地址注入只作用于生产装配的默认实现）。
	fetcherFor := opts.FetcherFor
	if opts.Fetcher != nil {
		fetcherFor = func(string, time.Duration) Fetcher { return opts.Fetcher }
	}
	interval := opts.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	initialDelay := opts.InitialDelay
	if initialDelay <= 0 {
		initialDelay = DefaultInitialDelay
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	// 更新源地址由构建期固定（P2）：零值按官方缺省补齐，避免测试构造出空地址。
	source := opts.Source
	if strings.TrimSpace(source.APIBase) == "" {
		source.APIBase = OfficialAPIBase
	}
	return &Service{
		store:        opts.Store,
		settings:     opts.Settings,
		source:       source,
		fetcher:      fetcher,
		fetcherFor:   fetcherFor,
		logger:       logger,
		now:          now,
		interval:     interval,
		initialDelay: initialDelay,
		stop:         make(chan struct{}),
	}
}

// fetcherForConfig 选出这一次检查用的访问器（契约 19.5 / 20.8）：注入的固定 Fetcher 优先
// （测试），否则按**构建期固定的** Source.APIBase 构造——地址不来自任何 HTTP 入参。
func (s *Service) fetcherForConfig() Fetcher {
	if s.fetcherFor != nil {
		return s.fetcherFor(s.source.APIBase, DefaultRequestTimeout)
	}
	return s.fetcher
}

// Check 实时检查（契约 19.3）：强制访问 GitHub，成功或失败都写入缓存后返回视图。
// 只有本地存储或设置读取失败才返回 error；GitHub 侧的失败体现在视图的 status 与 message 中。
func (s *Service) Check(ctx context.Context) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg, err := s.settings.Updates(ctx)
	if err != nil {
		return View{}, err
	}
	repo := cfg.Value.Repo
	next, err := s.load(ctx, repo)
	if err != nil {
		return View{}, err
	}

	release, err := s.fetcherForConfig().Latest(ctx, repo, cfg.Value.GitHubToken)
	if err != nil {
		// 调用方已放弃请求时不把「被取消」写成失败态，避免污染下一次展示。
		if ctxErr := ctx.Err(); ctxErr != nil {
			return View{}, ctxErr
		}
		failure := asFailure(err)
		next.Status = failure.Status
		next.Message = failure.Message
	} else {
		next.Release = &release
		next.CheckedAt = s.now().UTC().Format(time.RFC3339)
		next.Status = StatusOK
		next.Message = ""
		s.logger.Info("检查更新完成", "repo", repo, "latest_version", release.TagName)
	}
	next.Repo = repo
	if err := s.save(ctx, next); err != nil {
		return View{}, err
	}
	return s.view(next, repo), nil
}

// Cached 读取缓存视图（契约 19.3），绝不访问 GitHub。
func (s *Service) Cached(ctx context.Context) (View, error) {
	cfg, err := s.settings.Updates(ctx)
	if err != nil {
		return View{}, err
	}
	rec, err := s.load(ctx, cfg.Value.Repo)
	if err != nil {
		return View{}, err
	}
	return s.view(rec, cfg.Value.Repo), nil
}

// CachedRelease 返回缓存里的最新正式版（含产物清单）与所属仓库；尚无成功记录时 ok=false。
// 它只读缓存、**绝不发出网请求**，供 R13-B 的升级前置检查使用（契约 20.2）。
func (s *Service) CachedRelease(ctx context.Context) (Release, string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg, err := s.settings.Updates(ctx)
	if err != nil {
		return Release{}, "", false, err
	}
	rec, err := s.load(ctx, cfg.Value.Repo)
	if err != nil {
		return Release{}, "", false, err
	}
	if rec.Release == nil {
		return Release{}, cfg.Value.Repo, false, nil
	}
	return *rec.Release, cfg.Value.Repo, true, nil
}

// Platform 返回服务器运行平台（GOOS-GOARCH，契约 19.3）。
func Platform() string { return runtime.GOOS + "-" + runtime.GOARCH }

// MatchesPlatform 判断产物是否为指定平台的发布包（契约 19.3；.sha256 不匹配）。
func MatchesPlatform(name, platform string) bool { return matchesPlatform(name, platform) }

// Start 启动后台定时检查（契约 19.4）：启动后延迟 initialDelay 检查一次，之后每 interval 检查一次。
// 失败只记日志并写入缓存状态，不重试、不弹窗。只应调用一次（重复调用无效果）；Stop 后不可再 Start。
func (s *Service) Start() {
	s.startOnce.Do(func() {
		go s.loop()
	})
}

// Stop 停止后台循环（幂等）。
func (s *Service) Stop() {
	s.stopOnce.Do(func() { close(s.stop) })
}

func (s *Service) loop() {
	defer func() {
		if recovered := recover(); recovered != nil {
			s.logger.Error("检查更新后台任务发生 panic", "panic", recovered)
		}
	}()

	timer := time.NewTimer(s.initialDelay)
	defer timer.Stop()
	select {
	case <-s.stop:
		return
	case <-timer.C:
	}
	s.runIfDue()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.runScheduled()
		}
	}
}

// runIfDue 是启动后的首次检查：缓存缺失或距上次成功已超过一个间隔才出网（契约 19.4），
// 避免每次重启都访问 GitHub。
func (s *Service) runIfDue() {
	ctx, cancel := context.WithTimeout(context.Background(), scheduledCheckTimeout)
	defer cancel()
	cfg, err := s.settings.Updates(ctx)
	if err != nil {
		s.logger.Warn("定时检查更新失败", "error", err)
		return
	}
	rec, err := s.load(ctx, cfg.Value.Repo)
	if err != nil {
		s.logger.Warn("定时检查更新失败", "error", err)
		return
	}
	if checkedAt, parseErr := time.Parse(time.RFC3339, rec.CheckedAt); parseErr == nil && s.now().Sub(checkedAt) < s.interval {
		return
	}
	if _, err := s.Check(ctx); err != nil {
		s.logger.Warn("定时检查更新失败", "error", err)
	}
}

// runScheduled 执行一次定时检查（每个间隔无条件执行）；失败只记日志（Check 已把失败状态写入缓存）。
func (s *Service) runScheduled() {
	ctx, cancel := context.WithTimeout(context.Background(), scheduledCheckTimeout)
	defer cancel()
	if _, err := s.Check(ctx); err != nil {
		s.logger.Warn("定时检查更新失败", "error", err)
	}
}

// load 读取缓存；缓存缺失、损坏或属于其它仓库时视为从未检查（缓存可随时重建，不让页面报错）。
func (s *Service) load(ctx context.Context, repo string) (record, error) {
	setting, err := s.store.Setting(ctx, KeyCheck)
	if errors.Is(err, store.ErrNotFound) {
		return record{}, nil
	}
	if err != nil {
		return record{}, err
	}
	var rec record
	if err := json.Unmarshal([]byte(setting.Value), &rec); err != nil {
		s.logger.Warn("更新检查缓存无法解析，已忽略", "error", err)
		return record{}, nil
	}
	if rec.Repo != repo {
		return record{}, nil
	}
	return rec, nil
}

// save 写入缓存。
func (s *Service) save(ctx context.Context, rec record) error {
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = s.store.UpsertSettingBy(ctx, KeyCheck, string(raw), nil)
	return err
}

// view 把缓存记录组装成对外视图。比较结论在读取时按「当前运行版本」计算，
// 因此升级重启后无需重新检查也能得到正确的比较结论。
func (s *Service) view(rec record, repo string) View {
	platform := Platform()
	view := View{
		CurrentVersion: buildinfo.Version,
		Commit:         buildinfo.Commit,
		BuiltAt:        buildinfo.BuiltAt,
		Status:         rec.Status,
		Message:        rec.Message,
		Source:         sourceGitHub,
		Repo:           repo,
		Platform:       platform,
		Assets:         []AssetView{},
		CheckedAt:      rec.CheckedAt,
	}
	if view.Status == "" {
		view.Status = StatusNotChecked
		view.Message = messageNotChecked
	}
	if rel := rec.Release; rel != nil {
		view.LatestVersion = rel.TagName
		view.ReleaseName = rel.Name
		view.PublishedAt = rel.PublishedAt
		view.Notes = rel.Notes
		view.NotesTruncated = rel.NotesTruncated
		view.HTMLURL = rel.HTMLURL
		for _, asset := range rel.Assets {
			view.Assets = append(view.Assets, AssetView{
				Name:          asset.Name,
				Size:          asset.Size,
				DownloadURL:   asset.DownloadURL,
				PlatformMatch: matchesPlatform(asset.Name, platform),
			})
		}
	}

	result := compareLatest(buildinfo.Version, view.LatestVersion)
	view.Compare = result.compare
	view.HasUpdate = result.hasUpdate
	// 失败态与「尚未检查」沿用记录中的说明；成功态才补充比较结论的说明。
	if view.Status == StatusOK {
		switch result.compare {
		case compareDevBuild:
			view.Message = messageDevBuild
		case compareUnknown:
			view.Message = messageUnknownVersion
		}
	}
	return view
}

// matchesPlatform 判断产物是否为指定平台的发布包（契约 19.3：.sha256 校验文件不标记）。
func matchesPlatform(name, platform string) bool {
	return strings.HasSuffix(name, "-"+platform+".tar.gz") || strings.HasSuffix(name, "-"+platform+".zip")
}

// asFailure 把任意错误归一为 *Failure；未分类的错误按不可达处理。
func asFailure(err error) *Failure {
	var failure *Failure
	if errors.As(err, &failure) {
		return failure
	}
	return &Failure{Status: StatusUnreachable, Message: messageUnreachable, cause: err}
}
