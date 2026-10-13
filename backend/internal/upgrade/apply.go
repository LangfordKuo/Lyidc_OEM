package upgrade

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/buildinfo"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/updates"
)

// plan 是一次升级的执行参数：全部来自前置检查，后台协程只消费它，不再读设置或缓存。
type plan struct {
	job         *Job
	asset       updates.Asset
	checksum    updates.Asset
	downloadURL string
	checksumURL string
	expected    string // .sha256 里的期望摘要（小写十六进制）
	healthURL   string // 新版本的健康检查地址（前置检查阶段已校验非空）
}

// Apply 发起一次升级（契约 20.2 / 20.3）：同步完成全部前置检查与二次确认，落盘状态后立即返回任务视图，
// 下载、校验、解包与交接在后台协程里推进（阶段可经 GET /admin/updates/job 轮询）。
//
// 返回的 *RejectError 表示前置检查拒绝（接口层据此给出 40001 / 40002 / 409）。
func (s *Service) Apply(ctx context.Context, req ApplyRequest) (*JobView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !req.Confirm {
		return nil, reject(RejectInvalidParam, `二次确认缺失：请求体必须带 "confirm": true`)
	}
	target := strings.TrimSpace(req.TargetVersion)
	if target == "" {
		return nil, reject(RejectInvalidParam, "target_version 不能为空")
	}
	if !updates.ValidVersion(target) {
		return nil, reject(RejectInvalidParam, "target_version %q 不是合法的版本号（形如 v1.2.3）", target)
	}

	current := buildinfo.Version
	if updates.IsDevBuild(current) {
		return nil, reject(RejectRule,
			"当前为开发构建（版本 %s），没有可靠版本基线，无法判断升级目标；请手动解包替换并执行迁移", current)
	}

	if job := s.readJob(); job != nil && job.Status == StatusRunning && !s.stale(job) {
		return nil, reject(RejectConflict, "已有升级任务进行中（%s，阶段 %s）", job.ID, job.Phase)
	}

	cfg, err := s.settings.Updates(ctx)
	if err != nil {
		return nil, err
	}
	release, _, ok, err := s.checker.CachedRelease(ctx)
	if err != nil {
		return nil, err
	}
	if !ok || strings.TrimSpace(release.TagName) == "" {
		return nil, reject(RejectRule, "尚未检查更新或没有可用结果，请先「检查更新」再升级")
	}
	if !updates.SameVersion(target, release.TagName) {
		return nil, reject(RejectRule, "目标版本 %s 与最新正式版 %s 不一致，请重新检查更新",
			target, release.TagName)
	}
	if compare, hasUpdate := updates.Compare(current, release.TagName); compare != "ok" || !hasUpdate {
		return nil, reject(RejectRule, "目标版本 %s 不高于当前版本 %s（当前无法比较），不执行升级",
			release.TagName, current)
	}

	platform := updates.Platform()
	asset, found := findPlatformAsset(release.Assets, platform)
	if !found {
		return nil, reject(RejectRule, "发布包缺少本服务器平台（%s）的产物，无法升级", platform)
	}
	checksum, found := findChecksumAsset(release.Assets, asset.Name)
	if !found {
		return nil, reject(RejectRule, "发布包缺少同名 .sha256 校验文件，拒绝升级")
	}

	if err := checkWritable(s.dir); err != nil {
		return nil, reject(RejectRule, "安装目录不可写（%s）：%v", s.dir, err)
	}
	need := uint64(minFreeBytes)
	if asset.Size > 0 && uint64(asset.Size)*2 > need {
		need = uint64(asset.Size) * 2
	}
	if free, err := freeSpace(s.dir); err == nil && free < need {
		return nil, reject(RejectRule, "磁盘空间不足：需要约 %s，可用 %s", humanBytes(need), humanBytes(free))
	}

	// 唯一的前置出网动作：同步取回 .sha256（几百字节）。成功即说明更新源可达，
	// 期望摘要也在此定下，后台下载阶段只做纯粹比对（契约 20.2 第 11 条）。
	downloadBase := cfg.Value.DownloadBase
	checksumURL := resolveDownloadURL(downloadBase, checksum.DownloadURL)
	ctxChecksum, cancelChecksum := context.WithTimeout(ctx, checksumTimeout)
	expected, err := fetchChecksum(ctxChecksum, s.client, checksumURL, asset.Name)
	cancelChecksum()
	if err != nil {
		return nil, reject(RejectRule, "无法连接更新源：%v", err)
	}

	health := healthURL(s.addr)
	if health == "" {
		return nil, reject(RejectRule, "无法由监听地址（%q）推导健康检查地址，拒绝升级", s.addr)
	}

	job := &Job{
		ID:            s.jobID(),
		Status:        StatusRunning,
		Phase:         PhasePreparing,
		Progress:      phaseProgress[PhasePreparing],
		FromVersion:   current,
		TargetVersion: release.TagName,
		StartedAt:     s.now().UTC().Format(time.RFC3339),
		InstallDir:    s.dir,
	}
	// 清理陈旧任务目录（上一次失败或中断留下的；正在运行的 helper 副本删不掉时忽略）。
	workRoot := filepath.Join(s.dir, WorkDirName)
	if err := os.MkdirAll(filepath.Join(workRoot, job.ID), 0o700); err != nil {
		return nil, fmt.Errorf("创建升级工作目录失败：%w", err)
	}
	if err := s.setPhase(job, PhasePreparing); err != nil {
		return nil, fmt.Errorf("写入升级状态失败：%w", err)
	}
	s.logf(PhasePreparing, "升级任务 %s 开始：%s → %s（安装目录 %s，发布包 %s）",
		job.ID, job.FromVersion, job.TargetVersion, job.InstallDir, asset.Name)
	s.cleanStaleWorkDirs(job.ID)

	p := plan{
		job:         job,
		asset:       asset,
		checksum:    checksum,
		downloadURL: resolveDownloadURL(downloadBase, asset.DownloadURL),
		checksumURL: checksumURL,
		expected:    expected,
		healthURL:   health,
	}
	go s.run(p)

	view := s.view(job)
	return &view, nil
}

// run 是后台推进线程：下载 → 校验 → 解包 → 校验发布包 → 交接（契约 20.4 的前半段阶段）。
// 它**不持有** Service.mu（Apply 返回后并发的新请求由状态文件里的 running 判定挡住）。
func (s *Service) run(p plan) {
	job := p.job
	defer func() {
		if recovered := recover(); recovered != nil {
			s.logger.Error("升级任务 panic", "job_id", job.ID, "panic", recovered)
			s.fail(job, fmt.Sprintf("升级过程发生内部错误：%v", recovered))
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout+2*time.Minute)
	defer cancel()

	workDir := filepath.Join(s.dir, WorkDirName, job.ID)
	if err := s.setPhase(job, PhaseDownloading); err != nil {
		s.fail(job, "写入升级状态失败："+err.Error())
		return
	}
	s.logf(PhaseDownloading, "开始下载 %s（%s）", p.asset.Name, humanBytes(uint64(max64(p.asset.Size, 0))))
	archivePath := filepath.Join(workDir, filepath.Base(p.asset.Name))
	written, err := downloadAsset(ctx, s.client, p.downloadURL, archivePath, p.asset.Size, s.progressReporter(job))
	if err != nil {
		s.fail(job, "下载发布包失败："+err.Error())
		return
	}
	s.logf(PhaseDownloading, "下载完成：%s（%s）", p.asset.Name, humanBytes(uint64(written)))

	if err := s.setPhase(job, PhaseVerifying); err != nil {
		s.fail(job, "写入升级状态失败："+err.Error())
		return
	}
	actual, err := fileSHA256(archivePath)
	if err != nil {
		s.fail(job, "计算 sha256 失败："+err.Error())
		return
	}
	if !strings.EqualFold(actual, p.expected) {
		s.logf(PhaseVerifying, "sha256 校验失败：期望 %s，实际 %s", p.expected, actual)
		_ = os.RemoveAll(workDir)
		s.fail(job, "sha256 校验失败：发布包与 .sha256 不一致，已中止且未替换任何文件")
		return
	}
	s.logf(PhaseVerifying, "sha256 校验通过（%s）", actual)

	if err := s.setPhase(job, PhaseExtracting); err != nil {
		s.fail(job, "写入升级状态失败："+err.Error())
		return
	}
	stagedDir := filepath.Join(workDir, "staged")
	if err := extractArchive(archivePath, stagedDir); err != nil {
		s.fail(job, "解包失败："+err.Error())
		return
	}
	s.logf(PhaseExtracting, "解包完成：%s", filepath.Base(stagedDir))

	if err := s.setPhase(job, PhaseStaging); err != nil {
		s.fail(job, "写入升级状态失败："+err.Error())
		return
	}
	payload, err := locatePayload(stagedDir)
	if err != nil {
		s.fail(job, err.Error())
		return
	}
	if err := verifyStagedBinary(ctx, payload.server, job.TargetVersion); err != nil {
		s.fail(job, err.Error())
		return
	}
	s.logf(PhaseStaging, "发布包校验通过：%s / %s / %s 齐全，自报版本与目标一致", payload.server, payload.migrate, payload.dist)

	if err := s.setPhase(job, PhaseHandingOff); err != nil {
		s.fail(job, "写入升级状态失败："+err.Error())
		return
	}
	handoff := Handoff{
		Version:     handoffVersion,
		JobID:       job.ID,
		Dir:         s.dir,
		StatePath:   s.StatePath(),
		LogPath:     s.LogPath(),
		WorkDir:     workDir,
		StagedDir:   payload.root,
		OldPID:      os.Getpid(),
		HealthURL:   p.healthURL,
		ServerArgs:  s.args,
		FromVersion: job.FromVersion,
		Target:      job.TargetVersion,
	}
	helperExe := filepath.Join(workDir, "helper", binaryName(ServerBinary))
	if err := copySelf(helperExe); err != nil {
		s.fail(job, "复制升级辅助进程失败："+err.Error())
		return
	}
	handoffPath := filepath.Join(workDir, "handoff.json")
	if err := writeHandoff(handoffPath, handoff); err != nil {
		s.fail(job, "写入交接文件失败："+err.Error())
		return
	}
	pid, err := s.spawn(ctx, handoff)
	if err != nil {
		s.fail(job, "启动升级辅助进程失败："+err.Error())
		return
	}
	job.HelperPID = pid
	if err := s.writeJob(job); err != nil {
		s.logger.Error("记录辅助进程号失败", "error", err)
	}
	s.logf(PhaseHandingOff, "已交接给辅助进程（pid %d），本进程即将退出以便替换二进制", pid)
	s.Exit()
}

// progressReporter 返回下载进度回调：节流到每 1 秒一次，把进度映射到 downloading 阶段区间，
// 同时刷新 updated_at（长下载不会被误判为「任务中断」，契约 20.4）。
func (s *Service) progressReporter(job *Job) func(done, total int64) {
	start := phaseProgress[PhasePreparing]
	span := phaseProgress[PhaseDownloading] - start
	last := time.Time{}
	return func(done, total int64) {
		if total <= 0 {
			return
		}
		now := s.now()
		if !last.IsZero() && now.Sub(last) < time.Second {
			return
		}
		last = now
		ratio := float64(done) / float64(total)
		if ratio > 1 {
			ratio = 1
		}
		job.Progress = start + int(float64(span)*ratio)
		if err := s.writeJob(job); err != nil {
			s.logger.Warn("刷新升级进度失败", "error", err)
		}
	}
}

// cleanStaleWorkDirs 清理除当前任务外的陈旧工作目录（失败只记日志：可能正被另一个 helper 占用）。
func (s *Service) cleanStaleWorkDirs(currentID string) {
	root := filepath.Join(s.dir, WorkDirName)
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == currentID {
			continue
		}
		stale := filepath.Join(root, entry.Name())
		if err := os.RemoveAll(stale); err != nil {
			s.logger.Warn("清理陈旧升级目录失败", "dir", stale, "error", err)
		}
	}
}

// findPlatformAsset 选出本服务器平台的发布包（契约 19.3 的平台匹配口径）。
func findPlatformAsset(assets []updates.Asset, platform string) (updates.Asset, bool) {
	for _, asset := range assets {
		if updates.MatchesPlatform(asset.Name, platform) {
			return asset, true
		}
	}
	return updates.Asset{}, false
}

// findChecksumAsset 找出与发布包同名的 .sha256 资产（契约 20.2 第 8 条）。
func findChecksumAsset(assets []updates.Asset, assetName string) (updates.Asset, bool) {
	wanted := assetName + ".sha256"
	for _, asset := range assets {
		if asset.Name == wanted {
			return asset, true
		}
	}
	return updates.Asset{}, false
}

// resolveDownloadURL 按契约 20.8 换算下载地址：download_base 非空时用它替换原 URL 的 scheme+host，
// 保留路径（镜像与本地 mock 都能按同一路径布局提供产物）。
func resolveDownloadURL(base, rawURL string) string {
	if strings.TrimSpace(base) == "" {
		return rawURL
	}
	index := strings.Index(rawURL, "://")
	if index < 0 {
		return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(rawURL, "/")
	}
	rest := rawURL[index+3:]
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		return strings.TrimRight(base, "/")
	}
	return strings.TrimRight(base, "/") + rest[slash:]
}

// checkWritable 试写安装目录（契约 20.2 第 9 条）：只创建并删除一个临时文件，不触碰任何既有文件。
func checkWritable(dir string) error {
	probe, err := os.CreateTemp(dir, ".upgrade-probe-*")
	if err != nil {
		return err
	}
	name := probe.Name()
	_ = probe.Close()
	return os.Remove(name)
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
