package store

import (
	"errors"
	"testing"
)

// TestOperationLogKeywordExpr 覆盖关键字编译口径（契约 23.5）：
// 单词按短语整体匹配、多词 AND、单字符词明确报错（不退回全表 LIKE）。
func TestOperationLogKeywordExpr(t *testing.T) {
	cases := []struct {
		name    string
		keyword string
		want    string
	}{
		{name: "中文单词", keyword: "上游设置", want: `+"上游设置"`},
		{name: "英文单词", keyword: "admin", want: `+"admin"`},
		{name: "多词 AND", keyword: "备份 目录", want: `+"备份" +"目录"`},
		{name: "多余空白折叠", keyword: "  备份   目录  ", want: `+"备份" +"目录"`},
		{name: "转义双引号与反斜杠", keyword: `a"b\c`, want: `+"a\"b\\c"`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := OperationLogKeywordExpr(tt.keyword)
			if err != nil {
				t.Fatalf("OperationLogKeywordExpr(%q) 返回错误: %v", tt.keyword, err)
			}
			if got != tt.want {
				t.Fatalf("OperationLogKeywordExpr(%q) = %q，期望 %q", tt.keyword, got, tt.want)
			}
		})
	}

	invalid := []string{"", "   ", "上", "a", "备份 目", "商 品"}
	for _, keyword := range invalid {
		if _, err := OperationLogKeywordExpr(keyword); !errors.Is(err, ErrOperationLogKeyword) {
			t.Errorf("OperationLogKeywordExpr(%q) 应返回 ErrOperationLogKeyword，实际 err=%v", keyword, err)
		}
	}
}

// TestOperationLogClampHelpers 覆盖落库前的边界收敛（列宽与状态码范围）。
func TestOperationLogClampHelpers(t *testing.T) {
	if got := clampHTTPStatus(-1); got != 0 {
		t.Errorf("clampHTTPStatus(-1) = %d，期望 0", got)
	}
	if got := clampHTTPStatus(200); got != 200 {
		t.Errorf("clampHTTPStatus(200) = %d，期望 200", got)
	}
	if got := clampHTTPStatus(70000); got != 0 {
		t.Errorf("clampHTTPStatus(70000) = %d，期望 0（越界归零）", got)
	}
	if got := clampDurationMS(-5); got != 0 {
		t.Errorf("clampDurationMS(-5) = %d，期望 0", got)
	}
	if got := clampDurationMS(1234); got != 1234 {
		t.Errorf("clampDurationMS(1234) = %d，期望 1234", got)
	}
}
