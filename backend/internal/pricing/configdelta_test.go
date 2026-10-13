package pricing

import (
	"testing"
)

// fixture 构造一段最小可用的 config_json（形状与上游 /cart/get_product_config 的 data 一致）。
func fixture(groups string) string {
	return `{"config_groups":[` + groups + `]}`
}

// option 构造一个可配置项；subs 为若干 sub 对象拼接。
func option(id int, name string, optionType int, subs string) string {
	return `{"id":` + itoa(id) + `,"option_name":"` + name + `","option_type":` + itoa(optionType) +
		`,"hidden":0,"sub":[` + subs + `]}`
}

// tier 构造一个档位（可选值）；unit 为空串表示无 pricings。
func tier(id int, name string, min, max int, unit string) string {
	pricings := `[]`
	if unit != "" {
		pricings = `[{"type":"configoptions","code":"CNY","monthly":"` + unit +
			`","quarterly":"0.00","semiannually":"0.00","annually":"0.00","biennially":"0.00","triennially":"0.00"}]`
	}
	return `{"id":` + itoa(id) + `,"option_name":"` + name + `","hidden":0,` +
		`"qty_minimum":` + itoa(min) + `,"qty_maximum":` + itoa(max) + `,"pricings":` + pricings + `}`
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	digits := ""
	for v > 0 {
		digits = string(rune('0'+v%10)) + digits
		v /= 10
	}
	if neg {
		return "-" + digits
	}
	return digits
}

// TestParseConfigPricings_SingleTierQuantity 单档位数量型：差价 = 单位价 × 数量（契约 10.2.1）。
func TestParseConfigPricings_SingleTierQuantity(t *testing.T) {
	raw := fixture(`{"id":1,"name":"g","options":[` +
		option(10, "data_disk_size|数据盘", 14, tier(37, "数据盘", 0, 256, "0.50")) + `]}`)
	index := ParseConfigPricings(raw)
	optionPricing := index.Option(10)
	if optionPricing == nil {
		t.Fatal("未解析出配置项 10")
	}

	breakdown := index.Quote(Default(), ConfigSelection{"10": "50"}, CycleMonthly, "20.00")
	if breakdown.Base != "20.00" {
		t.Fatalf("base = %q, want 20.00", breakdown.Base)
	}
	if len(breakdown.Deltas) != 1 {
		t.Fatalf("deltas = %d, want 1", len(breakdown.Deltas))
	}
	line := breakdown.Deltas[0]
	if line.Kind != DeltaKindQuantity || line.Qty != 50 || line.Billable != 50 {
		t.Fatalf("line = %+v, want qty=50 billable=50", line)
	}
	if line.Amount != "25.00" {
		t.Fatalf("amount = %q, want 25.00 (0.50 × 50)", line.Amount)
	}
	if line.Note != "" {
		t.Fatalf("note = %q, want 空", line.Note)
	}
	if breakdown.Total != "45.00" {
		t.Fatalf("total = %q, want 45.00", breakdown.Total)
	}
}

// TestParseConfigPricings_MultiTierQuantity 多档位数量型：可计费数量 = 数量 − 低一档上限。
// 实测形态：[30-30 @0.00][31-256 @0.50]（商品 2 数据盘）。
func TestParseConfigPricings_MultiTierQuantity(t *testing.T) {
	raw := fixture(`{"id":1,"name":"g","options":[` +
		option(27, "data_disk_size|数据盘", 19,
			tier(88, "", 30, 30, "0.00")+`,`+tier(10664, "", 31, 256, "0.50")) + `]}`)
	index := ParseConfigPricings(raw)

	cases := []struct {
		qty      string
		billable int
		amount   string
	}{
		{"30", 30, "0.00"},  // 落在免费档：单价 0，差价 0
		{"31", 1, "0.50"},   // 只有超出低档上限的 1 个计费
		{"50", 20, "10.00"}, // (50 − 30) × 0.50，R11 验收算例
		{"60", 30, "15.00"}, // 60 − 30 = 30
		{"256", 226, "113.00"},
	}
	for _, tc := range cases {
		breakdown := index.Quote(Default(), ConfigSelection{"27": tc.qty}, CycleMonthly, "25.00")
		if len(breakdown.Deltas) != 1 {
			t.Fatalf("qty=%s deltas=%d, want 1", tc.qty, len(breakdown.Deltas))
		}
		line := breakdown.Deltas[0]
		if line.Billable != tc.billable {
			t.Fatalf("qty=%s billable=%d, want %d", tc.qty, line.Billable, tc.billable)
		}
		if line.Amount != tc.amount {
			t.Fatalf("qty=%s amount=%q, want %q", tc.qty, line.Amount, tc.amount)
		}
	}
}

// TestParseConfigPricings_MultiTierIP 首个免费的 IP 数量档位（实测商品 74：[1-1 @0][2-10 @38]）。
func TestParseConfigPricings_MultiTierIP(t *testing.T) {
	raw := fixture(`{"id":1,"name":"g","options":[` +
		option(69000, "ip_num|IP数量", 15,
			tier(304336, "IP数量", 1, 1, "0.00")+`,`+tier(304339, "IP数量", 2, 10, "38.00")) + `]}`)
	index := ParseConfigPricings(raw)

	for _, tc := range []struct {
		qty      string
		billable int
		amount   string
	}{{"1", 1, "0.00"}, {"2", 1, "38.00"}, {"10", 9, "342.00"}} {
		breakdown := index.Quote(Default(), ConfigSelection{"69000": tc.qty}, CycleMonthly, "38.00")
		line := breakdown.Deltas[0]
		if line.Billable != tc.billable || line.Amount != tc.amount {
			t.Fatalf("qty=%s billable=%d amount=%s, want %d/%s", tc.qty, line.Billable, line.Amount, tc.billable, tc.amount)
		}
	}
}

// TestParseConfigPricings_RangeUnion 数量区间取全部档位并集（R7 修正）。
func TestParseConfigPricings_RangeUnion(t *testing.T) {
	raw := fixture(`{"id":1,"name":"g","options":[` +
		option(27, "data_disk_size|数据盘", 19,
			tier(88, "", 30, 30, "0.00")+`,`+tier(10664, "", 31, 256, "0.50")) + `]}`)
	index := ParseConfigPricings(raw)
	min, max, bounded := index.Option(27).Range()
	if !bounded || min != 30 || max != 256 {
		t.Fatalf("range = %d~%d bounded=%v, want 30~256 bounded=true", min, max, bounded)
	}

	// 单档位并集即该档位自身。
	single := fixture(`{"id":1,"name":"g","options":[` +
		option(10, "data_disk_size|数据盘", 14, tier(37, "数据盘", 0, 256, "0.50")) + `]}`)
	min, max, bounded = ParseConfigPricings(single).Option(10).Range()
	if !bounded || min != 0 || max != 256 {
		t.Fatalf("single range = %d~%d bounded=%v, want 0~256", min, max, bounded)
	}
}

// TestParseConfigPricings_Select 选项型差价 = 该值该周期的单位差价。
func TestParseConfigPricings_Select(t *testing.T) {
	raw := fixture(`{"id":1,"name":"g","options":[` +
		option(69067, "cpu|CPU", 6,
			tier(304707, "2|2核", 0, 0, "0.00")+`,`+
				tier(304708, "4|4核", 0, 0, "20.00")+`,`+
				tier(304711, "32|32核", 0, 0, "300.00")) + `]}`)
	index := ParseConfigPricings(raw)

	breakdown := index.Quote(Default(), ConfigSelection{"69067": "304708"}, CycleMonthly, "38.00")
	if breakdown.Total != "58.00" {
		t.Fatalf("total = %q, want 58.00", breakdown.Total)
	}
	if breakdown.Deltas[0].Amount != "20.00" || breakdown.Deltas[0].Kind != DeltaKindSelect {
		t.Fatalf("line = %+v, want select 20.00", breakdown.Deltas[0])
	}
	if breakdown.Deltas[0].ValueID != 304708 {
		t.Fatalf("value_id = %d, want 304708", breakdown.Deltas[0].ValueID)
	}
}

// TestParseConfigPricings_NoPricingData 无差价数据降级为 0 并标注，不阻断。
func TestParseConfigPricings_NoPricingData(t *testing.T) {
	// 子项完全没有 pricings。
	raw := fixture(`{"id":1,"name":"g","options":[` +
		option(1, "area|区域", 12, tier(1, "1|HK^香港", 0, 0, "")) + `]}`)
	index := ParseConfigPricings(raw)
	breakdown := index.Quote(Default(), ConfigSelection{"1": "1"}, CycleMonthly, "20.00")
	if breakdown.Total != "20.00" {
		t.Fatalf("total = %q, want 20.00（差价按 0）", breakdown.Total)
	}
	if breakdown.Deltas[0].Note != NoteNoPricingData {
		t.Fatalf("note = %q, want %q", breakdown.Deltas[0].Note, NoteNoPricingData)
	}

	// 有 pricings 但该周期是 -1.00 哨兵 → 该周期无差价数据，其它周期照常。
	raw2 := fixture(`{"id":1,"name":"g","options":[` +
		option(10, "data_disk_size|数据盘", 14,
			`{"id":37,"option_name":"数据盘","hidden":0,"qty_minimum":0,"qty_maximum":256,`+
				`"pricings":[{"type":"configoptions","code":"CNY","monthly":"0.50","quarterly":"0.00",`+
				`"semiannually":"0.00","annually":"0.00","biennially":"-1.00","triennially":"-1.00"}]}`) + `]}`)
	index2 := ParseConfigPricings(raw2)
	monthly := index2.Quote(Default(), ConfigSelection{"10": "50"}, CycleMonthly, "20.00")
	if monthly.Deltas[0].Amount != "25.00" || monthly.Deltas[0].Note != "" {
		t.Fatalf("monthly = %+v, want 25.00 且无标注", monthly.Deltas[0])
	}
	biennial := index2.Quote(Default(), ConfigSelection{"10": "50"}, CycleBiennially, "20.00")
	if biennial.Deltas[0].Amount != "0.00" || biennial.Deltas[0].Note != NoteNoPricingData {
		t.Fatalf("biennial = %+v, want 0.00 且标注无差价数据", biennial.Deltas[0])
	}
}

// TestParseConfigPricings_PricingModes 三定价模式下差价口径（契约 10.2.1）。
func TestParseConfigPricings_PricingModes(t *testing.T) {
	raw := fixture(`{"id":1,"name":"g","options":[` +
		option(10, "data_disk_size|数据盘", 14, tier(37, "数据盘", 0, 256, "0.50")) + `]}`)
	index := ParseConfigPricings(raw)

	markup := Rule{Mode: ModeMarkup, MarkupPercent: floatPtr(10)}
	breakdown := index.Quote(markup, ConfigSelection{"10": "50"}, CycleMonthly, "20.00")
	// 差价 0.50 → 加价 0.55，再 × 50 = 27.50（加价作用于单位差价）。
	if breakdown.Deltas[0].Amount != "27.50" {
		t.Fatalf("markup amount = %q, want 27.50", breakdown.Deltas[0].Amount)
	}
	if breakdown.Total != "47.50" {
		t.Fatalf("markup total = %q, want 47.50", breakdown.Total)
	}

	fixed := Rule{Mode: ModeFixed, Fixed: map[string]string{CycleMonthly: "25.00"}}
	breakdown = index.Quote(fixed, ConfigSelection{"10": "50"}, CycleMonthly, "25.00")
	// 固定价覆盖基础价，差价原样叠加。
	if breakdown.Base != "25.00" || breakdown.Deltas[0].Amount != "25.00" {
		t.Fatalf("fixed = base=%s delta=%s, want 25.00/25.00", breakdown.Base, breakdown.Deltas[0].Amount)
	}
	if breakdown.Total != "50.00" {
		t.Fatalf("fixed total = %q, want 50.00", breakdown.Total)
	}

	upstream := Default()
	breakdown = index.Quote(upstream, ConfigSelection{"10": "50"}, CycleMonthly, "20.00")
	if breakdown.Deltas[0].Amount != "25.00" {
		t.Fatalf("upstream amount = %q, want 25.00", breakdown.Deltas[0].Amount)
	}
}

// TestParseConfigPricings_UnknownAndSelectMixed 多项合计与未知项跳过。
func TestParseConfigPricings_UnknownAndSelectMixed(t *testing.T) {
	raw := fixture(`{"id":1,"name":"g","options":[` +
		option(10, "data_disk_size|数据盘", 14, tier(37, "数据盘", 0, 256, "0.50")) + `,` +
		option(69067, "cpu|CPU", 6, tier(304708, "4|4核", 0, 0, "20.00")) + `]}`)
	index := ParseConfigPricings(raw)

	breakdown := index.Quote(Default(), ConfigSelection{
		"10":    "50",
		"69067": "304708",
		"9999":  "1", // 未知配置项，跳过
	}, CycleMonthly, "20.00")
	if len(breakdown.Deltas) != 2 {
		t.Fatalf("deltas = %d, want 2", len(breakdown.Deltas))
	}
	if breakdown.Total != "65.00" {
		t.Fatalf("total = %q, want 65.00 (20 + 25 + 20)", breakdown.Total)
	}
	// 明细按配置项 id 升序，保证快照稳定。
	if breakdown.Deltas[0].OptionID != 10 || breakdown.Deltas[1].OptionID != 69067 {
		t.Fatalf("明细顺序不稳定: %+v", breakdown.Deltas)
	}
}

// TestParseConfigPricings_Marshal 往返序列化（落库快照）。
func TestParseConfigPricings_Marshal(t *testing.T) {
	raw := fixture(`{"id":1,"name":"g","options":[` +
		option(10, "data_disk_size|数据盘", 14, tier(37, "数据盘", 0, 256, "0.50")) + `]}`)
	breakdown := ParseConfigPricings(raw).Quote(Default(), ConfigSelection{"10": "50"}, CycleMonthly, "20.00")
	encoded, err := breakdown.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	parsed := ParseBreakdown(encoded)
	if parsed == nil || parsed.Total != "45.00" || len(parsed.Deltas) != 1 {
		t.Fatalf("往返后不一致: %+v", parsed)
	}
	if ParseBreakdown("") != nil || ParseBreakdown("not-json") != nil {
		t.Fatal("异常输入应返回 nil")
	}
}

// TestIsQuantityOptionType 数量型编码（契约 10.3 第 5 条）。
func TestIsQuantityOptionType(t *testing.T) {
	for _, optionType := range []int{4, 11, 14, 15, 19} {
		if !IsQuantityOptionType(optionType) {
			t.Fatalf("option_type=%d 应为数量型", optionType)
		}
	}
	for _, optionType := range []int{1, 5, 6, 8, 10, 12, 13} {
		if IsQuantityOptionType(optionType) {
			t.Fatalf("option_type=%d 不应为数量型", optionType)
		}
	}
}

func floatPtr(v float64) *float64 { return &v }
