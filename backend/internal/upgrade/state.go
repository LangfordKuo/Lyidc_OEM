package upgrade

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// encodeJob 序列化任务状态（契约 20.5）。
func encodeJob(job *Job) ([]byte, error) { return json.Marshal(job) }

// decodeJob 解析状态文件；缺少任务号视为损坏。
func decodeJob(raw []byte) (*Job, error) {
	var job Job
	if err := json.Unmarshal(raw, &job); err != nil {
		return nil, err
	}
	if strings.TrimSpace(job.ID) == "" {
		return nil, errors.New("状态缺少任务号")
	}
	return &job, nil
}

// randomHex 生成 n 字节的随机十六进制串（任务号后缀）；随机源不可用时退化为全零,不影响正确性。
func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return strings.Repeat("0", n*2)
	}
	return hex.EncodeToString(buf)
}

// humanBytes 把字节数格式化成人类可读的近似值（与契约 20.2 的空间提示口径一致）。
func humanBytes(n uint64) string {
	const (
		kib = 1 << 10
		mib = 1 << 20
		gib = 1 << 30
	)
	switch {
	case n >= gib:
		return strconv.FormatUint(n/gib, 10) + " GiB"
	case n >= mib:
		return strconv.FormatUint(n/mib, 10) + " MiB"
	case n >= kib:
		return strconv.FormatUint(n/kib, 10) + " KiB"
	default:
		return strconv.FormatUint(n, 10) + " B"
	}
}
