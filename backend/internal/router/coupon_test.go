package router

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
)

func TestValidateCouponCode(t *testing.T) {
	valid := []string{"ABC", "abc", "WELCOME10", "a_1-B", "-abc", "A2345678901234567890123456789012"}
	for _, code := range valid {
		if err := validateCouponCode(code); err != nil {
			t.Errorf("validateCouponCode(%q) 期望通过，实际报错: %v", code, err)
		}
	}

	invalid := []string{"", "ab", "A23456789012345678901234567890123", "优惠码", "abc def", "abc.def", "abc@def", "a/b"}
	for _, code := range invalid {
		err := validateCouponCode(code)
		if err == nil {
			t.Errorf("validateCouponCode(%q) 期望报错，实际通过", code)
			continue
		}
		if !errors.Is(err, errCouponFormat) {
			t.Errorf("validateCouponCode(%q) 错误分类 = %v，期望格式类", code, err)
		}
	}
}

func TestValidateCouponType(t *testing.T) {
	for _, kind := range []string{pricing.CouponTypePercent, pricing.CouponTypeFixed} {
		if err := validateCouponType(kind); err != nil {
			t.Errorf("validateCouponType(%q) 期望通过，实际报错: %v", kind, err)
		}
	}
	for _, kind := range []string{"", "PERCENT", "amount", "half"} {
		err := validateCouponType(kind)
		if err == nil {
			t.Errorf("validateCouponType(%q) 期望报错，实际通过", kind)
			continue
		}
		if !errors.Is(err, errCouponFormat) {
			t.Errorf("validateCouponType(%q) 错误分类 = %v，期望格式类", kind, err)
		}
	}
}

func TestValidateCouponValue(t *testing.T) {
	cases := []struct {
		name    string
		kind    string
		value   string
		want    int64 // -1 表示期望报错
		wantErr error
	}{
		{"percent 10 合法", pricing.CouponTypePercent, "10", 1000, nil},
		{"percent 12.50 合法", pricing.CouponTypePercent, "12.50", 1250, nil},
		{"percent 0.01 合法（下限）", pricing.CouponTypePercent, "0.01", 1, nil},
		{"percent 100 合法（上限）", pricing.CouponTypePercent, "100", 10000, nil},
		{"percent 0 违规", pricing.CouponTypePercent, "0", -1, errCouponRule},
		{"percent 100.01 超上限", pricing.CouponTypePercent, "100.01", -1, errCouponRule},
		{"percent 负数", pricing.CouponTypePercent, "-1", -1, errCouponFormat},
		{"percent 三位小数", pricing.CouponTypePercent, "1.234", -1, errCouponFormat},
		{"percent 非数字", pricing.CouponTypePercent, "abc", -1, errCouponFormat},
		{"fixed 20 合法", pricing.CouponTypeFixed, "20", 2000, nil},
		{"fixed 0.01 合法", pricing.CouponTypeFixed, "0.01", 1, nil},
		{"fixed 0 违规", pricing.CouponTypeFixed, "0", -1, errCouponRule},
		{"fixed 超 DECIMAL(12,2) 上限", pricing.CouponTypeFixed, "10000000000", -1, errCouponFormat},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := validateCouponValue(testCase.kind, testCase.value)
			if testCase.wantErr == nil {
				if err != nil {
					t.Fatalf("期望通过，实际报错: %v", err)
				}
				if got != testCase.want {
					t.Fatalf("返回值 = %d，期望 %d", got, testCase.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("期望报错，实际通过（值 %d）", got)
			}
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("错误分类 = %v，期望 %v", err, testCase.wantErr)
			}
		})
	}
}

func TestValidateCouponMaxUses(t *testing.T) {
	for _, value := range []int{0, 1, 100, maxCouponMaxUses} {
		if err := validateCouponMaxUses(value); err != nil {
			t.Errorf("validateCouponMaxUses(%d) 期望通过，实际报错: %v", value, err)
		}
	}
	for _, value := range []int{-1, maxCouponMaxUses + 1} {
		err := validateCouponMaxUses(value)
		if err == nil {
			t.Errorf("validateCouponMaxUses(%d) 期望报错，实际通过", value)
			continue
		}
		if !errors.Is(err, errCouponRule) {
			t.Errorf("validateCouponMaxUses(%d) 错误分类 = %v，期望规则类", value, err)
		}
	}
}

func TestValidateCouponComment(t *testing.T) {
	if err := validateCouponComment(""); err != nil {
		t.Errorf("空备注应合法，实际报错: %v", err)
	}
	if err := validateCouponComment(strings.Repeat("码", maxCouponCommentRunes)); err != nil {
		t.Errorf("%d 个字符应合法，实际报错: %v", maxCouponCommentRunes, err)
	}
	if err := validateCouponComment(strings.Repeat("码", maxCouponCommentRunes+1)); err == nil {
		t.Error("超长备注期望报错，实际通过")
	}
}

func TestValidateCouponWindow(t *testing.T) {
	base := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	earlier := base.Add(-time.Hour)
	later := base.Add(time.Hour)

	cases := []struct {
		name      string
		startsAt  *time.Time
		expiresAt *time.Time
		wantErr   bool
	}{
		{"都为空", nil, nil, false},
		{"只给开始", &base, nil, false},
		{"只给结束", nil, &base, false},
		{"先开始后结束", &earlier, &later, false},
		{"同时刻违规", &base, &base, true},
		{"先结束后开始", &later, &earlier, true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateCouponWindow(testCase.startsAt, testCase.expiresAt)
			if testCase.wantErr {
				if err == nil {
					t.Fatal("期望报错，实际通过")
				}
				if !errors.Is(err, errCouponRule) {
					t.Fatalf("错误分类 = %v，期望规则类", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("期望通过，实际报错: %v", err)
			}
		})
	}
}

func TestNormalizeCouponCycles(t *testing.T) {
	cases := []struct {
		name   string
		cycles []string
		want   string
	}{
		{"空集合表示全部周期", nil, "[]"},
		{"单周期", []string{"annual"}, `["annual"]`},
		{"去重并去空格", []string{" annual ", "annual", "monthly"}, `["monthly","annual"]`},
		{"按标准周期顺序排列", []string{"triennial", "monthly", "biennial"}, `["monthly","biennial","triennial"]`},
		{"全 6 周期", pricing.Cycles, `["monthly","quarterly","semiannual","annual","biennial","triennial"]`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := normalizeCouponCycles(testCase.cycles)
			if err != nil {
				t.Fatalf("期望通过，实际报错: %v", err)
			}
			if got != testCase.want {
				t.Fatalf("规范化结果 = %q，期望 %q", got, testCase.want)
			}
		})
	}

	for _, cycles := range [][]string{{"weekly"}, {"annual", "ANNUAL"}, {""}, {"annually"}} {
		err := func() error { _, err := normalizeCouponCycles(cycles); return err }()
		if err == nil {
			t.Errorf("normalizeCouponCycles(%v) 期望报错，实际通过", cycles)
			continue
		}
		if !errors.Is(err, errCouponFormat) {
			t.Errorf("normalizeCouponCycles(%v) 错误分类 = %v，期望格式类", cycles, err)
		}
	}
}

func TestParseCouponCycles(t *testing.T) {
	for _, raw := range []string{"", "  ", "null"} {
		cycles, err := parseCouponCycles(raw)
		if err != nil || len(cycles) != 0 {
			t.Fatalf("parseCouponCycles(%q) = %v, err=%v，期望空集合", raw, cycles, err)
		}
	}

	cycles, err := parseCouponCycles(`["annual","monthly"]`)
	if err != nil || len(cycles) != 2 || cycles[0] != "annual" {
		t.Fatalf("parseCouponCycles 解析异常: %v, err=%v", cycles, err)
	}

	if _, err := parseCouponCycles(`{"oops":1}`); err == nil {
		t.Fatal("非数组 JSON 期望报错")
	}
}

// baseCoupon 是一张全部条件都通过校验的优惠码。
func baseCoupon() *model.Coupon {
	return &model.Coupon{
		ID:         1,
		Code:       "WELCOME10",
		Type:       pricing.CouponTypePercent,
		Value:      "10.00",
		CyclesJSON: `["annual"]`,
		Status:     model.CouponStatusOn,
	}
}

func TestCouponInvalidReasonMatrix(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	cases := []struct {
		name   string
		mutate func(*model.Coupon)
		cycles []string
		cycle  string
		want   string
	}{
		{"全部条件通过", nil, []string{"annual"}, "annual", ""},
		{"停用", func(c *model.Coupon) { c.Status = model.CouponStatusOff }, []string{"annual"}, "annual", ReasonCouponDisabled},
		{"未到生效时间", func(c *model.Coupon) { c.StartsAt = &future }, []string{"annual"}, "annual", ReasonCouponNotStarted},
		{"已过生效时间", func(c *model.Coupon) { c.StartsAt = &past }, []string{"annual"}, "annual", ""},
		{"已过期", func(c *model.Coupon) { c.ExpiresAt = &past }, []string{"annual"}, "annual", ReasonCouponExpired},
		{"未过期", func(c *model.Coupon) { c.ExpiresAt = &future }, []string{"annual"}, "annual", ""},
		{"次数用尽", func(c *model.Coupon) { c.MaxUses, c.UsedCount = 3, 3 }, []string{"annual"}, "annual", ReasonCouponUsedUp},
		{"次数超用尽", func(c *model.Coupon) { c.MaxUses, c.UsedCount = 3, 5 }, []string{"annual"}, "annual", ReasonCouponUsedUp},
		{"次数未用尽", func(c *model.Coupon) { c.MaxUses, c.UsedCount = 3, 2 }, []string{"annual"}, "annual", ""},
		{"不限次数（max_uses=0）", func(c *model.Coupon) { c.MaxUses, c.UsedCount = 0, 999 }, []string{"annual"}, "annual", ""},
		{"周期不适用", nil, []string{"annual"}, "monthly", ReasonCycleNotApplicable},
		{"空周期集合=全部周期", nil, nil, "monthly", ""},
		{"停用优先于其他原因", func(c *model.Coupon) {
			c.Status = model.CouponStatusOff
			c.ExpiresAt = &past
		}, []string{"annual"}, "monthly", ReasonCouponDisabled},
		{"过期优先于次数用尽", func(c *model.Coupon) {
			c.ExpiresAt = &past
			c.MaxUses, c.UsedCount = 1, 1
		}, []string{"annual"}, "annual", ReasonCouponExpired},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			coupon := baseCoupon()
			if testCase.mutate != nil {
				testCase.mutate(coupon)
			}
			if got := couponInvalidReason(coupon, testCase.cycles, testCase.cycle, now); got != testCase.want {
				t.Fatalf("reason = %q，期望 %q", got, testCase.want)
			}
		})
	}
}

func TestParseCouponTime(t *testing.T) {
	parsed, err := parseCouponTime("2026-10-08T20:00:00+08:00")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if parsed.Location() != time.UTC || parsed.Hour() != 12 {
		t.Fatalf("时间应转为 UTC 12 点，实际 %v", parsed)
	}

	if value, err := parseCouponTime(""); err != nil || value != nil {
		t.Fatalf("空串应返回 nil, nil，实际 %v, err=%v", value, err)
	}
	if _, err := parseCouponTime("2026-10-08 12:00:00"); !errors.Is(err, errCouponFormat) {
		t.Fatalf("非 RFC3339 时间错误 = %v，期望格式类", err)
	}
}

func TestParseCouponTimeRaw(t *testing.T) {
	value, err := parseCouponTimeRaw(json.RawMessage(`null`))
	if err != nil || value != nil {
		t.Fatalf("null 应返回 nil, nil，实际 %v, err=%v", value, err)
	}

	value, err = parseCouponTimeRaw(json.RawMessage(`"2026-10-08T12:00:00Z"`))
	if err != nil || value == nil || !value.Equal(time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("字符串解析异常: %v, err=%v", value, err)
	}

	if _, err := parseCouponTimeRaw(json.RawMessage(`123`)); !errors.Is(err, errCouponFormat) {
		t.Fatalf("数字时间错误 = %v，期望格式类", err)
	}
}
