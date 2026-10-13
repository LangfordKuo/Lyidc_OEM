package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/updates"
)

// handoffVersion 是交接文件的结构版本（helper 校验，便于将来演进）。
const handoffVersion = 1

// versionProbeTimeout 是执行「暂存二进制 -version」的超时（契约 20.7 第 5 条）。
const versionProbeTimeout = 30 * time.Second

// Handoff 是主进程写给 helper 的交接文件（契约 20.6.1）：helper 不读配置、不查库，只按它干活。
type Handoff struct {
	Version   int    `json:"version"`
	JobID     string `json:"job_id"`
	Dir       string `json:"dir"`
	StatePath string `json:"state_path"`
	LogPath   string `json:"log_path"`
	WorkDir   string `json:"work_dir"`
	StagedDir string `json:"staged_dir"`
	// OldPID 是旧进程号（helper 等它退出后再替换文件）。
	OldPID int `json:"old_pid"`
	// HealthURL 是新版本的 /api/v1/health 地址（由监听地址推导，契约 20.6.2）。
	HealthURL string `json:"health_url"`
	// ServerArgs 是旧进程的启动参数，原样传给新进程。
	ServerArgs  []string `json:"server_args"`
	FromVersion string   `json:"from_version"`
	Target      string   `json:"target_version"`

	WaitExitSeconds       int `json:"wait_exit_seconds"`
	MigrateSeconds        int `json:"migrate_seconds"`
	HealthSeconds         int `json:"health_seconds"`
	RollbackHealthSeconds int `json:"rollback_health_seconds"`
}

// normalize 补齐缺省超时（交接文件可人工核对，缺项不至于让 helper 卡死）。
func (h *Handoff) normalize() {
	if h.WaitExitSeconds <= 0 {
		h.WaitExitSeconds = int(waitExitTimeout / time.Second)
	}
	if h.MigrateSeconds <= 0 {
		h.MigrateSeconds = int(migrateTimeout / time.Second)
	}
	if h.HealthSeconds <= 0 {
		h.HealthSeconds = int(healthTimeout / time.Second)
	}
	if h.RollbackHealthSeconds <= 0 {
		h.RollbackHealthSeconds = int(rollbackHealthTimeout / time.Second)
	}
}

// writeHandoff 原子写交接文件。
func writeHandoff(path string, h Handoff) error {
	h.Version = handoffVersion
	h.normalize()
	raw, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// readHandoff 读取并校验交接文件。
func readHandoff(path string) (Handoff, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Handoff{}, err
	}
	var h Handoff
	if err := json.Unmarshal(raw, &h); err != nil {
		return Handoff{}, fmt.Errorf("交接文件不是合法 JSON：%w", err)
	}
	if h.Dir == "" || h.StatePath == "" || h.LogPath == "" || h.StagedDir == "" || h.HealthURL == "" {
		return Handoff{}, errors.New("交接文件缺少必要字段")
	}
	h.normalize()
	return h, nil
}

// copySelf 把**当前正在运行的二进制**复制一份当 helper（契约 20.6.1 第 3 步）：
// 复制品不在替换范围内（Windows 上也能安全替换安装目录里的原文件），且不新增发布产物。
func copySelf(dst string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	source, err := os.Open(self)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	target, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(target, source); err != nil {
		_ = target.Close()
		return err
	}
	return target.Close()
}

// spawnHelper 是生产的 helper 启动实现（契约 20.6.1 第 4 步）：分离进程、不做 Wait、
// stdout/stderr 追加到升级日志。
func spawnHelper(ctx context.Context, h Handoff) (int, error) {
	helperExe := filepath.Join(h.WorkDir, "helper", binaryName(ServerBinary))
	if !isRegularFile(helperExe) {
		return 0, fmt.Errorf("辅助进程副本不存在：%s", helperExe)
	}
	handoffPath := filepath.Join(h.WorkDir, "handoff.json")
	logFile, err := os.OpenFile(h.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	defer func() { _ = logFile.Close() }()

	cmd := exec.Command(helperExe, CommandName, handoffPath)
	cmd.Dir = h.Dir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = detachAttrs()
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	// 不 Wait：helper 必须活过父进程退出（分离进程，契约 20.6.1）。
	_ = cmd.Process.Release()
	_ = ctx
	return pid, nil
}

// verifyStagedBinary 用暂存的新二进制自报版本（契约 20.7 第 5 条）：
// 版本必须与目标版本一致（忽略 v 前缀），防止把别的版本包当目标版本装上。
//
// 声明为变量只为测试注入（暂存目录里放的是真实二进制，生产路径恒为本实现）。
var verifyStagedBinary = func(ctx context.Context, binary, target string) error {
	probeCtx, cancel := context.WithTimeout(ctx, versionProbeTimeout)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, binary, "-version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("发布包内的 %s 无法执行（-version 失败）：%v", filepath.Base(binary), sanitizeErrorText(err.Error()))
	}
	version := parseProbeVersion(string(output))
	if version == "" {
		return fmt.Errorf("发布包内的 %s 未自报版本（-version 输出异常）", filepath.Base(binary))
	}
	if !updates.SameVersion(version, target) {
		return fmt.Errorf("发布包内的 %s 自报版本 %s 与目标版本 %s 不一致，拒绝升级",
			filepath.Base(binary), version, target)
	}
	return nil
}

// parseProbeVersion 从 `-version` 输出里取出版本号（形如 `lyidc-oem server version=v1.0.0 commit=… built_at=…`）。
func parseProbeVersion(output string) string {
	for _, field := range strings.Fields(output) {
		if value, ok := strings.CutPrefix(field, "version="); ok {
			return value
		}
	}
	return ""
}
