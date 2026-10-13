package upgrade

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// errUnsafeEntry 表示归档里出现了不允许的条目（绝对路径、.. 逃逸、符号链接等，契约 20.7 第 4 条）。
var errUnsafeEntry = errors.New("归档包含不安全的条目")

// extractArchive 把发布包解到 destDir：按扩展名分派 .tar.gz / .zip（与 scripts/package.sh 的产物一致）。
// 全程只允许普通文件与目录，且总大小与单文件大小都有上限。
func extractArchive(archivePath, destDir string) error {
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return err
	}
	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		return extractTarGz(archivePath, destDir)
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(archivePath, destDir)
	default:
		return fmt.Errorf("不支持的发布包格式：%s（只支持 .tar.gz 与 .zip）", filepath.Base(archivePath))
	}
}

// safeEntryPath 把归档内的条目名解析成 destDir 下的安全路径：
// 拒绝绝对路径、盘符、`..` 逃逸与非普通条目；返回绝对路径。
func safeEntryPath(destDir, name string) (string, error) {
	// 前导斜杠（含 Windows 的反斜杠）与盘符一律拒绝：这类条目在不同平台上会
	// 被解释成绝对路径，宁可拒绝也不依赖 filepath 的平台差异。
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) {
		return "", errUnsafeEntry
	}
	cleaned := filepath.Clean(filepath.FromSlash(name))
	if cleaned == "." || cleaned == string(filepath.Separator) {
		return "", errUnsafeEntry
	}
	if filepath.IsAbs(cleaned) || filepath.VolumeName(cleaned) != "" ||
		strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, ".."+string(filepath.Separator)) {
		return "", errUnsafeEntry
	}
	target := filepath.Join(destDir, cleaned)
	// 二次确认：Join 之后必须仍在 destDir 之内（防 Windows 上的盘符与 UNC 花样）。
	relative, err := filepath.Rel(destDir, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errUnsafeEntry
	}
	return target, nil
}

// extractTarGz 解开 .tar.gz。
func extractTarGz(archivePath, destDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("发布包不是合法的 gzip：%w", err)
	}
	defer func() { _ = gz.Close() }()

	reader := tar.NewReader(gz)
	var total int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("读取归档失败：%w", err)
		}
		target, err := safeEntryPath(destDir, header.Name)
		if err != nil {
			return fmt.Errorf("%w：%s", errUnsafeEntry, header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if header.Size > maxExtractFileBytes {
				return fmt.Errorf("归档内单个文件超过 %s 上限：%s", humanBytes(maxExtractFileBytes), header.Name)
			}
			total += header.Size
			if total > maxExtractBytes {
				return fmt.Errorf("解包后总大小超过 %s 上限", humanBytes(maxExtractBytes))
			}
			mode := os.FileMode(0o644)
			if header.Mode&0o111 != 0 {
				mode = 0o755
			}
			if err := writeEntry(target, io.LimitReader(reader, header.Size), mode); err != nil {
				return err
			}
		default:
			// 符号链接、硬链接、设备文件、FIFO 一律拒绝（契约 20.7 第 4 条）。
			return fmt.Errorf("%w：%s（类型 %d）", errUnsafeEntry, header.Name, header.Typeflag)
		}
	}
}

// extractZip 解开 .zip。
func extractZip(archivePath, destDir string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("发布包不是合法的 zip：%w", err)
	}
	defer func() { _ = reader.Close() }()

	var total int64
	for _, entry := range reader.File {
		target, err := safeEntryPath(destDir, entry.Name)
		if err != nil {
			return fmt.Errorf("%w：%s", errUnsafeEntry, entry.Name)
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			continue
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w：%s（符号链接）", errUnsafeEntry, entry.Name)
		}
		if int64(entry.UncompressedSize64) > maxExtractFileBytes {
			return fmt.Errorf("归档内单个文件超过 %s 上限：%s", humanBytes(maxExtractFileBytes), entry.Name)
		}
		total += int64(entry.UncompressedSize64)
		if total > maxExtractBytes {
			return fmt.Errorf("解包后总大小超过 %s 上限", humanBytes(maxExtractBytes))
		}
		source, err := entry.Open()
		if err != nil {
			return err
		}
		// zip 不保存 Unix 可执行位：解包后统一按 0755 落盘，稍后由 locatePayload 只对二进制降级处理。
		err = writeEntry(target, io.LimitReader(source, int64(entry.UncompressedSize64)), 0o755)
		_ = source.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// writeEntry 落盘一个归档条目（父目录按需创建）。
func writeEntry(target string, source io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, source); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// payload 是发布包内定位到的三个待替换对象（契约 20.1 第 2 条的包内布局）。
type payload struct {
	root    string // 包根目录（含三个待替换对象的那一层）
	server  string
	migrate string
	dist    string
}

// locatePayload 在解包结果里定位发布包根目录：必须同时含两个二进制与 dist/index.html。
// 查找深度限制为 2 层（包根目录由 scripts/package.sh 固定为归档内唯一顶层目录）。
func locatePayload(stagedDir string) (payload, error) {
	serverName := binaryName(ServerBinary)
	migrateName := binaryName(MigrateBinary)
	candidates := []string{stagedDir}
	for _, entry := range listDirs(stagedDir) {
		candidates = append(candidates, entry)
		for _, nested := range listDirs(entry) {
			candidates = append(candidates, nested)
		}
	}
	sawServer := false
	sawMigrate := false
	sawDist := false
	for _, root := range candidates {
		server := filepath.Join(root, serverName)
		migrate := filepath.Join(root, migrateName)
		dist := filepath.Join(root, DistDir)
		if !isRegularFile(server) || !isRegularFile(migrate) || !isRegularFile(filepath.Join(dist, "index.html")) {
			sawServer = sawServer || isRegularFile(server)
			sawMigrate = sawMigrate || isRegularFile(migrate)
			sawDist = sawDist || isRegularFile(filepath.Join(dist, "index.html"))
			continue
		}
		// 二进制必须可执行（zip 不保留执行位；Windows 忽略该位）。
		for _, binary := range []string{server, migrate} {
			if err := os.Chmod(binary, 0o755); err != nil && runtime.GOOS != "windows" {
				return payload{}, fmt.Errorf("设置可执行权限失败：%w", err)
			}
		}
		return payload{root: root, server: server, migrate: migrate, dist: dist}, nil
	}
	switch {
	case !sawServer:
		return payload{}, fmt.Errorf("发布包里没有找到 %s（可能不是本系统的发布包）", serverName)
	case !sawMigrate:
		return payload{}, fmt.Errorf("发布包缺少 %s", migrateName)
	case !sawDist:
		return payload{}, fmt.Errorf("发布包缺少 %s/index.html（前端产物不完整）", DistDir)
	default:
		return payload{}, errors.New("发布包结构不完整")
	}
}

// listDirs 列出目录下的一级子目录（读取失败按空处理，由调用方的结构校验给出最终结论）。
func listDirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, filepath.Join(dir, entry.Name()))
		}
	}
	return dirs
}

// isRegularFile 判断路径是否为普通文件。
func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
