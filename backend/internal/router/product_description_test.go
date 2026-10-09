package router

import (
	"reflect"
	"strings"
	"testing"
)

// TestDescriptionLines 覆盖 R5 简介解析的关键口径：
// 转义/未转义 HTML、li 列表、富文本、纯文本、空简介、杂标签与超长行。
func TestDescriptionLines(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{
			name: "上游真实转义样本（7 行 li，实体转义 + 换行）",
			raw: "&lt;li&gt;CPU:2核心&lt;/li&gt;\n&lt;li&gt;内存:1G&lt;/li&gt;\n&lt;li&gt;带宽:20M&lt;/li&gt;\n" +
				"&lt;li&gt;流量:不限&lt;/li&gt;\n&lt;li&gt;系统盘:Lin30GWin50G&lt;/li&gt;\n" +
				"&lt;li&gt;数据盘:无&lt;/li&gt;\n&lt;li&gt;防御:5G&lt;/li&gt;",
			want: []string{"CPU:2核心", "内存:1G", "带宽:20M", "流量:不限",
				"系统盘:Lin30GWin50G", "数据盘:无", "防御:5G"},
		},
		{
			name: "未转义的 li（兼容解码后的存量与测试数据）",
			raw:  "<li>CPU:2核心</li>\n<li>内存:1G</li>",
			want: []string{"CPU:2核心", "内存:1G"},
		},
		{
			name: "li 带属性且大小写混杂",
			raw:  "<LI class=\"item\">CPU:2核心</LI>\n<li data-x='1'>内存:1G</li>",
			want: []string{"CPU:2核心", "内存:1G"},
		},
		{
			name: "li 内嵌 br 与 span，拆行并去标签",
			raw:  "<li><span>CPU:2核心</span><br/>内存:1G</li>\n<li>带宽:20M<br>流量:不限</li>",
			want: []string{"CPU:2核心", "内存:1G", "带宽:20M", "流量:不限"},
		},
		{
			name: "无 li 的纯文本：按换行切行",
			raw:  "第一行\n第二行\r\n\n第三行",
			want: []string{"第一行", "第二行", "第三行"},
		},
		{
			name: "无 li 的富文本：按 p/br 切行",
			raw:  "<p>段落一</p><p>段落二</p><br>收尾<div>块三</div>",
			want: []string{"段落一", "段落二", "收尾", "块三"},
		},
		{
			name: "未闭合的裸 li：剥离标签后保留文本",
			raw:  "<li>CPU:2核心",
			want: []string{"CPU:2核心"},
		},
		{
			name: "实体与行内空白折叠、空行剔除",
			raw:  "<li>CPU&amp;GPU:2 核  心</li>\n\n<li>   </li>\n<li>&nbsp;</li>\n<li>内存:1G</li>",
			want: []string{"CPU&GPU:2 核 心", "内存:1G"},
		},
		{
			name: "全角空格对齐的真实数据形态（br 分行）：折叠为单空格",
			raw:  "处理器：4核<br>\n内　存：4G<br>\n防　御：200G",
			want: []string{"处理器：4核", "内 存：4G", "防 御：200G"},
		},
		{
			name: "空与纯空白：返回空数组（非 nil，JSON 输出 []）",
			raw:  "",
			want: []string{},
		},
		{
			name: "只有空白字符",
			raw:  "  \n\t\r\n ",
			want: []string{},
		},
		{
			name: "只有标签没有文本",
			raw:  "<li></li><div><br></div>",
			want: []string{},
		},
		{
			name: "双重转义只解码一次（不重复反转义）",
			raw:  "&amp;lt;li&amp;gt;CPU:2核心&amp;lt;/li&amp;gt;",
			want: []string{"&lt;li&gt;CPU:2核心&lt;/li&gt;"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := descriptionLines(tc.raw)
			if got == nil {
				t.Fatal("解析结果不应为 nil（空简介必须是空数组）")
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("descriptionLines(%q) = %#v，期望 %#v", tc.raw, got, tc.want)
			}
		})
	}
}

// TestDescriptionLinesLongLineNotTruncated 断言单行超长不截断。
func TestDescriptionLinesLongLineNotTruncated(t *testing.T) {
	long := strings.Repeat("A", 5000)
	got := descriptionLines("<li>" + long + "</li>")
	if len(got) != 1 || got[0] != long {
		t.Fatalf("超长行应完整保留，实际 %d 行、首行长度 %d", len(got), len(got[0]))
	}
}
