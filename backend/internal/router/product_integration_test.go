package router

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upstream"
)

// ---------------------------------------------------------------------------
// 假上游（httptest）：/cart/all + /cart/get_product_config
// ---------------------------------------------------------------------------

// fakePrices 是假上游商品的周期价格；空串按上游习惯输出 "-1.00"（该周期不售）。
type fakePrices struct {
	Monthly      string
	Quarterly    string
	SemiAnnually string
	Annually     string
}

func (p fakePrices) monthly() string   { return orUnsold(p.Monthly) }
func (p fakePrices) quarterly() string { return orUnsold(p.Quarterly) }
func (p fakePrices) semi() string      { return orUnsold(p.SemiAnnually) }
func (p fakePrices) annual() string    { return orUnsold(p.Annually) }

// orUnsold 空串按上游习惯补 "-1.00"。
func orUnsold(amount string) string {
	if amount == "" {
		return "-1.00"
	}
	return amount
}

// fakeProduct 是假上游里的商品。
type fakeProduct struct {
	ID           int
	Name         string
	Description  string
	Type         string
	Module       string
	Prices       fakePrices
	StockControl int
	Qty          int
	Ontrial      int
}

// fakeGroup 是假上游里的产品分组。
type fakeGroup struct {
	ID       int
	Name     string
	Products []fakeProduct
}

// fakeCatalog 是可变的假上游目录（导入会 4 并发抓详情，因此加锁）。
type fakeCatalog struct {
	mu     sync.Mutex
	groups []fakeGroup
}

func (c *fakeCatalog) setGroups(groups []fakeGroup) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.groups = groups
}

func (c *fakeCatalog) all() []fakeGroup {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.groups
}

func (c *fakeCatalog) find(pid int) (fakeProduct, bool) {
	for _, group := range c.all() {
		for _, product := range group.Products {
			if product.ID == pid {
				return product, true
			}
		}
	}
	return fakeProduct{}, false
}

// catalogBody 生成 GET /cart/all 的响应（上游包格式，见 docs/api-contract.md 8.2）。
func (c *fakeCatalog) catalogBody() string {
	groups := make([]string, 0)
	for _, group := range c.all() {
		items := make([]string, 0, len(group.Products))
		for _, product := range group.Products {
			items = append(items, fmt.Sprintf(
				`{"id":%d,"gid":%d,"type":%q,"name":%q,"description":%q,"module":%q,`+
					`"stock_control":0,"qty":0,"ontrial":0}`,
				product.ID, group.ID, product.Type, product.Name, product.Description, product.Module))
		}
		groups = append(groups, fmt.Sprintf(`{"id":%d,"name":%q,"products":[%s]}`,
			group.ID, group.Name, strings.Join(items, ",")))
	}
	return fmt.Sprintf(
		`{"status":200,"msg":"请求成功","data":{"products":[%s],"count":%d,"currency":"CNY"},"is_aff":"1"}`,
		strings.Join(groups, ","), len(groups))
}

// detailBody 生成 GET /cart/get_product_config 的响应。
func detailBody(product fakeProduct) string {
	pricing := fmt.Sprintf(
		`{"id":%d,"type":"product","relid":%d,"currency":1,"code":"CNY",`+
			`"monthly":%q,"quarterly":%q,"semiannually":%q,"annually":%q,"biennially":"-1.00","triennially":"-1.00"}`,
		product.ID, product.ID, product.Prices.monthly(), product.Prices.quarterly(),
		product.Prices.semi(), product.Prices.annual())

	// 配置项里刻意带一个 hidden=1 的选项与一个 hidden=1 的值，用于验证会员端过滤。
	configGroups := `[{"id":1,"name":"区域","description":"","options":[` +
		`{"id":1,"gid":1,"option_name":"area|区域","option_type":12,"upstream_id":0,"hidden":0,"sub":[` +
		`{"id":1,"config_id":1,"option_name":"1|HK^香港","upstream_id":0,"hidden":0,"pricings":[]},` +
		`{"id":2,"config_id":1,"option_name":"2|US^美国","upstream_id":0,"hidden":1,"pricings":[]}]},` +
		`{"id":2,"gid":1,"option_name":"hidden_option","option_type":1,"upstream_id":0,"hidden":1,"sub":[]}]}]`

	return fmt.Sprintf(
		`{"status":200,"msg":"请求成功","data":{"flag":1,`+
			`"products":{"id":%d,"gid":1,"type":%q,"name":%q,"description":%q,"module":%q,`+
			`"stock_control":%d,"qty":%d,"ontrial":%d},`+
			`"product_pricings":[%s],"config_groups":%s,"customfields":[]},"is_aff":"1"}`,
		product.ID, product.Type, product.Name, product.Description, product.Module,
		product.StockControl, product.Qty, product.Ontrial, pricing, configGroups)
}

// newFakeCatalogClient 启动假上游并返回指向它的客户端（登录固定成功）。
func newFakeCatalogClient(t *testing.T, catalog *fakeCatalog) *upstream.Client {
	t.Helper()
	return fakeUpstreamClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cart/all":
			_, _ = io.WriteString(w, catalog.catalogBody())
		case "/cart/get_product_config":
			pid, err := strconv.Atoi(r.URL.Query().Get("pid"))
			if err != nil {
				_, _ = io.WriteString(w, `{"status":400,"msg":"参数错误"}`)
				return
			}
			product, ok := catalog.find(pid)
			if !ok {
				_, _ = io.WriteString(w, `{"status":406,"msg":"商品不存在"}`)
				return
			}
			_, _ = io.WriteString(w, detailBody(product))
		default:
			_, _ = io.WriteString(w, `{"status":404,"msg":"接口不存在"}`)
		}
	})
}

// standardCatalog 是标准测试目录：2 个有商品的分组 + 1 个空分组，共 3 个商品。
func standardCatalog() *fakeCatalog {
	return &fakeCatalog{groups: []fakeGroup{
		{ID: 1, Name: "香港二区", Products: []fakeProduct{
			{
				ID: 101, Name: "香港二区 CN2 A型", Description: "<li>CPU:2核心</li>",
				Type: "dcimcloud", Module: "idcsmart_common", StockControl: 1, Qty: 70,
				Prices: fakePrices{Monthly: "20.00", Quarterly: "60.00", SemiAnnually: "120.00", Annually: "200.00"},
			},
			{
				ID: 102, Name: "香港二区 CN2 B型", Description: "<li>CPU:4核心</li>",
				Type: "dcimcloud", Module: "idcsmart_common", StockControl: 1, Qty: 10,
				Prices: fakePrices{Monthly: "40.00", Quarterly: "120.00", SemiAnnually: "240.00", Annually: "400.00"},
			},
		}},
		{ID: 2, Name: "美国一区", Products: []fakeProduct{
			{
				ID: 201, Name: "美国一区 特价", Description: "<li>CPU:1核心</li>",
				Type: "dcimcloud", Module: "idcsmart_common", StockControl: 0, Qty: 0, Ontrial: 3,
				// 季付与年付上游不售（-1.00），用于验证回退与 null。
				Prices: fakePrices{Monthly: "30.00", Quarterly: "", SemiAnnually: "150.00", Annually: ""},
			},
		}},
		{ID: 3, Name: "空分组"},
	}}
}

// ---------------------------------------------------------------------------
// 测试辅助
// ---------------------------------------------------------------------------

// newProductEngine 构造带假上游（或 nil）的 gin 引擎。
func newProductEngine(t *testing.T, gdb *gorm.DB, client *upstream.Client) *gin.Engine {
	t.Helper()
	return New(Options{
		Logger:          silentLogger(),
		DB:              gdb,
		JWT:             config.JWTConfig{Secret: testJWTSecret, ExpireHours: 168},
		Upstream:        client,
		UpstreamTimeout: 5 * time.Second,
	})
}

// adminTokenFor 建一个指定角色的管理员并登录，返回 token。
func adminTokenFor(t *testing.T, engine http.Handler, gdb *gorm.DB, role string) string {
	t.Helper()

	const password = "admin123456"
	username := "product-" + role
	seedAdmin(t, gdb, username, password, role, model.StatusActive)
	token, _ := loginAdmin(t, engine, username, password)
	return token
}

// importProducts 调用导入接口并返回计数结果。
func importProducts(t *testing.T, engine http.Handler, token string) productImportView {
	t.Helper()

	rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/admin/products/import", token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("导入商品失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[productImportView](t, envelope)
}

// productIDByUpstreamPID 直接查库把上游商品 ID 映射成本地商品 ID。
func productIDByUpstreamPID(t *testing.T, gdb *gorm.DB, upstreamPID int) uint64 {
	t.Helper()

	var product model.Product
	if err := gdb.Where("upstream_pid = ?", upstreamPID).Take(&product).Error; err != nil {
		t.Fatalf("查询商品（upstream_pid=%d）失败: %v", upstreamPID, err)
	}
	return product.ID
}

// groupIDByUpstreamID 调管理端分组列表把上游分组 ID 映射成本地分组 ID。
func groupIDByUpstreamID(t *testing.T, engine http.Handler, token string, upstreamGroupID int) uint64 {
	t.Helper()

	for _, item := range listAdminGroups(t, engine, token).Items {
		if item.UpstreamGroupID == upstreamGroupID {
			return item.ID
		}
	}
	t.Fatalf("未找到上游分组 %d", upstreamGroupID)
	return 0
}

// listAdminGroups 调用 GET /admin/product-groups。
func listAdminGroups(t *testing.T, engine http.Handler, token string) adminGroupListView {
	t.Helper()

	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/product-groups", token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("分组列表请求失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[adminGroupListView](t, envelope)
}

// updateProductOK 调用 PUT /admin/products/:id 并断言成功，返回详情视图。
func updateProductOK(t *testing.T, engine http.Handler, token string, id uint64, body map[string]any) adminProductDetailView {
	t.Helper()

	rec, envelope := doAPI(t, engine, http.MethodPut, "/api/v1/admin/products/"+itoa(id), token, body)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("更新商品 %d 失败: HTTP %d, body=%s", id, rec.Code, rec.Body.String())
	}
	return decodeData[adminProductDetailView](t, envelope)
}

// memberCatalog 调用 GET /products（无需鉴权）。
func memberCatalog(t *testing.T, engine http.Handler) (memberProductListView, string) {
	t.Helper()

	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/products", "", nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("会员端商品目录请求失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[memberProductListView](t, envelope), rec.Body.String()
}

// memberDetail 调用 GET /products/:id。
func memberDetail(t *testing.T, engine http.Handler, id uint64) memberProductDetailView {
	t.Helper()

	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/products/"+itoa(id), "", nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("会员端商品详情请求失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[memberProductDetailView](t, envelope)
}

// priceOf 取某周期价格，null 时返回空串。
func priceOf(prices priceView, cycle string) string {
	if amount := prices[cycle]; amount != nil {
		return *amount
	}
	return ""
}

// assertAllCyclesPresent 断言四个周期的键都存在（不可售的周期必须是 null，不能省略键）。
func assertAllCyclesPresent(t *testing.T, prices priceView, label string) {
	t.Helper()

	for _, cycle := range pricing.Cycles {
		if _, ok := prices[cycle]; !ok {
			t.Fatalf("%s 缺少周期键 %s（不可售必须输出 null）", label, cycle)
		}
	}
}

// ---------------------------------------------------------------------------
// 导入：幂等、写入边界、上游变化
// ---------------------------------------------------------------------------

func TestProductImportCreatesAllAndIsIdempotent(t *testing.T) {
	gdb := testDatabase(t)
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, standardCatalog()))
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)

	first := importProducts(t, engine, token)
	want := productImportView{Created: 3, Updated: 0, Unchanged: 0, Groups: 3}
	if first.Created != want.Created || first.Updated != want.Updated ||
		first.Unchanged != want.Unchanged || first.Groups != want.Groups || first.Failed != 0 {
		t.Fatalf("首次导入计数 = %+v，期望 %+v", first, want)
	}

	second := importProducts(t, engine, token)
	want = productImportView{Created: 0, Updated: 0, Unchanged: 3, Groups: 3}
	if second.Created != want.Created || second.Updated != want.Updated ||
		second.Unchanged != want.Unchanged || second.Groups != want.Groups || second.Failed != 0 {
		t.Fatalf("重复导入计数 = %+v，期望 %+v", second, want)
	}

	// 新导入的商品默认下架，须人工定价后上架。
	var count int64
	if err := gdb.Model(&model.Product{}).Where("status = ?", model.ProductStatusOff).Count(&count).Error; err != nil {
		t.Fatalf("统计下架商品失败: %v", err)
	}
	if count != 3 {
		t.Fatalf("默认下架商品数 = %d，期望 3", count)
	}
}

func TestProductImportPreservesLocalPricingStatusAndGroupName(t *testing.T) {
	gdb := testDatabase(t)
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, standardCatalog()))
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)

	importProducts(t, engine, token)
	productID := productIDByUpstreamPID(t, gdb, 101)
	groupID := groupIDByUpstreamID(t, engine, token, 1)

	detail := updateProductOK(t, engine, token, productID, map[string]any{
		"pricing_json": `{"mode":"markup","markup_percent":10}`,
		"status":       model.ProductStatusOn,
		"sort":         5,
	})
	if detail.Pricing.Mode != "markup" || detail.Status != model.ProductStatusOn || detail.Sort != 5 {
		t.Fatalf("本地配置未生效: %+v", detail.adminProductView)
	}
	if got := priceOf(detail.Prices, "monthly"); got != "22.00" {
		t.Fatalf("加价后月付 = %q，期望 22.00", got)
	}

	rec, envelope := doAPI(t, engine, http.MethodPut, "/api/v1/admin/product-groups/"+itoa(groupID), token,
		map[string]any{"name": "香港 CN2 专区", "sort": 9})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("重命名分组失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 再次导入：本地字段与分组名都必须保持不变。
	result := importProducts(t, engine, token)
	if result.Created != 0 || result.Updated != 0 || result.Unchanged != 3 {
		t.Fatalf("重复导入计数 = %+v，期望 created=0 updated=0 unchanged=3", result)
	}

	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/products/"+itoa(productID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("读取商品详情失败: HTTP %d", rec.Code)
	}
	after := decodeData[adminProductDetailView](t, envelope)
	if after.Pricing.Mode != "markup" || after.Status != model.ProductStatusOn || after.Sort != 5 {
		t.Fatalf("重复导入冲掉了本地配置: %+v", after.adminProductView)
	}
	if after.GroupName != "香港 CN2 专区" {
		t.Fatalf("重复导入冲掉了分组名: %q", after.GroupName)
	}
	renamed := findAdminGroup(t, listAdminGroups(t, engine, token).Items, groupID)
	if renamed.Name != "香港 CN2 专区" || renamed.Sort != 9 {
		t.Fatalf("分组本地字段未保持: %+v", renamed)
	}
}

// findAdminGroup 按本地分组 ID 在分组列表里查找。
func findAdminGroup(t *testing.T, items []adminGroupView, id uint64) adminGroupView {
	t.Helper()
	for _, item := range items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("分组列表里未找到 id=%d", id)
	return adminGroupView{}
}

func TestProductImportDetectsUpstreamChanges(t *testing.T) {
	gdb := testDatabase(t)
	catalog := standardCatalog()
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, catalog))
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)

	importProducts(t, engine, token)

	// 上游改名 + 库存清零。
	groups := catalog.all()
	groups[0].Products[0].Name = "香港二区 CN2 A型（新）"
	groups[0].Products[0].Qty = 0
	catalog.setGroups(groups)

	result := importProducts(t, engine, token)
	if result.Updated != 1 || result.Unchanged != 2 || result.Created != 0 {
		t.Fatalf("上游变化后计数 = %+v，期望 updated=1 unchanged=2 created=0", result)
	}

	productID := productIDByUpstreamPID(t, gdb, 101)
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/products/"+itoa(productID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("读取商品详情失败: HTTP %d", rec.Code)
	}
	detail := decodeData[adminProductDetailView](t, envelope)
	if detail.Name != "香港二区 CN2 A型（新）" || detail.StockQty != 0 {
		t.Fatalf("上游字段未更新: name=%q stock=%d", detail.Name, detail.StockQty)
	}
}

func TestProductImportWithoutUpstreamFails(t *testing.T) {
	gdb := testDatabase(t)
	engine := newProductEngine(t, gdb, nil)
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)

	rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/admin/products/import", token, nil)
	if rec.Code != http.StatusInternalServerError || envelope.Code != response.CodeInternalError {
		t.Fatalf("未配置上游时导入应返回 500/500，实际 HTTP %d, code=%d", rec.Code, envelope.Code)
	}
	if !strings.Contains(envelope.Message, "上游未配置") {
		t.Fatalf("错误文案未说明上游未配置: %q", envelope.Message)
	}
}

// ---------------------------------------------------------------------------
// 会员端：只读目录、定价三模式 + 回退、隐藏商品
// ---------------------------------------------------------------------------

func TestMemberCatalogOnlyShowsOnShelfWithLocalPricing(t *testing.T) {
	gdb := testDatabase(t)
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, standardCatalog()))
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)

	importProducts(t, engine, token)

	// 全部默认下架：会员端目录为空。
	list, _ := memberCatalog(t, engine)
	if len(list.Groups) != 0 || list.Total != 0 {
		t.Fatalf("未上架时会员端目录应为空，实际 %+v", list)
	}

	idA := productIDByUpstreamPID(t, gdb, 101)
	idB := productIDByUpstreamPID(t, gdb, 102)
	idC := productIDByUpstreamPID(t, gdb, 201)

	// A：直接用上游价
	updateProductOK(t, engine, token, idA, map[string]any{"status": model.ProductStatusOn})
	// B：上游价 +10%
	updateProductOK(t, engine, token, idB, map[string]any{
		"status":       model.ProductStatusOn,
		"pricing_json": `{"mode":"markup","markup_percent":10}`,
	})
	// C：固定价覆盖 + 未覆盖周期回退（上游季付/年付为 -1.00 不售）
	updateProductOK(t, engine, token, idC, map[string]any{
		"status":       model.ProductStatusOn,
		"pricing_json": `{"mode":"fixed","fixed":{"monthly":"25.00","annual":"200.00"}}`,
	})

	list, body := memberCatalog(t, engine)
	if list.Total != 3 || len(list.Groups) != 2 {
		t.Fatalf("会员端目录 = %d 组 / %d 商品，期望 2 组 / 3 商品", len(list.Groups), list.Total)
	}
	// 空分组不出现、组内商品按本地排序
	if list.Groups[0].Name != "香港二区" || list.Groups[1].Name != "美国一区" {
		t.Fatalf("分组顺序异常: %+v", list.Groups)
	}
	// 会员端不暴露上游 ID
	for _, forbidden := range []string{"upstream_pid", "upstream_group_id"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("会员端目录不应包含 %s：%s", forbidden, body)
		}
	}

	pricesByID := map[uint64]priceView{}
	for _, group := range list.Groups {
		for _, product := range group.Products {
			pricesByID[product.ID] = product.Prices
		}
	}

	assertPrices := func(id uint64, want map[string]string) {
		t.Helper()
		prices, ok := pricesByID[id]
		if !ok {
			t.Fatalf("会员端目录缺少商品 %d", id)
		}
		for cycle, amount := range want {
			if got := priceOf(prices, cycle); got != amount {
				t.Fatalf("商品 %d 的 %s 价格 = %q，期望 %q", id, cycle, got, amount)
			}
		}
	}
	assertPrices(idA, map[string]string{
		"monthly": "20.00", "quarterly": "60.00", "semiannual": "120.00", "annual": "200.00",
	})
	assertPrices(idB, map[string]string{
		"monthly": "44.00", "quarterly": "132.00", "semiannual": "264.00", "annual": "440.00",
	})
	assertPrices(idC, map[string]string{
		"monthly": "25.00", "quarterly": "", "semiannual": "150.00", "annual": "200.00",
	})

	// 四个周期的键必须始终存在（不可售输出 null 而不是省略键）。
	for id, prices := range pricesByID {
		assertAllCyclesPresent(t, prices, fmt.Sprintf("会员端商品 %d", id))
	}
	// 上游不售的季付在原始响应里是 null。
	if !strings.Contains(body, `"quarterly":null`) {
		t.Fatalf("不可售周期应以 null 输出：%s", body)
	}
}

func TestMemberProductDetailAndOffShelfNotFound(t *testing.T) {
	gdb := testDatabase(t)
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, standardCatalog()))
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)

	importProducts(t, engine, token)
	idA := productIDByUpstreamPID(t, gdb, 101)
	idB := productIDByUpstreamPID(t, gdb, 102)
	updateProductOK(t, engine, token, idA, map[string]any{"status": model.ProductStatusOn})

	detail := memberDetail(t, engine, idA)
	if detail.Name != "香港二区 CN2 A型" || detail.Group.Name != "香港二区" {
		t.Fatalf("详情基础字段异常: %+v", detail)
	}
	if detail.StockQty != 70 || detail.StockControl != 1 || detail.OntrialMax != 0 {
		t.Fatalf("库存/试用信息异常: qty=%d control=%d ontrial=%d",
			detail.StockQty, detail.StockControl, detail.OntrialMax)
	}
	if len(detail.ConfigGroups) != 1 || len(detail.ConfigGroups[0].Options) != 1 {
		t.Fatalf("配置项应过滤 hidden，实际 %+v", detail.ConfigGroups)
	}
	if values := detail.ConfigGroups[0].Options[0].Values; len(values) != 1 || values[0].Name != "1|HK^香港" {
		t.Fatalf("可配置值应过滤 hidden，实际 %+v", values)
	}

	// 下架商品对会员端不可见（404）。
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/products/"+itoa(idB), "", nil)
	if rec.Code != http.StatusNotFound || envelope.Code != response.CodeNotFound {
		t.Fatalf("下架商品应返回 404，实际 HTTP %d, code=%d", rec.Code, envelope.Code)
	}

	// 不存在的商品同样 404；非法 ID 是参数错误。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/products/999999", "", nil)
	if rec.Code != http.StatusNotFound || envelope.Code != response.CodeNotFound {
		t.Fatalf("不存在商品应返回 404，实际 HTTP %d, code=%d", rec.Code, envelope.Code)
	}
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/products/abc", "", nil)
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("非法商品 ID 应返回 400/40001，实际 HTTP %d, code=%d", rec.Code, envelope.Code)
	}
}

// ---------------------------------------------------------------------------
// 权限
// ---------------------------------------------------------------------------

func TestProductRoleGuards(t *testing.T) {
	gdb := testDatabase(t)
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, standardCatalog()))
	adminUserToken := adminTokenFor(t, engine, gdb, model.RoleAdmin)
	supportToken := adminTokenFor(t, engine, gdb, model.RoleSupport)

	importProducts(t, engine, adminUserToken)
	productID := productIDByUpstreamPID(t, gdb, 101)
	groupID := groupIDByUpstreamID(t, engine, adminUserToken, 1)

	// support 可读
	for _, path := range []string{"/api/v1/admin/products", "/api/v1/admin/product-groups",
		"/api/v1/admin/products/" + itoa(productID)} {
		rec, envelope := doAPI(t, engine, http.MethodGet, path, supportToken, nil)
		if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
			t.Fatalf("support 读取 %s 应成功，实际 HTTP %d, code=%d", path, rec.Code, envelope.Code)
		}
	}

	// support 不可写：导入 / 改定价 / 上下架 / 改分组全部 403
	forbidden := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodPost, "/api/v1/admin/products/import", nil},
		{http.MethodPut, "/api/v1/admin/products/" + itoa(productID), map[string]any{"status": model.ProductStatusOn}},
		{http.MethodPut, "/api/v1/admin/product-groups/" + itoa(groupID), map[string]any{"name": "改名"}},
	}
	for _, item := range forbidden {
		rec, envelope := doAPI(t, engine, item.method, item.path, supportToken, item.body)
		if rec.Code != http.StatusForbidden || envelope.Code != response.CodeForbidden {
			t.Fatalf("support 调用 %s %s 应返回 403，实际 HTTP %d, code=%d",
				item.method, item.path, rec.Code, envelope.Code)
		}
	}

	// 未携带 token：401
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/products", "", nil)
	if rec.Code != http.StatusUnauthorized || envelope.Code != response.CodeUnauthorized {
		t.Fatalf("未鉴权访问管理端商品应为 401，实际 HTTP %d, code=%d", rec.Code, envelope.Code)
	}

	// finance 可写
	financeToken := adminTokenFor(t, engine, gdb, model.RoleFinance)
	updateProductOK(t, engine, financeToken, productID, map[string]any{"status": model.ProductStatusOn})
}

// ---------------------------------------------------------------------------
// 管理端列表：分页与过滤
// ---------------------------------------------------------------------------

func TestAdminProductListPaginationAndFilters(t *testing.T) {
	gdb := testDatabase(t)
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, standardCatalog()))
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)

	importProducts(t, engine, token)
	idA := productIDByUpstreamPID(t, gdb, 101)
	idC := productIDByUpstreamPID(t, gdb, 201)
	updateProductOK(t, engine, token, idA, map[string]any{"status": model.ProductStatusOn})
	updateProductOK(t, engine, token, idC, map[string]any{"status": model.ProductStatusOn})
	groupID := groupIDByUpstreamID(t, engine, token, 1)

	listProducts := func(query string) adminProductListView {
		t.Helper()
		rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/products"+query, token, nil)
		if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
			t.Fatalf("商品列表请求失败（%s）: HTTP %d, body=%s", query, rec.Code, rec.Body.String())
		}
		return decodeData[adminProductListView](t, envelope)
	}

	if list := listProducts(""); list.Total != 3 || len(list.Items) != 3 {
		t.Fatalf("全量列表 = %d 条 / total=%d，期望 3/3", len(list.Items), list.Total)
	}
	if list := listProducts("?page=1&page_size=2"); len(list.Items) != 2 || list.Total != 3 {
		t.Fatalf("第一页 = %d 条 / total=%d，期望 2/3", len(list.Items), list.Total)
	}
	if list := listProducts("?page=2&page_size=2"); len(list.Items) != 1 || list.Total != 3 {
		t.Fatalf("第二页 = %d 条 / total=%d，期望 1/3", len(list.Items), list.Total)
	}
	if list := listProducts("?group_id=" + itoa(groupID)); list.Total != 2 {
		t.Fatalf("按分组过滤 total=%d，期望 2", list.Total)
	}
	if list := listProducts("?status=on"); list.Total != 2 {
		t.Fatalf("按 status=on 过滤 total=%d，期望 2", list.Total)
	}
	if list := listProducts("?status=off"); list.Total != 1 {
		t.Fatalf("按 status=off 过滤 total=%d，期望 1", list.Total)
	}
	if list := listProducts("?keyword=CN2"); list.Total != 2 {
		t.Fatalf("按关键词过滤 total=%d，期望 2", list.Total)
	}
	if list := listProducts("?keyword=%E7%89%B9%E4%BB%B7"); list.Total != 1 { // 关键词「特价」
		t.Fatalf("按中文关键词过滤 total=%d，期望 1", list.Total)
	}

	// 非法与不存在的过滤参数
	invalid := []struct {
		query string
		code  int
		http  int
	}{
		{"?status=weird", response.CodeInvalidParam, http.StatusBadRequest},
		{"?group_id=abc", response.CodeInvalidParam, http.StatusBadRequest},
		{"?group_id=0", response.CodeInvalidParam, http.StatusBadRequest},
		{"?group_id=999999", response.CodeNotFound, http.StatusNotFound},
		{"?page_size=1000", response.CodeInvalidParam, http.StatusBadRequest},
		{"?page=0", response.CodeInvalidParam, http.StatusBadRequest},
	}
	for _, item := range invalid {
		rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/products"+item.query, token, nil)
		if rec.Code != item.http || envelope.Code != item.code {
			t.Fatalf("%s 应返回 HTTP %d/code %d，实际 HTTP %d/code %d",
				item.query, item.http, item.code, rec.Code, envelope.Code)
		}
	}
}

// ---------------------------------------------------------------------------
// 管理端更新：校验、404、409
// ---------------------------------------------------------------------------

func TestAdminProductUpdateValidation(t *testing.T) {
	gdb := testDatabase(t)
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, standardCatalog()))
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)

	importProducts(t, engine, token)
	productID := productIDByUpstreamPID(t, gdb, 101)

	cases := []struct {
		name string
		id   string
		body map[string]any
		http int
		code int
	}{
		{"定价规则格式非法（未知字段）", itoa(productID),
			map[string]any{"pricing_json": `{"mode":"markup","markup_percentt":10}`}, http.StatusBadRequest, response.CodeInvalidParam},
		{"定价规则格式非法（非法 JSON）", itoa(productID),
			map[string]any{"pricing_json": `{"mode":`}, http.StatusBadRequest, response.CodeInvalidParam},
		{"定价规则不成立（缺加价率）", itoa(productID),
			map[string]any{"pricing_json": `{"mode":"markup"}`}, http.StatusBadRequest, response.CodeValidationFailed},
		{"定价规则不成立（固定价为空）", itoa(productID),
			map[string]any{"pricing_json": `{"mode":"fixed","fixed":{}}`}, http.StatusBadRequest, response.CodeValidationFailed},
		{"金额格式非法", itoa(productID),
			map[string]any{"pricing_json": `{"mode":"fixed","fixed":{"monthly":"25 元"}}`}, http.StatusBadRequest, response.CodeInvalidParam},
		{"状态取值非法", itoa(productID),
			map[string]any{"status": "enabled"}, http.StatusBadRequest, response.CodeInvalidParam},
		{"排序越界", itoa(productID),
			map[string]any{"sort": 100000000}, http.StatusBadRequest, response.CodeInvalidParam},
		{"未提供任何字段", itoa(productID), map[string]any{}, http.StatusBadRequest, response.CodeInvalidParam},
		{"商品 ID 非法", "abc", map[string]any{"status": model.ProductStatusOn}, http.StatusBadRequest, response.CodeInvalidParam},
		{"商品不存在", "999999", map[string]any{"status": model.ProductStatusOn}, http.StatusNotFound, response.CodeNotFound},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rec, envelope := doAPI(t, engine, http.MethodPut, "/api/v1/admin/products/"+testCase.id, token, testCase.body)
			if rec.Code != testCase.http || envelope.Code != testCase.code {
				t.Fatalf("HTTP %d/code %d，期望 HTTP %d/code %d（message=%s）",
					rec.Code, envelope.Code, testCase.http, testCase.code, envelope.Message)
			}
		})
	}
}

// TestAdminProductUpdatePricingJSONForms 验证 pricing_json 的三种写法：对象、字符串、null（重置）。
func TestAdminProductUpdatePricingJSONForms(t *testing.T) {
	gdb := testDatabase(t)
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, standardCatalog()))
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)

	importProducts(t, engine, token)
	productID := productIDByUpstreamPID(t, gdb, 101)

	// 对象写法
	detail := updateProductOK(t, engine, token, productID, map[string]any{
		"pricing_json": map[string]any{"mode": "fixed", "fixed": map[string]string{"monthly": "9.90"}},
	})
	if detail.Pricing.Mode != "fixed" || detail.Pricing.Fixed["monthly"] != "9.90" {
		t.Fatalf("对象写法未生效: %+v", detail.Pricing)
	}
	if got := priceOf(detail.Prices, "monthly"); got != "9.90" {
		t.Fatalf("固定价月付 = %q，期望 9.90", got)
	}

	// 字符串写法
	detail = updateProductOK(t, engine, token, productID, map[string]any{
		"pricing_json": `{"mode":"markup","markup_percent":50}`,
	})
	if detail.Pricing.Mode != "markup" {
		t.Fatalf("字符串写法未生效: %+v", detail.Pricing)
	}
	if got := priceOf(detail.Prices, "monthly"); got != "30.00" {
		t.Fatalf("加价 50%% 后月付 = %q，期望 30.00", got)
	}

	// null 重置为缺省规则（直接用上游价）
	detail = updateProductOK(t, engine, token, productID, map[string]any{"pricing_json": nil})
	if detail.Pricing.Mode != "upstream" {
		t.Fatalf("null 重置未生效: %+v", detail.Pricing)
	}
	if got := priceOf(detail.Prices, "monthly"); got != "20.00" {
		t.Fatalf("重置后月付 = %q，期望 20.00", got)
	}
}

func TestAdminProductOnShelfRequiresUsablePrice(t *testing.T) {
	gdb := testDatabase(t)
	catalog := &fakeCatalog{groups: []fakeGroup{
		{ID: 1, Name: "无价分组", Products: []fakeProduct{
			// 四个周期上游都不售（-1.00），且没有本地固定价。
			{ID: 901, Name: "无价商品", Type: "dcimcloud", Module: "idcsmart_common"},
		}},
	}}
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, catalog))
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)

	importProducts(t, engine, token)
	productID := productIDByUpstreamPID(t, gdb, 901)

	rec, envelope := doAPI(t, engine, http.MethodPut, "/api/v1/admin/products/"+itoa(productID), token,
		map[string]any{"status": model.ProductStatusOn})
	if rec.Code != http.StatusConflict || envelope.Code != response.CodeConflict {
		t.Fatalf("无可用周期价格上架应返回 409，实际 HTTP %d, code=%d（message=%s）",
			rec.Code, envelope.Code, envelope.Message)
	}

	// 配上固定价后即可上架。
	updateProductOK(t, engine, token, productID, map[string]any{
		"pricing_json": `{"mode":"fixed","fixed":{"monthly":"9.90"}}`,
		"status":       model.ProductStatusOn,
	})

	// 上架后把定价改回「上游价」会让价格全空，同样拒绝（409）。
	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/admin/products/"+itoa(productID), token,
		map[string]any{"pricing_json": `{"mode":"upstream"}`})
	if rec.Code != http.StatusConflict || envelope.Code != response.CodeConflict {
		t.Fatalf("改回无价规则应返回 409，实际 HTTP %d, code=%d", rec.Code, envelope.Code)
	}
}

// ---------------------------------------------------------------------------
// 分组
// ---------------------------------------------------------------------------

func TestAdminProductGroupUpdate(t *testing.T) {
	gdb := testDatabase(t)
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, standardCatalog()))
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)

	importProducts(t, engine, token)
	groups := listAdminGroups(t, engine, token)
	if len(groups.Items) != 3 {
		t.Fatalf("分组数 = %d，期望 3", len(groups.Items))
	}
	if counts := groups.Items[0].Products; counts.Total != 2 || counts.Off != 2 || counts.On != 0 {
		t.Fatalf("分组商品计数异常: %+v", counts)
	}

	groupID := groupIDByUpstreamID(t, engine, token, 1)
	rec, envelope := doAPI(t, engine, http.MethodPut, "/api/v1/admin/product-groups/"+itoa(groupID), token,
		map[string]any{"name": "  香港 CN2 专区  ", "sort": 7})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("更新分组失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	updated := decodeData[adminGroupView](t, envelope)
	if updated.Name != "香港 CN2 专区" || updated.Sort != 7 {
		t.Fatalf("分组更新结果异常: %+v", updated)
	}

	invalid := []struct {
		name string
		id   string
		body map[string]any
		http int
		code int
	}{
		{"空名称", itoa(groupID), map[string]any{"name": "   "}, http.StatusBadRequest, response.CodeInvalidParam},
		{"无字段", itoa(groupID), map[string]any{}, http.StatusBadRequest, response.CodeInvalidParam},
		{"排序越界", itoa(groupID), map[string]any{"sort": -100000000}, http.StatusBadRequest, response.CodeInvalidParam},
		{"ID 非法", "abc", map[string]any{"name": "x"}, http.StatusBadRequest, response.CodeInvalidParam},
		{"分组不存在", "999999", map[string]any{"name": "x"}, http.StatusNotFound, response.CodeNotFound},
	}
	for _, item := range invalid {
		t.Run(item.name, func(t *testing.T) {
			rec, envelope := doAPI(t, engine, http.MethodPut, "/api/v1/admin/product-groups/"+item.id, token, item.body)
			if rec.Code != item.http || envelope.Code != item.code {
				t.Fatalf("HTTP %d/code %d，期望 HTTP %d/code %d", rec.Code, envelope.Code, item.http, item.code)
			}
		})
	}
}

// TestAdminProductDetailExposesUpstreamPrices 验证管理端详情包含上游价格原文与配置项。
func TestAdminProductDetailExposesUpstreamPrices(t *testing.T) {
	gdb := testDatabase(t)
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, standardCatalog()))
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)

	importProducts(t, engine, token)
	productID := productIDByUpstreamPID(t, gdb, 101)

	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/products/"+itoa(productID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("详情请求失败: HTTP %d", rec.Code)
	}
	detail := decodeData[adminProductDetailView](t, envelope)

	if detail.UpstreamPrices.Code != "CNY" {
		t.Fatalf("上游价格货币 = %q，期望 CNY", detail.UpstreamPrices.Code)
	}
	if detail.UpstreamPrices.Prices["annual"] != "200.00" {
		t.Fatalf("上游年付价 = %q，期望 200.00", detail.UpstreamPrices.Prices["annual"])
	}
	if len(detail.UpstreamPrices.Rows) != 1 {
		t.Fatalf("上游价格原文行数 = %d，期望 1", len(detail.UpstreamPrices.Rows))
	}
	if !json.Valid(detail.UpstreamPrices.Rows[0]) {
		t.Fatalf("上游价格原文不是合法 JSON: %s", detail.UpstreamPrices.Rows[0])
	}
	if detail.UpstreamPID != 101 || detail.UpstreamGroupID != 1 {
		t.Fatalf("管理端应暴露上游 ID: %+v", detail.adminProductView)
	}
	if len(detail.ConfigGroups) != 1 || len(detail.ConfigGroups[0].Options) != 2 {
		// 管理端不做 hidden 过滤，应看到 2 个选项。
		t.Fatalf("管理端配置项应包含全部选项，实际 %+v", detail.ConfigGroups)
	}
}
