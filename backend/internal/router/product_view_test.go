package router

import (
	"strconv"
	"testing"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upstream"
)

// R11（v21）：数量型多档位的价格联动修复。
//
// 根因是 R7 的并集归一化把每档 qty 区间改写成并集，前端按区间挑档位永远命中第一档。
// 本文件钉住两件事：
//  1. values[].qty_minimum/qty_maximum 仍是并集（控件范围，v20 语义不变，向后兼容）；
//  2. tiers[] 给出每档真实区间，且**用 tiers 算出的预览 = 后端 Quote**（前后端同口径）。
//
// 算例与 frontend/src/lib/priceQuote.test.ts 的「前后端同口径对照」逐条一致，改一处必须同步另一处。

// bTypeDataDiskJSON 是商品 2「香港二区 CN2 B型」数据盘的上游 config_json 片段：
// 两个档位 [30-30 @0.00] + [31-256 @0.50]（实测数据）。
const bTypeDataDiskJSON = `{"config_groups":[{"id":3,"name":"数据盘","options":[{` +
	`"id":27,"gid":3,"option_name":"data_disk_size|数据盘","option_type":19,"upstream_id":0,"hidden":0,"sub":[` +
	`{"id":88,"config_id":27,"option_name":"数据盘","upstream_id":0,"hidden":0,"qty_minimum":30,"qty_maximum":30,` +
	`"pricings":[{"type":"configoptions","code":"CNY","monthly":"0.00","quarterly":"0.00","semiannually":"0.00",` +
	`"annually":"0.00","biennially":"0.00","triennially":"0.00"}]},` +
	`{"id":10664,"config_id":27,"option_name":"数据盘","upstream_id":0,"hidden":0,"qty_minimum":31,"qty_maximum":256,` +
	`"pricings":[{"type":"configoptions","code":"CNY","monthly":"0.50","quarterly":"1.50","semiannually":"3.00",` +
	`"annually":"6.00","biennially":"0.00","triennially":"0.00"}]}]}]}]}`

// aTypeDataDiskJSON 是商品 1「香港二区 CN2 A型」数据盘：单档位 [0-256 @0.50]。
const aTypeDataDiskJSON = `{"config_groups":[{"id":3,"name":"数据盘","options":[{` +
	`"id":10,"gid":3,"option_name":"data_disk_size|数据盘","option_type":14,"upstream_id":0,"hidden":0,"sub":[` +
	`{"id":37,"config_id":10,"option_name":"数据盘","upstream_id":0,"hidden":0,"qty_minimum":0,"qty_maximum":256,` +
	`"pricings":[{"type":"configoptions","code":"CNY","monthly":"0.50","quarterly":"1.50","semiannually":"3.00",` +
	`"annually":"6.00","biennially":"0.00","triennially":"0.00"}]}]}]}]}`

func cacheOf(raw string, optionID, optionType, valueCount int) productConfigCache {
	values := make([]upstream.ConfigOptionValue, 0, valueCount)
	if valueCount == 2 {
		values = []upstream.ConfigOptionValue{
			{ID: 88, ConfigID: optionID, OptionName: "数据盘", QtyMinimum: 30, QtyMaximum: 30},
			{ID: 10664, ConfigID: optionID, OptionName: "数据盘", QtyMinimum: 31, QtyMaximum: 256},
		}
	} else {
		values = []upstream.ConfigOptionValue{
			{ID: 37, ConfigID: optionID, OptionName: "数据盘", QtyMinimum: 0, QtyMaximum: 256},
		}
	}
	return productConfigCache{
		Product: upstream.Product{ID: 1},
		ConfigGroups: []upstream.ConfigGroup{{
			ID: 3,
			Options: []upstream.ConfigOption{{
				ID:         optionID,
				GroupID:    3,
				OptionName: "data_disk_size|数据盘",
				OptionType: optionType,
				Values:     values,
			}},
		}},
		RawJSON: raw,
	}
}

// previewQuantityDelta 按**前端 priceQuote.ts 的公式**从视图算出数量型差价（单位：分）。
// 与 quotePrice/billableQuantity 逐行同构：挑档位用 tiers 的真实区间，
// 可计费数量 = qty − 低一档的 qty_max（无低一档时为 0）。
func previewQuantityDelta(t *testing.T, option configOptionView, qty int, cycle string) int64 {
	t.Helper()
	if len(option.Tiers) == 0 {
		t.Fatalf("数量型配置项 %d 未下发 tiers", option.ID)
	}
	var picked *configTierView
	for i := range option.Tiers {
		if qty >= option.Tiers[i].QtyMin && qty <= option.Tiers[i].QtyMax {
			picked = &option.Tiers[i]
			break
		}
	}
	if picked == nil {
		t.Fatalf("qty=%d 不在任何档位内: %+v", qty, option.Tiers)
	}
	floor := 0
	for _, tier := range option.Tiers {
		if tier.QtyMax < picked.QtyMin && tier.QtyMax > floor {
			floor = tier.QtyMax
		}
	}
	billable := qty - floor
	if billable < 0 {
		billable = 0
	}
	raw := picked.Pricings[cycle]
	if raw == nil {
		return 0
	}
	cents, err := pricing.ParseAmount(*raw)
	if err != nil {
		t.Fatalf("档位单价非法 %q: %v", *raw, err)
	}
	return cents * int64(billable)
}

// assertMatchesBackendQuote 前后端同口径对照：视图 tiers 算出的预览 = 后端 Quote 的差价行。
func assertMatchesBackendQuote(t *testing.T, cache productConfigCache, option configOptionView, qty int, cycle string) {
	t.Helper()
	want := previewQuantityDelta(t, option, qty, cycle)
	breakdown := pricing.ParseConfigPricings(cache.RawJSON).Quote(
		pricing.Default(), pricing.ConfigSelection{strconv.Itoa(option.ID): strconv.Itoa(qty)}, cycle, "25.00")
	if len(breakdown.Deltas) != 1 {
		t.Fatalf("qty=%d Quote 差价行 = %d, want 1", qty, len(breakdown.Deltas))
	}
	got, err := pricing.ParseAmount(breakdown.Deltas[0].Amount)
	if err != nil {
		t.Fatalf("Quote 金额非法 %q: %v", breakdown.Deltas[0].Amount, err)
	}
	if got != want {
		t.Fatalf("qty=%d 前端预览 %d 分 ≠ 后端 Quote %d 分（%s）", qty, want, got, breakdown.Deltas[0].Amount)
	}
}

// TestConfigGroupViewsKeepUnionAndExposeTiers 多档位数量型：values 保持并集，tiers 给真实区间（R11）。
func TestConfigGroupViewsKeepUnionAndExposeTiers(t *testing.T) {
	cache := cacheOf(bTypeDataDiskJSON, 27, 19, 2)
	groups := newConfigGroupViews(cache, pricing.Default(), false)
	if len(groups) != 1 || len(groups[0].Options) != 1 {
		t.Fatalf("视图结构 = %+v", groups)
	}
	option := groups[0].Options[0]

	// 一、values[] 仍是并集 30~256（v20 语义，控件范围 / 旧客户端不受影响）。
	if len(option.Values) != 2 {
		t.Fatalf("values = %d, want 2", len(option.Values))
	}
	for _, value := range option.Values {
		if value.QtyMinimum != 30 || value.QtyMaximum != 256 {
			t.Fatalf("值 %d 区间 = %d~%d, want 并集 30~256", value.ID, value.QtyMinimum, value.QtyMaximum)
		}
	}

	// 二、tiers[] 是真实档位区间，按 qty_min 升序。
	if len(option.Tiers) != 2 {
		t.Fatalf("tiers = %d, want 2", len(option.Tiers))
	}
	want := []configTierView{
		{ValueID: 88, QtyMin: 30, QtyMax: 30},
		{ValueID: 10664, QtyMin: 31, QtyMax: 256},
	}
	for i, tier := range option.Tiers {
		if tier.ValueID != want[i].ValueID || tier.QtyMin != want[i].QtyMin || tier.QtyMax != want[i].QtyMax {
			t.Fatalf("tiers[%d] = %+v, want %+v", i, tier, want[i])
		}
	}

	// 三、档位单价：免费档 0.00 / 计费档 0.50（月付）。
	if got := *option.Tiers[0].Pricings[pricing.CycleMonthly]; got != "0.00" {
		t.Fatalf("免费档月付 = %s, want 0.00", got)
	}
	if got := *option.Tiers[1].Pricings[pricing.CycleMonthly]; got != "0.50" {
		t.Fatalf("计费档月付 = %s, want 0.50", got)
	}
}

// TestConfigGroupViewsQuantityAcceptanceCases R11 验收算例 + 前后端同口径对照（B 型多档位）。
func TestConfigGroupViewsQuantityAcceptanceCases(t *testing.T) {
	cache := cacheOf(bTypeDataDiskJSON, 27, 19, 2)
	option := newConfigGroupViews(cache, pricing.Default(), false)[0].Options[0]

	cases := []struct {
		qty   int
		delta string
	}{
		{30, "0.00"},  // 免费档
		{31, "0.50"},  // 计费档起点，billable = 31 − 30 = 1
		{50, "10.00"}, // (50 − 30) × 0.50
	}
	for _, tc := range cases {
		preview := previewQuantityDelta(t, option, tc.qty, pricing.CycleMonthly)
		want, err := pricing.ParseAmount(tc.delta)
		if err != nil {
			t.Fatalf("算例金额非法 %q: %v", tc.delta, err)
		}
		if preview != want {
			t.Fatalf("qty=%d 前端预览 = %d 分, want %s（%d 分）", tc.qty, preview, tc.delta, want)
		}
		assertMatchesBackendQuote(t, cache, option, tc.qty, pricing.CycleMonthly)
	}
}

// TestConfigGroupViewsQuantitySingleTierRegression A 型单档位回归：billable = qty（无低一档）。
func TestConfigGroupViewsQuantitySingleTierRegression(t *testing.T) {
	cache := cacheOf(aTypeDataDiskJSON, 10, 14, 1)
	option := newConfigGroupViews(cache, pricing.Default(), false)[0].Options[0]

	if len(option.Tiers) != 1 {
		t.Fatalf("tiers = %d, want 1", len(option.Tiers))
	}
	if option.Tiers[0].QtyMin != 0 || option.Tiers[0].QtyMax != 256 {
		t.Fatalf("单档区间 = %d~%d, want 0~256", option.Tiers[0].QtyMin, option.Tiers[0].QtyMax)
	}
	// 单档时并集即自身，values 与 tiers 区间一致。
	if option.Values[0].QtyMinimum != 0 || option.Values[0].QtyMaximum != 256 {
		t.Fatalf("values 区间 = %d~%d, want 0~256", option.Values[0].QtyMinimum, option.Values[0].QtyMaximum)
	}

	for _, tc := range []struct {
		qty   int
		delta string
	}{{0, "0.00"}, {50, "25.00"}} {
		preview := previewQuantityDelta(t, option, tc.qty, pricing.CycleMonthly)
		want, err := pricing.ParseAmount(tc.delta)
		if err != nil {
			t.Fatalf("算例金额非法 %q: %v", tc.delta, err)
		}
		if preview != want {
			t.Fatalf("qty=%d 前端预览 = %d 分, want %s（%d 分）", tc.qty, preview, tc.delta, want)
		}
		assertMatchesBackendQuote(t, cache, option, tc.qty, pricing.CycleMonthly)
	}
}

// TestConfigGroupViewsSelectHasNoTiers 选项型不下发 tiers（omitempty）。
func TestConfigGroupViewsSelectHasNoTiers(t *testing.T) {
	raw := `{"config_groups":[{"id":1,"name":"区域","options":[{` +
		`"id":1,"gid":1,"option_name":"area|区域","option_type":12,"upstream_id":0,"hidden":0,"sub":[` +
		`{"id":11,"config_id":1,"option_name":"1|HK^香港","upstream_id":0,"hidden":0,` +
		`"qty_minimum":0,"qty_maximum":0,"pricings":[]}]}]}]}`
	cache := productConfigCache{
		ConfigGroups: []upstream.ConfigGroup{{
			ID: 1,
			Options: []upstream.ConfigOption{{
				ID: 1, GroupID: 1, OptionName: "area|区域", OptionType: 12,
				Values: []upstream.ConfigOptionValue{{ID: 11, ConfigID: 1, OptionName: "1|HK^香港"}},
			}},
		}},
		RawJSON: raw,
	}
	option := newConfigGroupViews(cache, pricing.Default(), false)[0].Options[0]
	if len(option.Tiers) != 0 {
		t.Fatalf("选项型下发了 tiers: %+v", option.Tiers)
	}
}
