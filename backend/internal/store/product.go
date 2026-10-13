package store

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
)

// ProductGroupInput 是导入时的分组数据（来自上游 GET /cart/all）。
type ProductGroupInput struct {
	UpstreamGroupID int
	Name            string
	Sort            int
}

// ProductInput 是导入时的商品数据（目录字段 + 单商品详情缓存）。
//
// 只包含**上游字段**：本地字段（pricing_json / status / sort）由导入规则单独处理。
type ProductInput struct {
	UpstreamPID        int
	UpstreamGroupID    int
	Name               string
	Description        string
	Type               string
	Module             string
	ConfigJSON         string
	UpstreamPricesJSON string
	StockQty           int
	OntrialMax         int
	// Sort 仅在首次导入（新建商品）时写入，之后由管理端控制。
	Sort int
	// Category 是 R7 派生的两级分类归属（契约 10.1）；nil 表示不参与分类派生。
	// category_source=manual 的商品由调用方置 nil，导入不覆盖其归属。
	Category *model.ProductCategoryInput
}

// ImportResult 是导入的计数结果：分组数 + 商品的三态计数。
type ImportResult struct {
	// Groups 是本次处理的分组数（分组只创建不覆盖，故不区分新增/更新）。
	Groups int
	// Created 是新建的商品数（新商品默认 status=off、pricing_json=mode:upstream）。
	Created int
	// Updated 是上游字段发生变化的商品数。
	Updated int
	// Unchanged 是与库内完全一致（上游字段无变化）的商品数。
	Unchanged int
}

// ProductFilter 是 GET /admin/products 的查询条件（page 从 1 开始）。
type ProductFilter struct {
	Page     int
	PageSize int
	// UpstreamGroupID 非 nil 时按分组过滤（由 handler 把本地分组 ID 解析成上游分组 ID）。
	UpstreamGroupID *int
	// CategoryID 非 nil 时按**子分类**过滤（R9，契约 10.4）；指针指向 0 表示只看未归类。
	CategoryID *uint64
	Status     string
	Keyword    string
}

// ProductUpdate 描述 PUT /admin/products/:id 的可选字段：nil 表示不修改。
type ProductUpdate struct {
	PricingJSON *string
	Status      *string
	Sort        *int
	// CategoryID 改挂子分类（R7，契约 10.4）：设置后 category_source 置 manual，导入不再覆盖。
	CategoryID *uint64
	// RebatePercent 商品级返佣比例（R10-A，契约 18.2）：nil 表示不修改；
	// 指向空串表示清空（落 NULL = 不覆盖，落到会员级/全局默认）；指向值表示设为该比例。
	RebatePercent *string
	// RebateDisabled 禁用返佣（R10-A）：非 nil 时写入。
	RebateDisabled *bool
}

// ProductGroupUpdate 描述 PUT /admin/product-groups/:id 的可选字段：nil 表示不修改。
type ProductGroupUpdate struct {
	Name *string
	Sort *int
}

// GroupCounts 是某个分组下商品的三态计数。
type GroupCounts struct {
	Total int64
	On    int64
	Off   int64
}

// ImportCatalog 按上游数据批量 upsert 分组与商品（单事务，幂等）。
//
// 写入边界：只写上游字段；本地字段（pricing_json / status / sort）在更新分支**绝不覆盖**，
// 保证重复导入不会冲掉管理端配置的定价与上下架状态。分组同理：name / sort 只在首次创建时写入。
//
// 返回的计数用于「重复导入仅计数变化」的幂等验证。
func (s *Store) ImportCatalog(ctx context.Context, groups []ProductGroupInput, products []ProductInput) (ImportResult, error) {
	db, err := s.session(ctx)
	if err != nil {
		return ImportResult{}, err
	}

	var result ImportResult
	err = db.Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()

		if err := importGroups(tx, groups, now, &result); err != nil {
			return err
		}
		// R7：先按稳定键解析/创建两级分类，再把子分类 id 挂到商品上（契约 10.1）。
		categoryIDs, err := importCategories(tx, products, now)
		if err != nil {
			return err
		}
		return importProducts(tx, products, categoryIDs, now, &result)
	})
	if err != nil {
		return ImportResult{}, err
	}
	return result, nil
}

// importGroups 创建缺失的分组；已存在的分组保持本地 name / sort 不变。
func importGroups(tx *gorm.DB, groups []ProductGroupInput, now time.Time, result *ImportResult) error {
	if len(groups) == 0 {
		return nil
	}

	upstreamIDs := make([]int, 0, len(groups))
	for _, group := range groups {
		upstreamIDs = append(upstreamIDs, group.UpstreamGroupID)
	}

	existing := make([]model.ProductGroup, 0, len(groups))
	if err := tx.Where("upstream_group_id IN ?", upstreamIDs).Find(&existing).Error; err != nil {
		return err
	}
	known := make(map[int]struct{}, len(existing))
	for i := range existing {
		known[existing[i].UpstreamGroupID] = struct{}{}
	}

	for _, group := range groups {
		if _, ok := known[group.UpstreamGroupID]; ok {
			result.Groups++
			continue
		}
		created := model.ProductGroup{
			UpstreamGroupID: group.UpstreamGroupID,
			Name:            group.Name,
			Sort:            group.Sort,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := tx.Create(&created).Error; err != nil {
			return err
		}
		known[group.UpstreamGroupID] = struct{}{}
		result.Groups++
	}
	return nil
}

// importCategories 按派生稳定键 find-or-create 两级分类，返回 key → 子分类 id。
//
// 幂等（契约 10.1）：key 唯一，已存在的分类**不改 name / sort**（都是本地字段），
// 因此重复导入不会新建、不改名、不漂移。返回的 map 的值是**子分类** id。
func importCategories(tx *gorm.DB, products []ProductInput, now time.Time) (map[string]uint64, error) {
	ids := make(map[string]uint64)
	if len(products) == 0 {
		return ids, nil
	}

	// 先收集本批用到的主/子分类键与首次出现的展示信息（保证同一 key 只创建一次）。
	type pending struct {
		key       string
		name      string
		sort      int
		parentKey string // 子分类的父键；空串表示自身是主分类
	}
	order := make([]string, 0)
	pendingByKey := make(map[string]*pending)

	for _, product := range products {
		if product.Category == nil {
			continue
		}
		cat := product.Category
		if _, ok := pendingByKey[cat.MainKey]; !ok {
			pendingByKey[cat.MainKey] = &pending{key: cat.MainKey, name: cat.MainName, sort: cat.MainSort}
			order = append(order, cat.MainKey)
		}
		if _, ok := pendingByKey[cat.SubKey]; !ok {
			pendingByKey[cat.SubKey] = &pending{
				key: cat.SubKey, name: cat.SubName, sort: cat.SubSort, parentKey: cat.MainKey,
			}
			order = append(order, cat.SubKey)
		}
	}

	// 读出已存在的分类（按 key），只补缺的。
	keys := make([]string, 0, len(order))
	keys = append(keys, order...)
	existing := make([]model.ProductCategory, 0, len(keys))
	if err := tx.Where("`key` IN ?", keys).Find(&existing).Error; err != nil {
		return nil, err
	}
	existingByKey := make(map[string]model.ProductCategory, len(existing))
	for _, item := range existing {
		existingByKey[item.Key] = item
	}

	for _, key := range order {
		item := pendingByKey[key]
		if found, ok := existingByKey[key]; ok {
			ids[key] = found.ID
			continue
		}
		// 新建：主分类先建（子分类要引用它的 id）。
		created := model.ProductCategory{
			Key:       item.key,
			Name:      item.name,
			Sort:      item.sort,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if item.parentKey != "" {
			parentID, ok := ids[item.parentKey]
			if !ok {
				return nil, fmt.Errorf("父分类未解析: %s", item.parentKey)
			}
			created.ParentID = &parentID
		}
		if err := tx.Create(&created).Error; err != nil {
			return nil, err
		}
		ids[key] = created.ID
	}
	return ids, nil
}

// importProducts 创建缺失的商品、更新上游字段有变化的商品、跳过完全一致的商品。
func importProducts(tx *gorm.DB, products []ProductInput, categoryIDs map[string]uint64, now time.Time, result *ImportResult) error {
	if len(products) == 0 {
		return nil
	}

	pids := make([]int, 0, len(products))
	for _, product := range products {
		pids = append(pids, product.UpstreamPID)
	}

	existing := make([]model.Product, 0, len(products))
	if err := tx.Where("upstream_pid IN ?", pids).Find(&existing).Error; err != nil {
		return err
	}
	byPID := make(map[int]*model.Product, len(existing))
	for i := range existing {
		byPID[existing[i].UpstreamPID] = &existing[i]
	}

	for _, product := range products {
		categoryID := resolveCategoryID(product, categoryIDs)
		current, ok := byPID[product.UpstreamPID]
		if !ok {
			created := model.Product{
				UpstreamPID:        product.UpstreamPID,
				UpstreamGroupID:    product.UpstreamGroupID,
				CategoryID:         categoryID,
				CategorySource:     model.CategorySourceDerived,
				Name:               product.Name,
				Description:        product.Description,
				Type:               product.Type,
				Module:             product.Module,
				ConfigJSON:         product.ConfigJSON,
				UpstreamPricesJSON: product.UpstreamPricesJSON,
				PricingJSON:        pricing.DefaultJSON,
				StockQty:           product.StockQty,
				OntrialMax:         product.OntrialMax,
				Status:             model.ProductStatusOff,
				Sort:               product.Sort,
				CreatedAt:          now,
				UpdatedAt:          now,
			}
			if err := tx.Create(&created).Error; err != nil {
				return err
			}
			byPID[product.UpstreamPID] = &created
			result.Created++
			continue
		}

		columns := productUpstreamColumns(current, product, categoryID)
		if len(columns) == 0 {
			result.Unchanged++
			continue
		}
		columns["updated_at"] = now
		if err := tx.Model(&model.Product{}).Where("id = ?", current.ID).Updates(columns).Error; err != nil {
			return err
		}
		result.Updated++
	}
	return nil
}

// resolveCategoryID 解析商品归属的子分类 id；无派生信息时返回 0。
func resolveCategoryID(in ProductInput, categoryIDs map[string]uint64) uint64 {
	if in.Category == nil {
		return 0
	}
	return categoryIDs[in.Category.SubKey]
}

// productUpstreamColumns 比较上游字段并返回需要更新的列（无变化时返回空 map）。
//
// 这是「重复导入即幂等」的判定核心：返回空即计为 unchanged。
// 本地字段（pricing_json / status / sort）不在比较与写入范围内。
// R7：category_id 参与比较，但**仅当 category_source != manual**——
// 管理员手工改挂的商品（manual）导入不得覆盖（契约 10.1）。
func productUpstreamColumns(existing *model.Product, in ProductInput, categoryID uint64) map[string]any {
	columns := make(map[string]any)

	if existing.UpstreamGroupID != in.UpstreamGroupID {
		columns["upstream_group_id"] = in.UpstreamGroupID
	}
	if in.Category != nil && existing.CategorySource != model.CategorySourceManual && existing.CategoryID != categoryID {
		columns["category_id"] = categoryID
		columns["category_source"] = model.CategorySourceDerived
	}
	if existing.Name != in.Name {
		columns["name"] = in.Name
	}
	if existing.Description != in.Description {
		columns["description"] = in.Description
	}
	if existing.Type != in.Type {
		columns["type"] = in.Type
	}
	if existing.Module != in.Module {
		columns["module"] = in.Module
	}
	if existing.ConfigJSON != in.ConfigJSON {
		columns["config_json"] = in.ConfigJSON
	}
	if existing.UpstreamPricesJSON != in.UpstreamPricesJSON {
		columns["upstream_prices_json"] = in.UpstreamPricesJSON
	}
	if existing.StockQty != in.StockQty {
		columns["stock_qty"] = in.StockQty
	}
	if existing.OntrialMax != in.OntrialMax {
		columns["ontrial_max"] = in.OntrialMax
	}
	return columns
}

// ListProducts 按条件分页查询商品（管理端），返回当页数据与总数。
func (s *Store) ListProducts(ctx context.Context, filter ProductFilter) ([]model.Product, int64, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, 0, err
	}

	query := db.Model(&model.Product{})
	if filter.UpstreamGroupID != nil {
		query = query.Where("upstream_group_id = ?", *filter.UpstreamGroupID)
	}
	if filter.CategoryID != nil {
		query = query.Where("category_id = ?", *filter.CategoryID)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Keyword != "" {
		query = query.Where("name LIKE ?", containsKeyword(filter.Keyword))
	}

	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	items := make([]model.Product, 0)
	if err := query.Session(&gorm.Session{}).
		Order("sort ASC, id ASC").
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// ProductByID 按本地主键查询商品（管理端，含下架商品）。
func (s *Store) ProductByID(ctx context.Context, id uint64) (*model.Product, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	return productBy(db, "id = ?", id)
}

// UpdateProduct 更新商品的本地字段（定价/状态/排序/分类/返佣）并返回最新记录。
func (s *Store) UpdateProduct(ctx context.Context, id uint64, upd ProductUpdate) (*model.Product, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := productBy(db, "id = ?", id); err != nil {
		return nil, err
	}

	columns := map[string]any{"updated_at": time.Now().UTC()}
	if upd.PricingJSON != nil {
		columns["pricing_json"] = *upd.PricingJSON
	}
	if upd.Status != nil {
		columns["status"] = *upd.Status
	}
	if upd.Sort != nil {
		columns["sort"] = *upd.Sort
	}
	if upd.CategoryID != nil {
		// R7 改挂子分类：标记为 manual，后续导入不再自动派生覆盖（契约 10.1 / 10.4）。
		columns["category_id"] = *upd.CategoryID
		columns["category_source"] = model.CategorySourceManual
	}
	if upd.RebatePercent != nil {
		// R10-A：空串 = 清空（落 NULL = 不覆盖，落到会员级/全局默认）。
		if *upd.RebatePercent == "" {
			columns["rebate_percent"] = nil
		} else {
			columns["rebate_percent"] = *upd.RebatePercent
		}
	}
	if upd.RebateDisabled != nil {
		columns["rebate_disabled"] = *upd.RebateDisabled
	}
	if err := db.Model(&model.Product{}).Where("id = ?", id).Updates(columns).Error; err != nil {
		return nil, err
	}
	return productBy(db, "id = ?", id)
}

// ListProductGroups 列出全部分组（管理端，按 sort 升序）。
func (s *Store) ListProductGroups(ctx context.Context) ([]model.ProductGroup, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}

	groups := make([]model.ProductGroup, 0)
	if err := db.Order("sort ASC, id ASC").Find(&groups).Error; err != nil {
		return nil, err
	}
	return groups, nil
}

// ProductGroupByID 按本地主键查询分组。
func (s *Store) ProductGroupByID(ctx context.Context, id uint64) (*model.ProductGroup, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	return productGroupBy(db, "id = ?", id)
}

// ProductGroupByUpstreamID 按上游分组 ID 查询分组。
func (s *Store) ProductGroupByUpstreamID(ctx context.Context, upstreamGroupID int) (*model.ProductGroup, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	return productGroupBy(db, "upstream_group_id = ?", upstreamGroupID)
}

// UpdateProductGroup 更新分组名/排序并返回最新记录。
func (s *Store) UpdateProductGroup(ctx context.Context, id uint64, upd ProductGroupUpdate) (*model.ProductGroup, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := productGroupBy(db, "id = ?", id); err != nil {
		return nil, err
	}

	columns := map[string]any{"updated_at": time.Now().UTC()}
	if upd.Name != nil {
		columns["name"] = *upd.Name
	}
	if upd.Sort != nil {
		columns["sort"] = *upd.Sort
	}
	if err := db.Model(&model.ProductGroup{}).Where("id = ?", id).Updates(columns).Error; err != nil {
		return nil, err
	}
	return productGroupBy(db, "id = ?", id)
}

// ProductGroupCounts 统计各分组下的商品数（键为上游分组 ID），供管理端分组列表展示。
func (s *Store) ProductGroupCounts(ctx context.Context) (map[int]GroupCounts, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}

	var rows []struct {
		UpstreamGroupID int
		Total           int64
		On              int64
		Off             int64
	}
	if err := db.Model(&model.Product{}).
		Select("upstream_group_id, COUNT(*) AS total, " +
			"SUM(status = 'on') AS `on`, SUM(status = 'off') AS off").
		Group("upstream_group_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	counts := make(map[int]GroupCounts, len(rows))
	for _, row := range rows {
		counts[row.UpstreamGroupID] = GroupCounts{Total: row.Total, On: row.On, Off: row.Off}
	}
	return counts, nil
}

// ListMemberProducts 列出全部**已上架**商品（会员端），按本地排序输出。
func (s *Store) ListMemberProducts(ctx context.Context) ([]model.Product, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}

	products := make([]model.Product, 0)
	if err := db.Where("status = ?", model.ProductStatusOn).
		Order("sort ASC, id ASC").
		Find(&products).Error; err != nil {
		return nil, err
	}
	return products, nil
}

// MemberProductByID 查询单个**已上架**商品；下架或不存在的商品一律返回 ErrNotFound（对外 404）。
func (s *Store) MemberProductByID(ctx context.Context, id uint64) (*model.Product, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	return productBy(db, "id = ? AND status = ?", id, model.ProductStatusOn)
}

// productBy 是内部按条件查询单条商品（复用已有 GORM 句柄）。
func productBy(db *gorm.DB, query string, args ...any) (*model.Product, error) {
	var product model.Product
	if err := db.Where(query, args...).Take(&product).Error; err != nil {
		return nil, notFoundIfNeeded(err)
	}
	return &product, nil
}

// productGroupBy 是内部按条件查询单条分组。
func productGroupBy(db *gorm.DB, query string, args ...any) (*model.ProductGroup, error) {
	var group model.ProductGroup
	if err := db.Where(query, args...).Take(&group).Error; err != nil {
		return nil, notFoundIfNeeded(err)
	}
	return &group, nil
}
