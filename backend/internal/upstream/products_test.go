package upstream

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
)

// realCatalogBody 是上游 GET /cart/all 的真实响应片段（2026-10-08 实测，已裁剪为 1 个分组 2 个商品）。
const realCatalogBody = `{
  "products": [
    {
      "id": 1,
      "name": "二区 CN2标准性能",
      "products": [
        {"id": 1, "type": "dcimcloud", "name": "香港二区 CN2 A型",
         "description": "&lt;li&gt;CPU:2核心&lt;/li&gt;",
         "pay_type": "{\"pay_type\":\"recurring\",\"pay_hour_cycle\":\"720\"}",
         "pay_method": "prepayment", "is_local_proxy": 1,
         "allowed_proxy_modes": ["local_proxy"], "proxy_mode": "local_proxy",
         "local_proxy_module": "idcsmart_common", "module": "idcsmart_common",
         "source_type": "finance", "direct_upstream_product_id": 1},
        {"id": 2, "type": "dcimcloud", "name": "香港二区 CN2 B型", "module": "idcsmart_common"}
      ]
    }
  ],
  "count": 167,
  "currency": "CNY"
}`

func TestProductsParsesRealCatalog(t *testing.T) {
	fake := newFakeUpstream(t, map[string]handlerFunc{
		pathCartAll: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, okBody(realCatalogBody)
		},
	})
	client := fake.client(t, nil)

	catalog, err := client.Products(context.Background())
	if err != nil {
		t.Fatalf("Products() 失败: %v", err)
	}
	if catalog.Count != 167 || catalog.Currency != "CNY" {
		t.Errorf("目录元信息 = (count=%d, currency=%s), 期望 (167, CNY)", catalog.Count, catalog.Currency)
	}
	if len(catalog.Groups) != 1 || len(catalog.Groups[0].Products) != 2 {
		t.Fatalf("分组/商品数量 = %d/%d, 期望 1/2", len(catalog.Groups), len(catalog.Groups[0].Products))
	}

	first := catalog.Groups[0].Products[0]
	if first.ID != 1 || first.Name != "香港二区 CN2 A型" || first.Type != "dcimcloud" {
		t.Errorf("首个商品 = %+v, 期望 id=1 name=香港二区 CN2 A型 type=dcimcloud", first)
	}
	if first.Module != "idcsmart_common" || first.SourceType != "finance" {
		t.Errorf("模块/来源 = (%s, %s), 期望 (idcsmart_common, finance)", first.Module, first.SourceType)
	}
	if len(first.AllowedProxyModes) != 1 || first.AllowedProxyModes[0] != "local_proxy" {
		t.Errorf("allowed_proxy_modes = %v, 期望 [local_proxy]", first.AllowedProxyModes)
	}

	calls := fake.requests(pathCartAll)
	if len(calls) != 1 || calls[0].Method != http.MethodGet {
		t.Fatalf("调用 = %+v, 期望 1 次 GET", calls)
	}
}

// realProductConfigBody 是 GET /cart/get_product_config 的结构样例（字段取自上游返回定义）。
const realProductConfigBody = `{
  "flag": "1",
  "products": {"id": 1, "gid": 1, "type": "dcimcloud", "name": "香港二区 CN2 A型",
               "stock_control": 1, "qty": 71, "upstream_pid": 1, "upstream_id": 0},
  "customfields": [
    {"id": 3, "fieldname": "附加说明", "fieldtype": "text", "required": 0, "showorder": 1}
  ],
  "product_pricings": [
    {"id": 11, "type": "product", "relid": 1, "currency": 1, "code": "CNY",
     "monthly": "20.00", "quarterly": "60.00", "annually": "240.00"}
  ],
  "config_groups": [
    {"id": 5, "name": "系统配置", "description": "",
     "options": [
       {"id": 9, "gid": 5, "option_name": "操作系统", "option_type": 1, "upstream_id": 21, "hidden": 0,
        "sub": [
          {"id": 31, "config_id": 9, "option_name": "CentOS 7", "upstream_id": 301, "hidden": 0,
           "pricings": [{"id": 51, "type": "configoptions", "relid": 31, "currency": 1, "monthly": "0.00"}]}
        ]}
     ]}
  ],
  "advanced": [{"id": 7, "config_id": 9, "sub_id": [31]}],
  "config_links": [5]
}`

func TestProductConfigParsesOptionsAndPricing(t *testing.T) {
	fake := newFakeUpstream(t, map[string]handlerFunc{
		pathProductConfig: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, okBody(realProductConfigBody)
		},
	})
	client := fake.client(t, nil)

	config, err := client.ProductConfig(context.Background(), 1)
	if err != nil {
		t.Fatalf("ProductConfig() 失败: %v", err)
	}

	if config.Product.ID != 1 || config.Product.StockControl != 1 || config.Product.Qty != 71 {
		t.Errorf("商品 = %+v, 期望 id=1 stock_control=1 qty=71", config.Product)
	}
	if len(config.Pricings) != 1 || config.Pricings[0].Monthly != "20.00" || config.Pricings[0].Annually != "240.00" {
		t.Errorf("价格 = %+v, 期望 monthly=20.00 annually=240.00", config.Pricings)
	}
	if len(config.ConfigGroups) != 1 || len(config.ConfigGroups[0].Options) != 1 {
		t.Fatalf("可配置项结构 = %+v, 期望 1 组 1 项", config.ConfigGroups)
	}
	option := config.ConfigGroups[0].Options[0]
	if option.UpstreamID != 21 || option.OptionName != "操作系统" {
		t.Errorf("可配置项 = %+v, 期望 upstream_id=21 option_name=操作系统", option)
	}
	if len(option.Values) != 1 || option.Values[0].UpstreamID != 301 {
		t.Fatalf("可配置项取值 = %+v, 期望 upstream_id=301", option.Values)
	}
	if len(config.CustomFields) != 1 || config.CustomFields[0].ID != 3 {
		t.Errorf("自定义字段 = %+v, 期望 id=3", config.CustomFields)
	}
	if len(config.Advanced) != 1 || len(config.Advanced[0].SubID) != 1 || config.Advanced[0].SubID[0] != 31 {
		t.Errorf("联动规则 = %+v, 期望 sub_id=[31]", config.Advanced)
	}

	calls := fake.requests(pathProductConfig)
	if len(calls) != 1 || calls[0].Query.Get("pid") != "1" {
		t.Fatalf("请求参数 = %+v, 期望 pid=1", calls)
	}
}

func TestProductConfigRejectsBadProductID(t *testing.T) {
	fake := newFakeUpstream(t, nil)
	client := fake.client(t, nil)

	if _, err := client.ProductConfig(context.Background(), 0); !errors.Is(err, ErrBusiness) {
		t.Fatalf("pid=0 错误 = %v, 期望 ErrBusiness", err)
	}
	if got := fake.count(pathProductConfig); got != 0 {
		t.Errorf("非法参数不应发起请求，实际调用 %d 次", got)
	}
}

func TestStockAndOntrialMax(t *testing.T) {
	fake := newFakeUpstream(t, map[string]handlerFunc{
		pathStockControl: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, okBody(`{"product":{"id":1,"qty":71,"stock_control":1,"hidden":0}}`)
		},
		pathCartOntrialMax: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, okBody(`{"product":{"ontrial":1,"qty":5}}`)
		},
	})
	client := fake.client(t, nil)

	stock, err := client.Stock(context.Background(), 1)
	if err != nil {
		t.Fatalf("Stock() 失败: %v", err)
	}
	if stock.Product.Qty != 71 || stock.Product.StockControl != 1 {
		t.Errorf("库存 = %+v, 期望 qty=71 stock_control=1", stock.Product)
	}

	ontrial, err := client.OntrialMax(context.Background(), 1)
	if err != nil {
		t.Fatalf("OntrialMax() 失败: %v", err)
	}
	if ontrial.Product.Ontrial != 1 || ontrial.Product.Qty != 5 {
		t.Errorf("试用/限购 = %+v, 期望 ontrial=1 qty=5", ontrial.Product)
	}

	if got := fake.requests(pathStockControl)[0].Query.Get("pid"); got != "1" {
		t.Errorf("库存请求 pid = %q, 期望 1", got)
	}
}
