// Package upgrade 实现主机直装形态的「一键自动升级」（契约 20，R13-B）：
// 下载 → 校验 → 解包 → 交接 → 替换 → 迁移 → 重启 → 健康检查 → 失败回滚。
//
// 两个执行者：
//   - 主进程（旧版服务本身）：前置检查、下载、sha256 校验、解包、落盘状态、分离启动 helper、退出；
//   - helper（**当前二进制的副本**，契约 20.6.1）：等旧进程退出、备份、替换二进制与 dist、
//     执行数据库迁移、拉起新服务、健康检查；失败则还原备份并重新拉起旧版。
//
// 状态与日志都落在**安装目录**（upgrade-state.json / upgrade.log，契约 20.5）：升级会重启进程、
// 替换二进制，迁移还可能改变库结构，只有安装目录下的文件能跨进程、跨版本、在库不可用时读到。
package upgrade

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/updates"
)

// CommandName 是 helper 子命令名：cmd/server 在解析 flag 之前识别它并转交本包（契约 20.6.1）。
const CommandName = "upgrade-helper"

// 安装目录内的固定名字（契约 20.1）。
const (
	// StateFile 是任务状态文件名。
	StateFile = "upgrade-state.json"
	// LogFile 是升级日志文件名。
	LogFile = "upgrade.log"
	// WorkDirName 是任务工作目录名（下载包、解包暂存、helper 副本）。
	WorkDirName = ".upgrade"
	// ServerBinary / MigrateBinary 是被替换的两个二进制（Windows 另有 .exe 后缀）。
	ServerBinary  = "lyidc-server"
	MigrateBinary = "lyidc-migrate"
	// DistDir 是被替换的前端产物目录。
	DistDir = "dist"
	// BackupSuffix 是备份后缀（同目录改名，跨设备安全）。
	BackupSuffix = ".bak"
)

// 任务状态（契约 20.4）。
const (
	StatusRunning    = "running"
	StatusDone       = "done"
	StatusFailed     = "failed"
	StatusRolledBack = "rolled_back"
)

// 阶段（契约 20.4）。
const (
	PhasePreparing   = "preparing"
	PhaseDownloading = "downloading"
	PhaseVerifying   = "verifying"
	PhaseExtracting  = "extracting"
	PhaseStaging     = "staging"
	PhaseHandingOff  = "handing_off"
	PhaseReplacing   = "replacing"
	PhaseMigrating   = "migrating"
	PhaseRestarting  = "restarting"
	PhaseHealthCheck = "health_check"
	PhaseDone        = "done"
	PhaseFailed      = "failed"
	PhaseRolledBack  = "rolled_back"
)

// phaseProgress 是各阶段完成时的进度百分比（契约 20.4 的阶段表）。
var phaseProgress = map[string]int{
	PhasePreparing:   5,
	PhaseDownloading: 20,
	PhaseVerifying:   35,
	PhaseExtracting:  50,
	PhaseStaging:     60,
	PhaseHandingOff:  65,
	PhaseReplacing:   70,
	PhaseMigrating:   78,
	PhaseRestarting:  85,
	PhaseHealthCheck: 92,
	PhaseDone:        100,
}

// phaseLabels 是阶段的中文名（接口随阶段一起下发，前端不再自建映射）。
var phaseLabels = map[string]string{
	PhasePreparing:   "准备",
	PhaseDownloading: "下载发布包",
	PhaseVerifying:   "校验完整性",
	PhaseExtracting:  "解包",
	PhaseStaging:     "校验发布包",
	PhaseHandingOff:  "交接给辅助进程",
	PhaseReplacing:   "替换文件",
	PhaseMigrating:   "执行数据库迁移",
	PhaseRestarting:  "启动新版本",
	PhaseHealthCheck: "健康检查",
	PhaseDone:        "完成",
	PhaseFailed:      "失败",
	PhaseRolledBack:  "已回滚",
}

// 超时与上限（契约 20.2 / 20.6）。
const (
	// checksumTimeout 是同步拉取 .sha256 的超时（唯一的前置出网动作）。
	checksumTimeout = 10 * time.Second
	// downloadTimeout 是发布包下载的整体超时；maxDownloadBytes 是下载上限。
	downloadTimeout  = 10 * time.Minute
	maxDownloadBytes = 512 << 20
	// maxExtractBytes / maxExtractFileBytes 是解包的总量与单文件上限。
	maxExtractBytes     = 1 << 30
	maxExtractFileBytes = 512 << 20
	// waitExitTimeout 是 helper 等待旧进程退出的上限。
	waitExitTimeout = 60 * time.Second
	// migrateTimeout 是 helper 执行迁移的上限。
	migrateTimeout = 10 * time.Minute
	// healthTimeout 是 helper 等待新版本健康的合计时间；rollbackHealthTimeout 是回滚后等待旧版的时间。
	healthTimeout         = 60 * time.Second
	rollbackHealthTimeout = 30 * time.Second
	healthInterval        = time.Second
	// staleRunningAfter 是「running 但 helper 已消失」的判定阈值（契约 20.4）。
	staleRunningAfter = 10 * time.Minute
	// exitDelay 是 202 响应写出后主进程退出前的等待（契约 20.3）。
	exitDelay = 500 * time.Millisecond
	// minFreeBytes 是磁盘空间下限：发布包大小的两倍与 512 MiB 取大（契约 20.2 第 10 条）。
	minFreeBytes = 512 << 20
	// logTailLines 是接口返回的日志行数上限；logLineMax 是单行长度上限。
	logTailLines = 100
	logLineMax   = 500
)

// RejectKind 是前置检查拒绝的分类，决定接口的错误码（契约 20.3）。
type RejectKind int

const (
	// RejectInvalidParam 参数非法（40001）。
	RejectInvalidParam RejectKind = iota
	// RejectRule 业务规则不满足（40002）。
	RejectRule
	// RejectConflict 已有任务进行中（409）。
	RejectConflict
)

// RejectError 是前置检查的拒绝结果；Message 面向管理员且不含任何密钥。
type RejectError struct {
	Kind    RejectKind
	Message string
}

func (e *RejectError) Error() string { return e.Message }

func reject(kind RejectKind, format string, args ...any) *RejectError {
	return &RejectError{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

// Checker 提供「最近一次成功检查到的正式版」；生产实现是 *updates.Service。
type Checker interface {
	// CachedRelease 返回缓存的正式版与所属仓库；尚无成功记录时 ok=false，绝不发出网请求。
	CachedRelease(ctx context.Context) (release updates.Release, repo string, ok bool, err error)
}

// SettingsReader 读取更新源设置（仓库、Token、两个地址注入项）。
type SettingsReader interface {
	Updates(ctx context.Context) (settings.UpdatesState, error)
}

// ApplyRequest 是 POST /api/v1/admin/updates/apply 的请求体（契约 20.3）。
type ApplyRequest struct {
	Confirm       bool   `json:"confirm"`
	TargetVersion string `json:"target_version"`
}

// Job 是任务的持久化结构（契约 20.5）：它就是 upgrade-state.json 的内容。
type Job struct {
	ID            string `json:"job_id"`
	Status        string `json:"status"`
	Phase         string `json:"phase"`
	Progress      int    `json:"progress"`
	FromVersion   string `json:"from_version"`
	TargetVersion string `json:"target_version"`
	StartedAt     string `json:"started_at"`
	FinishedAt    string `json:"finished_at"`
	UpdatedAt     string `json:"updated_at"`
	InstallDir    string `json:"install_dir"`
	Message       string `json:"message"`
	RolledBack    bool   `json:"rolled_back"`
	DBMigrated    bool   `json:"db_migrated"`
	// HelperPID 是 helper 进程号（0 表示尚未交接）；读取时用存活判定排除「迁移耗时较长」的误判。
	HelperPID int `json:"helper_pid"`
}

// Done 判断任务是否已结束（done / failed / rolled_back）。
func (j Job) Done() bool { return j.Status != StatusRunning }

// JobView 是任务对外的视图（契约 20.4）：Job 的字段平铺 + 阶段名 + 日志尾部。
type JobView struct {
	Job
	PhaseLabel   string   `json:"phase_label"`
	LogTail      []string `json:"log_tail"`
	LogTruncated bool     `json:"log_truncated"`
}

// Options 是 Service 的构造参数。
type Options struct {
	// Dir 是安装目录（契约 20.1）；空时取当前可执行文件的父目录。
	Dir string
	// Addr 是 HTTP 监听地址（如 0.0.0.0:8080），用于推导健康检查地址。
	Addr string
	// Args 是旧进程的启动参数，原样传给新进程（保证 --config 等继续生效）；空时取 os.Args[1:]。
	Args []string
	// Checker 提供缓存的最新正式版（生产为 *updates.Service）。
	Checker Checker
	// Settings 读取更新源设置（download_base 等）。
	Settings SettingsReader
	Logger   *slog.Logger
	// Now 注入时钟（测试用）；nil 时取 time.Now。
	Now func() time.Time
	// Exit 退出主进程（测试注入空实现）；nil 时取 os.Exit。
	Exit func(int)
	// Client 是下载与校验文件用的 HTTP 客户端；nil 时按超时构造（绝不携带 Token）。
	Client *http.Client
	// Spawn 是「复制 helper 并分离启动」的实现（测试注入假实现）；nil 时用生产实现。
	Spawn func(ctx context.Context, h Handoff) (int, error)
}

// Service 负责发起升级、落盘状态与对外提供任务视图。
type Service struct {
	dir       string
	addr      string
	args      []string
	checker   Checker
	settings  SettingsReader
	logger    *slog.Logger
	now       func() time.Time
	exit      func(int)
	client    *http.Client
	spawn     func(ctx context.Context, h Handoff) (int, error)
	logWriter io.Writer // upgrade.log 的追加写入器（可注入，测试用内存缓冲）

	// mu 让同一进程内至多有一个升级任务在推进（跨进程由状态文件的 running 判定兜底）。
	mu sync.Mutex
}

// NewService 构造升级服务。安装目录不存在或不可定位时返回错误（接口层按内部错误上报）。
func NewService(opts Options) (*Service, error) {
	dir := opts.Dir
	if dir == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("无法定位安装目录（os.Executable）：%w", err)
		}
		dir = filepath.Dir(exe)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("安装目录不是合法路径：%w", err)
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	exit := opts.Exit
	if exit == nil {
		exit = os.Exit
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: downloadTimeout}
	}
	spawn := opts.Spawn
	if spawn == nil {
		spawn = spawnHelper
	}
	args := opts.Args
	if args == nil {
		args = append([]string(nil), os.Args[1:]...)
	}
	return &Service{
		dir:      abs,
		addr:     opts.Addr,
		args:     args,
		checker:  opts.Checker,
		settings: opts.Settings,
		logger:   logger,
		now:      now,
		exit:     exit,
		client:   client,
		spawn:    spawn,
	}, nil
}

// Dir 返回安装目录（绝对路径）。
func (s *Service) Dir() string { return s.dir }

// StatePath 返回状态文件路径。
func (s *Service) StatePath() string { return filepath.Join(s.dir, StateFile) }

// LogPath 返回升级日志路径。
func (s *Service) LogPath() string { return filepath.Join(s.dir, LogFile) }

// Exit 启动「202 响应写出后退出主进程」的延时退出（契约 20.3）。测试注入的 Exit 为空实现。
func (s *Service) Exit() {
	time.AfterFunc(exitDelay, func() { s.exit(0) })
}

// binaryName 返回当前平台的可执行文件名（Windows 带 .exe）。
func binaryName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

// healthURL 由监听地址推导健康检查地址（契约 20.6.2）：通配地址探回环。
func healthURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = "", ""
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	if port == "" {
		return ""
	}
	return "http://" + net.JoinHostPort(host, port) + "/api/v1/health"
}

// appendLog 向 upgrade.log 追加一行（契约 20.5）：主进程与 helper 共写，时间与阶段由调用方给出。
// 日志**不含 Token**：调用方只传 URL、阶段与错误。
func (s *Service) appendLog(line string) {
	if s.logWriter != nil {
		fmt.Fprintln(s.logWriter, line)
		return
	}
	file, err := os.OpenFile(s.LogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		s.logger.Warn("写入升级日志失败", "error", err)
		return
	}
	defer func() { _ = file.Close() }()
	fmt.Fprintln(file, line)
}

// logf 追加一行带时间戳与阶段的日志。
func (s *Service) logf(phase, format string, args ...any) {
	s.appendLog(fmt.Sprintf("%s %s %s", s.now().UTC().Format(time.RFC3339), phase,
		fmt.Sprintf(format, args...)))
}

// readJob 读取状态文件；不存在返回 nil；内容损坏按「无任务」处理并告警（不阻断接口）。
func (s *Service) readJob() *Job {
	raw, err := readFileWithRetry(s.StatePath(), readRetryTimeout)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		s.logger.Warn("读取升级状态失败", "error", err)
		return nil
	}
	job, err := decodeJob(raw)
	if err != nil {
		s.logger.Warn("升级状态文件无法解析，已忽略", "error", err)
		return nil
	}
	return job
}

// writeJob 原子写状态文件（同目录临时文件 + rename），权限 0600（契约 20.5）。
func (s *Service) writeJob(job *Job) error {
	if job.ID == "" {
		return errors.New("状态缺少任务号")
	}
	job.UpdatedAt = s.now().UTC().Format(time.RFC3339)
	raw, err := encodeJob(job)
	if err != nil {
		return err
	}
	tmp := tempStatePath(s.StatePath())
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := renameWithRetry(tmp, s.StatePath(), removeRetryTimeout); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// setPhase 推进阶段（含进度）并落盘。
func (s *Service) setPhase(job *Job, phase string) error {
	job.Phase = phase
	if progress, ok := phaseProgress[phase]; ok {
		job.Progress = progress
	}
	return s.writeJob(job)
}

// fail 把任务置为失败并落盘（message 已脱敏）。
func (s *Service) fail(job *Job, message string) {
	job.Status = StatusFailed
	job.Phase = PhaseFailed
	job.Message = message
	job.FinishedAt = s.now().UTC().Format(time.RFC3339)
	job.HelperPID = 0
	if err := s.writeJob(job); err != nil {
		s.logger.Error("写入升级失败状态出错", "error", err)
	}
	s.logf(PhaseFailed, "升级失败：%s", message)
}

// jobID 生成任务号：UTC 时间戳 + 4 字节随机十六进制（同时作为 .upgrade 下的目录名）。
func (s *Service) jobID() string {
	return s.now().UTC().Format("20060102T150405Z") + "-" + randomHex(4)
}

// view 把 Job 组装成对外视图（含日志尾部）。
func (s *Service) view(job *Job) JobView {
	label := phaseLabels[job.Phase]
	if label == "" {
		label = job.Phase
	}
	tail, truncated := s.logTail()
	return JobView{
		Job:          *job,
		PhaseLabel:   label,
		LogTail:      tail,
		LogTruncated: truncated,
	}
}

// logTail 读取 upgrade.log 末尾的 logTailLines 行（每行按 logLineMax 字符截断）。
func (s *Service) logTail() ([]string, bool) {
	raw, err := os.ReadFile(s.LogPath())
	if err != nil {
		return []string{}, false
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return []string{}, false
	}
	truncated := false
	if len(lines) > logTailLines {
		lines = lines[len(lines)-logTailLines:]
		truncated = true
	}
	tail := make([]string, 0, len(lines))
	for _, line := range lines {
		if len(line) > logLineMax {
			line = line[:logLineMax] + "…"
			truncated = true
		}
		tail = append(tail, line)
	}
	return tail, truncated
}

// Job 返回最近一次任务的视图（契约 20.4）：从未升级过返回 nil。
// 读取时顺带做陈旧任务判定：running 但 helper 已消失且超过阈值 → 落盘为失败。
func (s *Service) Job(_ context.Context) (*JobView, error) {
	job := s.readJob()
	if job == nil {
		return nil, nil
	}
	if job.Status == StatusRunning && s.stale(job) {
		s.logger.Warn("升级任务疑似中断", "job_id", job.ID, "phase", job.Phase, "updated_at", job.UpdatedAt)
		s.fail(job, "升级辅助进程已退出，任务中断；请检查 upgrade.log 与安装目录备份")
	}
	view := s.view(job)
	return &view, nil
}

// stale 判断 running 任务是否已经中断：helper 还活着就不算（迁移可能耗时较长）。
func (s *Service) stale(job *Job) bool {
	if job.HelperPID > 0 && processAlive(job.HelperPID) {
		return false
	}
	updated, err := time.Parse(time.RFC3339, job.UpdatedAt)
	if err != nil {
		return false
	}
	return s.now().Sub(updated) > staleRunningAfter
}
