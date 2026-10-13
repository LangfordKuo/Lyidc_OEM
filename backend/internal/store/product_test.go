package store

import (
	"reflect"
	"testing"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

// sampleProductInput 是导入输入样本。
func sampleProductInput() ProductInput {
	return ProductInput{
		UpstreamPID:        1,
		UpstreamGroupID:    1,
		Name:               "香港二区 CN2 A型",
		Description:        "<li>CPU:2核心</li>",
		Type:               "dcimcloud",
		Module:             "idcsmart_common",
		ConfigJSON:         `{"products":{"id":1}}`,
		UpstreamPricesJSON: `{"code":"CNY","prices":{"monthly":"20.00"}}`,
		StockQty:           70,
		OntrialMax:         0,
		Sort:               0,
	}
}

// productFromInput 把导入输入转成库内记录（用于构造「已存在且完全一致」的场景）。
func productFromInput(input ProductInput) *model.Product {
	return &model.Product{
		ID:                 10,
		UpstreamPID:        input.UpstreamPID,
		UpstreamGroupID:    input.UpstreamGroupID,
		Name:               input.Name,
		Description:        input.Description,
		Type:               input.Type,
		Module:             input.Module,
		ConfigJSON:         input.ConfigJSON,
		UpstreamPricesJSON: input.UpstreamPricesJSON,
		PricingJSON:        `{"mode":"upstream"}`,
		StockQty:           input.StockQty,
		OntrialMax:         input.OntrialMax,
		Status:             model.ProductStatusOff,
		Sort:               input.Sort,
	}
}

func TestProductUpstreamColumnsUnchanged(t *testing.T) {
	input := sampleProductInput()
	existing := productFromInput(input)

	if columns := productUpstreamColumns(existing, input, 0); len(columns) != 0 {
		t.Fatalf("上游字段完全一致时应无更新列，实际 %v", columns)
	}
}

func TestProductUpstreamColumnsIgnoresLocalFields(t *testing.T) {
	input := sampleProductInput()
	existing := productFromInput(input)

	// 本地字段（定价/状态/排序）与导入输入无关，绝不能进入更新列。
	existing.PricingJSON = `{"mode":"markup","markup_percent":20}`
	existing.Status = model.ProductStatusOn
	existing.Sort = 99

	columns := productUpstreamColumns(existing, input, 0)
	if len(columns) != 0 {
		t.Fatalf("本地字段差异不应触发更新，实际更新列 %v", columns)
	}
	for _, key := range []string{"pricing_json", "status", "sort"} {
		if _, ok := columns[key]; ok {
			t.Fatalf("更新列不应包含本地字段 %s：%v", key, columns)
		}
	}
}

func TestProductUpstreamColumnsDetectsChanges(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ProductInput)
		want   map[string]any
	}{
		{
			name:   "改名",
			mutate: func(in *ProductInput) { in.Name = "香港二区 CN2 B型" },
			want:   map[string]any{"name": "香港二区 CN2 B型"},
		},
		{
			name:   "改描述",
			mutate: func(in *ProductInput) { in.Description = "<li>CPU:4核心</li>" },
			want:   map[string]any{"description": "<li>CPU:4核心</li>"},
		},
		{
			name:   "库存变化",
			mutate: func(in *ProductInput) { in.StockQty = 0 },
			want:   map[string]any{"stock_qty": 0},
		},
		{
			name:   "试用数量变化",
			mutate: func(in *ProductInput) { in.OntrialMax = 2 },
			want:   map[string]any{"ontrial_max": 2},
		},
		{
			name:   "价格变化",
			mutate: func(in *ProductInput) { in.UpstreamPricesJSON = `{"code":"CNY","prices":{"monthly":"25.00"}}` },
			want:   map[string]any{"upstream_prices_json": `{"code":"CNY","prices":{"monthly":"25.00"}}`},
		},
		{
			name:   "配置项变化",
			mutate: func(in *ProductInput) { in.ConfigJSON = `{"products":{"id":1},"config_groups":[]}` },
			want:   map[string]any{"config_json": `{"products":{"id":1},"config_groups":[]}`},
		},
		{
			name:   "分组调整",
			mutate: func(in *ProductInput) { in.UpstreamGroupID = 2 },
			want:   map[string]any{"upstream_group_id": 2},
		},
		{
			name: "多字段同时变化",
			mutate: func(in *ProductInput) {
				in.Type = "vm"
				in.Module = ""
			},
			want: map[string]any{"type": "vm", "module": ""},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			input := sampleProductInput()
			testCase.mutate(&input)

			columns := productUpstreamColumns(productFromInput(sampleProductInput()), input, 0)
			if !reflect.DeepEqual(columns, testCase.want) {
				t.Fatalf("更新列 = %v，期望 %v", columns, testCase.want)
			}
		})
	}
}

// TestResolveCategoryIDPrefersMapping 验证 R12 归类优先级：映射（MappedCategoryID）优先于派生。
func TestResolveCategoryIDPrefersMapping(t *testing.T) {
	derived := model.ProductCategoryInput{SubKey: "z:香港二区"}
	ids := map[string]uint64{"z:香港二区": 201}

	if got := resolveCategoryID(ProductInput{MappedCategoryID: 999, Category: &derived}, ids); got != 999 {
		t.Fatalf("映射应优先于派生：got %d，want 999", got)
	}
	if got := resolveCategoryID(ProductInput{Category: &derived}, ids); got != 201 {
		t.Fatalf("无映射时按派生解析：got %d，want 201", got)
	}
	if got := resolveCategoryID(ProductInput{}, ids); got != 0 {
		t.Fatalf("无分类信息应返回 0：got %d", got)
	}
}

// TestProductUpstreamColumnsMappedCategory 验证映射归属的写入口径：
// 写 category_source=derived、命中同分类时零写入、且绝不覆盖 manual。
func TestProductUpstreamColumnsMappedCategory(t *testing.T) {
	input := sampleProductInput()
	input.MappedCategoryID = 900
	existing := productFromInput(input)
	existing.CategoryID = 201
	existing.CategorySource = model.CategorySourceDerived

	columns := productUpstreamColumns(existing, input, 900)
	if columns["category_id"] != uint64(900) || columns["category_source"] != model.CategorySourceDerived {
		t.Fatalf("映射归属应以 derived 写入：%v", columns)
	}

	// 映射结果与库内一致：零写入（幂等）。
	existing.CategoryID = 900
	if columns := productUpstreamColumns(existing, input, 900); len(columns) != 0 {
		t.Fatalf("映射结果一致时应无更新列：%v", columns)
	}

	// manual 商品一律不被映射覆盖。
	existing.CategoryID = 201
	existing.CategorySource = model.CategorySourceManual
	if columns := productUpstreamColumns(existing, input, 900); len(columns) != 0 {
		t.Fatalf("manual 不应被映射覆盖：%v", columns)
	}
}
