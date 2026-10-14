//go:build !windows

package cache

import "os"

// openRead 打开文件只读。类 Unix 平台的删除 / 改名与已打开的文件天然并存
// （读者继续读旧 inode），无需特殊共享模式，直接 os.Open。
func openRead(path string) (*os.File, error) { return os.Open(path) }
