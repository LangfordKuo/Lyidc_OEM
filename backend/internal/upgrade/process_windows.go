//go:build windows

package upgrade

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"
)

const (
	// detachProcess 让 helper 与新服务不继承控制台：父进程退出（甚至关闭控制台）后继续存活。
	detachProcess = 0x00000008
	// createNewProcessGroup 让子进程独立于父进程的进程组，避免被一起终止。
	createNewProcessGroup = 0x00000200
	// stillActive 是 GetExitCodeProcess 的「仍在运行」取值。
	stillActive = 259
)

// detachAttrs 让 helper 与新服务脱离父进程（Windows：DETACHED_PROCESS + 新进程组，契约 20.6.1）。
func detachAttrs() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: detachProcess | createNewProcessGroup}
}

// processAlive 判断进程是否存活：打不开句柄视为已退出，能打开则看退出码。
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	const desiredAccess = syscall.STANDARD_RIGHTS_READ | syscall.PROCESS_QUERY_INFORMATION | syscall.SYNCHRONIZE
	handle, err := syscall.OpenProcess(desiredAccess, false, uint32(pid))
	if err != nil {
		return false
	}
	defer func() { _ = syscall.CloseHandle(handle) }()
	var code uint32
	if err := syscall.GetExitCodeProcess(handle, &code); err != nil {
		return true
	}
	return code == stillActive
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
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

// freeSpace 返回路径所在卷的可用字节数（GetDiskFreeSpaceExW，契约 20.2 第 10 条）。
func freeSpace(path string) (uint64, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	procedure := kernel32.NewProc("GetDiskFreeSpaceExW")
	target, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var available, total, totalFree uint64
	result, _, callErr := procedure.Call(
		uintptr(unsafe.Pointer(target)),
		uintptr(unsafe.Pointer(&available)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if result == 0 {
		return 0, callErr
	}
	return available, nil
}
