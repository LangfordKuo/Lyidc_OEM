package router

import (
	"sort"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upstream"
)

// priceView 是六周期价格表：null 表示该周期不可售（上游无价且本地未覆盖固定价）。
type priceView map[string]*string

// configOptionValueView 是可配置项的一个可选值（upstream_id 是下单参数 configoption 的键，
// 阶段 4 下单直接回传）。
//
// R6：qty_minimum / qty_maximum 是数量型（拉条型）配置的取值上下界，前端按此渲染
// 数字输入控件；选项型恒为 0。
// R7：Pricings 是该值六周期的**配置差价**（已按定价模式换算，数量型为单位差价），
// null 表示该周期无差价数据（契约 10.3 第 8 条）。
//
// R11（v21）：数量型下这两个字段是**全部可见档位的并集**（只给数量控件定 min/max 与默认值，
// 语义自 v20 起不变，向后兼容）。每档**真实区间**在 configOptionView.Tiers 里下发——
// 前端挑档位、算可计费数量必须用 Tiers，不能用这里的并集反推（并集后每档区间都相同，
// 按区间挑会永远命中第一档，见 10.2.1「档位区间怎么拿到」）。
type configOptionValueView struct {
	ID         int                `json:"id"`
	Name       string             `json:"name"`
	UpstreamID int                `json:"upstream_id"`
	QtyMinimum int                `json:"qty_minimum"`
	QtyMaximum int                `json:"qty_maximum"`
	Pricings   map[string]*string `json:"pricings"`
}

// configTierView 是数量型配置一个价格档位的真实区间与单位差价（契约 10.3 第 6 条，v21/R11）。
//
// ValueID 对应 values[].id（同一子项 = 同一档位）；QtyMin/QtyMax 是上游 sub 的原始 qty 区间
// （**不是**并集）；Pricings 与 values[].pricings 同形同口径。
// 按 QtyMin 升序下发；会员端同样剔除 hidden != 0 的档位。
type configTierView struct {
	ValueID  int                `json:"value_id"`
	QtyMin   int                `json:"qty_min"`
	QtyMax   int                `json:"qty_max"`
	Pricings map[string]*string `json:"pricings"`
}

// configOptionView 是一个可配置项；name 为上游客服文案原文（形如 `area|区域`，阶段 3a 不清洗）。
//
// Tiers 仅数量型下发（选项型 omitempty 不出现该键）：见 configTierView。
type configOptionView struct {
	ID         int                     `json:"id"`
	Name       string                  `json:"name"`
	Type       int                     `json:"type"`
	UpstreamID int                     `json:"upstream_id"`
	Values     []configOptionValueView `json:"values"`
	Tiers      []configTierView        `json:"tiers,omitempty"`
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

// adminCategoryRefView 是管理端商品视图里的分类精简引用（R9），
// 与会员端 memberCategoryRefView 同形（id + name），形状对齐 group.parent。
type adminCategoryRefView struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
}

// adminCategoryInfo 是商品所属分类的解析结果（R9）：ID 为**子分类** ID（0 = 未归类），
// Name 为子分类名，Parent 为主分类引用。未归类或分类行缺失时三者都是零值，
// 视图输出 0 / 空串 / `{id:0,name:""}`（契约 10.4：三键始终存在，且不回退成组名）。
type adminCategoryInfo struct {
	ID     uint64
	Name   string
	Parent adminCategoryRefView
}

// adminCategoryIndex 是 id → 分类行索引：列表/详情都一次性加载全部分类（表很小），
// 避免逐商品查库（N+1）。
type adminCategoryIndex map[uint64]model.ProductCategory

func newAdminCategoryIndex(items []model.ProductCategory) adminCategoryIndex {
	index := make(adminCategoryIndex, len(items))
	for i := range items {
		index[items[i].ID] = items[i]
	}
	return index
}

// resolve 解析商品挂到的子分类与所属主分类。
func (idx adminCategoryIndex) resolve(product *model.Product) adminCategoryInfo {
	if product.CategoryID == 0 {
		return adminCategoryInfo{}
	}
	sub, ok := idx[product.CategoryID]
	if !ok {
		// 分类行缺失（异常数据）：按未归类输出，不回退组名。
		return adminCategoryInfo{}
	}
	info := adminCategoryInfo{ID: sub.ID, Name: sub.Name}
	if sub.ParentID != nil {
		if parent, ok := idx[*sub.ParentID]; ok {
			info.Parent = adminCategoryRefView{ID: parent.ID, Name: parent.Name}
		}
	}
	return info
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
	// R9 分类归属（契约 10.4）：与 group_id / group_name（上游产品组）并列、互不替代。
	// CategoryID 为**子分类** ID（0 = 未归类）；三键始终存在（未归类输出 0/空串/零值引用）。
	CategoryID     uint64               `json:"category_id"`
	CategoryName   string               `json:"category_name"`
	CategoryParent adminCategoryRefView `json:"category_parent"`
	// R10-A 返佣（契约 18.2）：rebate_percent 为商品级比例（null = 不覆盖）；
	// rebate_disabled 为 true 时该商品完全不返佣（压过一切比例）。
	RebatePercent  *string `json:"rebate_percent"`
	RebateDisabled bool    `json:"rebate_disabled"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
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

// memberCategoryRefView 是主分类的精简引用（R7）。
type memberCategoryRefView struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
}

// memberSubcategoryView 是主分类下的子分类索引（R7，两级筛选用）。
type memberSubcategoryView struct {
	ID           uint64 `json:"id"`
	Name         string `json:"name"`
	Sort         int    `json:"sort"`
	ProductCount int    `json:"product_count"`
}

// memberCategoryView 是会员端主分类（R7）。
type memberCategoryView struct {
	ID            uint64                  `json:"id"`
	Name          string                  `json:"name"`
	Sort          int                     `json:"sort"`
	Subcategories []memberSubcategoryView `json:"subcategories"`
}

// memberGroupView 是会员端子分类分组（只含已上架商品）。
//
// R7：groups[] 的语义从「上游产品组」演进为「子分类」，骨架 id/name/sort/products 不变，
// 新增 parent（所属主分类）——旧客户端读 groups[].products 仍可用。
type memberGroupView struct {
	ID       uint64                `json:"id"`
	Name     string                `json:"name"`
	Sort     int                   `json:"sort"`
	Parent   memberCategoryRefView `json:"parent"`
	Products []memberProductView   `json:"products"`
}

// memberProductListView 是会员端商品目录。
type memberProductListView struct {
	Categories []memberCategoryView `json:"categories"`
	Groups     []memberGroupView    `json:"groups"`
	Total      int                  `json:"total"`
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

// productGroupRefView 是商品详情里的所属分类引用（R7 起为子分类 + 所属主分类）。
type productGroupRefView struct {
	ID     uint64                `json:"id"`
	Name   string                `json:"name"`
	Parent memberCategoryRefView `json:"parent"`
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

// newAdminProductView 组装管理端商品视图；分组可能为 nil（分组未导入时的兜底），
// category 传分类解析结果（adminCategoryIndex.resolve），未归类时为零值。
func newAdminProductView(product *model.Product, group *model.ProductGroup, category adminCategoryInfo, rule pricing.Rule, prices map[string]string, stockControl int) adminProductView {
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
		CategoryID:      category.ID,
		CategoryName:    category.Name,
		CategoryParent:  category.Parent,
		RebatePercent:   product.RebatePercent,
		RebateDisabled:  product.RebateDisabled,
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
	// RawJSON 是 config_json 原文（R7 配置差价解析用，见 pricing.ParseConfigPricings）。
	RawJSON string `json:"-"`
}

// newConfigGroupViews 把上游可配置项转成对外视图。
//
// includeHidden=false（会员端）时过滤掉上游标记为隐藏（hidden != 0）的项与值；
// includeHidden=true（管理端）时全量返回，便于管理员了解商品全部可配置项。
//
// R7：每个可选值带 `pricings`（六周期配置差价，已按定价模式换算）；
// 数量型的取值区间是**全部可见档位的并集**（契约 10.3 第 6 条，避免多档位被锁死在下界）。
//
// R11（v21）：数量型在并集归一化**之前**把每档真实区间 + 单位差价抄进 `tiers`。
// 并集只保留给控件取值范围（values[].qty_minimum/qty_maximum，语义不变、向后兼容），
// 挑档位与算可计费数量一律以 tiers 为准——两者混用就是 v21 修掉的价格联动 bug。
func newConfigGroupViews(cache productConfigCache, rule pricing.Rule, includeHidden bool) []configGroupView {
	pricings := pricing.ParseConfigPricings(cache.RawJSON)
	groups := make([]configGroupView, 0, len(cache.ConfigGroups))
	for _, source := range cache.ConfigGroups {
		options := make([]configOptionView, 0, len(source.Options))
		for _, option := range source.Options {
			if option.Hidden != 0 && !includeHidden {
				continue
			}
			optionPricing := pricings.Option(option.ID)
			values := make([]configOptionValueView, 0, len(option.Values))
			var tiers []configTierView
			for _, value := range option.Values {
				if value.Hidden != 0 && !includeHidden {
					continue
				}
				tier := optionPricing.TierByID(value.ID)
				pricingView := tier.UnitView(rule)
				values = append(values, configOptionValueView{
					ID:         value.ID,
					Name:       value.OptionName,
					UpstreamID: value.UpstreamID,
					QtyMinimum: value.QtyMinimum,
					QtyMaximum: value.QtyMaximum,
					Pricings:   pricingView,
				})
				if pricing.IsQuantityOptionType(option.OptionType) {
					// 真实档位区间必须在下面的并集归一化之前抄下来。
					tiers = append(tiers, configTierView{
						ValueID:  value.ID,
						QtyMin:   value.QtyMinimum,
						QtyMax:   value.QtyMaximum,
						Pricings: pricingView,
					})
				}
			}
			if pricing.IsQuantityOptionType(option.OptionType) && len(values) > 0 {
				// 档位并集：把各值的区间收敛到同一个 [min, max]，前端按一个数量控件渲染。
				// 只改控件范围；真实档位区间已在 tiers 里保留。
				min, max := values[0].QtyMinimum, values[0].QtyMaximum
				for _, value := range values[1:] {
					if value.QtyMinimum < min {
						min = value.QtyMinimum
					}
					if value.QtyMaximum > max {
						max = value.QtyMaximum
					}
				}
				for i := range values {
					values[i].QtyMinimum = min
					values[i].QtyMaximum = max
				}
				// 按档位下界升序，保证挑档位时区间判定稳定可预期。
				sort.SliceStable(tiers, func(i, j int) bool {
					if tiers[i].QtyMin != tiers[j].QtyMin {
						return tiers[i].QtyMin < tiers[j].QtyMin
					}
					return tiers[i].ValueID < tiers[j].ValueID
				})
			}
			options = append(options, configOptionView{
				ID:         option.ID,
				Name:       option.OptionName,
				Type:       option.OptionType,
				UpstreamID: option.UpstreamID,
				Values:     values,
				Tiers:      tiers,
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

// mustConfigJSON 已并入 productConfigCache.RawJSON（见上）。

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
