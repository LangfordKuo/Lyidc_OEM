package instanceops

import (
	"errors"
	"strings"
	"testing"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

// TestPowerOpMapping 验证电源操作的合法性、审计动作与中文标签映射。
func TestPowerOpMapping(t *testing.T) {
	cases := map[PowerOp]struct {
		action string
		label  string
	}{
		PowerSoftOn:     {model.ActionPowerOn, "开机"},
		PowerSoftOff:    {model.ActionPowerOff, "关机"},
		PowerReboot:     {model.ActionReboot, "重启"},
		PowerHardOff:    {model.ActionHardOff, "强制关机"},
		PowerHardReboot: {model.ActionHardReboot, "强制重启"},
	}
	for op, want := range cases {
		if !IsValidPowerOp(string(op)) {
			t.Fatalf("IsValidPowerOp(%q) = false", op)
		}
		if op.AuditAction() != want.action {
			t.Errorf("%s 审计动作 = %s，期望 %s", op, op.AuditAction(), want.action)
		}
		if op.label() != want.label {
			t.Errorf("%s 标签 = %s，期望 %s", op, op.label(), want.label)
		}
	}
	for _, invalid := range []string{"", "power_on", "on", "explode"} {
		if IsValidPowerOp(invalid) {
			t.Errorf("IsValidPowerOp(%q) 应为 false", invalid)
		}
	}
}

// TestValidatePassword 验证密码强度口径：8-64 字符且同时含字母与数字。
func TestValidatePassword(t *testing.T) {
	valid := []string{"Abcd1234", "abcd1234", "ABCD1234", "a1b2c3d4e5", strings.Repeat("a1", 32)}
	for _, password := range valid {
		if err := ValidatePassword(password); err != nil {
			t.Errorf("合法密码 %q 被拒: %v", password, err)
		}
	}
	invalid := []string{"", "abc", "12345678", "abcdefgh", "A1b2", strings.Repeat("a1", 33)}
	for _, password := range invalid {
		err := ValidatePassword(password)
		if !errors.Is(err, ErrWeakPassword) {
			t.Errorf("弱密码 %q 应返回 ErrWeakPassword，得到 %v", password, err)
		}
	}
}

// TestPreparePassword 验证空密码自动生成 16 位强密码、指定密码按强度校验。
func TestPreparePassword(t *testing.T) {
	generated, err := PreparePassword("")
	if err != nil {
		t.Fatalf("自动生成失败: %v", err)
	}
	if len([]rune(generated)) != 16 {
		t.Fatalf("自动生成长度 = %d，期望 16", len([]rune(generated)))
	}
	if err := ValidatePassword(generated); err != nil {
		t.Fatalf("自动生成的密码未通过强度校验: %v（%q）", err, generated)
	}

	specified, err := PreparePassword("MyPass123")
	if err != nil || specified != "MyPass123" {
		t.Fatalf("指定密码处理错误: %q, %v", specified, err)
	}
	if _, err := PreparePassword("12345678"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("弱密码应报错，得到 %v", err)
	}
}

// TestOsConfigOptionID 验证从商品配置缓存解析 os 配置项 ID 的各种形态。
func TestOsConfigOptionID(t *testing.T) {
	cases := []struct {
		name       string
		configJSON string
		want       int
		wantErr    bool
	}{
		{
			name:       "标准 os 项",
			configJSON: `{"config_groups":[{"id":2,"name":"系统","options":[{"id":87,"option_name":"os|操作系统"}]}]}`,
			want:       87,
		},
		{
			name:       "混合大小写与前缀",
			configJSON: `{"config_groups":[{"options":[{"id":1,"option_name":"area|区域"},{"id":2,"option_name":"OS|Operating System"}]}]}`,
			want:       2,
		},
		{
			name:       "无 os 项",
			configJSON: `{"config_groups":[{"options":[{"id":1,"option_name":"area|区域"}]}]}`,
			wantErr:    true,
		},
		{
			name:       "空配置",
			configJSON: ``,
			wantErr:    true,
		},
		{
			name:       "配置损坏",
			configJSON: `{not-json`,
			wantErr:    true,
		},
	}
	for _, tc := range cases {
		got, err := osConfigOptionID(tc.configJSON)
		if tc.wantErr {
			if !errors.Is(err, ErrNoOSOption) {
				t.Errorf("%s 应返回 ErrNoOSOption，得到 %v", tc.name, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s 解析失败: %v", tc.name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s 配置项 ID = %d，期望 %d", tc.name, got, tc.want)
		}
	}
}

// TestSanitize 验证审计消息脱敏：密码明文被替换为 ***，短串与空串不参与替换。
func TestSanitize(t *testing.T) {
	message := "重置密码失败：上游拒绝密码 Abcd1234（host 10923）"
	got := sanitize(message, "Abcd1234")
	if strings.Contains(got, "Abcd1234") || !strings.Contains(got, "***") {
		t.Fatalf("脱敏结果 = %q", got)
	}
	if sanitize("hello", "") != "hello" || sanitize("abc", "abc") != "abc" {
		t.Fatal("空串/过短敏感串不应参与替换")
	}
}
