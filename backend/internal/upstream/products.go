package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// 上游商品相关接口路径（实测见 docs/api-contract.md 8.3）。
const (
	pathCartAll        = "/cart/all"
	pathProductConfig  = "/cart/get_product_config"
	pathStockControl   = "/cart/stock_control"
	pathCartOntrialMax = "/cart/ontrialmax"
)

// Products 拉取上游商品目录（产品分组 → 商品），对应上游 GET /cart/all。
//
// 上游该接口在未登录时也会返回数据，本包仍要求先登录：匿名态拿到的目录可能缺少
// 按账号过滤后的库存/权限信息（见 docs/api-contract.md 8.5 第 5 条）。
func (c *Client) Products(ctx context.Context) (*ProductCatalog, error) {
	var out ProductCatalog
	if _, err := c.Get(ctx, pathCartAll, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ProductConfig 拉取单个商品的可配置项、价格周期与自定义字段，对应 GET /cart/get_product_config。
//
// 返回的 ConfigGroups[].Options[].UpstreamID / Values[].UpstreamID 即下单时需要提交的
// configoption 键（见 CreateHostRequest.ConfigOptions）。
func (c *Client) ProductConfig(ctx context.Context, productID int) (*ProductConfig, error) {
	params, err := productParams(productID)
	if err != nil {
		return nil, err
	}

	var out ProductConfig
	if _, err := c.Get(ctx, pathProductConfig, params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ProductConfigRaw 拉取商品配置并返回上游 data 原文，用于把详情原样缓存进商品表
// （config_json：可配置项、自定义字段、价格行；实测单商品最大约 22KB）。
func (c *Client) ProductConfigRaw(ctx context.Context, productID int) (json.RawMessage, error) {
	params, err := productParams(productID)
	if err != nil {
		return nil, err
	}

	resp, err := c.Get(ctx, pathProductConfig, params, nil)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// Stock 查询商品库存，对应 GET /cart/stock_control。
func (c *Client) Stock(ctx context.Context, productID int) (*Stock, error) {
	params, err := productParams(productID)
	if err != nil {
		return nil, err
	}

	var out Stock
	if _, err := c.Get(ctx, pathStockControl, params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// OntrialMax 查询商品的试用数量与最大购买数量，对应 GET /cart/ontrialmax。
func (c *Client) OntrialMax(ctx context.Context, productID int) (*OntrialMax, error) {
	params, err := productParams(productID)
	if err != nil {
		return nil, err
	}

	var out OntrialMax
	if _, err := c.Get(ctx, pathCartOntrialMax, params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// productParams 校验并构造单商品查询参数。
func productParams(productID int) (url.Values, error) {
	if productID <= 0 {
		return nil, fmt.Errorf("%w: productID 必须为正整数，收到 %d", ErrBusiness, productID)
	}
	params := url.Values{}
	params.Set("pid", strconv.Itoa(productID))
	return params, nil
}
