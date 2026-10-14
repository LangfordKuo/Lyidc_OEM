package router

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/category"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upstream"
)

const (
	// importWorkers 是导入时并发抓取上游商品详情的协程数（实测 159 个商品 4 并发约 6.5 秒）。
	importWorkers = 4
	// importTimeout 是单次导入的整体超时（单次上游请求另有 upstream.timeout_seconds 超时）。
	importTimeout = 5 * time.Minute
	// maxReportedFailedPIDs 是导入响应里回带的失败商品 ID 上限（避免响应体过大）。
	maxReportedFailedPIDs = 20
)

// productImportView 是 POST /api/v1/admin/products/import 的数据体。
type productImportView struct {
	Created    int   `json:"created"`
	Updated    int   `json:"updated"`
	Unchanged  int   `json:"unchanged"`
	Groups     int   `json:"groups"`
	Failed     int   `json:"failed"`
	FailedPIDs []int `json:"failed_pids,omitempty"`
	// SelectedGroups 是本次选中的上游分组 ID（升序去重）；**全量导入时不出现**（R12，契约 10.4）。
	SelectedGroups []int `json:"selected_groups,omitempty"`
	// MapFallbackGroupIDs 是因映射目标失效而回落自动派生的上游分组 ID（R12，契约 10.1）；
	// 无失效映射时不出现。
	MapFallbackGroupIDs []int `json:"map_fallback_group_ids,omitempty"`
}

// productImportRequest 是 POST /api/v1/admin/products/import 的**可选**请求体（R12，契约 10.4）。
//
// UpstreamGroupIDs 用指针承接，以区分「省略请求体 / null」（nil → 全量导入）与
// 「显式数组」（非 nil）：空数组同样按全量导入处理，保持向后兼容。
type productImportRequest struct {
	UpstreamGroupIDs *[]int `json:"upstream_group_ids"`
}

// parseImportSelection 解析可选请求体并返回选中的上游分组 ID（去重升序）。
//
// 返回 selection=nil 表示**全量导入**：省略请求体 / `{}` / `null` / `[]` 都归一到这一支
// （契约 10.4：缺省即全量，与 R11 前完全一致）。非法时已写出响应并返回 ok=false。
func parseImportSelection(c *gin.Context) ([]int, bool) {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		response.Fail(c, response.CodeInvalidParam, "读取请求体失败")
		return nil, false
	}
	trimmed := strings.TrimSpace(string(raw))
	// 省略请求体 / null / 空数组（`[]` 或 `{"upstream_group_ids":[]}`）一律归一到全量导入（契约 10.4）。
	switch trimmed {
	case "", "[]", "null":
		return nil, true
	}

	var req productImportRequest
	if err := json.Unmarshal([]byte(trimmed), &req); err != nil {
		response.Fail(c, response.CodeInvalidParam,
			`请求体格式不正确：应为 {"upstream_group_ids": [上游分组 ID]}`)
		return nil, false
	}
	if req.UpstreamGroupIDs == nil || len(*req.UpstreamGroupIDs) == 0 {
		return nil, true
	}

	seen := make(map[int]struct{}, len(*req.UpstreamGroupIDs))
	ids := make([]int, 0, len(*req.UpstreamGroupIDs))
	for _, id := range *req.UpstreamGroupIDs {
		if id <= 0 {
			response.Fail(c, response.CodeInvalidParam,
				"upstream_group_ids 只能是正整数（不能为 0 或负数）")
			return nil, false
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids, true
}

// importJob 是一个待抓取详情的上游商品。
type importJob struct {
	// index 是在扁平商品列表中的下标（并发结果按下标回填）。
	index   int
	groupID int
	sort    int
	product upstream.Product
}

// importFetchResult 是单个商品的详情抓取结果。
type importFetchResult struct {
	input store.ProductInput
	err   error
}

// importProducts 处理 POST /api/v1/admin/products/import：
// 拉取上游目录与逐个商品详情（价格/库存/可配置项），批量 upsert 到本地。
//
// 上游侧全程只读（GET /cart/all、GET /cart/get_product_config）。
//
// R12：可选请求体 `{"upstream_group_ids":[...]}` 只导入选中的上游分组（缺省/空/`null` = 全量）；
// 归类优先级为 manual 不覆盖 > 上游分组映射 > 自动派生（契约 10.1）。
func (h *productHandler) importProducts(c *gin.Context) {
	// 先解析可选请求体（纯格式校验，不依赖上游）；非法直接 40001，不发起任何上游调用。
	selection, ok := parseImportSelection(c)
	if !ok {
		return
	}

	client, err := h.upstreamClient(c.Request.Context())
	if err != nil {
		// 上游参数来自后台设置（upstream 键）：未配置齐全时明确提示，不发起任何请求。
		response.Fail(c, response.CodeInternalError, "上游未配置，无法导入商品（请在后台设置中填写上游参数）")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), importTimeout)
	defer cancel()

	catalog, err := client.Products(ctx)
	if err != nil {
		h.logger.Error("商品导入失败：拉取上游目录失败", "error", err)
		response.Fail(c, response.CodeInternalError, "上游商品目录拉取失败："+upstreamProbeError(err))
		return
	}

	// 选中的分组必须在本次上游目录里：否则会「静默导入 0 个商品还报成功」（契约 10.4）。
	if selection != nil {
		missing := missingGroupIDs(selection, catalog.Groups)
		if len(missing) > 0 {
			response.Fail(c, response.CodeValidationFailed, "上游分组不存在："+formatIntList(missing))
			return
		}
	}

	// R12：载入「上游分组 → 本地子分类」映射；目标已失效的降级为自动派生（不 500、不静默）。
	mappings, fallbackGroups, err := h.resolveGroupMappings(ctx)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	selected := make(map[int]struct{}, len(selection))
	for _, id := range selection {
		selected[id] = struct{}{}
	}

	groups := make([]store.ProductGroupInput, 0, len(catalog.Groups))
	jobs := make([]importJob, 0)
	groupNames := make(map[int]string, len(catalog.Groups))
	for groupIndex, group := range catalog.Groups {
		if selection != nil {
			if _, ok := selected[group.ID]; !ok {
				continue
			}
		}
		groupNames[group.ID] = group.Name
		groups = append(groups, store.ProductGroupInput{
			UpstreamGroupID: group.ID,
			Name:            group.Name,
			// 首次导入按上游目录顺序写入 sort（分组与商品都是）。
			Sort: groupIndex,
		})
		for _, product := range group.Products {
			jobs = append(jobs, importJob{
				index:   len(jobs),
				groupID: group.ID,
				sort:    len(jobs),
				product: product,
			})
		}
	}

	results := fetchProductDetails(ctx, client, jobs)

	inputs := make([]store.ProductInput, 0, len(jobs))
	failedPIDs := make([]int, 0)
	var firstErr error
	for i := range results {
		if results[i].err != nil {
			if firstErr == nil {
				firstErr = results[i].err
			}
			failedPIDs = append(failedPIDs, jobs[i].product.ID)
			continue
		}
		input := results[i].input
		if mappedID, ok := mappings[jobs[i].groupID]; ok {
			// R12：命中「上游分组 → 子分类」映射，直接采用（优先于自动派生）。
			// 映射是导入规则的一部分，写入 category_source=derived（改映射后重导可刷新）；
			// manual 商品仍由 store 跳过覆盖（契约 10.1 优先级第 1 条）。
			input.MappedCategoryID = mappedID
		} else {
			// R7：无映射时自动派生两级分类（契约 10.1）。派生是纯函数，重复导入收敛。
			derived := category.Derive(input.Name, groupNames[jobs[i].groupID], jobs[i].groupID, jobs[i].sort)
			input.Category = &derived
		}
		inputs = append(inputs, input)
	}

	// 目录里有商品但一个详情都没抓到，属上游整体异常：直接失败，而不是回一个「0 商品」的成功。
	if len(jobs) > 0 && len(inputs) == 0 {
		h.logger.Error("商品导入失败：全部商品详情抓取失败", "error", firstErr, "products", len(jobs))
		response.Fail(c, response.CodeInternalError, "上游商品详情全部拉取失败："+upstreamProbeError(firstErr))
		return
	}

	importResult, err := h.store.ImportCatalog(ctx, groups, inputs)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	view := productImportView{
		Created:   importResult.Created,
		Updated:   importResult.Updated,
		Unchanged: importResult.Unchanged,
		Groups:    importResult.Groups,
		Failed:    len(failedPIDs),
	}
	if len(failedPIDs) > maxReportedFailedPIDs {
		view.FailedPIDs = failedPIDs[:maxReportedFailedPIDs]
	} else {
		view.FailedPIDs = failedPIDs
	}
	if selection != nil {
		view.SelectedGroups = selection
	}
	if len(fallbackGroups) > 0 {
		sort.Ints(fallbackGroups)
		view.MapFallbackGroupIDs = fallbackGroups
	}

	if len(failedPIDs) > 0 {
		h.logger.Warn("商品导入部分失败", "failed", len(failedPIDs), "pids", view.FailedPIDs, "error", firstErr)
	}
	h.logger.Info("商品导入完成", "groups", view.Groups, "created", view.Created,
		"updated", view.Updated, "unchanged", view.Unchanged, "failed", view.Failed,
		"selected_groups", view.SelectedGroups, "map_fallback_group_ids", view.MapFallbackGroupIDs)
	setOplogSummary(c, fmt.Sprintf("已从上游导入商品：分组 %d、新建 %d、更新 %d、未变 %d、失败 %d",
		view.Groups, view.Created, view.Updated, view.Unchanged, view.Failed))
	// R19 批 2：导入会改商品与分类（覆盖上游字段、按映射/派生归类、写分组名与计数），
	// products: 与 categories: 两个键族一起失效。**尽力而为**，失败只记日志（TTL 兜底）。
	h.cache.invalidate(ctx, cachePrefixProducts, cachePrefixCategories)
	response.Success(c, view)
}

// resolveGroupMappings 载入「上游分组 → 本地子分类」映射（R12，契约 10.1）。
//
// 第二个返回值是**目标已失效**（分类被删除、或被外部改坏成主分类）的映射分组 ID：
// 这些分组本次导入回落自动派生，并由调用方写进响应的 map_fallback_group_ids 与日志。
func (h *productHandler) resolveGroupMappings(ctx context.Context) (map[int]uint64, []int, error) {
	maps, err := h.store.ListGroupCategoryMaps(ctx)
	if err != nil {
		return nil, nil, err
	}
	if len(maps) == 0 {
		return map[int]uint64{}, nil, nil
	}

	// 一次性加载分类表，判定映射目标是否仍是存在的**子分类**（避免逐条查库）。
	categories, err := h.store.ListProductCategories(ctx)
	if err != nil {
		return nil, nil, err
	}
	subIDs := make(map[uint64]struct{}, len(categories))
	for i := range categories {
		if !categories[i].IsMain() {
			subIDs[categories[i].ID] = struct{}{}
		}
	}

	resolved := make(map[int]uint64, len(maps))
	fallback := make([]int, 0)
	for _, item := range maps {
		if _, ok := subIDs[item.CategoryID]; !ok {
			h.logger.Warn("上游分组映射的目标分类已失效，本次导入回落自动派生",
				"upstream_group_id", item.UpstreamGroupID, "category_id", item.CategoryID)
			fallback = append(fallback, item.UpstreamGroupID)
			continue
		}
		resolved[item.UpstreamGroupID] = item.CategoryID
	}
	return resolved, fallback, nil
}

// missingGroupIDs 返回 selection 中不在本次上游目录里的分组 ID（升序）。
func missingGroupIDs(selection []int, catalog []upstream.ProductGroup) []int {
	present := make(map[int]struct{}, len(catalog))
	for _, group := range catalog {
		present[group.ID] = struct{}{}
	}
	missing := make([]int, 0)
	for _, id := range selection {
		if _, ok := present[id]; !ok {
			missing = append(missing, id)
		}
	}
	sort.Ints(missing)
	return missing
}

// formatIntList 把整数列表拼成「1、3、5」形式（错误文案用）。
func formatIntList(values []int) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.Itoa(value))
	}
	return strings.Join(parts, "、")
}

// fetchProductDetails 以固定并发抓取全部商品详情，结果按 jobs 下标回填。
// client 在导入开始时取一次（同一次导入全程用同一份设置下的客户端）。
func fetchProductDetails(ctx context.Context, client *upstream.Client, jobs []importJob) []importFetchResult {
	results := make([]importFetchResult, len(jobs))
	if len(jobs) == 0 {
		return results
	}

	semaphore := make(chan struct{}, importWorkers)
	var waitGroup sync.WaitGroup
	for i := range jobs {
		waitGroup.Add(1)
		go func(job importJob) {
			defer waitGroup.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			results[job.index] = fetchProductDetail(ctx, client, job)
		}(jobs[i])
	}
	waitGroup.Wait()
	return results
}

// fetchProductDetail 抓取单个商品的上游详情，组装成落库输入。
func fetchProductDetail(ctx context.Context, client *upstream.Client, job importJob) importFetchResult {
	raw, err := client.ProductConfigRaw(ctx, job.product.ID)
	if err != nil {
		return importFetchResult{err: err}
	}

	var detail upstream.ProductConfig
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &detail); err != nil {
			return importFetchResult{err: fmt.Errorf("%w: 商品 %d 详情解析失败: %v",
				upstream.ErrDecode, job.product.ID, err)}
		}
	}

	upstreamPrices, err := buildUpstreamPrices(raw, detail.Pricings)
	if err != nil {
		return importFetchResult{err: err}
	}
	encodedPrices, err := upstreamPrices.Marshal()
	if err != nil {
		return importFetchResult{err: err}
	}

	return importFetchResult{input: store.ProductInput{
		UpstreamPID:     job.product.ID,
		UpstreamGroupID: job.groupID,
		Name:            firstNonEmpty(job.product.Name, detail.Product.Name),
		// Description 落库前做一次 HTML 实体反转义（R5 口径，见迁移 0012 与契约 10.3）：
		// 上游把 <li> 存成 &lt;li&gt;，库里统一存解码后的原始 HTML，接口层再解析成行数组。
		// 该转换是确定性的：导入幂等口径不变（首次导入刷新存量后，重复导入仍计 unchanged）。
		Description:        html.UnescapeString(firstNonEmpty(job.product.Description, detail.Product.Description)),
		Type:               firstNonEmpty(job.product.Type, detail.Product.Type),
		Module:             firstNonEmpty(job.product.Module, detail.Product.Module),
		ConfigJSON:         string(raw),
		UpstreamPricesJSON: encodedPrices,
		StockQty:           detail.Product.Qty,
		OntrialMax:         detail.Product.Ontrial,
		Sort:               job.sort,
	}}
}

// buildUpstreamPrices 组装上游价格缓存：保留 product_pricings 原文 + 挑选出生效的六周期价格。
//
// 库存与试用数量取 /cart/get_product_config 的 products.qty / products.ontrial
// （实测 /cart/all 的 qty / ontrial / stock_control 恒为 0，不可用）。
func buildUpstreamPrices(raw json.RawMessage, rows []upstream.Pricing) (pricing.UpstreamPrices, error) {
	result := pricing.UpstreamPrices{Prices: map[string]string{}}

	if len(raw) > 0 {
		var envelope struct {
			Pricings []json.RawMessage `json:"product_pricings"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return pricing.UpstreamPrices{}, fmt.Errorf("%w: 上游价格行解析失败: %v", upstream.ErrDecode, err)
		}
		result.Rows = envelope.Pricings
	}

	selected, ok := selectPricingRow(rows)
	if !ok {
		return result, nil
	}
	result.Code = selected.Code
	result.Prices = map[string]string{
		pricing.CycleMonthly:      selected.Monthly,
		pricing.CycleQuarterly:    selected.Quarterly,
		pricing.CycleSemiAnnually: selected.SemiAnnually,
		pricing.CycleAnnually:     selected.Annually,
		pricing.CycleBiennially:   selected.Biennially,
		pricing.CycleTriennially:  selected.Triennially,
	}
	return result, nil
}

// selectPricingRow 挑选生效的价格行：优先货币代码为 CNY 的行，
// 其次第一行「月付价可用」的行，最后回退首行。
func selectPricingRow(rows []upstream.Pricing) (upstream.Pricing, bool) {
	for _, row := range rows {
		if strings.EqualFold(strings.TrimSpace(row.Code), "CNY") {
			return row, true
		}
	}
	for _, row := range rows {
		if cents, err := pricing.ParseAmount(row.Monthly); err == nil && cents >= 0 {
			return row, true
		}
	}
	if len(rows) > 0 {
		return rows[0], true
	}
	return upstream.Pricing{}, false
}

// firstNonEmpty 返回第一个非空字符串（目录字段优先，商品详情兜底）。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
