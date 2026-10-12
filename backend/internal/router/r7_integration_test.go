package router

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
)

// ---------------------------------------------------------------------------
// R7 集成测试：配置差价联动计价 + 商品两级分类
//
// 数据口径来自 2026-10-13 对开发库 159 个商品 config_json 的全量核对：
//   - 选择型（CPU/内存）子项带周期差价，换值即变价；
//   - 数量型子项是**价格档位**（多档上下衔接），差价 = 档位单价 × 可计费数量；
//   - 无 pricings 的子项（区域/系统）差价按 0 并标注「无差价数据」。
// ---------------------------------------------------------------------------

// r7ConfigGroups 是带差价的可配置项（形状与上游 /cart/get_product_config 一致）。
//
//	id=1  area|区域        option_type=12  无 pricings（无差价数据）
//	id=5  cpu|CPU          option_type=6   选择型：2核=0.00 / 4核=20.00
//	id=10 data_disk_size   option_type=14  数量型单档位 [0-256 @0.50]
//	id=7  bw|带宽          option_type=11  数量型双档位 [20-20 @0.00][21-100 @1.00]
const r7ConfigGroups = `[{"id":1,"name":"配置","description":"","options":[` +
	`{"id":1,"gid":1,"option_name":"area|区域","option_type":12,"upstream_id":0,"hidden":0,"sub":[` +
	`{"id":1,"config_id":1,"option_name":"1|HK^香港","upstream_id":0,"hidden":0,"qty_minimum":0,"qty_maximum":0,"pricings":[]}]},` +
	`{"id":5,"gid":1,"option_name":"cpu|CPU","option_type":6,"upstream_id":0,"hidden":0,"sub":[` +
	`{"id":51,"config_id":5,"option_name":"2|2核","upstream_id":0,"hidden":0,"qty_minimum":0,"qty_maximum":0,` +
	`"pricings":[{"type":"configoptions","code":"CNY","monthly":"0.00","quarterly":"0.00","semiannually":"0.00","annually":"0.00","biennially":"0.00","triennially":"0.00"}]},` +
	`{"id":52,"config_id":5,"option_name":"4|4核","upstream_id":0,"hidden":0,"qty_minimum":0,"qty_maximum":0,` +
	`"pricings":[{"type":"configoptions","code":"CNY","monthly":"20.00","quarterly":"60.00","semiannually":"120.00","annually":"240.00","biennially":"0.00","triennially":"0.00"}]}]},` +
	`{"id":10,"gid":1,"option_name":"data_disk_size|数据盘","option_type":14,"upstream_id":0,"hidden":0,"sub":[` +
	`{"id":101,"config_id":10,"option_name":"数据盘","upstream_id":0,"hidden":0,"qty_minimum":0,"qty_maximum":256,` +
	`"pricings":[{"type":"configoptions","code":"CNY","monthly":"0.50","quarterly":"1.50","semiannually":"3.00","annually":"6.00","biennially":"0.00","triennially":"0.00"}]}]},` +
	`{"id":7,"gid":1,"option_name":"bw|带宽","option_type":11,"upstream_id":0,"hidden":0,"sub":[` +
	`{"id":71,"config_id":7,"option_name":"带宽","upstream_id":0,"hidden":0,"qty_minimum":20,"qty_maximum":20,` +
	`"pricings":[{"type":"configoptions","code":"CNY","monthly":"0.00","quarterly":"0.00","semiannually":"0.00","annually":"0.00","biennially":"0.00","triennially":"0.00"}]},` +
	`{"id":72,"config_id":7,"option_name":"带宽","upstream_id":0,"hidden":0,"qty_minimum":21,"qty_maximum":100,` +
	`"pricings":[{"type":"configoptions","code":"CNY","monthly":"1.00","quarterly":"3.00","semiannually":"6.00","annually":"12.00","biennially":"0.00","triennially":"0.00"}]}]}]}]`

// r7Catalog 是带差价配置的测试目录：单分组「二区 CN2标准性能」+ 单商品。
func r7Catalog() *fakeCatalog {
	return &fakeCatalog{groups: []fakeGroup{
		{ID: 1, Name: "二区 CN2标准性能", Products: []fakeProduct{
			{
				ID: 101, Name: "香港二区 CN2 A型", Type: "dcimcloud", Module: "idcsmart_common",
				StockControl: 1, Qty: 70,
				Prices: fakePrices{
					Monthly: "20.00", Quarterly: "60.00", SemiAnnually: "120.00", Annually: "200.00",
				},
				ConfigGroups: r7ConfigGroups,
			},
		}},
	}}
}

// setupR7 导入 r7Catalog 并全部上架，返回引擎与本地商品 id（下单用）。
// 导入的新商品默认下架，而会员端下单/详情只认上架商品，故须显式上架。
func setupR7(t *testing.T) (*gin.Engine, *gorm.DB, uint64, string) {
	t.Helper()
	gdb := testDatabase(t)
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, r7Catalog()))
	adminToken := adminTokenFor(t, engine, gdb, model.RoleAdmin)
	importProducts(t, engine, adminToken)
	mustShelveAll(t, gdb)
	pid := productIDByUpstreamPID(t, gdb, 101)
	return engine, gdb, pid, adminToken
}

// createOrderAPI 调 POST /orders 并返回订单视图。
func createOrderAPI(t *testing.T, engine http.Handler, token string, body map[string]any) orderView {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/orders", token, body)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("下单失败: HTTP %d body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[orderView](t, envelope)
}

// mustAmount 断言金额字符串。
func mustAmount(t *testing.T, got, want, label string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %q, want %q", label, got, want)
	}
}

// ---------------------------------------------------------------------------
// 一、配置差价联动计价
// ---------------------------------------------------------------------------

// TestOrderAmountIncludesConfigDeltas 成交价 = 基础价 + Σ 配置差价（契约 10.2.1 / 12.4）。
func TestOrderAmountIncludesConfigDeltas(t *testing.T) {
	engine, _, pid, _ := setupR7(t)
	token, _ := memberTokenFor(t, engine, "deltamember")

	// 只选差价为 0 的区域（无 pricings）→ 成交价 = 基础价 20.00。
	order := createOrderAPI(t, engine, token, map[string]any{
		"product_id": pid, "cycle": "monthly", "config": map[string]string{"1": "1"},
	})
	mustAmount(t, order.Amount, "20.00", "无差价数据成交价")
	if order.PriceBreakdown == nil {
		t.Fatal("订单应带 price_breakdown 明细")
	}
	mustAmount(t, order.PriceBreakdown.Base, "20.00", "明细基础价")
	mustAmount(t, order.PriceBreakdown.Total, "20.00", "明细总额")
	if len(order.PriceBreakdown.Deltas) != 1 || order.PriceBreakdown.Deltas[0].Note != "无差价数据" {
		t.Fatalf("区域差价应标注无差价数据: %+v", order.PriceBreakdown.Deltas)
	}

	// 选择型换值：4核 +20.00 → 40.00。
	order = createOrderAPI(t, engine, token, map[string]any{
		"product_id": pid, "cycle": "monthly",
		"config": map[string]string{"1": "1", "5": "52"},
	})
	mustAmount(t, order.Amount, "40.00", "选择型差价成交价")
	var cpuDelta *pricing.DeltaLine
	for i := range order.PriceBreakdown.Deltas {
		if order.PriceBreakdown.Deltas[i].OptionID == 5 {
			cpuDelta = &order.PriceBreakdown.Deltas[i]
		}
	}
	if cpuDelta == nil || cpuDelta.Amount != "20.00" || cpuDelta.Kind != "select" || cpuDelta.ValueID != 52 {
		t.Fatalf("CPU 差价明细不对: %+v", cpuDelta)
	}

	// 数量型单档位：数据盘 50 → 0.50 × 50 = 25.00 → 65.00。
	order = createOrderAPI(t, engine, token, map[string]any{
		"product_id": pid, "cycle": "monthly",
		"config": map[string]string{"1": "1", "5": "52", "10": "50"},
	})
	mustAmount(t, order.Amount, "65.00", "数量型差价成交价")

	// 数量型双档位：带宽 30 落在 [21-100 @1.00]，可计费 = 30 − 20 = 10 → 10.00 → 75.00。
	order = createOrderAPI(t, engine, token, map[string]any{
		"product_id": pid, "cycle": "monthly",
		"config": map[string]string{"1": "1", "5": "52", "10": "50", "7": "30"},
	})
	mustAmount(t, order.Amount, "75.00", "双档位数量型成交价")
	var bwDelta *pricing.DeltaLine
	for i := range order.PriceBreakdown.Deltas {
		if order.PriceBreakdown.Deltas[i].OptionID == 7 {
			bwDelta = &order.PriceBreakdown.Deltas[i]
		}
	}
	if bwDelta == nil || bwDelta.Billable != 10 || bwDelta.Amount != "10.00" || bwDelta.Qty != 30 {
		t.Fatalf("带宽档位明细不对: %+v", bwDelta)
	}

	// 带宽取免费档 20 → 可计费 20（单价 0）→ 差价 0。
	order = createOrderAPI(t, engine, token, map[string]any{
		"product_id": pid, "cycle": "monthly",
		"config": map[string]string{"1": "1", "5": "52", "10": "50", "7": "20"},
	})
	mustAmount(t, order.Amount, "65.00", "免费档成交价")
}

// TestOrderConfigDeltaQuantityScalesWithQty 数量型差价随数量线性变化（用户验收：数据盘 20→50）。
func TestOrderConfigDeltaQuantityScalesWithQty(t *testing.T) {
	engine, _, pid, _ := setupR7(t)
	token, _ := memberTokenFor(t, engine, "qtymember")

	at20 := createOrderAPI(t, engine, token, map[string]any{
		"product_id": pid, "cycle": "monthly", "config": map[string]string{"10": "20"},
	})
	at50 := createOrderAPI(t, engine, token, map[string]any{
		"product_id": pid, "cycle": "monthly", "config": map[string]string{"10": "50"},
	})
	// 20GB → 10.00 差价，50GB → 25.00 差价，成交价 30.00 / 45.00。
	mustAmount(t, at20.Amount, "30.00", "数据盘 20 成交价")
	mustAmount(t, at50.Amount, "45.00", "数据盘 50 成交价")
	mustAmount(t, at50.PriceBreakdown.Deltas[0].Amount, "25.00", "数据盘 50 差价")
	if at50.PriceBreakdown.Deltas[0].Billable != 50 {
		t.Fatalf("可计费数量 = %d, want 50", at50.PriceBreakdown.Deltas[0].Billable)
	}
}

// TestOrderConfigDeltaFollowsPricingRule 三定价模式下差价口径（契约 10.2.1）。
func TestOrderConfigDeltaFollowsPricingRule(t *testing.T) {
	engine, _, pid, adminToken := setupR7(t)
	token, _ := memberTokenFor(t, engine, "rulemember")

	putPricing := func(rule string) {
		t.Helper()
		rec, envelope := doAPI(t, engine, http.MethodPut, "/api/v1/admin/products/"+itoa(pid), adminToken,
			map[string]any{"pricing_json": rule})
		if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
			t.Fatalf("改定价失败: HTTP %d body=%s", rec.Code, rec.Body.String())
		}
	}
	orderWith := func() orderView {
		t.Helper()
		return createOrderAPI(t, engine, token, map[string]any{
			"product_id": pid, "cycle": "monthly",
			"config": map[string]string{"5": "52", "10": "50"},
		})
	}

	// upstream：基础 20.00 + CPU 20.00 + 磁盘 25.00 = 65.00。
	putPricing(`{"mode":"upstream"}`)
	mustAmount(t, orderWith().Amount, "65.00", "upstream 成交价")

	// markup 10%：基础 22.00 + CPU 22.00 + 磁盘 27.50（单位 0.55 × 50）= 71.50。
	putPricing(`{"mode":"markup","markup_percent":10}`)
	markup := orderWith()
	mustAmount(t, markup.Amount, "71.50", "markup 成交价")

	// fixed 25.00 覆盖基础价，差价原样叠加：25.00 + 20.00 + 25.00 = 70.00。
	putPricing(`{"mode":"fixed","fixed":{"monthly":"25.00"}}`)
	fixed := orderWith()
	mustAmount(t, fixed.Amount, "70.00", "fixed 成交价")
	mustAmount(t, fixed.PriceBreakdown.Base, "25.00", "fixed 明细基础价")
	mustAmount(t, fixed.PriceBreakdown.Deltas[0].Amount, "20.00", "fixed 下差价原样")
}

// TestOrderCouponAppliesToTotalWithDeltas 优惠码对**成交总额**抵扣（口径不变）。
func TestOrderCouponAppliesToTotalWithDeltas(t *testing.T) {
	engine, gdb, pid, _ := setupR7(t)
	token, _ := memberTokenFor(t, engine, "couponmember")

	seedCoupon(t, gdb, "SAVE5", pricing.CouponTypeFixed, "5", `[]`, nil)
	order := createOrderAPI(t, engine, token, map[string]any{
		"product_id": pid, "cycle": "monthly",
		"config": map[string]string{"5": "52", "10": "50"}, "coupon_code": "SAVE5",
	})
	// 总额 65.00 − 5.00 = 60.00（折扣作用于基础价+差价，而不是只作用于基础价）。
	mustAmount(t, order.Amount, "65.00", "含差价总额")
	mustAmount(t, order.DiscountAmount, "5.00", "优惠折扣")
	mustAmount(t, order.FinalAmount, "60.00", "折后应付")
}

// TestOrderConfigDeltaCycleSensitivity 各周期差价独立（月付 vs 年付）。
func TestOrderConfigDeltaCycleSensitivity(t *testing.T) {
	engine, _, pid, _ := setupR7(t)
	token, _ := memberTokenFor(t, engine, "cyclemember")

	monthly := createOrderAPI(t, engine, token, map[string]any{
		"product_id": pid, "cycle": "monthly", "config": map[string]string{"5": "52", "10": "50"},
	})
	annual := createOrderAPI(t, engine, token, map[string]any{
		"product_id": pid, "cycle": "annual", "config": map[string]string{"5": "52", "10": "50"},
	})
	// 年付：基础 200.00 + CPU 240.00 + 磁盘 6.00 × 50 = 740.00。
	mustAmount(t, monthly.Amount, "65.00", "月付成交价")
	mustAmount(t, annual.Amount, "740.00", "年付成交价")
}

// ---------------------------------------------------------------------------
// 二、数量型取值区间（多档位并集）
// ---------------------------------------------------------------------------

// TestOrderQuantityRangeSpansAllTiers 多档位数量型的合法区间是全部档位并集（R7 修正）。
func TestOrderQuantityRangeSpansAllTiers(t *testing.T) {
	engine, _, pid, _ := setupR7(t)
	token, _ := memberTokenFor(t, engine, "rangemember")

	// 带宽档位 [20-20][21-100]，并集 20~100：30 合法（此前只取首档会被拒）。
	rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/orders", token, map[string]any{
		"product_id": pid, "cycle": "monthly", "config": map[string]string{"7": "30"},
	})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("并集区间内数量应可下单: HTTP %d body=%s", rec.Code, rec.Body.String())
	}

	// 超出并集上界拒绝。
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/orders", token, map[string]any{
		"product_id": pid, "cycle": "monthly", "config": map[string]string{"7": "101"},
	})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("超出区间应 40002: HTTP %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestRenewAmountIncludesConfigDeltas 续费金额 = 基础价 + Σ 配置差价，
// 配置取**实例开通时的配置快照**（原订单 config_json，契约 15.2）。
func TestRenewAmountIncludesConfigDeltas(t *testing.T) {
	gdb := testDatabase(t)
	engine := newProductEngine(t, gdb, nil)

	// 商品：基础价月付 20.00，数据盘单档位 [0-256 @0.50]，CPU 4核 +20.00。
	now := time.Now().UTC()
	product := model.Product{
		UpstreamPID:     9101, UpstreamGroupID: 1, Name: "香港二区 CN2 A型",
		Type: "dcimcloud", Module: "idcsmart_common",
		ConfigJSON:         `{"config_groups":` + r7ConfigGroups + `}`,
		UpstreamPricesJSON: `{"code":"CNY","prices":{"monthly":"20.00","annual":"200.00"}}`,
		PricingJSON:        `{"mode":"upstream"}`,
		Status:             model.ProductStatusOn,
		CreatedAt:          now, UpdatedAt: now,
	}
	if err := gdb.Create(&product).Error; err != nil {
		t.Fatalf("写商品失败: %v", err)
	}

	token, member := memberTokenFor(t, engine, "renewdelta")

	// 原订单：配置快照 = CPU 4核 + 数据盘 50（差价 20.00 + 25.00）。
	original := model.Order{
		TradeNo: "O20261013000000ORIG01", MemberID: member.ID,
		ProductID: product.ID, ProductName: product.Name, Cycle: "monthly", Qty: 1,
		ConfigJSON: `{"5":"52","10":"50"}`,
		Amount: "65.00", DiscountAmount: "0.00", FinalAmount: "65.00",
		Status: model.OrderStatusActive, Type:   model.OrderTypeNew,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := gdb.Create(&original).Error; err != nil {
		t.Fatalf("写原订单失败: %v", err)
	}

	due := now.AddDate(0, 1, 0)
	instance := model.Instance{
		MemberID: member.ID, OrderID: original.ID, HostID: 9001,
		ProductID: product.ID, ProductName: product.Name, Name: "vm-1",
		BillingCycle: "monthly", NextDueDate: &due,
		Status: model.InstanceStatusActive, UpstreamStatus: "Active",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := gdb.Create(&instance).Error; err != nil {
		t.Fatalf("写实例失败: %v", err)
	}

	rec, envelope := doAPI(t, engine, http.MethodPost,
		"/api/v1/instances/"+itoa(instance.ID)+"/renew", token,
		map[string]any{"cycle": "monthly"})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("续费下单失败: HTTP %d body=%s", rec.Code, rec.Body.String())
	}
	order := decodeData[orderView](t, envelope)
	if order.Type != model.OrderTypeRenew {
		t.Fatalf("订单类型 = %s, want renew", order.Type)
	}
	// 基础 20.00 + CPU 20.00 + 数据盘 25.00 = 65.00（不是只有基础价 20.00）。
	mustAmount(t, order.Amount, "65.00", "续费成交价")
	mustAmount(t, order.FinalAmount, "65.00", "续费应付")
	if order.PriceBreakdown == nil || len(order.PriceBreakdown.Deltas) != 2 {
		t.Fatalf("续费单应带 2 条差价明细: %+v", order.PriceBreakdown)
	}
	mustAmount(t, order.PriceBreakdown.Base, "20.00", "续费明细基础价")
}

// ---------------------------------------------------------------------------
// 三、商品两级分类
// ---------------------------------------------------------------------------

// TestProductImportDerivesCategoriesAndConverges 导入自动派生两级分类且重复导入收敛。
func TestProductImportDerivesCategoriesAndConverges(t *testing.T) {
	gdb := testDatabase(t)
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, standardCatalog()))
	adminToken := adminTokenFor(t, engine, gdb, model.RoleAdmin)

	first := importProducts(t, engine, adminToken)
	if first.Created != 3 || first.Unchanged != 0 {
		t.Fatalf("首次导入 = %+v，期望 created=3", first)
	}

	var categories []model.ProductCategory
	if err := gdb.Order("id").Find(&categories).Error; err != nil {
		t.Fatalf("查分类失败: %v", err)
	}
	// 香港二区 A/B → 主「香港」+ 子「香港二区」；美国一区 特价 → 主「美国」+ 子「美国一区」。
	byKey := map[string]model.ProductCategory{}
	for _, item := range categories {
		byKey[item.Key] = item
	}
	hk, ok := byKey["m:香港"]
	if !ok {
		t.Fatalf("缺少主分类 m:香港，实际 keys=%v", keysOf(categories))
	}
	hkSub, ok := byKey["z:香港二区"]
	if !ok {
		t.Fatalf("缺少子分类 z:香港二区，实际 keys=%v", keysOf(categories))
	}
	if hkSub.ParentID == nil || *hkSub.ParentID != hk.ID {
		t.Fatalf("子分类父级不对: %+v", hkSub)
	}
	if hkSub.Name != "香港二区" {
		t.Fatalf("子分类名 = %q, want 香港二区", hkSub.Name)
	}

	// 商品已挂到子分类。
	var assigned int64
	if err := gdb.Model(&model.Product{}).Where("category_id = ?", hkSub.ID).Count(&assigned).Error; err != nil {
		t.Fatalf("统计归类商品失败: %v", err)
	}
	if assigned != 2 {
		t.Fatalf("香港二区商品数 = %d, want 2", assigned)
	}

	// 第二次导入必须全 unchanged（分类不重建、不改名、不漂移）。
	second := importProducts(t, engine, adminToken)
	if second.Created != 0 || second.Updated != 0 || second.Unchanged != 3 {
		t.Fatalf("重复导入未收敛: %+v", second)
	}
	var afterCount int64
	if err := gdb.Model(&model.ProductCategory{}).Count(&afterCount).Error; err != nil {
		t.Fatalf("统计分类失败: %v", err)
	}
	if afterCount != int64(len(categories)) {
		t.Fatalf("重复导入后分类数从 %d 变成 %d（不应重建）", len(categories), afterCount)
	}

	// 管理员改名后再次导入：分类 key 不变、name 不被覆盖。
	if err := gdb.Model(&model.ProductCategory{}).Where("id = ?", hkSub.ID).
		Update("name", "HK-2").Error; err != nil {
		t.Fatalf("改名失败: %v", err)
	}
	third := importProducts(t, engine, adminToken)
	if third.Created != 0 || third.Updated != 0 || third.Unchanged != 3 {
		t.Fatalf("改名后导入未收敛: %+v", third)
	}
	var renamed model.ProductCategory
	if err := gdb.Where("id = ?", hkSub.ID).First(&renamed).Error; err != nil {
		t.Fatalf("查改名分类失败: %v", err)
	}
	if renamed.Name != "HK-2" {
		t.Fatalf("导入覆盖了本地分类名: %q", renamed.Name)
	}
}

// TestMemberProductsExposeCategoryTree 会员端下发 categories[] 树与 groups[].parent。
func TestMemberProductsExposeCategoryTree(t *testing.T) {
	gdb := testDatabase(t)
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, standardCatalog()))
	adminToken := adminTokenFor(t, engine, gdb, model.RoleAdmin)
	importProducts(t, engine, adminToken)
	mustShelveAll(t, gdb)

	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/products", "", nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("商品目录请求失败: HTTP %d body=%s", rec.Code, rec.Body.String())
	}
	list := decodeData[memberProductListView](t, envelope)

	if len(list.Categories) != 2 {
		t.Fatalf("主分类数 = %d, want 2 (%+v)", len(list.Categories), list.Categories)
	}
	if len(list.Groups) != 2 {
		t.Fatalf("子分类分组数 = %d, want 2", len(list.Groups))
	}
	if list.Total != 3 {
		t.Fatalf("商品总数 = %d, want 3", list.Total)
	}

	names := map[string]bool{}
	for _, cat := range list.Categories {
		names[cat.Name] = true
		if len(cat.Subcategories) == 0 {
			t.Fatalf("主分类 %s 没有子分类", cat.Name)
		}
		for _, sub := range cat.Subcategories {
			if sub.ProductCount <= 0 {
				t.Fatalf("子分类 %s 商品数 = %d", sub.Name, sub.ProductCount)
			}
		}
	}
	if !names["香港"] || !names["美国"] {
		t.Fatalf("主分类 = %v, want 香港 + 美国", names)
	}

	for _, group := range list.Groups {
		if group.Parent.ID == 0 || group.Parent.Name == "" {
			t.Fatalf("子分类 %s 缺少 parent: %+v", group.Name, group.Parent)
		}
	}

	// 详情的 group 也带 parent。
	detail := memberDetail(t, engine, list.Groups[0].Products[0].ID)
	if detail.Group.Parent.ID == 0 || detail.Group.Parent.Name == "" {
		t.Fatalf("商品详情 group 缺少 parent: %+v", detail.Group)
	}
}

// TestAdminCategoryManagement 分类管理：改名 / 子分类改挂主分类 / 商品改挂子分类。
func TestAdminCategoryManagement(t *testing.T) {
	gdb := testDatabase(t)
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, standardCatalog()))
	adminToken := adminTokenFor(t, engine, gdb, model.RoleAdmin)
	importProducts(t, engine, adminToken)

	list := adminCategories(t, engine, adminToken)
	if len(list.Items) != 2 {
		t.Fatalf("主分类数 = %d, want 2", len(list.Items))
	}
	hk := list.Items[0]
	if hk.Name != "香港" || len(hk.Subcategories) != 1 {
		t.Fatalf("首个主分类 = %+v", hk)
	}

	// 改名（主分类）。
	updated := putCategory(t, engine, adminToken, hk.ID, map[string]any{"name": "香港云"})
	if updated.(adminCategoryView).Name != "香港云" {
		t.Fatalf("改名结果 = %+v", updated)
	}

	// 子分类改挂到另一个主分类（美国）。
	us := list.Items[1]
	sub := hk.Subcategories[0]
	moved := putCategory(t, engine, adminToken, sub.ID, map[string]any{"parent_id": us.ID})
	movedView := moved.(adminSubcategoryView)
	if movedView.Name != sub.Name {
		t.Fatalf("改挂后名字应不变: %+v", movedView)
	}
	after := adminCategories(t, engine, adminToken)
	// 改挂后：原主分类（已改名「香港云」）下无子分类，目标主分类「美国」下有 2 个。
	if len(after.Items[0].Subcategories) != 0 || len(after.Items[1].Subcategories) != 2 {
		t.Fatalf("改挂后分类树不对: %+v", after.Items)
	}

	// 主分类不允许设置 parent_id。
	rec, envelope := doAPI(t, engine, http.MethodPut, "/api/v1/admin/categories/"+itoa(us.ID), adminToken,
		map[string]any{"parent_id": hk.ID})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("主分类设 parent_id 应 40002: HTTP %d body=%s", rec.Code, rec.Body.String())
	}

	// 商品改挂子分类（并标记 manual，导入不再覆盖）。
	pid := productIDByUpstreamPID(t, gdb, 101)
	otherSub := uint64(0)
	for _, candidate := range after.Items[1].Subcategories {
		if candidate.Name == "美国一区" {
			otherSub = candidate.ID
		}
	}
	if otherSub == 0 {
		t.Fatalf("找不到改挂目标子分类: %+v", after.Items[1].Subcategories)
	}
	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/admin/products/"+itoa(pid), adminToken,
		map[string]any{"category_id": otherSub})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("商品改挂失败: HTTP %d body=%s", rec.Code, rec.Body.String())
	}
	var product model.Product
	if err := gdb.Where("id = ?", pid).First(&product).Error; err != nil {
		t.Fatalf("查商品失败: %v", err)
	}
	if product.CategoryID != otherSub || product.CategorySource != model.CategorySourceManual {
		t.Fatalf("商品改挂未生效: %+v", product)
	}

	// manual 后重复导入不得覆盖。
	reimport := importProducts(t, engine, adminToken)
	if reimport.Unchanged != 3 {
		t.Fatalf("manual 商品导入未收敛: %+v", reimport)
	}
	if err := gdb.Where("id = ?", pid).First(&product).Error; err != nil {
		t.Fatalf("查商品失败: %v", err)
	}
	if product.CategoryID != otherSub {
		t.Fatalf("导入覆盖了手工归类: %d", product.CategoryID)
	}

	// 挂主分类 id 一律拒绝。
	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/admin/products/"+itoa(pid), adminToken,
		map[string]any{"category_id": us.ID})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("挂主分类应 40002: HTTP %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestAdminCategoryRoleMatrix 分类接口角色矩阵（契约 10.5 / 12.7）。
func TestAdminCategoryRoleMatrix(t *testing.T) {
	gdb := testDatabase(t)
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, standardCatalog()))
	adminToken := adminTokenFor(t, engine, gdb, model.RoleAdmin)
	importProducts(t, engine, adminToken)
	list := adminCategories(t, engine, adminToken)
	categoryID := list.Items[0].ID

	// 三角色逐一验证；admin 复用上面已建账号（seedAdmin 按用户名唯一）。
	roleTokens := map[string]string{model.RoleAdmin: adminToken}
	for _, role := range []string{model.RoleFinance, model.RoleSupport} {
		roleTokens[role] = adminTokenFor(t, engine, gdb, role)
	}
	for _, role := range []string{model.RoleAdmin, model.RoleFinance, model.RoleSupport} {
		token := roleTokens[role]
		// 分类树三角色只读。
		rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/categories", token, nil)
		if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
			t.Fatalf("%s 读分类失败: HTTP %d body=%s", role, rec.Code, rec.Body.String())
		}
		// 改分类：仅 admin。
		rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/admin/categories/"+itoa(categoryID), token,
			map[string]any{"name": "X"})
		if role == model.RoleAdmin {
			if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
				t.Fatalf("admin 改分类失败: HTTP %d body=%s", rec.Code, rec.Body.String())
			}
			continue
		}
		if rec.Code != http.StatusForbidden || envelope.Code != response.CodeForbidden {
			t.Fatalf("%s 改分类应 403: HTTP %d body=%s", role, rec.Code, rec.Body.String())
		}
	}

	// 会员 token 一律 401。
	memberToken, _ := memberTokenFor(t, engine, "catmember")
	rec, _ := doAPI(t, engine, http.MethodGet, "/api/v1/admin/categories", memberToken, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("会员读分类应 401: %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// 辅助
// ---------------------------------------------------------------------------

// adminCategories 调 GET /admin/categories。
func adminCategories(t *testing.T, engine http.Handler, token string) adminCategoryListView {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/categories", token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("查分类失败: HTTP %d body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[adminCategoryListView](t, envelope)
}

// putCategory 调 PUT /admin/categories/:id 并返回 data（主/子分类视图不同，用 any）。
func putCategory(t *testing.T, engine http.Handler, token string, id uint64, body map[string]any) any {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodPut, "/api/v1/admin/categories/"+itoa(id), token, body)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("改分类失败: HTTP %d body=%s", rec.Code, rec.Body.String())
	}
	raw := string(envelope.Data)
	if strings.Contains(raw, `"subcategories"`) {
		return decodeData[adminCategoryView](t, envelope)
	}
	return decodeData[adminSubcategoryView](t, envelope)
}

// keysOf 汇总分类稳定键（断言失败时给出可读清单）。
func keysOf(categories []model.ProductCategory) []string {
	out := make([]string, 0, len(categories))
	for _, item := range categories {
		out = append(out, item.Key)
	}
	return out
}

// mustShelveAll 把全部商品上架（会员端目录只出上架商品）。
func mustShelveAll(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	if err := gdb.Model(&model.Product{}).
		Where("1 = 1").
		Update("status", model.ProductStatusOn).Error; err != nil {
		t.Fatalf("上架商品失败: %v", err)
	}
}
