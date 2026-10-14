package router

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/category"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upstream"
)

// 商品「同步」（R20，契约 10.7）：后台商品列表多选 → 把**已存在**商品的上游来源字段刷成上游最新。
//
// 与「导入」（10.4）的语义边界：
//   - 导入 = 发现/新增商品（可建分组与分类、写上游字段），缺省全量；
//   - 同步 = **只更新已存在的本地商品**：不新增、不删除；ID 不存在、上游已无该商品、详情抓取失败
//     都只是该条失败，不影响其它条目。
//
// 两者共用同一套写入边界与归类口径（store 的 productUpstreamColumns + 映射 > 自动派生 + manual 守卫），
// 不另起一套：被刷新的永远是「上游来源字段」，本地字段（定价/售价/上下架/排序/返佣/手工分类）永不改动。
//
// 上游调用量（2026-10-15 对生产上游实测）：上游**没有**按 pids 批量取商品详情的接口——
// `/cart/get_product_config` 只接受单个 pid（`pid=1,2` 只回商品 1、`pids=1,2` 返回「产品不存在」），
// `/cart/all` 忽略 pid/pids/gid 过滤参数、恒回全量目录。因此这里的口径是：
// 1 次 `/cart/all`（批量：目录字段、分组归属与「商品是否还在上游」）+ 每个商品 1 次
// `/cart/get_product_config`（详情/价格/库存，4 并发，与导入共用同一条抓取路径）。
const (
	// maxSyncProducts 是单次同步的商品条数上限（去重后计数）：超出返回 40002 可读原因。
	maxSyncProducts = 100
	// syncTimeout 是整次同步的**上游调用**超时预算（单次上游请求另有 upstream.timeout_seconds 超时）；
	// 预算耗尽后未完成的上游抓取按该条失败返回，已完成的条目照常落库（不做整次回滚）。
	syncTimeout = 2 * time.Minute
	// syncWriteTimeout 是落库阶段的独立超时：上游调用已经完成，落库不该被客户端断连掐断
	// （与缓存失效的独立超时同一取舍，契约 24.6.4）。
	syncWriteTimeout = 30 * time.Second
)

// 同步条目的三种结果（契约 10.7）：updated=上游字段有变化并已刷新；unchanged=与库内完全一致；
// failed=该条失败（原因见 reason），不影响其它条目。
const (
	syncStatusUpdated   = "updated"
	syncStatusUnchanged = "unchanged"
	syncStatusFailed    = "failed"
)

// productSyncRequest 是 POST /api/v1/admin/products/sync 的请求体（契约 10.7）。
type productSyncRequest struct {
	// ProductIDs 是**本地商品 ID** 列表（不是上游 pid）；去重后 1-100 个。
	ProductIDs []int `json:"product_ids"`
}

// productSyncItemView 是单条同步结果（成功/失败 + 可读原因）。
type productSyncItemView struct {
	ProductID uint64 `json:"product_id"`
	// UpstreamPID 是商品的上游 pid；本地 ID 不存在时为 0。
	UpstreamPID int `json:"upstream_pid"`
	// Name 是**库内当前**商品名（失败条目用于指明是哪个商品；本地 ID 不存在时为空串）。
	Name   string `json:"name"`
	Status string `json:"status"` // updated / unchanged / failed
	// Reason 仅失败条目出现（可读原因，不含密钥与请求体）。
	Reason string `json:"reason,omitempty"`
}

// productSyncView 是 POST /api/v1/admin/products/sync 的数据体（契约 10.7）。
type productSyncView struct {
	// Total 是本次去重后的条目数（= succeeded + failed）。
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	// Updated 是上游字段发生变化的商品数；Unchanged 是与库内完全一致的数。
	Updated   int                   `json:"updated"`
	Unchanged int                   `json:"unchanged"`
	Items     []productSyncItemView `json:"items"`
}

// catalogEntry 是本次上游目录里一个商品的位置信息（分组 + 目录顺序 + 目录字段）。
type catalogEntry struct {
	groupID int
	// sort 是该商品在整份上游目录里的顺序（与导入首次创建时写入的分类 SubSort 同口径）。
	sort    int
	product upstream.Product
}

// syncProducts 处理 POST /api/v1/admin/products/sync：把选中的本地商品刷成上游最新（契约 10.7）。
//
// 上游侧全程只读（GET /cart/all + GET /cart/get_product_config）。角色守卫（仅 admin）在路由注册处挂载。
func (h *productHandler) syncProducts(c *gin.Context) {
	var req productSyncRequest
	if !bindJSON(c, &req) {
		return
	}
	ids, ok := parseSyncProductIDs(c, req.ProductIDs)
	if !ok {
		return
	}

	client, err := h.upstreamClient(c.Request.Context())
	if err != nil {
		// 上游凭据来自后台设置：未配置齐全时明确提示，不发起任何请求。
		response.Fail(c, response.CodeInternalError, "上游未配置，无法同步商品（请在后台设置中填写上游参数）")
		return
	}

	// 逐条结果按「去重升序的本地商品 ID」对齐；先占位，后续按结果就地改写。
	items := make([]productSyncItemView, 0, len(ids))
	for _, id := range ids {
		items = append(items, productSyncItemView{ProductID: id, Status: syncStatusFailed})
	}
	positionByID := make(map[uint64]int, len(ids))
	for i, id := range ids {
		positionByID[id] = i
	}

	// 载入本地商品：不存在的 ID 逐条记失败（商品不存在 / 已删除），不影响其它条目。
	existing, err := h.store.ProductsByIDs(c.Request.Context(), ids)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	localByID := make(map[uint64]*model.Product, len(existing))
	for i := range existing {
		product := &existing[i]
		localByID[product.ID] = product
		items[positionByID[product.ID]].UpstreamPID = product.UpstreamPID
		items[positionByID[product.ID]].Name = product.Name
	}

	// R12：归类映射与导入同一优先级（映射 > 自动派生；manual 商品一律不覆盖，由 store 保证）。
	mappings, _, err := h.resolveGroupMappings(c.Request.Context())
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	fetchCtx, cancelFetch := context.WithTimeout(c.Request.Context(), syncTimeout)
	defer cancelFetch()

	catalog, err := client.Products(fetchCtx)
	if err != nil {
		// 目录是整个同步的存在性依据：拉取失败属上游整体异常，整次失败（与导入同口径）。
		h.logger.Error("商品同步失败：拉取上游目录失败", "error", err)
		response.Fail(c, response.CodeInternalError, "上游商品目录拉取失败："+upstreamProbeError(err))
		return
	}
	catalogIndex := indexCatalog(catalog)

	// 组装抓取任务：本地不存在的 ID、上游已无该商品的商品都在这里逐条记失败。
	jobs := make([]importJob, 0, len(ids))
	jobPosition := make([]int, 0, len(ids))
	groupNames := make(map[int]string, len(catalog.Groups))
	groupsByID := make(map[int]store.ProductGroupInput, len(catalog.Groups))
	for groupIndex, group := range catalog.Groups {
		groupNames[group.ID] = group.Name
		// 分组只创建不覆盖；首次创建时的 sort 与导入同口径（分组在目录中的位置）。
		groupsByID[group.ID] = store.ProductGroupInput{
			UpstreamGroupID: group.ID, Name: group.Name, Sort: groupIndex,
		}
	}
	for _, id := range ids {
		product, found := localByID[id]
		if !found {
			items[positionByID[id]].Reason = "商品不存在"
			continue
		}
		entry, ok := catalogIndex[product.UpstreamPID]
		if !ok {
			items[positionByID[id]].Reason = "上游已不存在该商品（可能已下架或删除）"
			continue
		}
		jobs = append(jobs, importJob{
			index:   len(jobs),
			groupID: entry.groupID,
			sort:    entry.sort,
			product: entry.product,
		})
		jobPosition = append(jobPosition, positionByID[id])
	}

	results := fetchProductDetails(fetchCtx, client, jobs)

	// 落库阶段用独立超时：上游调用已完成，成功抓到的条目必须落库（不随请求取消）。
	writeCtx, cancelWrite := context.WithTimeout(context.WithoutCancel(c.Request.Context()), syncWriteTimeout)
	defer cancelWrite()

	usedGroups := make(map[int]store.ProductGroupInput)
	for i := range jobs {
		job := jobs[i]
		item := &items[jobPosition[i]]
		if results[i].err != nil {
			item.Reason = "上游商品详情拉取失败：" + upstreamProbeError(results[i].err)
			h.logger.Warn("商品同步：详情抓取失败，该条跳过",
				"product_id", item.ProductID, "upstream_pid", item.UpstreamPID, "error", results[i].err)
			continue
		}

		input := results[i].input
		if mappedID, ok := mappings[job.groupID]; ok {
			// 映射命中直接采用（优先于自动派生，与导入同一口径，契约 10.1）。
			input.MappedCategoryID = mappedID
		} else {
			derived := category.Derive(input.Name, groupNames[job.groupID], job.groupID, job.sort)
			input.Category = &derived
		}

		changed, err := h.store.SyncProductUpstream(writeCtx, item.ProductID, input)
		switch {
		case errors.Is(err, store.ErrNotFound):
			// 抓取期间商品被删（极端并发）：如实记失败，不回滚其它条目。
			item.Reason = "商品不存在"
		case err != nil:
			h.logger.Error("商品同步：写入本地库失败", "error", err, "product_id", item.ProductID)
			item.Reason = "写入本地库失败"
		default:
			if changed {
				item.Status = syncStatusUpdated
			} else {
				item.Status = syncStatusUnchanged
			}
			usedGroups[job.groupID] = groupsByID[job.groupID]
		}
	}

	// 商品可能在上游被移到尚未导入的新分组：像导入一样补建缺失的分组行（只创建不覆盖）。
	h.ensureSyncGroups(writeCtx, usedGroups)

	view := productSyncView{Total: len(items), Items: items}
	for i := range items {
		switch items[i].Status {
		case syncStatusUpdated:
			view.Succeeded++
			view.Updated++
		case syncStatusUnchanged:
			view.Succeeded++
			view.Unchanged++
		default:
			view.Failed++
		}
	}

	h.logger.Info("商品同步完成", "total", view.Total, "succeeded", view.Succeeded,
		"updated", view.Updated, "unchanged", view.Unchanged, "failed", view.Failed)
	setOplogSummary(c, fmt.Sprintf("已从上游同步商品：成功 %d（更新 %d、未变 %d）、失败 %d",
		view.Succeeded, view.Updated, view.Unchanged, view.Failed))
	// R19 批 2（契约 24.6.2）：同步会覆盖上游字段、按映射/派生归类、补建分组行——只要本次有成功条目，
	// products: 与 categories: 两个键族一起失效（全部失败时数据没有变化，不动缓存）。
	// **尽力而为**，失败只记日志（TTL 兜底）。
	if view.Succeeded > 0 {
		h.cache.invalidate(c.Request.Context(), cachePrefixProducts, cachePrefixCategories)
	}
	response.Success(c, view)
}

// parseSyncProductIDs 校验并归一 product_ids：去重、升序、正整数、上限 maxSyncProducts。
// 非法时已写出响应（格式类 40001、超限 40002）并返回 ok=false。
func parseSyncProductIDs(c *gin.Context, raw []int) ([]uint64, bool) {
	if len(raw) == 0 {
		response.Fail(c, response.CodeInvalidParam, "product_ids 不能为空（至少提供 1 个本地商品 ID）")
		return nil, false
	}

	seen := make(map[int]struct{}, len(raw))
	ids := make([]int, 0, len(raw))
	for _, id := range raw {
		if id <= 0 {
			response.Fail(c, response.CodeInvalidParam, "product_ids 只能是正整数（不能为 0 或负数）")
			return nil, false
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	// 上限按**去重后**条目数判定（重复 ID 只是一条）：超限明确拒绝，不静默截断。
	if len(ids) > maxSyncProducts {
		response.Fail(c, response.CodeValidationFailed,
			fmt.Sprintf("单次最多同步 %d 个商品（本次去重后 %d 个），请分批提交", maxSyncProducts, len(ids)))
		return nil, false
	}

	sort.Ints(ids)
	out := make([]uint64, 0, len(ids))
	for _, id := range ids {
		out = append(out, uint64(id))
	}
	return out, true
}

// indexCatalog 把上游目录摊平成 pid → 位置信息（分组 + 目录顺序 + 目录字段）。
func indexCatalog(catalog *upstream.ProductCatalog) map[int]catalogEntry {
	index := make(map[int]catalogEntry, len(catalog.Groups))
	position := 0
	for _, group := range catalog.Groups {
		for _, product := range group.Products {
			index[product.ID] = catalogEntry{
				groupID: group.ID,
				sort:    position,
				product: product,
			}
			position++
		}
	}
	return index
}

// ensureSyncGroups 补建本次同步涉及、本地尚未建行的上游分组（只创建不覆盖，与导入同一口径）。
//
// 失败不改变同步结果（分组行缺失只影响管理端「分组」列的展示与分组筛选，再次同步可补齐）。
func (h *productHandler) ensureSyncGroups(ctx context.Context, used map[int]store.ProductGroupInput) {
	if len(used) == 0 {
		return
	}

	existing, err := h.store.ListProductGroups(ctx)
	if err != nil {
		h.logger.Warn("商品同步：读取本地分组失败，跳过分组补齐", "error", err)
		return
	}
	known := make(map[int]struct{}, len(existing))
	for i := range existing {
		known[existing[i].UpstreamGroupID] = struct{}{}
	}

	missing := make([]store.ProductGroupInput, 0)
	for id, group := range used {
		if _, ok := known[id]; ok {
			continue
		}
		missing = append(missing, group)
	}
	if len(missing) == 0 {
		return
	}
	sort.Slice(missing, func(i, j int) bool {
		return missing[i].UpstreamGroupID < missing[j].UpstreamGroupID
	})
	if _, err := h.store.ImportCatalog(ctx, missing, nil); err != nil {
		h.logger.Warn("商品同步：补齐上游分组失败（不影响商品同步结果）", "error", err)
	}
}
