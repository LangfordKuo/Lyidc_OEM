package delivery

import (
	"strings"
	"testing"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upstream"
)

// TestGeneratePassword 验证密码长度、四类字符齐备与随机性（无重复）。
func TestGeneratePassword(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		password, err := GeneratePassword()
		if err != nil {
			t.Fatalf("生成密码失败: %v", err)
		}
		if len([]rune(password)) != PasswordLength {
			t.Fatalf("密码长度 = %d, 期望 %d（%q）", len([]rune(password)), PasswordLength, password)
		}
		if seen[password] {
			t.Fatalf("密码重复生成: %q", password)
		}
		seen[password] = true

		var upper, lower, digit, special bool
		for _, ch := range password {
			switch {
			case ch >= 'A' && ch <= 'Z':
				upper = true
			case ch >= 'a' && ch <= 'z':
				lower = true
			case ch >= '0' && ch <= '9':
				digit = true
			default:
				special = true
			}
		}
		if !upper || !lower || !digit || !special {
			t.Fatalf("密码未覆盖四类字符: %q（upper=%v lower=%v digit=%v special=%v）",
				password, upper, lower, digit, special)
		}
	}
}

// TestParseConfigOptions 验证配置项快照解析（字符串/整数取值、空快照与非法输入）。
func TestParseConfigOptions(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    map[int]string
		wantErr bool
	}{
		{name: "空串", raw: "", want: nil},
		{name: "空对象", raw: "{}", want: nil},
		{name: "null", raw: "null", want: nil},
		{name: "字符串取值", raw: `{"101":"201","102":"301"}`, want: map[int]string{101: "201", 102: "301"}},
		{name: "整数取值", raw: `{"101":201}`, want: map[int]string{101: "201"}},
		{name: "键非正整数", raw: `{"abc":"1"}`, wantErr: true},
		{name: "键为 0", raw: `{"0":"1"}`, wantErr: true},
		{name: "值为空串", raw: `{"101":""}`, wantErr: true},
		{name: "值类型非法", raw: `{"101":true}`, wantErr: true},
		{name: "JSON 损坏", raw: `{"101":`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseConfigOptions(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望错误，得到 %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("解析结果 = %v, 期望 %v", got, tc.want)
			}
			for key, value := range tc.want {
				if got[key] != value {
					t.Fatalf("解析结果 = %v, 期望 %v", got, tc.want)
				}
			}
		})
	}
}

// TestHostnameFor 验证主机名生成规则与可回溯性。
func TestHostnameFor(t *testing.T) {
	got := HostnameFor("O20261008143015K7Q2ZP")
	if got != "oem-o20261008143015k7q2zp" {
		t.Fatalf("主机名 = %q", got)
	}
	if strings.ToLower(got) != got {
		t.Fatalf("主机名应全小写: %q", got)
	}
}

// TestDueDateOf 验证上游到期时间解析（unix 秒 / 缺省 / 非正数）。
func TestDueDateOf(t *testing.T) {
	if got := DueDateOf(nil); got != nil {
		t.Fatalf("nil 主机应返回 nil，得到 %v", got)
	}
	if got := DueDateOf(&upstream.Host{NextDueDate: 0}); got != nil {
		t.Fatalf("零值应返回 nil，得到 %v", got)
	}

	const stamp = int64(1796718606) // 实测样例（契约 8.5 第 11 条）
	got := DueDateOf(&upstream.Host{NextDueDate: stamp})
	if got == nil {
		t.Fatal("期望非 nil")
	}
	if !got.Equal(time.Unix(stamp, 0).UTC()) {
		t.Fatalf("到期时间 = %v, 期望 %v", got, time.Unix(stamp, 0).UTC())
	}
}
