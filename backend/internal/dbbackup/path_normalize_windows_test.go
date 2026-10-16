//go:build windows

package dbbackup

import (
	"os"
	execmd "os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

// shortPath 返回路径的 8.3 短名（GetShortPathNameW）；该卷未给这个目录生成短名时返回原路径。
func shortPath(t *testing.T, path string) string {
	t.Helper()
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetShortPathNameW")
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatalf("转换路径失败: %v", err)
	}
	buf := make([]uint16, 1024)
	n, _, _ := proc.Call(uintptr(unsafe.Pointer(ptr)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 || int(n) > len(buf) {
		t.Skipf("本机取不到 %s 的短名，跳过", path)
	}
	return syscall.UTF16ToString(buf[:n])
}

// TestCheckDirRejectsShortNameWebRoot 覆盖 8.3 短名口径（契约 21.4）：Web 根写成短名同样要判出来。
// 两侧走同一个 canonicalDir，短名与长名必须归一到同一目录。
func TestCheckDirRejectsShortNameWebRoot(t *testing.T) {
	install := t.TempDir()
	webRoot := filepath.Join(install, WebRootName)
	if err := os.MkdirAll(webRoot, 0o755); err != nil {
		t.Fatalf("建立 Web 根失败: %v", err)
	}
	short := shortPath(t, webRoot)
	if strings.EqualFold(short, webRoot) {
		t.Skipf("本卷没有为该目录生成 8.3 短名（%s），跳过", webRoot)
	}
	t.Logf("Web 根短名 = %s", short)

	service := newDirService(install)
	if err := service.checkDir(short); err == nil {
		t.Fatalf("Web 根的 8.3 短名写法 %q 必须被拒绝", short)
	} else if !strings.Contains(err.Error(), "Web 根") {
		t.Fatalf("短名写法应被判为落在 Web 根内：%v", err)
	}
	if err := service.checkDir(filepath.Join(short, "backups")); err == nil {
		t.Fatalf("短名写法 + 子目录必须被拒绝")
	}
	// 短名与扩展长度路径前缀混用（一侧短一侧长）同样必须判出来。
	if err := service.checkDir(`\\?\` + short); err == nil {
		t.Fatalf("`\\\\?\\` + 短名写法必须被拒绝")
	}
}

// TestCheckDirRejectsJunctionIntoWebRoot 覆盖 junction 指向 Web 根的绕过（Windows 上创建 junction
// 不需要额外权限，符号链接才需要，因此这条在 Windows 上必须有真实用例）。
//
// 口径说明：`filepath.EvalSymlinks` **不解析** Windows junction（实测返回 junction 自身路径），
// 因此包含判定拦不住它；真正拦住的是 os.Lstat 的「不是真实目录」判定——
// 目录本身是重解析点时一律拒绝，绝不允许经由它落进 Web 根。
func TestCheckDirRejectsJunctionIntoWebRoot(t *testing.T) {
	install := t.TempDir()
	webRoot := filepath.Join(install, WebRootName)
	if err := os.MkdirAll(webRoot, 0o755); err != nil {
		t.Fatalf("建立 Web 根失败: %v", err)
	}
	link := filepath.Join(install, "sneaky")
	if out, err := execmd.Command("cmd", "/c", "mklink", "/J", link, webRoot).CombinedOutput(); err != nil {
		t.Skipf("创建 junction 失败（跳过）：%v / %s", err, out)
	}

	service := newDirService(install)
	for _, dir := range []string{link, `\\?\` + link} {
		err := service.checkDir(dir)
		if err == nil {
			t.Fatalf("指向 Web 根的 junction（%s）必须被拒绝", dir)
		}
		if !strings.Contains(err.Error(), "不是真实目录") && !strings.Contains(err.Error(), "符号链接") {
			t.Fatalf("junction 的拒绝原因应说明不是真实目录/符号链接：%v", err)
		}
	}
}
