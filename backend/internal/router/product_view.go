package router

import (
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upstream"
)

// priceView 是六周期价格表：null 表示该周期不可售（上游无价且本地未覆盖固定价）。
type priceView map[string]*string

// configOptionValueView 是可配置项的一个可选值（upstream_id 是下单参数 configoption 的键，
// 阶段 4 下单直接回传）。
type configOptionValueView struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	UpstreamID int    `json:"upstream_id"`
}

// configOptionView 是一个可配置项；name 为上游客服文案原文（形如 `area|区域`，阶段 3a 不清洗）。
type configOptionView struct {
	ID         int                     `json:"id"`
	Name       string                  `json:"name"`
	Type       int                     `json:"type"`
	UpstreamID int                     `json:"upstream_id"`
	Values     []configOptionValueView `json:"values"`
}

// configGroupView 是可配置项分组。
type configGroupView struct {
	ID      int                `json:"id"`
	Name    string             `json:"name"`
	Options []configOptionView `json:"options"`
}

// customFieldView 是上游商品的自定义字段。
type customFieldView struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Regexpr     string `json:"regexpr"`
}

// adminProductView 是管理端商品列表项（含本地定价与计算后的售价）。
type adminProductView struct {
	ID              uint64       `json:"id"`
	UpstreamPID     int          `json:"upstream_pid"`
	UpstreamGroupID int          `json:"upstream_group_id"`
	GroupID         uint64       `json:"group_id"`
	GroupName       string       `json:"group_name"`
	Name            string       `json:"name"`
	Type            string       `json:"type"`
	Module          string       `json:"module"`
	Status          string       `json:"status"`
	Sort            int          `json:"sort"`
	StockQty        int          `json:"stock_qty"`
	StockControl    int          `json:"stock_control"`
	OntrialMax      int          `json:"ontrial_max"`
	Pricing         pricing.Rule `json:"pricing"`
	Prices          priceView    `json:"prices"`
	CreatedAt       string       `json:"created_at"`
	UpdatedAt       string       `json:"updated_at"`
}

// adminProductDetailView 是管理端商品详情（列表项 + 描述 + 可配置项 + 上游价格原文）。
type adminProductDetailView struct {
	adminProductView
	Description      string                 `json:"description"`
	DescriptionLines []string               `json:"description_lines"`
	ConfigGroups     []configGroupView      `json:"config_groups"`
	CustomFields     []customFieldView      `json:"custom_fields"`
	UpstreamPrices   pricing.UpstreamPrices `json:"upstream_prices"`
}

// adminProductListView 是管理端商品分页列表。
type adminProductListView struct {
	Items    []adminProductView `json:"items"`
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
	Total    int64              `json:"total"`
}

// adminGroupCountsView 是分组下的商品计数。
type adminGroupCountsView struct {
	Total int64 `json:"total"`
	On    int64 `json:"on"`
	Off   int64 `json:"off"`
}

// adminGroupView 是管理端分组项。
type adminGroupView struct {
	ID              uint64               `json:"id"`
	UpstreamGroupID int                  `json:"upstream_group_id"`
	Name            string               `json:"name"`
	Sort            int                  `json:"sort"`
	Products        adminGroupCountsView `json:"products"`
	CreatedAt       string               `json:"created_at"`
	UpdatedAt       string               `json:"updated_at"`
}

// memberProductView 是会员端商品列表项：不暴露任何上游 ID。
//
// DescriptionLines 是商品简介的展示行数组（R5：列表与详情都下发，商品卡直接渲染配置列表；
// 解析规则见 descriptionLines，空简介恒为 `[]`）。description 原文只在详情下发。
type memberProductView struct {
	ID               uint64    `json:"id"`
	Name             string    `json:"name"`
	Type             string    `json:"type"`
	Sort             int       `json:"sort"`
	Prices           priceView `json:"prices"`
	StockQty         int       `json:"stock_qty"`
	StockControl     int       `json:"stock_control"`
	OntrialMax       int       `json:"ontrial_max"`
	DescriptionLines []string  `json:"description_lines"`
}

// memberGroupView 是会员端分组（只含已上架商品的分组）。
type memberGroupView struct {
	ID       uint64              `json:"id"`
	Name     string              `json:"name"`
	Sort     int                 `json:"sort"`
	Products []memberProductView `json:"products"`
}

// memberProductListView 是会员端商品目录。
type memberProductListView struct {
	Groups []memberGroupView `json:"groups"`
	Total  int               `json:"total"`
}

// memberProductDetailView 是会员端商品详情（配置项 + 六周期价格 + 库存/试用信息）。
type memberProductDetailView struct {
	memberProductView
	Description  string              `json:"description"`
	Group        productGroupRefView `json:"group"`
	ConfigGroups []configGroupView   `json:"config_groups"`
	CustomFields []customFieldView   `json:"custom_fields"`
	UpdatedAt    string              `json:"updated_at"`
}

// productGroupRefView 是商品详情里的所属分组引用。
type productGroupRefView struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
}

// productPrices 解析商品的定价规则与上游价格缓存，返回本地售价、规则与上游价格。
func productPrices(product *model.Product) (pricing.Rule, pricing.UpstreamPrices, map[string]string, error) {
	rule, err := pricing.Parse(product.PricingJSON)
	if err != nil {
		return pricing.Rule{}, pricing.UpstreamPrices{}, nil, err
	}
	upstreamPrices, err := pricing.ParseUpstreamPrices(product.UpstreamPricesJSON)
	if err != nil {
		return pricing.Rule{}, pricing.UpstreamPrices{}, nil, err
	}
	return rule, upstreamPrices, rule.Prices(upstreamPrices.Prices), nil
}

// newPriceView 把六周期价格转成对外视图：六个周期的键**始终存在**，
// 不可售的周期输出 null（而不是省略键，便于前端稳定地按周期渲染）。
func newPriceView(prices map[string]string) priceView {
	view := make(priceView, len(pricing.Cycles))
	for _, cycle := range pricing.Cycles {
		amount := prices[cycle]
		if amount == "" {
			view[cycle] = nil
			continue
		}
		view[cycle] = &amount
	}
	return view
}

// newAdminProductView 组装管理端商品视图；分组可能为 nil（分组未导入时的兜底）。
func newAdminProductView(product *model.Product, group *model.ProductGroup, rule pricing.Rule, prices map[string]string, stockControl int) adminProductView {
	view := adminProductView{
		ID:              product.ID,
		UpstreamPID:     product.UpstreamPID,
		UpstreamGroupID: product.UpstreamGroupID,
		Name:            product.Name,
		Type:            product.Type,
		Module:          product.Module,
		Status:          product.Status,
		Sort:            product.Sort,
		StockQty:        product.StockQty,
		StockControl:    stockControl,
		OntrialMax:      product.OntrialMax,
		Pricing:         rule,
		Prices:          newPriceView(prices),
		CreatedAt:       formatTime(product.CreatedAt),
		UpdatedAt:       formatTime(product.UpdatedAt),
	}
	if group != nil {
		view.GroupID = group.ID
		view.GroupName = group.Name
	}
	return view
}

// newMemberProductView 组装会员端商品视图。
func newMemberProductView(product *model.Product, prices map[string]string, stockControl int) memberProductView {
	return memberProductView{
		ID:               product.ID,
		Name:             product.Name,
		Type:             product.Type,
		Sort:             product.Sort,
		Prices:           newPriceView(prices),
		StockQty:         product.StockQty,
		StockControl:     stockControl,
		OntrialMax:       product.OntrialMax,
		DescriptionLines: descriptionLines(product.Description),
	}
}

// productConfigCache 是 config_json 的解析视图（上游 /cart/get_product_config 的 data 原文）。
type productConfigCache struct {
	Product      upstream.Product       `json:"products"`
	ConfigGroups []upstream.ConfigGroup `json:"config_groups"`
	CustomFields []upstream.CustomField `json:"customfields"`
}

// newConfigGroupViews 把上游可配置项转成对外视图。
//
// includeHidden=false（会员端）时过滤掉上游标记为隐藏（hidden != 0）的项与值；
// includeHidden=true（管理端）时全量返回，便于管理员了解商品全部可配置项。
func newConfigGroupViews(cache productConfigCache, includeHidden bool) []configGroupView {
	groups := make([]configGroupView, 0, len(cache.ConfigGroups))
	for _, source := range cache.ConfigGroups {
		options := make([]configOptionView, 0, len(source.Options))
		for _, option := range source.Options {
			if option.Hidden != 0 && !includeHidden {
				continue
			}
			values := make([]configOptionValueView, 0, len(option.Values))
			for _, value := range option.Values {
				if value.Hidden != 0 && !includeHidden {
					continue
				}
				values = append(values, configOptionValueView{
					ID:         value.ID,
					Name:       value.OptionName,
					UpstreamID: value.UpstreamID,
				})
			}
			options = append(options, configOptionView{
				ID:         option.ID,
				Name:       option.OptionName,
				Type:       option.OptionType,
				UpstreamID: option.UpstreamID,
				Values:     values,
			})
		}
		groups = append(groups, configGroupView{
			ID:      source.ID,
			Name:    source.Name,
			Options: options,
		})
	}
	return groups
}

// newCustomFieldViews 把上游自定义字段转成对外视图。
func newCustomFieldViews(cache productConfigCache) []customFieldView {
	fields := make([]customFieldView, 0, len(cache.CustomFields))
	for _, field := range cache.CustomFields {
		fields = append(fields, customFieldView{
			ID:          field.ID,
			Name:        field.FieldName,
			Description: field.Description,
			Type:        field.FieldType,
			Required:    field.Required != 0,
			Regexpr:     field.Regexpr,
		})
	}
	return fields
}
