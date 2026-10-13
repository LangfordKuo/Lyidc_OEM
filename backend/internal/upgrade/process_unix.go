//go:build !windows

package upgrade

import (
	"errors"
	"fmt"
	"syscall"
	"time"
)

// detachAttrs 让 helper 与新服务脱离父进程会话（Unix：新会话，父进程退出后继续存活，契约 20.6.1）。
func detachAttrs() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// processAlive 判断进程是否存活（信号 0 探测；EPERM 表示存在但不是本进程所有）。
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}

// waitForExit 轮询等待进程退出（契约 20.6.2 第 1 步）。
func waitForExit(pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if !processAlive(pid) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("等待进程 %d 退出超时（%s）", pid, timeout)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// killProcess 结束进程（回滚时结束启动失败的新服务）。
func killProcess(pid int) error {
	if pid <= 0 {
		return nil
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

// freeSpace 返回路径所在文件系统的可用字节数（契约 20.2 第 10 条）。
func freeSpace(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}
