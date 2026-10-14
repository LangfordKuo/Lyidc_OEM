package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/oplog"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upstream"
)

// ---------------------------------------------------------------------------
// R20 商品同步（契约 10.7）：只更新已存在商品 / 本地字段不动 / 逐条失败与上限 / 权限 / 缓存失效
// ---------------------------------------------------------------------------

// adminTokenNamed 建一个指定用户名与角色的管理员并登录（同一引擎下多次建号时避免重名）。
func adminTokenNamed(t *testing.T, engine http.Handler, gdb *gorm.DB, username, role string) string {
	t.Helper()
	const password = "admin123456"
	seedAdmin(t, gdb, username, password, role, model.StatusActive)
	token, _ := loginAdmin(t, engine, username, password)
	return token
}

// syncProductsRaw 调 POST /admin/products/sync，返回记录器与响应包（供错误码断言）。
func syncProductsRaw(t *testing.T, engine http.Handler, token string, body any) (*httptest.ResponseRecorder, apiEnvelope) {
	t.Helper()
	return doAPI(t, engine, http.MethodPost, "/api/v1/admin/products/sync", token, body)
}

// syncProducts 调 POST /admin/products/sync 并断言成功，返回逐条结果视图。
func syncProducts(t *testing.T, engine http.Handler, token string, ids []uint64) productSyncView {
	t.Helper()
	raw := make([]int, 0, len(ids))
	for _, id := range ids {
		raw = append(raw, int(id))
	}
	rec, envelope := syncProductsRaw(t, engine, token, map[string]any{"product_ids": raw})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("同步商品失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[productSyncView](t, envelope)
}

// syncItemOf 取某条同步结果。
func syncItemOf(t *testing.T, view productSyncView, productID uint64) productSyncItemView {
	t.Helper()
	for _, item := range view.Items {
		if item.ProductID == productID {
			return item
		}
	}
	t.Fatalf("同步结果里没有商品 %d（items=%+v）", productID, view.Items)
	return productSyncItemView{}
}

// loadProductRow 直接查库取商品行（断言落库结果）。
func loadProductRow(t *testing.T, gdb *gorm.DB, id uint64) model.Product {
	t.Helper()
	var product model.Product
	if err := gdb.Where("id = ?", id).Take(&product).Error; err != nil {
		t.Fatalf("查询商品 %d 失败: %v", id, err)
	}
	return product
}

// countProductRows 统计商品表行数（断言同步不新增、不删除）。
func countProductRows(t *testing.T, gdb *gorm.DB) int64 {
	t.Helper()
	var total int64
	if err := gdb.Model(&model.Product{}).Count(&total).Error; err != nil {
		t.Fatalf("统计商品数失败: %v", err)
	}
	return total
}

// changedCatalog 返回标准目录的变体：商品 101 的名称/简介/库存/价格都变了，102 保持原样。
func changedCatalog() *fakeCatalog {
	return &fakeCatalog{groups: []fakeGroup{
		{ID: 1, Name: "香港二区", Products: []fakeProduct{
			{
				ID: 101, Name: "香港二区 CN2 A型（新）",
				Description: "&lt;li&gt;CPU:4核心&lt;/li&gt;",
				Type:        "dcimcloud", Module: "idcsmart_common", StockControl: 1, Qty: 55,
				Prices: fakePrices{Monthly: "25.00", Quarterly: "75.00", SemiAnnually: "150.00", Annually: "250.00"},
			},
			{
				ID: 102, Name: "香港二区 CN2 B型", Description: "&lt;li&gt;CPU:4核心&lt;/li&gt;",
				Type: "dcimcloud", Module: "idcsmart_common", StockControl: 1, Qty: 10,
				Prices: fakePrices{Monthly: "40.00", Quarterly: "120.00", SemiAnnually: "240.00", Annually: "400.00"},
			},
		}},
		{ID: 2, Name: "美国一区", Products: []fakeProduct{
			{
				ID: 201, Name: "美国一区 特价", Description: "",
				Type: "dcimcloud", Module: "idcsmart_common", StockControl: 0, Qty: 0, Ontrial: 3,
				Prices: fakePrices{Monthly: "30.00", Quarterly: "", SemiAnnually: "150.00", Annually: ""},
			},
		}},
		{ID: 3, Name: "空分组"},
	}}
}

// TestProductSyncRefreshesUpstreamFieldsAndKeepsLocalOnes 覆盖同步的核心语义：
// 只更新已存在商品的上游来源字段（名称/简介/价格缓存/库存），本地字段（定价/上下架/排序/返佣）
// 一律不动；无变化的商品计 unchanged；不新增、不删除商品。
func TestProductSyncRefreshesUpstreamFieldsAndKeepsLocalOnes(t *testing.T) {
	gdb := testDatabase(t)
	catalog := standardCatalog()
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, catalog))
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)
	importProducts(t, engine, token)

	idA := productIDByUpstreamPID(t, gdb, 101)
	idB := productIDByUpstreamPID(t, gdb, 102)
	before := countProductRows(t, gdb)

	// 本地字段全部改一遍：定价（markup 15%）、上架、排序、返佣比例——同步必须一个都不碰。
	updateProductOK(t, engine, token, idA, map[string]any{
		"pricing_json":   map[string]any{"mode": "markup", "markup_percent": 15},
		"status":         "on",
		"sort":           7,
		"rebate_percent": "8.00",
	})

	// 上游变了（商品 101 名称/简介/库存/价格，102 原样）。
	catalog.setGroups(changedCatalog().groups)
	view := syncProducts(t, engine, token, []uint64{idA, idB})

	if view.Total != 2 || view.Succeeded != 2 || view.Failed != 0 || view.Updated != 1 || view.Unchanged != 1 {
		t.Fatalf("汇总计数不符: %+v", view)
	}
	if item := syncItemOf(t, view, idA); item.Status != syncStatusUpdated || item.UpstreamPID != 101 || item.Reason != "" {
		t.Fatalf("商品 101 应为 updated: %+v", item)
	}
	if item := syncItemOf(t, view, idB); item.Status != syncStatusUnchanged || item.UpstreamPID != 102 {
		t.Fatalf("商品 102 应为 unchanged: %+v", item)
	}

	// 上游来源字段被刷成最新。
	updated := loadProductRow(t, gdb, idA)
	if updated.Name != "香港二区 CN2 A型（新）" {
		t.Fatalf("名称未被刷新: %q", updated.Name)
	}
	if updated.Description != "<li>CPU:4核心</li>" {
		t.Fatalf("简介未被刷新（应做一次 HTML 反转义）: %q", updated.Description)
	}
	if updated.StockQty != 55 || updated.OntrialMax != 0 {
		t.Fatalf("库存未被刷新: qty=%d ontrial=%d", updated.StockQty, updated.OntrialMax)
	}
	if !strings.Contains(updated.UpstreamPricesJSON, `"monthly":"25.00"`) {
		t.Fatalf("上游价格缓存未被刷新: %s", updated.UpstreamPricesJSON)
	}
	if !strings.Contains(updated.ConfigJSON, `"qty":55`) {
		t.Fatalf("config_json 详情缓存未被刷新: %s", updated.ConfigJSON)
	}

	// 本地字段一个都没动。
	if !strings.Contains(updated.PricingJSON, `"markup_percent":15`) {
		t.Fatalf("本地定价被同步改动了: %s", updated.PricingJSON)
	}
	if updated.Status != model.ProductStatusOn || updated.Sort != 7 {
		t.Fatalf("本地上架状态/排序被同步改动了: status=%s sort=%d", updated.Status, updated.Sort)
	}
	if updated.RebatePercent == nil || *updated.RebatePercent != "8.00" || updated.RebateDisabled {
		t.Fatalf("本地返佣设置被同步改动了: percent=%v disabled=%v", updated.RebatePercent, updated.RebateDisabled)
	}

	// 不新增、不删除。
	if after := countProductRows(t, gdb); after != before {
		t.Fatalf("同步不应新增或删除商品：前 %d、后 %d", before, after)
	}

	// 再同步一次：全部 unchanged（幂等收敛）。
	again := syncProducts(t, engine, token, []uint64{idA, idB})
	if again.Updated != 0 || again.Unchanged != 2 {
		t.Fatalf("重复同步应全部 unchanged: %+v", again)
	}
}

// TestProductSyncCategoryRules 覆盖分类口径与导入一致：
// manual 归类不被同步覆盖；derived 归类按上游名称/组变化重新派生；命中分组映射时映射优先。
func TestProductSyncCategoryRules(t *testing.T) {
	gdb := testDatabase(t)
	catalog := standardCatalog()
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, catalog))
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)
	importProducts(t, engine, token)

	idA := productIDByUpstreamPID(t, gdb, 101)
	idB := productIDByUpstreamPID(t, gdb, 102)

	// A 手工改挂到「美国一区」（category_source=manual），B 保持派生归类。
	categories := adminCategories(t, engine, token)
	var usaSubID uint64
	for _, main := range categories.Items {
		for _, sub := range main.Subcategories {
			if sub.Name == "美国一区" {
				usaSubID = sub.ID
			}
		}
	}
	if usaSubID == 0 {
		t.Fatalf("标准目录应派生出「美国一区」子分类: %+v", categories.Items)
	}
	updateProductOK(t, engine, token, idA, map[string]any{"category_id": usaSubID})
	if row := loadProductRow(t, gdb, idA); row.CategorySource != model.CategorySourceManual {
		t.Fatalf("改挂后应为 manual: %+v", row)
	}

	// 上游改名：101 改成「美国一区」形态（派生本会漂移），102 也改到别的区域形态。
	catalog.setGroups([]fakeGroup{
		{ID: 1, Name: "香港二区", Products: []fakeProduct{
			{ID: 101, Name: "美国一区 CN2 A型", Type: "dcimcloud", Module: "idcsmart_common", Qty: 70,
				Prices: fakePrices{Monthly: "20.00", Quarterly: "60.00", SemiAnnually: "120.00", Annually: "200.00"}},
			{ID: 102, Name: "香港二区 CN2 B型", Type: "dcimcloud", Module: "idcsmart_common", Qty: 10,
				Prices: fakePrices{Monthly: "40.00", Quarterly: "120.00", SemiAnnually: "240.00", Annually: "400.00"}},
		}},
		{ID: 2, Name: "美国一区", Products: []fakeProduct{
			{ID: 201, Name: "美国一区 特价", Type: "dcimcloud", Module: "idcsmart_common",
				Prices: fakePrices{Monthly: "30.00", SemiAnnually: "150.00"}},
		}},
	})

	// 给上游分组 1 建一条映射（指向「美国一区」子分类）：派生商品应改为命中映射。
	rec, envelope := doAPI(t, engine, http.MethodPut, "/api/v1/admin/product-group-maps/1", token,
		map[string]any{"category_id": usaSubID})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("建立分组映射失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	view := syncProducts(t, engine, token, []uint64{idA, idB})
	if view.Updated != 2 || view.Failed != 0 {
		t.Fatalf("两条都应更新（名称/分类变化）: %+v", view)
	}

	// A：manual 归类不被覆盖（仍是「美国一区」，但因为本来就是它，这里改挂到别处更能证明守卫）。
	manual := loadProductRow(t, gdb, idA)
	if manual.CategorySource != model.CategorySourceManual || manual.CategoryID != usaSubID {
		t.Fatalf("manual 商品归类不应被同步覆盖: category_id=%d source=%s", manual.CategoryID, manual.CategorySource)
	}
	// B：derived 商品命中分组 1 的映射 → 归到映射目标，且仍是 derived。
	derived := loadProductRow(t, gdb, idB)
	if derived.CategoryID != usaSubID || derived.CategorySource != model.CategorySourceDerived {
		t.Fatalf("派生商品应命中映射归类: category_id=%d source=%s", derived.CategoryID, derived.CategorySource)
	}
}

// TestProductSyncManualCategoryNotOverwritten 单独覆盖 manual 守卫：上游组/名称变化会改派生归类，
// 但 manual 商品一个字段都不动（分类与来源都保持）。
func TestProductSyncManualCategoryNotOverwritten(t *testing.T) {
	gdb := testDatabase(t)
	catalog := standardCatalog()
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, catalog))
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)
	importProducts(t, engine, token)

	idA := productIDByUpstreamPID(t, gdb, 101)
	// 手工改挂到「美国一区」。
	categories := adminCategories(t, engine, token)
	var usaSubID uint64
	for _, main := range categories.Items {
		for _, sub := range main.Subcategories {
			if sub.Name == "美国一区" {
				usaSubID = sub.ID
			}
		}
	}
	updateProductOK(t, engine, token, idA, map[string]any{"category_id": usaSubID})
	categoryBefore := loadProductRow(t, gdb, idA).CategoryID

	// 上游把该商品移到分组 2（派生归类会变成美国一区），名称也变。
	catalog.setGroups([]fakeGroup{
		{ID: 1, Name: "香港二区", Products: []fakeProduct{
			{ID: 102, Name: "香港二区 CN2 B型", Type: "dcimcloud", Module: "idcsmart_common", Qty: 10,
				Prices: fakePrices{Monthly: "40.00", Quarterly: "120.00", SemiAnnually: "240.00", Annually: "400.00"}},
		}},
		{ID: 2, Name: "美国一区", Products: []fakeProduct{
			{ID: 101, Name: "美国一区 特价", Type: "dcimcloud", Module: "idcsmart_common", Qty: 20,
				Prices: fakePrices{Monthly: "30.00", SemiAnnually: "150.00"}},
		}},
	})

	view := syncProducts(t, engine, token, []uint64{idA})
	if view.Updated != 1 {
		t.Fatalf("上游名称/分组/库存都变了，应计 updated: %+v", view)
	}
	row := loadProductRow(t, gdb, idA)
	// 上游字段刷新了……
	if row.Name != "美国一区 特价" || row.UpstreamGroupID != 2 {
		t.Fatalf("上游字段应刷新: name=%q group=%d", row.Name, row.UpstreamGroupID)
	}
	// ……但 manual 归类保持原样。
	if row.CategoryID != categoryBefore || row.CategorySource != model.CategorySourceManual {
		t.Fatalf("manual 归类不应被覆盖: category_id=%d（原 %d）source=%s",
			row.CategoryID, categoryBefore, row.CategorySource)
	}
}

// TestProductSyncFailureIsolation 覆盖逐条失败与原因：
// 本地 ID 不存在 / 上游已无该商品 / 详情抓取失败都只影响该条，其它条目照常落库。
func TestProductSyncFailureIsolation(t *testing.T) {
	gdb := testDatabase(t)
	catalog := standardCatalog()
	// 自定义假上游：商品 102 在目录里存在，但详情接口返回业务失败（模拟单条详情异常）。
	// 开关在导入之后才打开——否则导入阶段就会跳过 102，本地根本没有这个商品。
	var failDetail atomic.Bool
	client := fakeUpstreamClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cart/all":
			_, _ = io.WriteString(w, catalog.catalogBody())
		case "/cart/get_product_config":
			pid, _ := strconv.Atoi(r.URL.Query().Get("pid"))
			if pid == 102 && failDetail.Load() {
				_, _ = io.WriteString(w, `{"status":406,"msg":"产品不存在"}`)
				return
			}
			product, ok := catalog.find(pid)
			if !ok {
				_, _ = io.WriteString(w, `{"status":406,"msg":"产品不存在"}`)
				return
			}
			_, _ = io.WriteString(w, detailBody(product))
		default:
			_, _ = io.WriteString(w, `{"status":404,"msg":"接口不存在"}`)
		}
	})
	engine := newProductEngine(t, gdb, client)
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)
	importProducts(t, engine, token)
	failDetail.Store(true)

	idA := productIDByUpstreamPID(t, gdb, 101)
	idB := productIDByUpstreamPID(t, gdb, 102)

	// 一个本地商品，其上游 pid 不在本次目录里（上游已删除）。
	now := time.Now().UTC()
	gone := model.Product{
		UpstreamPID: 999, UpstreamGroupID: 1, Name: "上游已删除的商品",
		PricingJSON: `{"mode":"upstream"}`, Status: model.ProductStatusOff, CreatedAt: now, UpdatedAt: now,
	}
	if err := gdb.Create(&gone).Error; err != nil {
		t.Fatalf("写入测试商品失败: %v", err)
	}
	const missingLocalID = uint64(424242)

	// 上游把 101 改掉，用于证明失败条目不影响其它条目落库。
	catalog.setGroups(changedCatalog().groups)
	view := syncProducts(t, engine, token, []uint64{idA, gone.ID, missingLocalID, idB})

	if view.Total != 4 || view.Succeeded != 1 || view.Failed != 3 || view.Updated != 1 {
		t.Fatalf("汇总计数不符: %+v", view)
	}
	if item := syncItemOf(t, view, idA); item.Status != syncStatusUpdated {
		t.Fatalf("101 应成功更新: %+v", item)
	}
	if item := syncItemOf(t, view, gone.ID); item.Status != syncStatusFailed ||
		item.Reason != "上游已不存在该商品（可能已下架或删除）" || item.Name != "上游已删除的商品" {
		t.Fatalf("上游已删除商品应记失败并给出原因: %+v", item)
	}
	if item := syncItemOf(t, view, missingLocalID); item.Status != syncStatusFailed ||
		item.Reason != "商品不存在" || item.UpstreamPID != 0 {
		t.Fatalf("不存在的本地 ID 应记失败: %+v", item)
	}
	if item := syncItemOf(t, view, idB); item.Status != syncStatusFailed ||
		!strings.HasPrefix(item.Reason, "上游商品详情拉取失败：") {
		t.Fatalf("详情抓取失败应记失败并给出可读原因: %+v", item)
	}

	// 单条失败不影响其它：101 的上游字段确实已落库。
	if row := loadProductRow(t, gdb, idA); row.Name != "香港二区 CN2 A型（新）" {
		t.Fatalf("成功条目应照常落库: %q", row.Name)
	}
	// 失败条目在库里保持原样（不删、不改本地字段）。
	if row := loadProductRow(t, gdb, gone.ID); row.Name != "上游已删除的商品" || row.Status != model.ProductStatusOff {
		t.Fatalf("失败条目不应被改动: %+v", row)
	}
}

// TestProductSyncValidationAndLimit 覆盖参数口径：空/非法 40001，超上限 40002，重复 ID 去重。
func TestProductSyncValidationAndLimit(t *testing.T) {
	gdb := testDatabase(t)
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, standardCatalog()))
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)

	cases := []struct {
		name    string
		body    any
		code    int
		message string
	}{
		{name: "缺少 product_ids", body: map[string]any{}, code: response.CodeInvalidParam, message: "product_ids 不能为空"},
		{name: "空数组", body: map[string]any{"product_ids": []int{}}, code: response.CodeInvalidParam, message: "product_ids 不能为空"},
		{name: "零与负数", body: map[string]any{"product_ids": []int{1, 0}}, code: response.CodeInvalidParam, message: "只能是正整数"},
		// 合法 JSON 但不是对象（绑定失败）：格式类错误 40001。
		{name: "请求体形状不符", body: "not-json", code: response.CodeInvalidParam, message: "请求体不是合法的 JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, envelope := syncProductsRaw(t, engine, token, tc.body)
			if rec.Code != http.StatusBadRequest || envelope.Code != tc.code {
				t.Fatalf("应回 %d：HTTP %d, body=%s", tc.code, rec.Code, rec.Body.String())
			}
			if !strings.Contains(envelope.Message, tc.message) {
				t.Fatalf("错误文案应可读（含 %q）：%s", tc.message, envelope.Message)
			}
		})
	}

	// 超上限：去重后 101 条 → 40002 可读原因（不做静默截断）。
	over := make([]int, 0, maxSyncProducts+1)
	for i := 1; i <= maxSyncProducts+1; i++ {
		over = append(over, i)
	}
	rec, envelope := syncProductsRaw(t, engine, token, map[string]any{"product_ids": over})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("超上限应回 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(envelope.Message, "100") {
		t.Fatalf("上限文案应写明条数: %s", envelope.Message)
	}

	// 重复 ID 去重：同一 ID 重复出现只是一条（不因重复触发上限，也不重复计数）。
	dup := make([]int, 0, maxSyncProducts+1)
	for i := 0; i < maxSyncProducts+1; i++ {
		dup = append(dup, 1)
	}
	rec, envelope = syncProductsRaw(t, engine, token, map[string]any{"product_ids": dup})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("重复 ID 去重后不应超限：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	view := decodeData[productSyncView](t, envelope)
	if view.Total != 1 || len(view.Items) != 1 {
		t.Fatalf("重复 ID 应只产生一条结果: %+v", view)
	}
}

// TestProductSyncRoleMatrix 覆盖权限：仅 admin 可用；finance / support 403；未登录 / 会员 token 401。
func TestProductSyncRoleMatrix(t *testing.T) {
	gdb := testDatabase(t)
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, standardCatalog()))

	body := map[string]any{"product_ids": []int{1}}

	// 未登录 → 401。
	if rec, _ := doAPI(t, engine, http.MethodPost, "/api/v1/admin/products/sync", "", body); rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应 401，实际 %d", rec.Code)
	}
	// 会员 token → 401（两套 token 互不通用）。
	memberToken, _ := memberTokenFor(t, engine, "syncmember1")
	if rec, _ := doAPI(t, engine, http.MethodPost, "/api/v1/admin/products/sync", memberToken, body); rec.Code != http.StatusUnauthorized {
		t.Fatalf("会员 token 应 401，实际 %d", rec.Code)
	}

	for _, role := range []string{model.RoleFinance, model.RoleSupport} {
		token := adminTokenFor(t, engine, gdb, role)
		rec, envelope := syncProductsRaw(t, engine, token, body)
		if rec.Code != http.StatusForbidden || envelope.Code != response.CodeForbidden {
			t.Fatalf("%s 应 403：HTTP %d, body=%s", role, rec.Code, rec.Body.String())
		}
	}

	adminToken := adminTokenFor(t, engine, gdb, model.RoleAdmin)
	rec, envelope := syncProductsRaw(t, engine, adminToken, body)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("admin 应可用：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
}

// TestProductSyncUpstreamNotConfiguredAndCatalogFailure 覆盖上游侧整体失败：
// 未配置 500、目录拉取失败 500（整次失败，不做逐条结果）。
func TestProductSyncUpstreamNotConfiguredAndCatalogFailure(t *testing.T) {
	gdb := testDatabase(t)
	body := map[string]any{"product_ids": []int{1}}

	// 未配置上游（StaticProvider 未持有客户端）。
	engine := newProductEngine(t, gdb, nil)
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)
	rec, envelope := syncProductsRaw(t, engine, token, body)
	if rec.Code != http.StatusInternalServerError || envelope.Code != response.CodeInternalError {
		t.Fatalf("上游未配置应 500：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(envelope.Message, "上游未配置") {
		t.Fatalf("应给出可读原因: %s", envelope.Message)
	}

	// 目录接口业务失败 → 500。
	broken := fakeUpstreamClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":400,"msg":"暂未开通API功能"}`)
	})
	engine = newProductEngine(t, gdb, broken)
	token = adminTokenNamed(t, engine, gdb, "sync-admin-broken", model.RoleAdmin)
	rec, envelope = syncProductsRaw(t, engine, token, body)
	if rec.Code != http.StatusInternalServerError || envelope.Code != response.CodeInternalError {
		t.Fatalf("目录拉取失败应 500：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(envelope.Message, "上游商品目录拉取失败") {
		t.Fatalf("应给出可读原因: %s", envelope.Message)
	}
}

// TestProductSyncUpstreamCallPattern 锁定上游调用量口径（契约 10.7）：
// 整次同步只打 1 次 /cart/all（批量目录）+ 每个商品 1 次详情，不因条目数放大目录调用。
func TestProductSyncUpstreamCallPattern(t *testing.T) {
	gdb := testDatabase(t)
	catalog := standardCatalog()

	var catalogCalls, detailCalls atomic.Int64
	client := fakeUpstreamClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cart/all":
			catalogCalls.Add(1)
			_, _ = io.WriteString(w, catalog.catalogBody())
		case "/cart/get_product_config":
			detailCalls.Add(1)
			pid, _ := strconv.Atoi(r.URL.Query().Get("pid"))
			product, ok := catalog.find(pid)
			if !ok {
				_, _ = io.WriteString(w, `{"status":406,"msg":"产品不存在"}`)
				return
			}
			_, _ = io.WriteString(w, detailBody(product))
		default:
			_, _ = io.WriteString(w, `{"status":404,"msg":"接口不存在"}`)
		}
	})
	engine := newProductEngine(t, gdb, client)
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)
	importProducts(t, engine, token)

	catalogCalls.Store(0)
	detailCalls.Store(0)
	idA := productIDByUpstreamPID(t, gdb, 101)
	idB := productIDByUpstreamPID(t, gdb, 102)
	syncProducts(t, engine, token, []uint64{idA, idB})

	if got := catalogCalls.Load(); got != 1 {
		t.Fatalf("整次同步应只打 1 次目录接口，实际 %d", got)
	}
	if got := detailCalls.Load(); got != 2 {
		t.Fatalf("两个商品应各打 1 次详情，实际 %d", got)
	}
}

// TestProductSyncInvalidatesProductAndCategoryCaches 覆盖缓存失效（契约 24.6.2）：
// 同步成功后 products: 与 categories: 两个键族立即失效（不等 TTL），下一次读就是新值；
// 全部条目失败时数据没有变化，不失效（已文档化的口径）。
func TestProductSyncInvalidatesProductAndCategoryCaches(t *testing.T) {
	gdb := testDatabase(t)
	catalog := standardCatalog()
	cacheStore := newFakeCacheStore()
	manager := newFakeCacheManager(t, cacheStore)
	engine := newBizCacheEngine(t, gdb, manager, newFakeCatalogClient(t, catalog))
	token := seedCatalog(t, engine, gdb)

	idA := productIDByUpstreamPID(t, gdb, 101)

	// 预热两族（会员目录、管理端列表、分类树）。
	memberCatalog(t, engine)
	adminCategories(t, engine, token)
	if !cacheStore.has(cacheKeyProductsCatalog) || !cacheStore.has(cacheKeyAdminCategories) {
		t.Fatalf("前置：两族都应已入缓存，实际键: %v", cacheStore.keys())
	}

	// 上游改名 → 同步 → 两族整体失效。
	catalog.setGroups(changedCatalog().groups)
	view := syncProducts(t, engine, token, []uint64{idA})
	if view.Updated != 1 {
		t.Fatalf("应有一条更新: %+v", view)
	}
	for _, key := range cacheStore.keys() {
		if strings.HasPrefix(key, cachePrefixProducts) || strings.HasPrefix(key, cachePrefixCategories) {
			t.Fatalf("同步成功后 products:/categories: 键族应整体失效，残留 %s（全部键 %v）", key, cacheStore.keys())
		}
	}
	// 下一次读立刻是新值（不等 TTL）。
	_, raw := memberCatalog(t, engine)
	if !strings.Contains(raw, "香港二区 CN2 A型（新）") {
		t.Fatal("同步后会员目录应立即反映上游新名称")
	}

	// 全部失败（本地 ID 不存在）不失效：重新预热后调用，键仍在。
	memberCatalog(t, engine)
	adminCategories(t, engine, token)
	failed := syncProducts(t, engine, token, []uint64{424242})
	if failed.Failed != 1 || failed.Succeeded != 0 {
		t.Fatalf("应全部失败: %+v", failed)
	}
	if !cacheStore.has(cacheKeyProductsCatalog) || !cacheStore.has(cacheKeyAdminCategories) {
		t.Fatalf("全部失败时数据没变，不应失效缓存，实际键: %v", cacheStore.keys())
	}
}

// TestProductSyncWritesOplogSummary 覆盖 R18 留痕：同步是管理端写操作，
// 动作名来自映射表（「同步上游商品」），摘要写清逐条汇总。
func TestProductSyncWritesOplogSummary(t *testing.T) {
	gdb := testDatabase(t)
	catalog := standardCatalog()
	writer := oplog.NewWriter(oplog.Options{
		Store:         store.New(gdb),
		Logger:        silentLogger(),
		FlushInterval: 20 * time.Millisecond,
	})
	t.Cleanup(writer.Close)
	engine := New(Options{
		CacheDir: t.TempDir(),
		Logger:   silentLogger(),
		DB:       gdb,
		JWT:      config.JWTConfig{Secret: testJWTSecret, ExpireHours: 168},
		Upstream: upstream.StaticProvider{C: newFakeCatalogClient(t, catalog)},
		Oplog:    writer,
	})
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)
	importProducts(t, engine, token)
	idA := productIDByUpstreamPID(t, gdb, 101)

	catalog.setGroups(changedCatalog().groups)
	syncProducts(t, engine, token, []uint64{idA})
	mustFlushOplog(t, writer)

	rows := oplogRows(t, gdb, "admin")
	found := false
	for _, row := range rows {
		if row.Action == "同步上游商品" {
			found = true
			if row.Path != "/api/v1/admin/products/sync" || row.Status != "success" {
				t.Fatalf("同步留痕形状不符: %+v", row)
			}
			if !strings.Contains(row.Summary, "成功 1") {
				t.Fatalf("摘要应含汇总计数: %s", row.Summary)
			}
		}
	}
	if !found {
		t.Fatalf("应有一条「同步上游商品」留痕: %+v", rows)
	}
}
