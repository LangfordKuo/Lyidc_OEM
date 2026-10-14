//go:build windows

package cache

import (
	"os"
	"syscall"
)

// openRead 以「允许并发删除 / 改名」的共享模式打开文件（Windows 专用）。
//
// Go 的 os.Open 在 Windows 上只请求 FILE_SHARE_READ|FILE_SHARE_WRITE：只要有读者持有句柄，
// 对同一路径的原子替换（rename）与删除就会被系统拒绝，表现为间歇性的「Access is denied」
// 「The process cannot access the file because it is being used by another process」。
// 文件缓存的真实并发形态恰恰是「读着旧值的同时写入新值」，所以读者必须带上 FILE_SHARE_DELETE：
// 此后删除 / 改名替换与读者可以并存（旧文件在最后一个句柄关闭时消失，读者读到的仍是旧内容），
// 这正是缓存想要的语义。非 Windows 平台删除与改名本就与读者并存，直接用 os.Open
// （见 openfile_other.go）。
func openRead(path string) (*os.File, error) {
	pathp, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	handle, err := syscall.CreateFile(
		pathp,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(handle), path), nil
}
