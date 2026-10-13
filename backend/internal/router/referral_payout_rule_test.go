package router

import (
	"strings"
	"testing"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
)

// ── 折扣上限（契约 18.5）────────────────────────────────────────────────
//
// 上限 = min(settings.referral.max_coupon_percent, 邀请人的生效返佣比例)
// 「生效返佣比例」= 会员级 rebate_percent_override 优先，否则全局 default_percent。

func TestResolveCouponCapPrefersMemberOverride(t *testing.T) {
	ref := settings.Referral{DefaultPercent: "50.00", MaxCouponPercent: "80.00"}
	member := &model.Member{RebatePercentOverride: ptr("20.00")}

	cap := resolveCouponCap(ref, member)
	if cap.Cap != 2000 {
		t.Fatalf("上限 = %d，期望 2000（会员级特批 20.00 压过全局 50.00）", cap.Cap)
	}
	if cap.EffectivePercent != 2000 || cap.MaxCouponPercent != 8000 {
		t.Fatalf("来源拆分 = %d/%d，期望 2000/8000", cap.EffectivePercent, cap.MaxCouponPercent)
	}
	if cap.EffectiveSource != "会员级特批" {
		t.Fatalf("来源 = %q，期望 会员级特批", cap.EffectiveSource)
	}
}

func TestResolveCouponCapFallsBackToGlobal(t *testing.T) {
	ref := settings.Referral{DefaultPercent: "50.00", MaxCouponPercent: "80.00"}
	member := &model.Member{}

	cap := resolveCouponCap(ref, member)
	if cap.Cap != 5000 {
		t.Fatalf("上限 = %d，期望 5000（无特批 → 全局 50.00）", cap.Cap)
	}
	if cap.EffectiveSource != "全局默认" {
		t.Fatalf("来源 = %q，期望 全局默认", cap.EffectiveSource)
	}
}

func TestResolveCouponCapTakesMinOfSettingAndRatio(t *testing.T) {
	// 设置比生效比例更松 → 上限取生效比例。
	ref := settings.Referral{DefaultPercent: "20.00", MaxCouponPercent: "100.00"}
	if got := resolveCouponCap(ref, &model.Member{}).Cap; got != 2000 {
		t.Fatalf("设置更松时上限 = %d，期望 2000", got)
	}

	// 设置比生效比例更紧 → 上限取设置（管理员再收紧）。
	ref = settings.Referral{DefaultPercent: "50.00", MaxCouponPercent: "10.00"}
	if got := resolveCouponCap(ref, &model.Member{}).Cap; got != 1000 {
		t.Fatalf("设置更紧时上限 = %d，期望 1000", got)
	}
}

func TestResolveCouponCapZeroWhenRatioZero(t *testing.T) {
	// 生效比例 0.00 → 上限 0.00，任何自建券都发不出（否则白送折扣、无返佣对冲）。
	ref := settings.Referral{DefaultPercent: "0.00", MaxCouponPercent: "100.00"}
	cap := resolveCouponCap(ref, &model.Member{})
	if cap.Cap != 0 {
		t.Fatalf("比例 0 时上限 = %d，期望 0", cap.Cap)
	}
	if !strings.Contains(cap.describe(), "0.00") {
		t.Fatalf("拒绝 message 应含当前上限值，实际 %q", cap.describe())
	}
}

func TestResolveCouponCapCorruptValuesFallBackToZero(t *testing.T) {
	// 库内取值损坏（越界/非法）按 0 处理：宁可不发券，也不放行倒挂。
	ref := settings.Referral{DefaultPercent: "abc", MaxCouponPercent: "999.00"}
	cap := resolveCouponCap(ref, &model.Member{RebatePercentOverride: ptr("xyz")})
	if cap.Cap != 0 {
		t.Fatalf("取值损坏时上限 = %d，期望 0", cap.Cap)
	}
}

func TestCouponCapDescribeMentionsBothSources(t *testing.T) {
	ref := settings.Referral{DefaultPercent: "30.00", MaxCouponPercent: "50.00"}
	cap := resolveCouponCap(ref, &model.Member{})
	text := cap.describe()
	for _, want := range []string{"30.00", "50.00", "全局默认"} {
		if !strings.Contains(text, want) {
			t.Fatalf("拒绝 message %q 缺少 %q", text, want)
		}
	}
}

// ── 敏感数据脱敏（契约 18.5）────────────────────────────────────────────

func TestMaskAccountName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"张", "**"},
		{"张三", "张**"},
		{"欧阳娜娜", "欧**"},
		{"  张三  ", "张**"},
	}
	for _, c := range cases {
		if got := maskAccountName(c.in); got != c.want {
			t.Errorf("maskAccountName(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

func TestMaskAccountNo(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"6", "****"},
		{"62", "6****2"},
		{"6222", "6****2"},
		{"622202", "62****02"},
		{"6222021234567890", "62****90"},
		{"  6222021234567890  ", "62****90"},
	}
	for _, c := range cases {
		if got := maskAccountNo(c.in); got != c.want {
			t.Errorf("maskAccountNo(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

func TestMaskedAccountNoteNeverContainsRaw(t *testing.T) {
	note := maskedAccountNote("wechat", "张三", "6222021234567890")
	if strings.Contains(note, "张三") || strings.Contains(note, "6222021234567890") {
		t.Fatalf("脱敏备注泄漏原文：%q", note)
	}
	if !strings.Contains(note, "张**") || !strings.Contains(note, "62****90") {
		t.Fatalf("脱敏备注缺少掩码片段：%q", note)
	}
}

// ── 字段校验 ─────────────────────────────────────────────────────────────

func TestValidatePayoutAmount(t *testing.T) {
	if _, err := validatePayoutAmount("10.00"); err != nil {
		t.Fatalf("合法金额被拒：%v", err)
	}
	if _, err := validatePayoutAmount("abc"); err == nil {
		t.Fatal("非法写法应被拒")
	}
	if _, err := validatePayoutAmount("10.005"); err == nil {
		t.Fatal("三位小数应被拒")
	}
	if _, err := validatePayoutAmount("0"); err == nil {
		t.Fatal("0 应被拒")
	}
	if _, err := validatePayoutAmount("-1.00"); err == nil {
		t.Fatal("负数应被拒")
	}
}

func TestValidatePayoutAccount(t *testing.T) {
	if got, err := validatePayoutAccount("account_name", " 张三 "); err != nil || got != "张三" {
		t.Fatalf("合法姓名被拒或未裁剪：%q / %v", got, err)
	}
	if _, err := validatePayoutAccount("account_name", "   "); err == nil {
		t.Fatal("纯空白应被拒")
	}
	if _, err := validatePayoutAccount("account_no", strings.Repeat("a", 65)); err == nil {
		t.Fatal("超长应被拒")
	}
}

func TestParsePayoutAction(t *testing.T) {
	if got, err := parsePayoutAction("approve"); err != nil || got != payoutActionApprove {
		t.Fatalf("approve 解析 = %v/%v", got, err)
	}
	if got, err := parsePayoutAction(" REJECT "); err != nil || got != payoutActionReject {
		t.Fatalf("reject 解析 = %v/%v", got, err)
	}
	if _, err := parsePayoutAction("mark-paid"); err == nil {
		t.Fatal("非法动作应被拒")
	}
}

func ptr(value string) *string { return &value }
