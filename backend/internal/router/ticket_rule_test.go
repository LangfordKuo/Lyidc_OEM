package router

import (
	"errors"
	"strings"
	"testing"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

// TestValidateTicketSubject 验证标题校验：空白、长度边界（按 rune 计）与首尾空白裁剪。
func TestValidateTicketSubject(t *testing.T) {
	cases := []struct {
		name    string
		subject string
		want    string
		wantErr bool
	}{
		{"空串拒绝", "", "", true},
		{"纯空白拒绝", "  \t\n ", "", true},
		{"4 字符拒绝", "四个字符", "", true},
		{"5 字符通过（边界）", "五个字符啊", "五个字符啊", false},
		{"100 字符通过（边界）", strings.Repeat("标", 100), strings.Repeat("标", 100), false},
		{"101 字符拒绝", strings.Repeat("标", 101), "", true},
		{"首尾空白裁剪后判长", "  五个字符啊  ", "五个字符啊", false},
		{"裁剪后不足 5 字符拒绝", "  四个字符  ", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validateTicketSubject(tc.subject)
			if tc.wantErr {
				if err == nil || !errors.Is(err, errTicketRule) {
					t.Fatalf("err = %v，期望 errTicketRule", err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got = %q, err = %v，期望 %q", got, err, tc.want)
			}
		})
	}
}

// TestValidateTicketContent 验证正文校验：空白、长度边界与内部空白保留。
func TestValidateTicketContent(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
		wantErr bool
	}{
		{"空串拒绝", "", "", true},
		{"纯空白拒绝", " \n\t ", "", true},
		{"1 字符通过（边界）", "好", "好", false},
		{"5000 字符通过（边界）", strings.Repeat("内", 5000), strings.Repeat("内", 5000), false},
		{"5001 字符拒绝", strings.Repeat("内", 5001), "", true},
		{"保留内部换行与缩进", "  第一行\n    第二行  ", "第一行\n    第二行", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validateTicketContent(tc.content)
			if tc.wantErr {
				if err == nil || !errors.Is(err, errTicketRule) {
					t.Fatalf("err = %v，期望 errTicketRule", err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got = %q, err = %v，期望 %q", got, err, tc.want)
			}
		})
	}
}

// TestTicketModelValidators 验证工单模型层的取值判定（状态机与限流计数共用）。
func TestTicketModelValidators(t *testing.T) {
	for _, status := range []string{model.TicketStatusOpen, model.TicketStatusReplied} {
		if !model.IsValidTicketStatus(status) || !model.IsTicketOpen(status) {
			t.Errorf("状态 %s 应为合法且「未关闭」", status)
		}
	}
	if !model.IsValidTicketStatus(model.TicketStatusClosed) || model.IsTicketOpen(model.TicketStatusClosed) {
		t.Error("closed 应为合法状态且不算「未关闭」（不计入提单上限）")
	}
	for _, invalid := range []string{"", "pending", "OPEN"} {
		if model.IsValidTicketStatus(invalid) {
			t.Errorf("状态 %q 不应合法", invalid)
		}
	}
	for _, category := range model.TicketCategories {
		if !model.IsValidTicketCategory(category) {
			t.Errorf("分类 %s 应合法", category)
		}
	}
	if model.IsValidTicketCategory("urgent") {
		t.Error("分类 urgent 不应合法")
	}
	if model.TicketTradeNoPrefix != "T" {
		t.Errorf("工单号前缀 = %q，期望 T", model.TicketTradeNoPrefix)
	}
}
