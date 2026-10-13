package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/buildinfo"
)

// userAgent 是下载与取校验文件时使用的 User-Agent（与检查更新同口径）。
func userAgent() string {
	return "lyidc-oem/" + buildinfo.Version
}

// downloadAsset 下载发布包到 dst（契约 20.6.2 的 downloading 阶段）：
// 只发 GET，**绝不携带 Authorization**（download_base 可指向任意主机，带 Token 就是泄露，契约 20.7 第 3 条）；
// 超过 maxDownloadBytes 即中止；onProgress 按「已下载/总量」回调（总量未知时不回调）。
func downloadAsset(ctx context.Context, client *http.Client, rawURL, dst string, total int64, onProgress func(done, total int64)) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, fmt.Errorf("下载地址不合法：%w", err)
	}
	req.Header.Set("User-Agent", userAgent())
	resp, err := client.Do(req)
	if err != nil {
		return 0, describeHTTPError(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("更新源返回 HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxDownloadBytes {
		return 0, fmt.Errorf("发布包 %s 超过 %s 上限", humanBytes(uint64(resp.ContentLength)), humanBytes(maxDownloadBytes))
	}
	if total <= 0 {
		total = resp.ContentLength
	}

	file, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	reader := io.Reader(io.LimitReader(resp.Body, maxDownloadBytes+1))
	if onProgress != nil && total > 0 {
		reader = &progressReader{inner: reader, total: total, onProgress: onProgress}
	}
	written, copyErr := io.Copy(file, reader)
	closeErr := file.Close()
	if copyErr != nil {
		return written, describeHTTPError(copyErr)
	}
	if closeErr != nil {
		return written, closeErr
	}
	if written > maxDownloadBytes {
		return written, fmt.Errorf("发布包超过 %s 上限", humanBytes(maxDownloadBytes))
	}
	return written, nil
}

// progressReader 按读取进度回调（节流由回调方负责）。
type progressReader struct {
	inner      io.Reader
	total      int64
	done       int64
	onProgress func(done, total int64)
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.inner.Read(p)
	if n > 0 {
		r.done += int64(n)
		r.onProgress(r.done, r.total)
	}
	return n, err
}

// fetchChecksum 取回并解析同名 .sha256（契约 20.7 第 2 条）：响应上限 8 KiB。
func fetchChecksum(ctx context.Context, client *http.Client, rawURL, assetName string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("校验文件地址不合法：%w", err)
	}
	req.Header.Set("User-Agent", userAgent())
	resp, err := client.Do(req)
	if err != nil {
		return "", describeHTTPError(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("取校验文件时更新源返回 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if err != nil {
		return "", describeHTTPError(err)
	}
	return parseChecksum(string(body), assetName)
}

// parseChecksum 解析 .sha256 文件内容（契约 20.7 第 2 条）：接受 sha256sum 的
// `<hex>  <文件名>` / `<hex> *<文件名>` 与纯 hex 两种写法；带文件名时必须与产物同名。
func parseChecksum(body, assetName string) (string, error) {
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		digest := strings.ToLower(fields[0])
		if !isHexDigest(digest) {
			continue
		}
		if len(fields) > 1 {
			name := strings.TrimPrefix(fields[1], "*")
			if !strings.EqualFold(name, assetName) {
				return "", fmt.Errorf("校验文件指向的是 %s，与发布包 %s 不一致", name, assetName)
			}
		}
		return digest, nil
	}
	return "", errors.New("校验文件里没有 64 位十六进制摘要")
}

// isHexDigest 判断是否为 64 位小写十六进制摘要。
func isHexDigest(text string) bool {
	if len(text) != 64 {
		return false
	}
	for _, c := range text {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// fileSHA256 计算文件摘要（小写十六进制）。
func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// describeHTTPError 把网络类错误翻译成面向管理员、不含凭据的说明（契约 20.7 第 6 条）。
func describeHTTPError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("连接更新源超时")
	}
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		return errors.New("连接更新源超时")
	}
	if errors.Is(err, context.Canceled) {
		return errors.New("下载已取消")
	}
	return fmt.Errorf("网络错误：%s", sanitizeErrorText(err.Error()))
}

// sanitizeErrorText 去掉错误文本里的 URL 查询串（更新源地址可能带签名参数）。
func sanitizeErrorText(text string) string {
	if index := strings.IndexByte(text, '?'); index >= 0 {
		text = text[:index]
	}
	return text
}
