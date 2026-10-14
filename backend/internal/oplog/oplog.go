// Package oplog 实现操作日志（R18，契约第 23 节）：三通道条目模型、异步写入器与脱敏。
//
// 分工：
//   - router 的中间件把管理端/会员端的**每个写请求**（含被鉴权拒绝的）编成 Entry 入队，
//     处理器可选地补充可读动作名与业务摘要；
//   - scheduler 每轮扫描写一条汇总（任务名/结果/处理数/耗时/错误），子步骤有实际动作时写明细；
//   - **写入异步**：Enqueue 永不阻塞业务请求，写失败只记服务日志（与 instance_operation_logs
//     的既有口径一致）；缓冲满时丢弃并记服务日志；进程关闭时 Flush/Close 冲出缓冲。
//
// 脱敏（契约 23.7）：**请求体整体不落库**——中间件只记录方法、路由模板、状态码、耗时与 IP，
// 摘要由处理器显式构造；Sanitize 作为第二道闸门，把摘要里形如「password=值」的片段替换为 ***，
// 密码、API 密钥、JWT、SMTP 口令等绝不进库。
package oplog

import (
	"regexp"
	"strings"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

// 通道与结果的取值别名（与 model 中的常量一一对应，供本包调用方少 import 一个包）。
const (
	ChannelAdmin     = model.OplogChannelAdmin
	ChannelMember    = model.OplogChannelMember
	ChannelScheduler = model.OplogChannelScheduler

	StatusSuccess = model.OplogStatusSuccess
	StatusFailed  = model.OplogStatusFailed
)

// 各字段的字符上限（与迁移 0019 的列宽一致，按字符截断）。
const (
	maxActorNameRunes = 64
	maxActionRunes    = 128
	maxPathRunes      = 191
	maxSummaryRunes   = 500
)

// Entry 是一条待写入的操作日志（尚未落库、尚未截断）。
//
// CreatedAt 为零值时由写入器在落库时填当前 UTC 时间（入队与落库之间可能有数百毫秒的异步窗口，
// 语义上「发生时间」以入队为准，故中间件与调度器都显式带上入队时间）。
type Entry struct {
	Channel    string
	ActorID    uint64
	ActorName  string
	Action     string
	Method     string
	Path       string
	HTTPStatus int
	Status     string
	IP         string
	DurationMS int64
	Summary    string
	CreatedAt  time.Time
}

// StatusFromHTTPStatus 由 HTTP 状态码推导日志结果（2xx/3xx 为成功，其余为失败）。
func StatusFromHTTPStatus(status int) string {
	if status >= 200 && status < 400 {
		return StatusSuccess
	}
	if status == 0 {
		// 未写出响应的内部路径（正常不会出现）：按成功记录，避免误报。
		return StatusSuccess
	}
	return StatusFailed
}

// secretPattern 匹配「密钥名 = 值」形式的片段（第二道闸门）。
//
// 只认 ASCII 密钥名 + 等号/冒号（含带前缀的写法如 smtp_password），避免误伤中文摘要
// （如「修改密码」没有等号，不会被改写）；第一道闸门是「请求体整体不落库」，
// 本规则只用于兜底意外拼接进来的凭据。
var secretPattern = regexp.MustCompile(
	`(?i)(password|passwd|pwd|token|secret|api[_-]?key|apikey|access[_-]?key|authorization)\s*[=:]\s*[^\s,;]+`)

// Sanitize 对摘要做防御性脱敏：把「密钥名=值」替换为「密钥名=***」。
func Sanitize(text string) string {
	if text == "" {
		return text
	}
	return secretPattern.ReplaceAllString(text, "$1=***")
}

// truncateRunes 按字符截断（与 MySQL VARCHAR 的字符语义一致），超长时截断到 limit 个字符。
func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

// normalize 把 Entry 归一为可安全落库的输入：截断到列宽、脱敏摘要、结果取值兜底。
func normalize(entry Entry) Entry {
	status := entry.Status
	if status != StatusSuccess && status != StatusFailed {
		status = StatusFromHTTPStatus(entry.HTTPStatus)
	}
	createdAt := entry.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	return Entry{
		Channel:    entry.Channel,
		ActorID:    entry.ActorID,
		ActorName:  truncateRunes(strings.TrimSpace(entry.ActorName), maxActorNameRunes),
		Action:     truncateRunes(strings.TrimSpace(entry.Action), maxActionRunes),
		Method:     truncateRunes(strings.TrimSpace(entry.Method), 10),
		Path:       truncateRunes(strings.TrimSpace(entry.Path), maxPathRunes),
		HTTPStatus: entry.HTTPStatus,
		Status:     status,
		IP:         truncateRunes(strings.TrimSpace(entry.IP), 45),
		DurationMS: entry.DurationMS,
		Summary:    truncateRunes(Sanitize(strings.TrimSpace(entry.Summary)), maxSummaryRunes),
		CreatedAt:  createdAt.UTC(),
	}
}
