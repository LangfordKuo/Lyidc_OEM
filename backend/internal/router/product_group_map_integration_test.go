package router

import (
	"net/http"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
)

// ---------------------------------------------------------------------------
// R12 集成测试：导入按上游分组筛选 + 「上游分组 → 本地子分类」映射
//
// standardCatalog 结构：分组 1「香港二区」2 个商品（101/102）、
// 分组 2「美国一区」1 个商品（201）、分组 3「空分组」0 个商品。
// ---------------------------------------------------------------------------

// importSelected 调导入接口（可带可选请求体）并断言成功。
func importSelected(t *testing.T, engine http.Handler, token string, body any) productImportView {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/admin/products/import", token, body)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("导入失败: HTTP %d body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[productImportView](t, envelope)
}

// listGroupMaps 调 GET /admin/product-group-maps。
func listGroupMaps(t *testing.T, engine http.Handler, token string) groupMapListView {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/product-group-maps", token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("查映射失败: HTTP %d body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[groupMapListView](t, envelope)
}

// productRow 直接查库取商品行（断言归属用）。
func productRow(t *testing.T, gdb *gorm.DB, upstreamPID int) model.Product {
	t.Helper()
	var product model.Product
	if err := gdb.Where("upstream_pid = ?", upstreamPID).First(&product).Error; err != nil {
		t.Fatalf("查询商品（upstream_pid=%d）失败: %v", upstreamPID, err)
	}
	return product
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// 一、导入按上游分组筛选
// ---------------------------------------------------------------------------

func TestProductImportFiltersByUpstreamGroup(t *testing.T) {
	t.Run("只选一个分组", func(t *testing.T) {
		gdb := testDatabase(t)
		engine := newProductEngine(t, gdb, newFakeCatalogClient(t, standardCatalog()))
		token := adminTokenFor(t, engine, gdb, model.RoleAdmin)

		view := importSelected(t, engine, token, map[string]any{"upstream_group_ids": []int{1}})
		if view.Created != 2 || view.Updated != 0 || view.Unchanged != 0 || view.Groups != 1 || view.Failed != 0 {
			t.Fatalf("只选分组 1 的计数 = %+v，期望 created=2 groups=1", view)
		}
		if !equalInts(view.SelectedGroups, []int{1}) {
			t.Fatalf("selected_groups = %v，期望 [1]", view.SelectedGroups)
		}
		if view.MapFallbackGroupIDs != nil {
			t.Fatalf("无失效映射时不应出现 map_fallback_group_ids: %v", view.MapFallbackGroupIDs)
		}

		// 只导入了选中分组的商品与分组本身。
		var productCount, groupCount int64
		gdb.Model(&model.Product{}).Count(&productCount)
		gdb.Model(&model.ProductGroup{}).Count(&groupCount)
		if productCount != 2 || groupCount != 1 {
			t.Fatalf("落库商品/分组数 = %d/%d，期望 2/1", productCount, groupCount)
		}

		// 同一子集重复导入幂等。
		second := importSelected(t, engine, token, map[string]any{"upstream_group_ids": []int{1}})
		if second.Created != 0 || second.Updated != 0 || second.Unchanged != 2 || second.Groups != 1 {
			t.Fatalf("重复导入（同子集）计数 = %+v，期望 unchanged=2 groups=1", second)
		}
	})

	t.Run("选两个分组（含空分组）", func(t *testing.T) {
		gdb := testDatabase(t)
		engine := newProductEngine(t, gdb, newFakeCatalogClient(t, standardCatalog()))
		token := adminTokenFor(t, engine, gdb, model.RoleAdmin)

		view := importSelected(t, engine, token, map[string]any{"upstream_group_ids": []int{3, 1}})
		if view.Created != 2 || view.Groups != 2 {
			t.Fatalf("选 {1,3} 的计数 = %+v，期望 created=2 groups=2", view)
		}
		// 选中的分组 ID 升序去重回带。
		if !equalInts(view.SelectedGroups, []int{1, 3}) {
			t.Fatalf("selected_groups = %v，期望 [1 3]（升序）", view.SelectedGroups)
		}
		var groupCount int64
		gdb.Model(&model.ProductGroup{}).Count(&groupCount)
		if groupCount != 2 {
			t.Fatalf("落库分组数 = %d，期望 2（含空分组）", groupCount)
		}
	})

	t.Run("缺省即全量：省略请求体/空对象/空数组/null", func(t *testing.T) {
		bodies := map[string]any{
			"省略请求体":   nil,
			"空对象":     map[string]any{},
			"空数组（顶层）": []any{},
			"字段空数组":   map[string]any{"upstream_group_ids": []int{}},
			"字段 null": map[string]any{"upstream_group_ids": nil},
		}
		for name, body := range bodies {
			t.Run(name, func(t *testing.T) {
				gdb := testDatabase(t)
				engine := newProductEngine(t, gdb, newFakeCatalogClient(t, standardCatalog()))
				token := adminTokenFor(t, engine, gdb, model.RoleAdmin)

				view := importSelected(t, engine, token, body)
				if view.Created != 3 || view.Groups != 3 {
					t.Fatalf("%s 应按全量导入（created=3 groups=3），实际 %+v", name, view)
				}
				if view.SelectedGroups != nil {
					t.Fatalf("%s 为全量导入，不应回带 selected_groups: %v", name, view.SelectedGroups)
				}
			})
		}
	})
}

func TestProductImportSelectionValidation(t *testing.T) {
	gdb := testDatabase(t)
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, standardCatalog()))
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)

	cases := []struct {
		name string
		body any
		code int
	}{
		{"非数组（数字）", map[string]any{"upstream_group_ids": 1}, response.CodeInvalidParam},
		{"顶层数组", []any{1, 2}, response.CodeInvalidParam},
		{"含 0", map[string]any{"upstream_group_ids": []int{0}}, response.CodeInvalidParam},
		{"含负数", map[string]any{"upstream_group_ids": []int{-1, 2}}, response.CodeInvalidParam},
		{"分组不存在（上游目录里没有）", map[string]any{"upstream_group_ids": []int{999}}, response.CodeValidationFailed},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/admin/products/import", token, testCase.body)
			if rec.Code != http.StatusBadRequest || envelope.Code != testCase.code {
				t.Fatalf("HTTP %d/code %d，期望 HTTP 400/code %d（message=%s）",
					rec.Code, envelope.Code, testCase.code, envelope.Message)
			}
		})
	}

	// 非法请求不得产生任何写入（校验早于落库）。
	var productCount, groupCount int64
	gdb.Model(&model.Product{}).Count(&productCount)
	gdb.Model(&model.ProductGroup{}).Count(&groupCount)
	if productCount != 0 || groupCount != 0 {
		t.Fatalf("非法请求产生了写入：商品 %d 个 / 分组 %d 个", productCount, groupCount)
	}
}

// ---------------------------------------------------------------------------
// 二、映射 CRUD 与角色矩阵
// ---------------------------------------------------------------------------

func TestProductGroupMapCRUDAndRoles(t *testing.T) {
	gdb := testDatabase(t)
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, standardCatalog()))
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)
	importProducts(t, engine, token)

	categories := adminCategories(t, engine, token)
	hkMain := categories.Items[0]
	hkSub := hkMain.Subcategories[0]
	usMain := categories.Items[1]
	usSub := usMain.Subcategories[0]

	if items := listGroupMaps(t, engine, token).Items; len(items) != 0 {
		t.Fatalf("初始应无映射，实际 %+v", items)
	}

	// 新增：分组 1 → 子分类「美国一区」。
	rec, envelope := doAPI(t, engine, http.MethodPut, "/api/v1/admin/product-group-maps/1", token,
		map[string]any{"category_id": usSub.ID})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("新增映射失败: HTTP %d body=%s", rec.Code, rec.Body.String())
	}
	created := decodeData[groupMapView](t, envelope)
	if created.UpstreamGroupID != 1 || created.CategoryID != usSub.ID ||
		created.UpstreamGroupName != "香港二区" || created.CategoryName != "美国一区" ||
		created.CategoryParentName != "美国" {
		t.Fatalf("新增映射视图异常: %+v", created)
	}

	items := listGroupMaps(t, engine, token).Items
	if len(items) != 1 || items[0].CategoryID != usSub.ID {
		t.Fatalf("映射列表异常: %+v", items)
	}

	// 更新（同一分组 upsert）：分组 1 → 子分类「香港二区」。
	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/admin/product-group-maps/1", token,
		map[string]any{"category_id": hkSub.ID})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("更新映射失败: HTTP %d body=%s", rec.Code, rec.Body.String())
	}
	items = listGroupMaps(t, engine, token).Items
	if len(items) != 1 || items[0].CategoryID != hkSub.ID || items[0].CategoryName != "香港二区" {
		t.Fatalf("upsert 未就地更新: %+v", items)
	}

	// 非法：主分类 / 不存在的分类 / 缺 category_id / 分组未导入 / 路径非法。
	invalid := []struct {
		name string
		path string
		body any
		code int
	}{
		{"主分类不可作为目标", "/api/v1/admin/product-group-maps/1", map[string]any{"category_id": hkMain.ID}, response.CodeValidationFailed},
		{"分类不存在", "/api/v1/admin/product-group-maps/1", map[string]any{"category_id": 999999}, response.CodeValidationFailed},
		{"缺 category_id", "/api/v1/admin/product-group-maps/1", map[string]any{}, response.CodeInvalidParam},
		{"category_id 为 0", "/api/v1/admin/product-group-maps/1", map[string]any{"category_id": 0}, response.CodeInvalidParam},
		{"分组未导入本地", "/api/v1/admin/product-group-maps/77", map[string]any{"category_id": hkSub.ID}, response.CodeValidationFailed},
		{"路径 ID 非法", "/api/v1/admin/product-group-maps/abc", map[string]any{"category_id": hkSub.ID}, response.CodeInvalidParam},
		{"路径 ID 为 0", "/api/v1/admin/product-group-maps/0", map[string]any{"category_id": hkSub.ID}, response.CodeInvalidParam},
	}
	for _, item := range invalid {
		t.Run(item.name, func(t *testing.T) {
			rec, envelope := doAPI(t, engine, http.MethodPut, item.path, token, item.body)
			if rec.Code != http.StatusBadRequest || envelope.Code != item.code {
				t.Fatalf("HTTP %d/code %d，期望 400/code %d（message=%s）",
					rec.Code, envelope.Code, item.code, envelope.Message)
			}
		})
	}

	// 删除：存在 → 200；再次删除 → 404。
	rec, envelope = doAPI(t, engine, http.MethodDelete, "/api/v1/admin/product-group-maps/1", token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("删除映射失败: HTTP %d body=%s", rec.Code, rec.Body.String())
	}
	if items := listGroupMaps(t, engine, token).Items; len(items) != 0 {
		t.Fatalf("删除后应为空，实际 %+v", items)
	}
	rec, envelope = doAPI(t, engine, http.MethodDelete, "/api/v1/admin/product-group-maps/1", token, nil)
	if rec.Code != http.StatusNotFound || envelope.Code != response.CodeNotFound {
		t.Fatalf("重复删除应 404，实际 HTTP %d code=%d", rec.Code, envelope.Code)
	}
}

func TestProductGroupMapRoleMatrix(t *testing.T) {
	gdb := testDatabase(t)
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, standardCatalog()))
	adminToken := adminTokenFor(t, engine, gdb, model.RoleAdmin)
	importProducts(t, engine, adminToken)
	subID := adminCategories(t, engine, adminToken).Items[0].Subcategories[0].ID
	// 先建一条映射，供删除用例使用。
	if rec, _ := doAPI(t, engine, http.MethodPut, "/api/v1/admin/product-group-maps/1",
		adminToken, map[string]any{"category_id": subID}); rec.Code != http.StatusOK {
		t.Fatalf("预置映射失败: HTTP %d body=%s", rec.Code, rec.Body.String())
	}

	roleTokens := map[string]string{model.RoleAdmin: adminToken}
	for _, role := range []string{model.RoleFinance, model.RoleSupport} {
		roleTokens[role] = adminTokenFor(t, engine, gdb, role)
	}

	for _, role := range []string{model.RoleAdmin, model.RoleFinance, model.RoleSupport} {
		token := roleTokens[role]
		// 查看类：三角色均可读。
		rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/product-group-maps", token, nil)
		if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
			t.Fatalf("%s 读映射失败: HTTP %d body=%s", role, rec.Code, rec.Body.String())
		}
		// 写操作：仅 admin。
		rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/admin/product-group-maps/2", token,
			map[string]any{"category_id": subID})
		if role == model.RoleAdmin {
			if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
				t.Fatalf("admin 写映射失败: HTTP %d body=%s", rec.Code, rec.Body.String())
			}
			continue
		}
		if rec.Code != http.StatusForbidden || envelope.Code != response.CodeForbidden {
			t.Fatalf("%s 写映射应 403，实际 HTTP %d code=%d", role, rec.Code, envelope.Code)
		}
		rec, envelope = doAPI(t, engine, http.MethodDelete, "/api/v1/admin/product-group-maps/1", token, nil)
		if rec.Code != http.StatusForbidden || envelope.Code != response.CodeForbidden {
			t.Fatalf("%s 删映射应 403，实际 HTTP %d code=%d", role, rec.Code, envelope.Code)
		}
	}

	// 会员 token 一律 401。
	memberToken, _ := memberTokenFor(t, engine, "mapmember")
	rec, _ := doAPI(t, engine, http.MethodGet, "/api/v1/admin/product-group-maps", memberToken, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("会员读映射应 401，实际 %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// 三、映射生效：导入归类优先级（manual > 映射 > 自动派生）
// ---------------------------------------------------------------------------

func TestProductImportAppliesGroupMap(t *testing.T) {
	gdb := testDatabase(t)
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, standardCatalog()))
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)
	importProducts(t, engine, token)

	categories := adminCategories(t, engine, token)
	hkSub := categories.Items[0].Subcategories[0] // 香港二区（分组 1 的派生归属）
	usSub := categories.Items[1].Subcategories[0] // 美国一区

	if got := productRow(t, gdb, 101); got.CategoryID != hkSub.ID {
		t.Fatalf("导入后商品 101 应派生到香港二区，实际 %d", got.CategoryID)
	}

	assertProductsCategory := func(want uint64, label string) {
		t.Helper()
		for _, pid := range []int{101, 102} {
			row := productRow(t, gdb, pid)
			if row.CategoryID != want {
				t.Fatalf("%s：商品 %d category_id = %d，期望 %d", label, pid, row.CategoryID, want)
			}
			if row.CategorySource != model.CategorySourceDerived {
				t.Fatalf("%s：商品 %d category_source = %q，期望 derived（映射是导入规则的一部分）",
					label, pid, row.CategorySource)
			}
		}
	}

	putMap := func(categoryID uint64) {
		t.Helper()
		rec, envelope := doAPI(t, engine, http.MethodPut, "/api/v1/admin/product-group-maps/1", token,
			map[string]any{"category_id": categoryID})
		if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
			t.Fatalf("写映射失败: HTTP %d body=%s", rec.Code, rec.Body.String())
		}
	}

	// 映射 分组 1 → 美国一区：重新导入后 101/102 归到该子分类（category_source 仍 derived）。
	putMap(usSub.ID)
	view := importSelected(t, engine, token, nil)
	if view.Updated != 2 || view.Unchanged != 1 {
		t.Fatalf("映射生效后导入计数 = %+v，期望 updated=2 unchanged=1（只有分组 1 的两个商品归属变化）", view)
	}
	assertProductsCategory(usSub.ID, "映射到美国一区")

	// 改映射 → 香港二区：重导刷新。
	putMap(hkSub.ID)
	view = importSelected(t, engine, token, nil)
	if view.Updated != 2 || view.Unchanged != 1 {
		t.Fatalf("改映射后导入计数 = %+v，期望 updated=2 unchanged=1", view)
	}
	assertProductsCategory(hkSub.ID, "改映射回香港二区")

	// 再映射到美国一区后删除映射：重导回落自动派生（香港二区）。
	putMap(usSub.ID)
	importSelected(t, engine, token, nil)
	assertProductsCategory(usSub.ID, "重新映射")
	if rec, _ := doAPI(t, engine, http.MethodDelete, "/api/v1/admin/product-group-maps/1", token, nil); rec.Code != http.StatusOK {
		t.Fatalf("删映射失败: HTTP %d body=%s", rec.Code, rec.Body.String())
	}
	view = importSelected(t, engine, token, nil)
	if view.Updated != 2 || view.Unchanged != 1 {
		t.Fatalf("删映射后导入计数 = %+v，期望 updated=2 unchanged=1", view)
	}
	assertProductsCategory(hkSub.ID, "删映射后回落自动派生")
}

func TestGroupMapDoesNotOverrideManualCategory(t *testing.T) {
	gdb := testDatabase(t)
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, standardCatalog()))
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)
	importProducts(t, engine, token)

	categories := adminCategories(t, engine, token)
	hkSub := categories.Items[0].Subcategories[0]
	usSub := categories.Items[1].Subcategories[0]

	// 管理员手工把 101 改挂到美国一区（category_source=manual）。
	pid := productIDByUpstreamPID(t, gdb, 101)
	updateProductOK(t, engine, token, pid, map[string]any{"category_id": usSub.ID})

	// 再把分组 1 映射到香港二区：重导后 102 被映射改写，101 必须保持 manual 不动。
	if rec, _ := doAPI(t, engine, http.MethodPut, "/api/v1/admin/product-group-maps/1", token,
		map[string]any{"category_id": hkSub.ID}); rec.Code != http.StatusOK {
		t.Fatalf("写映射失败: HTTP %d", rec.Code)
	}
	importProducts(t, engine, token)

	manualRow := productRow(t, gdb, 101)
	if manualRow.CategoryID != usSub.ID || manualRow.CategorySource != model.CategorySourceManual {
		t.Fatalf("manual 商品被映射覆盖：%+v", manualRow)
	}
	derivedRow := productRow(t, gdb, 102)
	if derivedRow.CategoryID != hkSub.ID || derivedRow.CategorySource != model.CategorySourceDerived {
		t.Fatalf("映射未作用于非 manual 商品：%+v", derivedRow)
	}
}

// ---------------------------------------------------------------------------
// 四、映射目标失效的降级
// ---------------------------------------------------------------------------

func TestGroupMapTargetInvalidFallsBackToDerive(t *testing.T) {
	gdb := testDatabase(t)
	engine := newProductEngine(t, gdb, newFakeCatalogClient(t, standardCatalog()))
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)
	importProducts(t, engine, token)

	categories := adminCategories(t, engine, token)
	hkMain := categories.Items[0]
	hkSub := hkMain.Subcategories[0]
	usSub := categories.Items[1].Subcategories[0]

	// 预置一个临时子分类，映射分组 1 到它，然后直接从库中删除该分类（模拟外部删除）。
	now := time.Now().UTC()
	parentID := hkMain.ID
	extra := model.ProductCategory{
		Key: "z:临时专用区", ParentID: &parentID, Name: "临时专用区", Sort: 99, CreatedAt: now, UpdatedAt: now,
	}
	if err := gdb.Create(&extra).Error; err != nil {
		t.Fatalf("建临时分类失败: %v", err)
	}
	if rec, _ := doAPI(t, engine, http.MethodPut, "/api/v1/admin/product-group-maps/1", token,
		map[string]any{"category_id": extra.ID}); rec.Code != http.StatusOK {
		t.Fatalf("写映射失败: HTTP %d", rec.Code)
	}
	if err := gdb.Delete(&model.ProductCategory{}, extra.ID).Error; err != nil {
		t.Fatalf("删临时分类失败: %v", err)
	}
	// 直接插入一条指向**主分类**的映射（绕过 PUT 校验，模拟外部改坏）——分组 2。
	if err := gdb.Create(&model.ProductGroupCategoryMap{
		UpstreamGroupID: 2, CategoryID: hkMain.ID, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("写坏映射失败: %v", err)
	}

	// 重新导入：不 500，两个失效映射都回落自动派生并在响应中体现。
	view := importSelected(t, engine, token, nil)
	if !equalInts(view.MapFallbackGroupIDs, []int{1, 2}) {
		t.Fatalf("map_fallback_group_ids = %v，期望 [1 2]", view.MapFallbackGroupIDs)
	}
	if got := productRow(t, gdb, 101); got.CategoryID != hkSub.ID {
		t.Fatalf("分组 1 回落自动派生失败：商品 101 category_id = %d，期望 %d", got.CategoryID, hkSub.ID)
	}
	if got := productRow(t, gdb, 201); got.CategoryID != usSub.ID {
		t.Fatalf("分组 2 回落自动派生失败：商品 201 category_id = %d，期望 %d", got.CategoryID, usSub.ID)
	}
}
