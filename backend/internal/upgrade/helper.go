package upgrade

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// HelperOptions 注入 helper 的外部动作（生产用 DefaultHelperOptions，测试注入假实现）。
type HelperOptions struct {
	Now func() time.Time
	// WaitExit 等待旧进程退出（按 pid，超时返回错误）。
	WaitExit func(pid int, timeout time.Duration) error
	// Migrate 用新包里的 lyidc-migrate 执行一次迁移。
	Migrate func(ctx context.Context, dir, exe string, args []string, timeout time.Duration, log io.Writer) error
	// Start 分离启动一个服务进程并返回 pid。
	Start func(dir, exe string, args []string, log io.Writer) (int, error)
	// Health 轮询健康检查地址直到通过或超时。
	Health func(ctx context.Context, url string, timeout time.Duration) error
	// Kill 结束一个进程（回滚新版本时使用）。
	Kill func(pid int) error
	// Settle 是替换文件前的静置时长（等操作系统释放文件句柄）。
	Settle time.Duration
}

// DefaultHelperOptions 返回生产实现。
func DefaultHelperOptions() HelperOptions {
	return HelperOptions{
		Now:      time.Now,
		WaitExit: waitForExit,
		Migrate:  runMigrateCommand,
		Start:    startServerProcess,
		Health:   pollHealth,
		Kill:     killProcess,
		Settle:   settleBeforeReplace,
	}
}

// settleBeforeReplace 是替换文件前的静置时长：Windows 上进程退出到句柄真正释放之间有一个窗口。
const settleBeforeReplace = 500 * time.Millisecond

// replaceSettleAfterKill 是结束新进程后、回滚替换文件前的额外静置（同上）。
const replaceSettleAfterKill = 300 * time.Millisecond

// RunHelperCommand 是 `lyidc-server upgrade-helper <handoff.json>` 的入口（cmd/server 调用），
// 返回进程退出码。它不读配置、不连数据库、不监听端口。
func RunHelperCommand(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "用法: lyidc-server "+CommandName+" <handoff.json>")
		return 2
	}
	h, err := readHandoff(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取交接文件失败: %v\n", err)
		return 1
	}
	return RunHelper(h, DefaultHelperOptions())
}

// RunHelper 执行 helper 的全部工作（契约 20.6.2 / 20.6.3）：返回 0 表示升级成功；
// 返回 1 表示失败或已回滚（两者的区别体现在状态文件里，供接口与页面展示）。
func RunHelper(h Handoff, opts HelperOptions) int {
	r := &helperRun{h: h, opts: opts, now: opts.Now}
	if r.now == nil {
		r.now = time.Now
	}
	defer r.closeLog()
	r.openLog()
	r.logf("helper", "辅助进程已启动（任务 %s，安装目录 %s）", h.JobID, h.Dir)
	r.loadJob()

	if h.OldPID > 0 {
		r.logf("helper", "等待旧进程（pid %d）退出…", h.OldPID)
		if err := r.opts.WaitExit(h.OldPID, time.Duration(h.WaitExitSeconds)*time.Second); err != nil {
			r.finish(StatusFailed, "等待旧进程退出超时，已中止且未替换任何文件", false)
			r.logf("helper", "等待旧进程退出超时，任务中止")
			return 1
		}
	}
	time.Sleep(r.opts.Settle)

	payload, err := locatePayload(h.StagedDir)
	if err != nil {
		r.finish(StatusFailed, "暂存目录中的发布包结构不完整："+err.Error(), false)
		return 1
	}

	if err := r.setPhase(PhaseReplacing); err != nil {
		r.finish(StatusFailed, "写入升级状态失败："+err.Error(), false)
		return 1
	}
	if err := r.backupAndReplace(payload); err != nil {
		r.logf(PhaseReplacing, "替换文件失败：%v", err)
		if restoreErr := r.restore(); restoreErr != nil {
			r.finish(StatusFailed, "替换文件失败（"+err.Error()+"），且还原备份失败："+restoreErr.Error()+"；请人工介入并检查 upgrade.log", true)
			return 1
		}
		r.finish(StatusFailed, "替换文件失败："+err.Error()+"（已还原备份，未启动新版本）", false)
		return 1
	}
	r.logf(PhaseReplacing, "已备份并替换 %s / %s / %s",
		binaryName(ServerBinary), binaryName(MigrateBinary), DistDir)

	if err := r.setPhase(PhaseMigrating); err != nil {
		r.finish(StatusFailed, "写入升级状态失败："+err.Error(), false)
		return 1
	}
	migrateExe := filepath.Join(h.Dir, binaryName(MigrateBinary))
	r.logf(PhaseMigrating, "执行数据库迁移：%s up", binaryName(MigrateBinary))
	if err := r.opts.Migrate(context.Background(), h.Dir, migrateExe, migrateArgs(h.ServerArgs),
		time.Duration(h.MigrateSeconds)*time.Second, r.logWriter); err != nil {
		r.logf(PhaseMigrating, "数据库迁移失败：%v", err)
		return r.rollback("数据库迁移失败："+err.Error(), false)
	}
	r.job.DBMigrated = true
	_ = r.saveJob()
	r.logf(PhaseMigrating, "数据库迁移完成")

	if err := r.setPhase(PhaseRestarting); err != nil {
		r.finish(StatusFailed, "写入升级状态失败："+err.Error(), false)
		return 1
	}
	serverExe := filepath.Join(h.Dir, binaryName(ServerBinary))
	pid, err := r.opts.Start(h.Dir, serverExe, h.ServerArgs, r.logWriter)
	if err != nil {
		r.logf(PhaseRestarting, "启动新版本失败：%v", err)
		return r.rollback("启动新版本失败："+err.Error(), false)
	}
	r.newPID = pid
	r.logf(PhaseRestarting, "新版本已启动（pid %d），开始健康检查", pid)

	if err := r.setPhase(PhaseHealthCheck); err != nil {
		r.finish(StatusFailed, "写入升级状态失败："+err.Error(), false)
		return 1
	}
	if err := r.opts.Health(context.Background(), h.HealthURL,
		time.Duration(h.HealthSeconds)*time.Second); err != nil {
		r.logf(PhaseHealthCheck, "健康检查未通过：%v", err)
		r.stopNewProcess()
		return r.rollback("新版本健康检查未通过："+err.Error(), true)
	}
	r.logf(PhaseHealthCheck, "健康检查通过（%s）", h.HealthURL)

	r.finish(StatusDone, "", false)
	r.logf(PhaseDone, "升级完成：%s → %s", h.FromVersion, h.Target)
	r.cleanup()
	return 0
}

// stateRetryInterval 是状态文件读写重试的间隔：Windows 上「打开不带 FILE_SHARE_DELETE」
// 使并发读与原子替换（rename）互相短暂阻塞，用短间隔重试即可平滑解决。
const stateRetryInterval = 25 * time.Millisecond

// readRetryTimeout 是读取状态文件的重试窗口（读取方是接口轮询，宁可稍等也不要误报「没有任务」）。
const readRetryTimeout = time.Second

// readFileWithRetry 读取文件并容忍「文件正在被替换」的瞬时失败；文件确实不存在时立即返回。
func readFileWithRetry(path string, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	for {
		raw, err := os.ReadFile(path)
		if err == nil {
			return raw, nil
		}
		if errors.Is(err, os.ErrNotExist) || time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(stateRetryInterval)
	}
}

// removeRetryTimeout 是「删除可能仍被刚结束的进程占用的文件」的重试窗口。
//
// 必要性（Windows 实测）：TerminateProcess 之后进程立刻不可见（processAlive=false），但内核释放
// 可执行映像映射是异步的，紧接着删除该 exe 会短暂返回「Access is denied」（实测约 300ms 后消失）。
// 回滚路径必须容忍这段窗口，否则会出现「进程已结束却回滚不了」的假失败（契约 20.6.3）。
const removeRetryTimeout = 5 * time.Second

// removeWithRetry 删除文件或目录；被暂时占用时按 200ms 重试到窗口结束。
func removeWithRetry(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := os.RemoveAll(path)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(stateRetryInterval)
	}
}

// renameWithRetry 原子替换文件；Windows 上目标文件正在被读取时 rename 可能短暂返回
// 「Access is denied」（状态文件既被接口读、又被主进程与 helper 写），按 200ms 重试到窗口结束。
// Unix 上覆盖式 rename 本就是原子的，第一次即成功。
func renameWithRetry(oldPath, newPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := os.Rename(oldPath, newPath)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(stateRetryInterval)
	}
}

// tempStatePath 返回状态文件的临时名：带 pid，避免主进程与 helper 同时写时互相踩到对方的临时文件。
func tempStatePath(path string) string {
	return fmt.Sprintf("%s.tmp-%d", path, os.Getpid())
}

// helperRun 是 helper 的运行状态。
type helperRun struct {
	h         Handoff
	opts      HelperOptions
	now       func() time.Time
	job       *Job
	logWriter *os.File
	newPID    int
	// replaced 记录替换步骤是否已经动手（回滚与清理据此判断）。
	replaced bool
}

func (r *helperRun) openLog() {
	file, err := os.OpenFile(r.h.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	r.logWriter = file
}

func (r *helperRun) closeLog() {
	if r.logWriter != nil {
		_ = r.logWriter.Close()
	}
}

// logf 追加一行带时间与来源的日志（来源为阶段名或 helper）。
func (r *helperRun) logf(source, format string, args ...any) {
	line := fmt.Sprintf("%s %s %s", r.now().UTC().Format(time.RFC3339), source, fmt.Sprintf(format, args...))
	if r.logWriter != nil {
		fmt.Fprintln(r.logWriter, line)
		return
	}
	fmt.Fprintln(os.Stderr, line)
}

// loadJob 读取主进程落盘的任务状态；缺失或损坏时用交接信息兜底构造，保证结果一定能被记录。
func (r *helperRun) loadJob() {
	raw, err := readFileWithRetry(r.h.StatePath, readRetryTimeout)
	if err == nil {
		if job, decodeErr := decodeJob(raw); decodeErr == nil {
			r.job = job
			return
		}
		r.logf("helper", "状态文件无法解析，按交接信息重建：%v", err)
	}
	r.job = &Job{
		ID:            r.h.JobID,
		Status:        StatusRunning,
		Phase:         PhaseHandingOff,
		Progress:      phaseProgress[PhaseHandingOff],
		FromVersion:   r.h.FromVersion,
		TargetVersion: r.h.Target,
		StartedAt:     r.now().UTC().Format(time.RFC3339),
		InstallDir:    r.h.Dir,
	}
}

// saveJob 原子写状态文件（与主进程同一套语义，契约 20.5）。
func (r *helperRun) saveJob() error {
	r.job.UpdatedAt = r.now().UTC().Format(time.RFC3339)
	raw, err := encodeJob(r.job)
	if err != nil {
		return err
	}
	tmp := tempStatePath(r.h.StatePath)
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := renameWithRetry(tmp, r.h.StatePath, removeRetryTimeout); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// setPhase 推进阶段并落盘。
func (r *helperRun) setPhase(phase string) error {
	r.job.Status = StatusRunning
	r.job.Phase = phase
	if progress, ok := phaseProgress[phase]; ok {
		r.job.Progress = progress
	}
	return r.saveJob()
}

// finish 写入终态（done / failed / rolled_back）。
func (r *helperRun) finish(status, message string, rolledBack bool) {
	job := r.job
	job.Status = status
	job.Message = message
	job.RolledBack = rolledBack
	job.FinishedAt = r.now().UTC().Format(time.RFC3339)
	job.HelperPID = 0
	switch status {
	case StatusDone:
		job.Phase = PhaseDone
		job.Progress = phaseProgress[PhaseDone]
	case StatusRolledBack:
		job.Phase = PhaseRolledBack
	case StatusFailed:
		job.Phase = PhaseFailed
	}
	if err := r.saveJob(); err != nil {
		r.logf("helper", "写入最终状态失败：%v", err)
	}
}

// backupPaths 返回「原名 → 备份名」的三组路径（契约 20.6.2 第 2 步）。
func (r *helperRun) backupPaths() []string {
	return []string{
		filepath.Join(r.h.Dir, binaryName(ServerBinary)),
		filepath.Join(r.h.Dir, binaryName(MigrateBinary)),
		filepath.Join(r.h.Dir, DistDir),
	}
}

// payloadPaths 返回发布包内对应的三个待替换对象。
func (r *helperRun) payloadPaths(p payload) []string { return []string{p.server, p.migrate, p.dist} }

// backupAndReplace 备份并替换二进制与 dist（契约 20.6.2 第 2、3 步）。
// 要求三个对象在安装目录里都存在（缺一即失败且**不做任何改动**），避免出现半套状态。
func (r *helperRun) backupAndReplace(p payload) error {
	targets := r.backupPaths()
	sources := r.payloadPaths(p)
	for _, target := range targets {
		if _, err := os.Stat(target); err != nil {
			return fmt.Errorf("安装目录里缺少 %s，无法备份与替换", filepath.Base(target))
		}
	}

	// 清理上一轮遗留的备份（删除失败不阻断：后面还会 rename 覆盖）。
	for _, target := range targets {
		_ = removeWithRetry(target+BackupSuffix, removeRetryTimeout)
	}
	// 先改名旧文件（Windows 上运行中的 .exe 不能覆盖，只能改名）。
	backedUp := make([]string, 0, len(targets))
	for _, target := range targets {
		if err := os.Rename(target, target+BackupSuffix); err != nil {
			for _, done := range backedUp {
				_ = os.Rename(done+BackupSuffix, done)
			}
			return fmt.Errorf("备份 %s 失败：%w", filepath.Base(target), err)
		}
		backedUp = append(backedUp, target)
	}
	// 再移入新文件。
	for index, target := range targets {
		if err := os.Rename(sources[index], target); err != nil {
			// 尽力还原：新文件已移入的部分由 restore 处理。
			r.replaced = true
			return fmt.Errorf("移入新 %s 失败：%w", filepath.Base(target), err)
		}
	}
	r.replaced = true
	return nil
}

// restore 还原备份（契约 20.6.3 的回滚步骤）。
func (r *helperRun) restore() error {
	var failures []string
	for _, target := range r.backupPaths() {
		backup := target + BackupSuffix
		if _, err := os.Stat(backup); err != nil {
			continue
		}
		if err := removeWithRetry(target, removeRetryTimeout); err != nil {
			failures = append(failures, fmt.Sprintf("移开新 %s 失败：%v", filepath.Base(target), err))
			continue
		}
		if err := os.Rename(backup, target); err != nil {
			failures = append(failures, fmt.Sprintf("还原 %s 失败：%v", filepath.Base(target), err))
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "；"))
	}
	r.replaced = false
	return nil
}

// stopNewProcess 结束刚启动的新进程（回滚前必须先放掉文件句柄）。
func (r *helperRun) stopNewProcess() {
	if r.newPID <= 0 {
		return
	}
	if err := r.opts.Kill(r.newPID); err != nil {
		r.logf(PhaseRolledBack, "结束新进程（pid %d）失败：%v", r.newPID, err)
	}
	deadline := r.now().Add(5 * time.Second)
	for processAlive(r.newPID) && r.now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	r.newPID = 0
	// 进程「不可见」不等于文件已可删除（见 removeRetryTimeout 说明）：替换文件前再静置一下。
	time.Sleep(replaceSettleAfterKill)
}

// rollback 回滚到旧版（契约 20.6.3）：结束新进程 → 还原备份 → 拉起旧版 → 健康检查。
// 返回进程退出码（恒为 1：无论回滚是否成功，本次升级都算失败）。
func (r *helperRun) rollback(reason string, newStarted bool) int {
	if newStarted {
		r.stopNewProcess()
	}
	if err := r.restore(); err != nil {
		r.finish(StatusFailed, "升级失败（"+reason+"），且还原备份失败："+err.Error()+"；请人工介入并检查 upgrade.log", true)
		r.logf(PhaseFailed, "回滚失败：%v", err)
		return 1
	}
	r.logf(PhaseRolledBack, "已还原备份，重新拉起旧版本（%s）", r.h.FromVersion)

	serverExe := filepath.Join(r.h.Dir, binaryName(ServerBinary))
	pid, err := r.opts.Start(r.h.Dir, serverExe, r.h.ServerArgs, r.logWriter)
	if err == nil {
		r.logf(PhaseRolledBack, "旧版本已启动（pid %d），进行健康检查", pid)
		err = r.opts.Health(context.Background(), r.h.HealthURL,
			time.Duration(r.h.RollbackHealthSeconds)*time.Second)
	}
	if err != nil {
		r.finish(StatusFailed, "升级失败（"+reason+"），备份已还原但旧版本未能恢复："+err.Error()+"；请人工介入并检查 upgrade.log", true)
		r.logf(PhaseFailed, "旧版本恢复失败：%v", err)
		return 1
	}
	r.finish(StatusRolledBack, reason, true)
	r.logf(PhaseRolledBack, "已回滚到 %s 并确认服务可用", r.h.FromVersion)
	return 1
}

// cleanup 成功后清理备份与工作目录（helper 自身副本在 Windows 上删不掉时忽略，契约 20.6.2 第 7 步）。
func (r *helperRun) cleanup() {
	for _, target := range r.backupPaths() {
		if err := removeWithRetry(target+BackupSuffix, removeRetryTimeout); err != nil {
			r.logf(PhaseDone, "清理备份 %s 失败：%v", filepath.Base(target), err)
		}
	}
	if err := os.RemoveAll(r.h.WorkDir); err != nil {
		r.logf(PhaseDone, "清理工作目录失败（可后续手动删除）：%v", err)
	}
}

// migrateArgs 从旧进程参数里透传配置路径（cmd/migrate 与 cmd/server 共用 -config 口径）。
func migrateArgs(serverArgs []string) []string {
	for index, arg := range serverArgs {
		value, ok := strings.CutPrefix(arg, "-config=")
		if ok {
			return []string{"-config", value}
		}
		if arg == "-config" || arg == "--config" {
			if index+1 < len(serverArgs) {
				return []string{"-config", serverArgs[index+1]}
			}
		}
	}
	return nil
}

// runMigrateCommand 用新二进制执行一次 `up`（生产实现）。
func runMigrateCommand(ctx context.Context, dir, exe string, args []string, timeout time.Duration, log io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, append(args, "up")...)
	cmd.Dir = dir
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("迁移超时（%s）", timeout)
		}
		return err
	}
	return nil
}

// startServerProcess 分离启动一个服务进程（生产实现）：不 Wait，stdout/stderr 进升级日志。
func startServerProcess(dir, exe string, args []string, log io.Writer) (int, error) {
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.SysProcAttr = detachAttrs()
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	return pid, nil
}

// pollHealth 轮询健康检查地址（契约 20.6.2 第 6 步）：HTTP 200 且响应包 code=0 才算通过。
func pollHealth(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 5 * time.Second}
	last := "服务尚未响应"
	for {
		if err := probeHealth(ctx, client, url); err == nil {
			return nil
		} else {
			last = err.Error()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("健康检查超时（%s）：%s", timeout, last)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("健康检查被取消：%s", last)
		case <-time.After(healthInterval):
		}
	}
}

// probeHealth 探测一次健康检查地址。
func probeHealth(ctx context.Context, client *http.Client, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent())
	resp, err := client.Do(req)
	if err != nil {
		return describeHTTPError(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("健康检查返回 HTTP %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), `"code":0`) {
		return errors.New("健康检查响应包 code 非 0")
	}
	return nil
}
