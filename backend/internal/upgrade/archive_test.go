package upgrade

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// serverNameForTest 返回当前平台的服务二进制名（Windows 带 .exe）。
func serverNameForTest() string { return binaryName(ServerBinary) }

// writeTarGz 打包一组条目到 .tar.gz（条目名 → 内容；名字以 / 结尾视为目录）。
func writeTarGz(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建归档失败：%v", err)
	}
	defer func() { _ = file.Close() }()
	gz := gzip.NewWriter(file)
	writer := tar.NewWriter(gz)
	for name, content := range entries {
		header := &tar.Header{Name: name, Mode: 0o755}
		if strings.HasSuffix(name, "/") {
			header.Typeflag = tar.TypeDir
			header.Mode = 0o755
			header.Size = 0
		} else {
			header.Typeflag = tar.TypeReg
			header.Size = int64(len(content))
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatalf("写归档头失败：%v", err)
		}
		if header.Typeflag == tar.TypeReg {
			if _, err := writer.Write([]byte(content)); err != nil {
				t.Fatalf("写归档内容失败：%v", err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("关闭归档失败：%v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("关闭 gzip 失败：%v", err)
	}
}

// writeZip 打包一组条目到 .zip。
func writeZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建归档失败：%v", err)
	}
	defer func() { _ = file.Close() }()
	writer := zip.NewWriter(file)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("写 zip 条目失败：%v", err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("写 zip 内容失败：%v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("关闭 zip 失败：%v", err)
	}
}

func TestExtractTarGzKeepsPayload(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "pkg.tar.gz")
	writeTarGz(t, archive, map[string]string{
		"pkg/":                             "",
		"pkg/" + serverNameForTest():       "server-binary",
		"pkg/" + binaryName(MigrateBinary): "migrate-binary",
		"pkg/dist/":                        "",
		"pkg/dist/index.html":              "<html></html>",
	})
	target := filepath.Join(dir, "out")
	if err := extractArchive(archive, target); err != nil {
		t.Fatalf("解包失败：%v", err)
	}
	payload, err := locatePayload(target)
	if err != nil {
		t.Fatalf("定位发布包失败：%v", err)
	}
	if payload.root != filepath.Join(target, "pkg") {
		t.Fatalf("根目录不符：%s", payload.root)
	}
	content, err := os.ReadFile(filepath.Join(payload.dist, "index.html"))
	if err != nil || string(content) != "<html></html>" {
		t.Fatalf("dist 内容不符：%v %q", err, content)
	}
}

func TestExtractZipKeepsPayload(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "pkg.zip")
	writeZip(t, archive, map[string]string{
		"pkg/" + serverNameForTest():       "server-binary",
		"pkg/" + binaryName(MigrateBinary): "migrate-binary",
		"pkg/dist/index.html":              "<html></html>",
	})
	target := filepath.Join(dir, "out")
	if err := extractArchive(archive, target); err != nil {
		t.Fatalf("解包失败：%v", err)
	}
	if _, err := locatePayload(target); err != nil {
		t.Fatalf("定位发布包失败：%v", err)
	}
}

func TestExtractRejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	for name, archiveName := range map[string]string{
		"parent":   "../escape.txt",
		"absolute": "/tmp/escape.txt",
	} {
		archive := filepath.Join(dir, name+".tar.gz")
		writeTarGz(t, archive, map[string]string{archiveName: "boom"})
		err := extractArchive(archive, filepath.Join(dir, "out-"+name))
		if err == nil {
			t.Fatalf("%s：路径穿越未被拒绝", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "escape.txt")); err == nil {
		t.Fatal("路径穿越真的写出了文件")
	}
}

func TestExtractRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "link.tar.gz")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatalf("创建归档失败：%v", err)
	}
	gz := gzip.NewWriter(file)
	writer := tar.NewWriter(gz)
	if err := writer.WriteHeader(&tar.Header{
		Name: "pkg/lyidc-server", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd",
	}); err != nil {
		t.Fatalf("写归档头失败：%v", err)
	}
	_ = writer.Close()
	_ = gz.Close()
	_ = file.Close()

	if err := extractArchive(archive, filepath.Join(dir, "out")); err == nil {
		t.Fatal("符号链接条目未被拒绝")
	}
}

func TestExtractRejectsUnknownFormat(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "pkg.rar")
	if err := os.WriteFile(archive, []byte("not an archive"), 0o600); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}
	if err := extractArchive(archive, filepath.Join(dir, "out")); err == nil {
		t.Fatal("未知格式未被拒绝")
	}
}

func TestLocatePayloadReportsMissingParts(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(filepath.Join(root, DistDir), 0o755); err != nil {
		t.Fatalf("建目录失败：%v", err)
	}
	if err := os.WriteFile(filepath.Join(root, DistDir, "index.html"), []byte("x"), 0o644); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}

	if _, err := locatePayload(dir); err == nil || !strings.Contains(err.Error(), serverNameForTest()) {
		t.Fatalf("缺少服务二进制时的报错不符：%v", err)
	}
	if err := os.WriteFile(filepath.Join(root, serverNameForTest()), []byte("bin"), 0o644); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}
	if _, err := locatePayload(dir); err == nil || !strings.Contains(err.Error(), binaryName(MigrateBinary)) {
		t.Fatalf("缺少迁移二进制时的报错不符：%v", err)
	}
	if err := os.WriteFile(filepath.Join(root, binaryName(MigrateBinary)), []byte("bin"), 0o644); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}
	payload, err := locatePayload(dir)
	if err != nil {
		t.Fatalf("结构齐全时仍失败：%v", err)
	}
	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(payload.server)
		if statErr != nil || info.Mode().Perm()&0o111 == 0 {
			t.Fatalf("解包后二进制应可执行：%v %v", info, statErr)
		}
	}
}
