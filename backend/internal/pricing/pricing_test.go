package pricing

import (
	"errors"
	"testing"
)

// fullUpstream 是四周期都可用的一组上游价。
func fullUpstream() map[string]string {
	return map[string]string{
		CycleMonthly:      "20.00",
		CycleQuarterly:    "60.00",
		CycleSemiAnnually: "120.00",
		CycleAnnually:     "200.00",
	}
}

// assertPrices 断言四个周期的计算结果。
func assertPrices(t *testing.T, rule Rule, upstream map[string]string, want map[string]string) {
	t.Helper()

	got := rule.Prices(upstream)
	for _, cycle := range Cycles {
		if got[cycle] != want[cycle] {
			t.Errorf("周期 %s 价格 = %q，期望 %q（完整结果 %v）", cycle, got[cycle], want[cycle], got)
		}
	}
}

func TestParseDefaultsToUpstream(t *testing.T) {
	for _, raw := range []string{"", "null", "{}", `{"mode":"upstream"}`, `{"mode":"UPSTREAM"}`} {
		rule, err := Parse(raw)
		if err != nil {
			t.Fatalf("Parse(%q) 返回错误: %v", raw, err)
		}
		if rule.Mode != ModeUpstream {
			t.Errorf("Parse(%q).Mode = %q，期望 %q", raw, rule.Mode, ModeUpstream)
		}
	}
}

func TestParseNormalizesRule(t *testing.T) {
	rule, err := Parse(`{"mode":"Markup","markup_percent":10.5,"fixed":{"annual":"200.00"}}`)
	if err != nil {
		t.Fatalf("Parse 返回错误: %v", err)
	}
	if rule.Mode != ModeMarkup {
		t.Errorf("Mode = %q，期望 %q", rule.Mode, ModeMarkup)
	}
	if rule.MarkupPercent == nil || *rule.MarkupPercent != 10.5 {
		t.Errorf("MarkupPercent = %v，期望 10.5", rule.MarkupPercent)
	}
	if rule.Fixed[CycleAnnually] != "200.00" {
		t.Errorf("Fixed[annual] = %q，期望 200.00", rule.Fixed[CycleAnnually])
	}
}

func TestParseRejectsInvalidRules(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want error
	}{
		{"非法 JSON", `{"mode":`, ErrFormat},
		{"未知字段（拼写错误）", `{"mode":"markup","markup_percentt":10}`, ErrFormat},
		{"未知模式", `{"mode":"percent"}`, ErrFormat},
		{"未知周期", `{"mode":"fixed","fixed":{"weekly":"1.00"}}`, ErrFormat},
		{"金额格式非法", `{"mode":"fixed","fixed":{"monthly":"25 元"}}`, ErrFormat},
		{"金额小数超过两位", `{"mode":"fixed","fixed":{"monthly":"25.001"}}`, ErrFormat},
		{"金额为负", `{"mode":"fixed","fixed":{"monthly":"-25.00"}}`, ErrFormat},
		{"加价率小数超过两位", `{"mode":"markup","markup_percent":10.123}`, ErrFormat},
		{"结尾有多余内容", `{"mode":"upstream"} {}`, ErrFormat},
		{"markup 缺加价率", `{"mode":"markup"}`, ErrRule},
		{"upstream 带加价率", `{"mode":"upstream","markup_percent":10}`, ErrRule},
		{"upstream 带固定价", `{"mode":"upstream","fixed":{"monthly":"1.00"}}`, ErrRule},
		{"fixed 无固定价", `{"mode":"fixed"}`, ErrRule},
		{"fixed 只给空对象", `{"mode":"fixed","fixed":{}}`, ErrRule},
		{"加价率低于下限", `{"mode":"markup","markup_percent":-100.01}`, ErrRule},
		{"加价率高于上限", `{"mode":"markup","markup_percent":1000.01}`, ErrRule},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := Parse(testCase.raw)
			if err == nil {
				t.Fatalf("Parse(%q) 期望报错，实际通过", testCase.raw)
			}
			if !errors.Is(err, testCase.want) {
				t.Fatalf("Parse(%q) 错误 = %v，期望可判定为 %v", testCase.raw, err, testCase.want)
			}
		})
	}
}

func TestPricesUpstreamMode(t *testing.T) {
	rule := Default()
	assertPrices(t, rule, fullUpstream(), map[string]string{
		CycleMonthly:      "20.00",
		CycleQuarterly:    "60.00",
		CycleSemiAnnually: "120.00",
		CycleAnnually:     "200.00",
	})
}

func TestPricesMarkupRoundsHalfUp(t *testing.T) {
	cases := []struct {
		name        string
		upstream    string
		percent     float64
		wantMonthly string
	}{
		{"常规加价", "20.00", 10, "22.00"},
		{"小数进位（20.01×1.10=22.011）", "20.01", 10, "22.01"},
		{"半进位（20.05×1.10=22.055）", "20.05", 10, "22.06"},
		{"零值", "0.00", 10, "0.00"},
		{"加价两位小数（×1.125）", "20.00", 12.5, "22.50"},
		{"负加价即折扣", "20.00", -10, "18.00"},
		{"加价 100%", "20.00", 100, "40.00"},
		{"加价 -100% 归零", "20.00", -100, "0.00"},
		{"小额分进位（0.01×1.5=0.015）", "0.01", 50, "0.02"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			percent := testCase.percent
			rule := Rule{Mode: ModeMarkup, MarkupPercent: &percent}
			upstream := fullUpstream()
			upstream[CycleMonthly] = testCase.upstream
			prices := rule.Prices(upstream)
			if prices[CycleMonthly] != testCase.wantMonthly {
				t.Fatalf("月付 = %q，期望 %q", prices[CycleMonthly], testCase.wantMonthly)
			}
		})
	}
}

func TestPricesFixedOverridesAndFallsBack(t *testing.T) {
	rule := Rule{Mode: ModeFixed, Fixed: map[string]string{
		CycleMonthly:  "25.00",
		CycleAnnually: "200.00",
	}}
	// 上游缺 semiannual 且值为 -1.00（上游用负值表示该周期不售）。
	upstream := map[string]string{
		CycleMonthly:      "20.00",
		CycleQuarterly:    "60.00",
		CycleSemiAnnually: "-1.00",
		CycleAnnually:     "210.00",
	}
	assertPrices(t, rule, upstream, map[string]string{
		CycleMonthly:      "25.00",  // 固定价覆盖
		CycleQuarterly:    "60.00",  // 回落上游价
		CycleSemiAnnually: "",       // 上游不售且无固定价 → 不可售
		CycleAnnually:     "200.00", // 固定价覆盖上游价
	})
}

func TestPricesFixedWithoutUpstreamPrice(t *testing.T) {
	// 上游完全没有价格行时，固定价仍可单独定价（固定价不依赖上游价）。
	rule := Rule{Mode: ModeFixed, Fixed: map[string]string{CycleMonthly: "9.90"}}
	assertPrices(t, rule, nil, map[string]string{
		CycleMonthly:      "9.90",
		CycleQuarterly:    "",
		CycleSemiAnnually: "",
		CycleAnnually:     "",
	})
}

func TestPricesMarkupCombinedWithFixed(t *testing.T) {
	percent := 10.0
	rule := Rule{Mode: ModeMarkup, MarkupPercent: &percent, Fixed: map[string]string{
		CycleMonthly: "25.00",
	}}
	assertPrices(t, rule, fullUpstream(), map[string]string{
		CycleMonthly:      "25.00", // 固定价优先
		CycleQuarterly:    "66.00", // 60.00 ×1.10
		CycleSemiAnnually: "132.00",
		CycleAnnually:     "220.00",
	})
}

func TestPricesIgnoresUnusableUpstreamValues(t *testing.T) {
	rule := Default()
	upstream := map[string]string{
		CycleMonthly:      "",      // 缺省
		CycleQuarterly:    "abc",   // 非法
		CycleSemiAnnually: "-1.00", // 上游标记不售
		CycleAnnually:     " 200 ", // 前后空格可解析
	}
	assertPrices(t, rule, upstream, map[string]string{
		CycleMonthly:      "",
		CycleQuarterly:    "",
		CycleSemiAnnually: "",
		CycleAnnually:     "200.00",
	})
}

func TestParseAmount(t *testing.T) {
	valid := map[string]int64{
		"0":               0,
		"0.00":            0,
		"20":              2000,
		"20.5":            2050,
		"20.05":           2005,
		" 20.05 ":         2005,
		"999999999999.99": maxAmountCents,
	}
	for input, want := range valid {
		got, err := ParseAmount(input)
		if err != nil {
			t.Errorf("ParseAmount(%q) 返回错误: %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("ParseAmount(%q) = %d，期望 %d", input, got, want)
		}
	}

	invalid := []string{"", "abc", "-1", "-0.01", "1.234", "1.", ".5", "1,5", "1e3", "1000000000000", "999999999999.999"}
	for _, input := range invalid {
		if got, err := ParseAmount(input); err == nil {
			t.Errorf("ParseAmount(%q) 期望报错，实际返回 %d", input, got)
		}
	}
}

func TestFormatAmount(t *testing.T) {
	cases := map[int64]string{
		0:     "0.00",
		5:     "0.05",
		2000:  "20.00",
		2005:  "20.05",
		99999: "999.99",
	}
	for input, want := range cases {
		if got := FormatAmount(input); got != want {
			t.Errorf("FormatAmount(%d) = %q，期望 %q", input, got, want)
		}
	}
}

func TestNormalizeProducesCanonicalJSON(t *testing.T) {
	normalized, err := Normalize(`{"fixed":{"annual":"200.00","monthly":"25.00"},"markup_percent":10,"mode":"markup"}`)
	if err != nil {
		t.Fatalf("Normalize 返回错误: %v", err)
	}
	want := `{"mode":"markup","markup_percent":10,"fixed":{"annual":"200.00","monthly":"25.00"}}`
	if normalized != want {
		t.Fatalf("Normalize = %q，期望 %q", normalized, want)
	}

	if _, err := Normalize(`{"mode":"nope"}`); !errors.Is(err, ErrFormat) {
		t.Fatalf("Normalize 非法规则错误 = %v，期望可判定为 ErrFormat", err)
	}
}

func TestParseUpstreamPrices(t *testing.T) {
	// 注意键名是定价周期名（annual），不是上游字段名（annually）。
	raw := `{"code":"CNY","prices":{"monthly":"20.00","quarterly":"60.00","semiannual":"-1.00","annual":"200.00"},` +
		`"rows":[{"id":1,"monthly":"20.00"},{"id":2,"monthly":"5.00"}]}`
	parsed, err := ParseUpstreamPrices(raw)
	if err != nil {
		t.Fatalf("ParseUpstreamPrices 返回错误: %v", err)
	}
	if parsed.Code != "CNY" || len(parsed.Rows) != 2 {
		t.Fatalf("解析结果异常: %+v", parsed)
	}

	rule := Default()
	prices := rule.Prices(parsed.Prices)
	if prices[CycleSemiAnnually] != "" || prices[CycleAnnually] != "200.00" {
		t.Fatalf("上游价格计算异常: %v", prices)
	}

	empty, err := ParseUpstreamPrices("")
	if err != nil || len(empty.Prices) != 0 {
		t.Fatalf("空缓存应解析为零值，实际 %+v, err=%v", empty, err)
	}

	if _, err := ParseUpstreamPrices("{oops"); err == nil {
		t.Fatal("非法 JSON 期望报错")
	}
}
