package pricing

import (
	"encoding/json"
	"fmt"
	"strings"
)

// UpstreamPrices 是 products.upstream_prices_json 的结构：上游价格原文 + 本次导入挑选出的六周期价格。
//
//	{
//	  "code": "CNY",
//	  "prices": {"monthly":"20.00","quarterly":"60.00","semiannually":"-1.00","annually":"200.00",
//	             "biennial":"-1.00","triennial":"-1.00"},
//	  "rows": [ ...上游 product_pricings 原文... ]
//	}
//
// Prices 的键是**定价周期名**（monthly / quarterly / semiannual / annual / biennial / triennial，
// 即 Cycles），与上游价格行的字段名（annually / semiannually / biennially / triennially）不同；
// 值保留上游原值（含 "-1.00" 这类「该周期不售」标记，由 Rule.Prices 判定为不可售）。
// Rows 是上游原文，仅管理端详情接口展示，便于与上游对账。
type UpstreamPrices struct {
	Code   string            `json:"code"`
	Prices map[string]string `json:"prices"`
	Rows   []json.RawMessage `json:"rows,omitempty"`
}

// ParseUpstreamPrices 解析库内的上游价格缓存；空串或 null 返回零值（无任何周期价格）。
func ParseUpstreamPrices(raw string) (UpstreamPrices, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "null" {
		return UpstreamPrices{}, nil
	}

	var parsed UpstreamPrices
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return UpstreamPrices{}, fmt.Errorf("上游价格缓存解析失败: %w", err)
	}
	return parsed, nil
}

// Marshal 序列化用于落库/对外输出。
func (u UpstreamPrices) Marshal() (string, error) {
	encoded, err := json.Marshal(u)
	if err != nil {
		return "", fmt.Errorf("上游价格缓存序列化失败: %w", err)
	}
	return string(encoded), nil
}
