package router

import (
	"html"
	"regexp"
	"strings"
)

// 商品简介解析的三个正则（R5）。
//
// 口径（契约 10.3）：description 是「上游原文经一次 HTML 实体反转义后的原始 HTML」，
// 常见形态是若干 <li>行</li>（真实数据即 CPU/内存/带宽…逐行配置），也可能混入
// <p>/<br>/<div> 等富文本标签。解析只用正则与 strings，不引入 HTML 解析依赖。
var (
	// listItemPattern 提取 <li> 行内容（大小写不敏感、跨行匹配）。
	listItemPattern = regexp.MustCompile(`(?is)<li\b[^>]*>(.*?)</li>`)
	// lineBreakPattern 是行分隔符：<br>（任意写法）、块级标签（开闭两侧都算）与真实换行。
	lineBreakPattern = regexp.MustCompile(`(?i)<\s*br\s*/?\s*>|</?\s*(?:p|div|li|tr|td|th|h[1-6])\b[^>]*>|[\r\n]+`)
	// tagPattern 是任意 HTML 标签（去标签留纯文本）。
	tagPattern = regexp.MustCompile(`(?s)<[^>]*>`)
)

// descriptionLines 把商品简介解析成展示行数组（契约 10.3 的 description_lines）。
//
// 规则：
//  1. 先做一次 HTML 实体反转义（兼容 R5 前的转义存量与 R5 后的解码存量，两种输入同一结果）；
//  2. 有 <li> 时逐条取其内容（li 内嵌 <br> 再拆分），无 <li> 时按 <br>/块级闭合/换行整体切行
//     （未闭合的裸 <li> 也会在这一步被按标签剥掉）；
//  3. 每行去标签、折叠空白、去空行；单行超长不截断。
//
// 空/无简介返回**空数组**（而非 nil），保证 JSON 输出 `[]` 而不是 `null`。
func descriptionLines(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}

	decoded := html.UnescapeString(raw)

	var segments []string
	if matches := listItemPattern.FindAllStringSubmatch(decoded, -1); len(matches) > 0 {
		segments = make([]string, 0, len(matches))
		for _, match := range matches {
			segments = append(segments, lineBreakPattern.Split(match[1], -1)...)
		}
	} else {
		segments = lineBreakPattern.Split(decoded, -1)
	}

	lines := make([]string, 0, len(segments))
	for _, segment := range segments {
		// 去标签 → 折叠行内空白（含全角空格以外的 Unicode 空白）→ 跳过空行。
		text := strings.Join(strings.Fields(tagPattern.ReplaceAllString(segment, "")), " ")
		if text != "" {
			lines = append(lines, text)
		}
	}
	return lines
}
