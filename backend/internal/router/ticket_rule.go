package router

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// 工单（阶段 6a）相关的纯校验逻辑。错误分类沿用既有约定：
// 格式类（枚举取值非法、ID 非法、分页越界）→ 40001；规则类（长度、空白、上限、状态不允许）→ 40002。

// errTicketRule 表示工单规则不成立（对应 40002，契约 16.4）；
// 格式类（枚举取值非法、ID 非法）在 handler 内直接回 40001，不经过本哨兵。
var errTicketRule = errors.New("工单规则不成立")

// 工单字段约束（契约 16.4 第 2 条）。
const (
	// minTicketSubjectRunes / maxTicketSubjectRunes 是标题长度区间（裁剪首尾空白后按字符计）。
	minTicketSubjectRunes = 5
	maxTicketSubjectRunes = 100
	// minTicketContentRunes / maxTicketContentRunes 是正文长度区间（同上）。
	minTicketContentRunes = 1
	maxTicketContentRunes = 5000
	// maxOpenTicketsPerMember 是单个会员的**未关闭工单**上限（契约 16.4 第 1 条）。
	maxOpenTicketsPerMember = 20
)

// normalizeTicketText 裁剪首尾空白（保留内部换行与缩进）：存储与长度校验都以此为准。
func normalizeTicketText(value string) string {
	return strings.TrimSpace(value)
}

// validateTicketSubject 裁剪并校验标题：非空白、5-100 字符（按 rune 计，中文按字符算）。
func validateTicketSubject(subject string) (string, error) {
	trimmed := normalizeTicketText(subject)
	if trimmed == "" {
		return "", fmt.Errorf("%w: 标题不能为空或纯空白", errTicketRule)
	}
	if count := utf8.RuneCountInString(trimmed); count < minTicketSubjectRunes || count > maxTicketSubjectRunes {
		return "", fmt.Errorf("%w: 标题需为 %d-%d 个字符（当前 %d）",
			errTicketRule, minTicketSubjectRunes, maxTicketSubjectRunes, count)
	}
	return trimmed, nil
}

// validateTicketContent 裁剪并校验正文：非空白、1-5000 字符（按 rune 计）。
// 超长一律拒绝而不是截断，避免静默丢失会员内容（契约 16.4 第 2 条）。
func validateTicketContent(content string) (string, error) {
	trimmed := normalizeTicketText(content)
	if trimmed == "" {
		return "", fmt.Errorf("%w: 内容不能为空或纯空白", errTicketRule)
	}
	if count := utf8.RuneCountInString(trimmed); count < minTicketContentRunes || count > maxTicketContentRunes {
		return "", fmt.Errorf("%w: 内容需为 %d-%d 个字符（当前 %d）",
			errTicketRule, minTicketContentRunes, maxTicketContentRunes, count)
	}
	return trimmed, nil
}
