package router

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

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
func (h *productHandler) importProducts(c *gin.Context) {
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

	groups := make([]store.ProductGroupInput, 0, len(catalog.Groups))
	jobs := make([]importJob, 0)
	for groupIndex, group := range catalog.Groups {
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
		inputs = append(inputs, results[i].input)
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

	if len(failedPIDs) > 0 {
		h.logger.Warn("商品导入部分失败", "failed", len(failedPIDs), "pids", view.FailedPIDs, "error", firstErr)
	}
	h.logger.Info("商品导入完成", "groups", view.Groups, "created", view.Created,
		"updated", view.Updated, "unchanged", view.Unchanged, "failed", view.Failed)
	response.Success(c, view)
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
