package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/category"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upstream"
)

// 商品相关提示文案与取值边界。
const (
	msgProductMissing       = "商品不存在"
	msgProductGroupMissing  = "分组不存在"
	msgProductStatusInvalid = "status 只能是 on 或 off"
	maxProductSortValue     = 999999
	maxGroupNameRunes       = 128
)

// productHandler 处理商品与计费接口：管理端（导入/定价/上下架）与会员端（只读目录）。
//
// upstream 是**动态提供者**（阶段 4 起上游参数来自后台设置，契约 8.6 / 12.1），
// 每次使用都取当前设置下的客户端，管理员改完设置下一次调用即生效。
type productHandler struct {
	store    *store.Store
	upstream upstream.Provider
	logger   *slog.Logger
}

// upstreamClient 取当前设置下的上游客户端；未配置齐全时返回 upstream.ErrNotConfigured。
func (h *productHandler) upstreamClient(ctx context.Context) (*upstream.Client, error) {
	if h.upstream == nil {
		return nil, upstream.ErrNotConfigured
	}
	client, enabled, err := h.upstream.Current(ctx)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, upstream.ErrNotConfigured
	}
	return client, nil
}

// updateProductRequest 是 PUT /admin/products/:id 请求体，字段均可选但至少提供一个。
//
// PricingJSON 用 json.RawMessage 承接（而不是 *string），以便区分「未提供」与「显式 null」：
// 既能传对象（{"mode":"markup",…}），也能传字符串（"{\"mode\":\"markup\"}"），
// 传 null 表示重置为缺省规则（直接用上游价）。
type updateProductRequest struct {
	PricingJSON json.RawMessage `json:"pricing_json"`
	Status      *string         `json:"status"`
	Sort        *int            `json:"sort"`
	// CategoryID 改挂子分类（R7，契约 10.4）：必须是子分类 id，0 表示取消归类。
	// 设置后 category_source 置 manual，后续导入不再自动派生覆盖。
	CategoryID *uint64 `json:"category_id"`
}

// updateProductGroupRequest 是 PUT /admin/product-groups/:id 请求体。
type updateProductGroupRequest struct {
	Name *string `json:"name"`
	Sort *int    `json:"sort"`
}

// adminGroupListView 是管理端分组列表响应。
type adminGroupListView struct {
	Items []adminGroupView `json:"items"`
}

// listProducts 处理 GET /api/v1/admin/products：分页 + 分组/状态/关键词过滤。
func (h *productHandler) listProducts(c *gin.Context) {
	page, err := intQuery(c, "page", defaultPage, 1, maxPage)
	if err != nil {
		response.Fail(c, response.CodeInvalidParam, err.Error())
		return
	}
	pageSize, err := intQuery(c, "page_size", defaultPageSize, 1, maxPageSize)
	if err != nil {
		response.Fail(c, response.CodeInvalidParam, err.Error())
		return
	}

	status := strings.TrimSpace(c.Query("status"))
	if status != "" && !model.IsProductStatusValid(status) {
		response.Fail(c, response.CodeInvalidParam, msgProductStatusInvalid)
		return
	}

	filter := store.ProductFilter{
		Page:     page,
		PageSize: pageSize,
		Status:   status,
		Keyword:  strings.TrimSpace(c.Query("keyword")),
	}
	if rawGroupID := strings.TrimSpace(c.Query("group_id")); rawGroupID != "" {
		groupID, err := strconv.ParseUint(rawGroupID, 10, 64)
		if err != nil || groupID == 0 {
			response.Fail(c, response.CodeInvalidParam, "group_id 必须为正整数（本地分组 ID）")
			return
		}
		group, err := h.store.ProductGroupByID(c.Request.Context(), groupID)
		switch {
		case errors.Is(err, store.ErrNotFound):
			response.Fail(c, response.CodeNotFound, msgProductGroupMissing)
			return
		case err != nil:
			failDB(c, h.logger, err)
			return
		}
		filter.UpstreamGroupID = &group.UpstreamGroupID
	}
	// R9：按子分类过滤（0 = 只看未归类），非 0 必须是存在的**子分类**（与改挂同一口径）。
	if rawCategoryID := strings.TrimSpace(c.Query("category_id")); rawCategoryID != "" {
		categoryID, err := strconv.ParseUint(rawCategoryID, 10, 64)
		if err != nil {
			response.Fail(c, response.CodeInvalidParam, "category_id 必须为非负整数（本地子分类 ID）")
			return
		}
		if categoryID > 0 {
			if _, err := h.store.SubCategoryByID(c.Request.Context(), categoryID); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					response.Fail(c, response.CodeNotFound, "分类不存在")
					return
				}
				failDB(c, h.logger, err)
				return
			}
		}
		filter.CategoryID = &categoryID
	}

	items, total, err := h.store.ListProducts(c.Request.Context(), filter)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	groups, err := h.store.ListProductGroups(c.Request.Context())
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	groupByUpstreamID := make(map[int]*model.ProductGroup, len(groups))
	for i := range groups {
		groupByUpstreamID[groups[i].UpstreamGroupID] = &groups[i]
	}
	// R9：分类归属随列表一起下发（一次性加载分类表，避免逐商品查库）。
	categoryIndex, err := h.categoryIndex(c.Request.Context())
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	views := make([]adminProductView, 0, len(items))
	for i := range items {
		product := &items[i]
		rule, prices, stockControl := h.resolvePricing(product)
		views = append(views, newAdminProductView(
			product, groupByUpstreamID[product.UpstreamGroupID], categoryIndex.resolve(product), rule, prices, stockControl))
	}

	response.Success(c, adminProductListView{
		Items:    views,
		Page:     page,
		PageSize: pageSize,
		Total:    total,
	})
}

// getProduct 处理 GET /api/v1/admin/products/:id：返回商品详情（含配置项与上游价格原文）。
func (h *productHandler) getProduct(c *gin.Context) {
	product, ok := h.loadProduct(c)
	if !ok {
		return
	}
	response.Success(c, h.adminProductDetail(c, product))
}

// updateProduct 处理 PUT /api/v1/admin/products/:id：改定价规则 / 上下架 / 排序。
// 角色守卫（admin/finance）在路由注册处挂载。
func (h *productHandler) updateProduct(c *gin.Context) {
	id, ok := productIDParam(c)
	if !ok {
		return
	}

	var req updateProductRequest
	if !bindJSON(c, &req) {
		return
	}
	if len(req.PricingJSON) == 0 && req.Status == nil && req.Sort == nil && req.CategoryID == nil {
		response.Fail(c, response.CodeInvalidParam, "至少提供一个字段：pricing_json / status / sort / category_id")
		return
	}

	update := store.ProductUpdate{}
	if len(req.PricingJSON) > 0 {
		raw, err := pricingRawJSON(req.PricingJSON)
		if err != nil {
			failRuleError(c, err)
			return
		}
		normalized, err := pricing.Normalize(raw)
		if err != nil {
			failRuleError(c, err)
			return
		}
		update.PricingJSON = &normalized
	}
	if req.Status != nil {
		status := strings.TrimSpace(*req.Status)
		if !model.IsProductStatusValid(status) {
			response.Fail(c, response.CodeInvalidParam, msgProductStatusInvalid)
			return
		}
		update.Status = &status
	}
	if req.Sort != nil {
		if *req.Sort < -maxProductSortValue || *req.Sort > maxProductSortValue {
			response.Fail(c, response.CodeInvalidParam,
				fmt.Sprintf("sort 需为 %d 到 %d 之间的整数", -maxProductSortValue, maxProductSortValue))
			return
		}
		update.Sort = req.Sort
	}
	if req.CategoryID != nil {
		// R7 改挂子分类：0 = 取消归类；非 0 必须是**子分类**（契约 10.4）。
		if *req.CategoryID > 0 {
			if _, err := h.store.SubCategoryByID(c.Request.Context(), *req.CategoryID); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					response.Fail(c, response.CodeValidationFailed,
						"category_id 必须是存在的子分类 ID（主分类 id 不可直接挂商品）")
					return
				}
				failDB(c, h.logger, err)
				return
			}
		}
		update.CategoryID = req.CategoryID
	}

	ctx := c.Request.Context()
	current, err := h.store.ProductByID(ctx, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgProductMissing)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	if err := h.ensureSellable(c, current, update); err != nil {
		return
	}

	updated, err := h.store.UpdateProduct(ctx, id, update)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgProductMissing)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}
	response.Success(c, h.adminProductDetail(c, updated))
}

// ensureSellable 校验「更新后处于上架状态」的商品至少有一个周期的可用价格，
// 不满足时以 409 拒绝（状态冲突：没有价格无法上架），避免上架一个买不到的商品。
func (h *productHandler) ensureSellable(c *gin.Context, current *model.Product, update store.ProductUpdate) error {
	status := current.Status
	if update.Status != nil {
		status = *update.Status
	}
	if status != model.ProductStatusOn {
		return nil
	}

	_, upstreamPrices, prices, err := productPrices(current)
	if err != nil {
		h.logger.Warn("商品定价数据异常", "error", err, "product_id", current.ID)
		upstreamPrices, prices = pricing.UpstreamPrices{}, nil
	}
	if update.PricingJSON != nil {
		forced, err := pricing.Parse(*update.PricingJSON)
		if err != nil {
			failRuleError(c, err)
			return err
		}
		prices = forced.Prices(upstreamPrices.Prices)
	}
	if !hasAnyPrice(prices) {
		response.Fail(c, response.CodeConflict, "商品没有任何可用周期的价格，无法上架（请先配置固定价或确认上游价格可用）")
		return errors.New("商品无可用周期价格")
	}
	return nil
}

// listProductGroups 处理 GET /api/v1/admin/product-groups：全部分组 + 商品计数。
func (h *productHandler) listProductGroups(c *gin.Context) {
	ctx := c.Request.Context()

	groups, err := h.store.ListProductGroups(ctx)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	counts, err := h.store.ProductGroupCounts(ctx)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	views := make([]adminGroupView, 0, len(groups))
	for i := range groups {
		group := &groups[i]
		count := counts[group.UpstreamGroupID]
		views = append(views, adminGroupView{
			ID:              group.ID,
			UpstreamGroupID: group.UpstreamGroupID,
			Name:            group.Name,
			Sort:            group.Sort,
			Products: adminGroupCountsView{
				Total: count.Total,
				On:    count.On,
				Off:   count.Off,
			},
			CreatedAt: formatTime(group.CreatedAt),
			UpdatedAt: formatTime(group.UpdatedAt),
		})
	}

	response.Success(c, adminGroupListView{Items: views})
}

// updateProductGroup 处理 PUT /api/v1/admin/product-groups/:id：重命名 / 改排序。
func (h *productHandler) updateProductGroup(c *gin.Context) {
	groupID, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || groupID == 0 {
		response.Fail(c, response.CodeInvalidParam, "分组 ID 必须为正整数")
		return
	}

	var req updateProductGroupRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.Name == nil && req.Sort == nil {
		response.Fail(c, response.CodeInvalidParam, "至少提供一个字段：name / sort")
		return
	}

	update := store.ProductGroupUpdate{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			response.Fail(c, response.CodeInvalidParam, "分组名不能为空")
			return
		}
		if utf8.RuneCountInString(name) > maxGroupNameRunes {
			response.Fail(c, response.CodeInvalidParam,
				fmt.Sprintf("分组名长度不能超过 %d 个字符", maxGroupNameRunes))
			return
		}
		update.Name = &name
	}
	if req.Sort != nil {
		if *req.Sort < -maxProductSortValue || *req.Sort > maxProductSortValue {
			response.Fail(c, response.CodeInvalidParam,
				fmt.Sprintf("sort 需为 %d 到 %d 之间的整数", -maxProductSortValue, maxProductSortValue))
			return
		}
		update.Sort = req.Sort
	}

	group, err := h.store.UpdateProductGroup(c.Request.Context(), groupID, update)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgProductGroupMissing)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	response.Success(c, adminGroupView{
		ID:              group.ID,
		UpstreamGroupID: group.UpstreamGroupID,
		Name:            group.Name,
		Sort:            group.Sort,
		CreatedAt:       formatTime(group.CreatedAt),
		UpdatedAt:       formatTime(group.UpdatedAt),
	})
}

// listMemberProducts 处理 GET /api/v1/products：已上架商品按**子分类**组织的只读目录
// （R7：groups[] 语义为子分类并带 parent，另下发 categories[] 主分类树）。
func (h *productHandler) listMemberProducts(c *gin.Context) {
	ctx := c.Request.Context()

	groups, err := h.store.ListProductGroups(ctx)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	products, err := h.store.ListMemberProducts(ctx)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	// 读时兜底：升级后未重跑导入期间 category_id 可能全是 0，按同一派生规则补齐（幂等）。
	if err := h.store.EnsureProductCategories(ctx, groups, products, category.Derive); err != nil {
		h.logger.Warn("商品分类兜底归类失败", "error", err)
	}
	categories, err := h.store.ListProductCategories(ctx)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	subByID := make(map[uint64]*model.ProductCategory, len(categories))
	mainByID := make(map[uint64]*model.ProductCategory, len(categories))
	for i := range categories {
		item := &categories[i]
		if item.IsMain() {
			mainByID[item.ID] = item
		} else {
			subByID[item.ID] = item
		}
	}

	bySub := make(map[uint64][]memberProductView)
	for i := range products {
		product := &products[i]
		_, prices, stockControl := h.resolvePricing(product)
		bySub[product.CategoryID] = append(bySub[product.CategoryID],
			newMemberProductView(product, prices, stockControl))
	}

	// groups[]：子分类（只含已上架商品），按 (主分类 sort, 子分类 sort, id) 稳定排序。
	type subEntry struct {
		sub      *model.ProductCategory
		main     *model.ProductCategory
		products []memberProductView
	}
	entries := make([]subEntry, 0, len(subByID))
	for id, items := range bySub {
		sub := subByID[id]
		if sub == nil || len(items) == 0 {
			continue
		}
		main := mainByID[sub.ParentIDValue()]
		entries = append(entries, subEntry{sub: sub, main: main, products: items})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		left, right := entries[i], entries[j]
		leftMain, rightMain := left.main, right.main
		if (leftMain == nil) != (rightMain == nil) {
			return leftMain != nil
		}
		if leftMain != nil && rightMain != nil && leftMain.Sort != rightMain.Sort {
			return leftMain.Sort < rightMain.Sort
		}
		if left.sub.Sort != right.sub.Sort {
			return left.sub.Sort < right.sub.Sort
		}
		return left.sub.ID < right.sub.ID
	})

	views := make([]memberGroupView, 0, len(entries))
	total := 0
	usedMain := make(map[uint64]bool)
	for _, entry := range entries {
		ref := memberCategoryRefView{}
		if entry.main != nil {
			ref = memberCategoryRefView{ID: entry.main.ID, Name: entry.main.Name}
			usedMain[entry.main.ID] = true
		}
		views = append(views, memberGroupView{
			ID:       entry.sub.ID,
			Name:     entry.sub.Name,
			Sort:     entry.sub.Sort,
			Parent:   ref,
			Products: entry.products,
		})
		total += len(entry.products)
	}

	// categories[]：只含**有上架商品**的主分类，子分类同样只列有货的。
	categoryViews := make([]memberCategoryView, 0, len(usedMain))
	for i := range categories {
		main := &categories[i]
		if !main.IsMain() || !usedMain[main.ID] {
			continue
		}
		subs := make([]memberSubcategoryView, 0)
		for _, entry := range entries {
			if entry.main == nil || entry.main.ID != main.ID {
				continue
			}
			subs = append(subs, memberSubcategoryView{
				ID:           entry.sub.ID,
				Name:         entry.sub.Name,
				Sort:         entry.sub.Sort,
				ProductCount: len(entry.products),
			})
		}
		categoryViews = append(categoryViews, memberCategoryView{
			ID:            main.ID,
			Name:          main.Name,
			Sort:          main.Sort,
			Subcategories: subs,
		})
	}

	response.Success(c, memberProductListView{
		Categories: categoryViews,
		Groups:     views,
		Total:      total,
	})
}

// getMemberProduct 处理 GET /api/v1/products/:id：详情（配置项 + 六周期价格 + 库存/试用）。
// 下架商品与不存在的商品统一返回 404。
func (h *productHandler) getMemberProduct(c *gin.Context) {
	id, ok := productIDParam(c)
	if !ok {
		return
	}

	ctx := c.Request.Context()
	product, err := h.store.MemberProductByID(ctx, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgProductMissing)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	rule, prices, stockControl := h.resolvePricing(product)
	cache := h.configCache(product)

	groupRef, err := h.categoryRef(ctx, product)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	response.Success(c, memberProductDetailView{
		memberProductView: newMemberProductView(product, prices, stockControl),
		Description:       product.Description,
		Group:             groupRef,
		ConfigGroups:      newConfigGroupViews(cache, rule, false),
		CustomFields:      newCustomFieldViews(cache),
		UpdatedAt:         formatTime(product.UpdatedAt),
	})
}

// categoryRef 组装商品所属分类引用（R7：子分类 + 所属主分类）。
// 尚未归类（category_id=0）或分类行缺失时回退到上游组名，保证字段始终有值。
func (h *productHandler) categoryRef(ctx context.Context, product *model.Product) (productGroupRefView, error) {
	if product.CategoryID > 0 {
		sub, err := h.store.ProductCategoryByID(ctx, product.CategoryID)
		switch {
		case err == nil:
			ref := productGroupRefView{ID: sub.ID, Name: sub.Name}
			if sub.ParentID != nil {
				if main, err := h.store.ProductCategoryByID(ctx, *sub.ParentID); err == nil {
					ref.Parent = memberCategoryRefView{ID: main.ID, Name: main.Name}
				}
			}
			return ref, nil
		case errors.Is(err, store.ErrNotFound):
			// 分类行缺失（异常数据）：落到组名兜底。
		default:
			return productGroupRefView{}, err
		}
	}
	group, err := h.store.ProductGroupByUpstreamID(ctx, product.UpstreamGroupID)
	switch {
	case err == nil:
		return productGroupRefView{ID: group.ID, Name: group.Name}, nil
	case errors.Is(err, store.ErrNotFound):
		return productGroupRefView{ID: 0, Name: ""}, nil
	default:
		return productGroupRefView{}, err
	}
}

// loadProduct 按路径参数加载商品（管理端，含下架商品），失败时已写出响应。
func (h *productHandler) loadProduct(c *gin.Context) (*model.Product, bool) {
	id, ok := productIDParam(c)
	if !ok {
		return nil, false
	}

	product, err := h.store.ProductByID(c.Request.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgProductMissing)
		return nil, false
	case err != nil:
		failDB(c, h.logger, err)
		return nil, false
	}
	return product, true
}

// adminProductDetail 组装管理端商品详情视图（含配置项、自定义字段与上游价格原文）。
func (h *productHandler) adminProductDetail(c *gin.Context, product *model.Product) adminProductDetailView {
	ctx := c.Request.Context()

	var group *model.ProductGroup
	if found, err := h.store.ProductGroupByUpstreamID(ctx, product.UpstreamGroupID); err == nil {
		group = found
	} else if !errors.Is(err, store.ErrNotFound) {
		h.logger.Warn("查询商品分组失败", "error", err, "product_id", product.ID)
	}

	// R9 分类归属：与分组同口径降级（查库失败只告警，视图按未归类输出）。
	categoryIndex, err := h.categoryIndex(ctx)
	if err != nil {
		h.logger.Warn("查询商品分类失败", "error", err, "product_id", product.ID)
		categoryIndex = adminCategoryIndex{}
	}

	rule, upstreamPrices, prices, err := productPrices(product)
	if err != nil {
		h.logger.Warn("商品定价数据异常，回退缺省规则", "error", err, "product_id", product.ID)
		rule, upstreamPrices, prices = pricing.Default(), pricing.UpstreamPrices{}, nil
	}
	cache := h.configCache(product)

	return adminProductDetailView{
		adminProductView: newAdminProductView(product, group, categoryIndex.resolve(product), rule, prices, cache.Product.StockControl),
		Description:      product.Description,
		DescriptionLines: descriptionLines(product.Description),
		ConfigGroups:     newConfigGroupViews(cache, rule, true),
		CustomFields:     newCustomFieldViews(cache),
		UpstreamPrices:   upstreamPrices,
	}
}

// categoryIndex 一次性加载全部分类的 id 索引（主分类 + 子分类，表很小）。
func (h *productHandler) categoryIndex(ctx context.Context) (adminCategoryIndex, error) {
	items, err := h.store.ListProductCategories(ctx)
	if err != nil {
		return nil, err
	}
	return newAdminCategoryIndex(items), nil
}

// resolvePricing 解析商品的定价规则与本地售价；异常时回退缺省规则并告警
// （pricing_json 只能经管理端接口写入，正常不会异常）。
func (h *productHandler) resolvePricing(product *model.Product) (pricing.Rule, map[string]string, int) {
	rule, _, prices, err := productPrices(product)
	if err != nil {
		h.logger.Warn("商品定价数据异常，回退缺省规则", "error", err, "product_id", product.ID)
		rule, prices = pricing.Default(), nil
	}
	return rule, prices, h.configCache(product).Product.StockControl
}

// configCache 解析 config_json 缓存；解析失败时返回零值并告警（详情类接口不因此失败）。
func (h *productHandler) configCache(product *model.Product) productConfigCache {
	cache, err := parseProductConfigCache(product.ConfigJSON)
	if err != nil {
		h.logger.Warn("商品配置缓存解析失败", "error", err, "product_id", product.ID)
	}
	return cache
}

// productIDParam 解析路径参数 :id。
func productIDParam(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, response.CodeInvalidParam, "商品 ID 必须为正整数")
		return 0, false
	}
	return id, true
}

// pricingRawJSON 兼容 pricing_json 的两种写法：JSON 对象直接使用；
// JSON 字符串先反转义为对象文本（空串与 null 交给 pricing.Normalize 按缺省规则处理）。
func pricingRawJSON(raw json.RawMessage) (string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(trimmed, `"`) {
		return trimmed, nil
	}

	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return "", fmt.Errorf("%w: pricing_json 字符串解析失败: %v", pricing.ErrFormat, err)
	}
	return text, nil
}

// failRuleError 把「格式/规则」两类业务校验错误映射为错误码：
// 规则类（定价/优惠码/设置项/财务规则不成立）→ 40002，其余格式类 → 40001。
func failRuleError(c *gin.Context, err error) {
	if errors.Is(err, pricing.ErrRule) || errors.Is(err, errCouponRule) ||
		errors.Is(err, settings.ErrRule) || errors.Is(err, errFinanceRule) ||
		errors.Is(err, errTicketRule) {
		response.Fail(c, response.CodeValidationFailed, err.Error())
		return
	}
	response.Fail(c, response.CodeInvalidParam, err.Error())
}

// hasAnyPrice 判断六个周期里是否至少有一个可用价格。
func hasAnyPrice(prices map[string]string) bool {
	for _, cycle := range pricing.Cycles {
		if prices[cycle] != "" {
			return true
		}
	}
	return false
}

// parseProductConfigCache 解析 config_json（上游 /cart/get_product_config 的 data 原文）。
func parseProductConfigCache(raw string) (productConfigCache, error) {
	var cache productConfigCache
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "null" {
		return cache, nil
	}
	if err := json.Unmarshal([]byte(trimmed), &cache); err != nil {
		return productConfigCache{}, fmt.Errorf("商品配置缓存解析失败: %w", err)
	}
	cache.RawJSON = trimmed
	return cache, nil
}
