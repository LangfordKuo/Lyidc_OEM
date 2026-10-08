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

	if columns := productUpstreamColumns(existing, input); len(columns) != 0 {
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

	columns := productUpstreamColumns(existing, input)
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

			columns := productUpstreamColumns(productFromInput(sampleProductInput()), input)
			if !reflect.DeepEqual(columns, testCase.want) {
				t.Fatalf("更新列 = %v，期望 %v", columns, testCase.want)
			}
		})
	}
}
