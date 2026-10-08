package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
)

// ---------------------------------------------------------------------------
// 测试辅助（真 MySQL + httptest，与商品集成测试同一模式）
// ---------------------------------------------------------------------------

// newCouponEngine 构造不带上游的 gin 引擎（优惠码接口不依赖上游）。
func newCouponEngine(t *testing.T, gdb *gorm.DB) *gin.Engine {
	t.Helper()
	return New(Options{
		Logger: silentLogger(),
		DB:     gdb,
		JWT:    config.JWTConfig{Secret: testJWTSecret, ExpireHours: 168},
	})
}

// seedCouponProduct 写一个商品：月付 30.00、年付 200.00，markup 10%（→ 33.00 / 220.00）；
// 两年付与三年付上游不售（-1.00 → 本地 null）。status 传 on / off。
func seedCouponProduct(t *testing.T, gdb *gorm.DB, upstreamPID int, status string) uint64 {
	t.Helper()

	now := time.Now().UTC()
	product := model.Product{
		UpstreamPID:        upstreamPID,
		UpstreamGroupID:    900,
		Name:               "优惠码测试商品",
		Type:               "dcimcloud",
		Module:             "idcsmart_common",
		UpstreamPricesJSON: `{"code":"CNY","prices":{"monthly":"30.00","annual":"200.00","biennial":"-1.00","triennial":"-1.00"}}`,
		PricingJSON:        `{"mode":"markup","markup_percent":10}`,
		Status:             status,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := gdb.Create(&product).Error; err != nil {
		t.Fatalf("写入测试商品失败: %v", err)
	}
	return product.ID
}

// seedCoupon 直接写库创建优惠码（默认启用、不限周期、不限次数），mutate 用于改状态。
func seedCoupon(t *testing.T, gdb *gorm.DB, code, kind, value, cyclesJSON string, mutate func(*model.Coupon)) *model.Coupon {
	t.Helper()

	now := time.Now().UTC()
	coupon := model.Coupon{
		Code:       code,
		Type:       kind,
		Value:      model.Money(value),
		CyclesJSON: cyclesJSON,
		Status:     model.CouponStatusOn,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if mutate != nil {
		mutate(&coupon)
	}
	if err := gdb.Create(&coupon).Error; err != nil {
		t.Fatalf("写入测试优惠码失败: %v", err)
	}
	return &coupon
}

// createCoupon 调用创建接口并断言成功，返回视图。
func createCoupon(t *testing.T, engine http.Handler, token string, body map[string]any) couponView {
	t.Helper()

	rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/admin/coupons", token, body)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("创建优惠码失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[couponView](t, envelope)
}

// updateCouponRequest 调用更新接口，返回记录器与响应包。
func updateCouponRequest(t *testing.T, engine http.Handler, token string, id uint64, body any) (*httptest.ResponseRecorder, apiEnvelope) {
	t.Helper()
	return doAPI(t, engine, http.MethodPut, "/api/v1/admin/coupons/"+itoa(id), token, body)
}

// validateRequest 调用公开校验接口。
func validateRequest(t *testing.T, engine http.Handler, code, query string) (*httptest.ResponseRecorder, apiEnvelope) {
	t.Helper()
	return doAPI(t, engine, http.MethodGet, "/api/v1/coupons/"+code+"/validate"+query, "", nil)
}

// validateOK 调用公开校验接口并断言 HTTP 200。
func validateOK(t *testing.T, engine http.Handler, code, query string) couponValidateView {
	t.Helper()

	rec, envelope := validateRequest(t, engine, code, query)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("优惠码校验失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[couponValidateView](t, envelope)
}

// ---------------------------------------------------------------------------
// 管理端：创建 / 读取 / 列表
// ---------------------------------------------------------------------------

func TestCouponCreateAndRead(t *testing.T) {
	gdb := testDatabase(t)
	engine := newCouponEngine(t, gdb)
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)

	startsAt := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	expiresAt := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	created := createCoupon(t, engine, token, map[string]any{
		"code":       "Welcome10",
		"type":       "percent",
		"value":      "10",
		"cycles":     []string{"annual", "monthly", "annual"}, // 去重 + 规范化
		"starts_at":  startsAt,
		"expires_at": expiresAt,
		"max_uses":   100,
		"comment":    "新人优惠",
	})

	if created.Code != "Welcome10" || created.Type != pricing.CouponTypePercent || created.Value != "10.00" {
		t.Fatalf("创建结果异常: %+v", created)
	}
	if len(created.Cycles) != 2 || created.Cycles[0] != "monthly" || created.Cycles[1] != "annual" {
		t.Fatalf("cycles 规范化异常: %v", created.Cycles)
	}
	if created.MaxUses != 100 || created.UsedCount != 0 || created.Status != model.CouponStatusOn {
		t.Fatalf("创建结果异常: %+v", created)
	}
	if created.StartsAt == nil || created.ExpiresAt == nil {
		t.Fatalf("时间应回带，实际 starts=%v expires=%v", created.StartsAt, created.ExpiresAt)
	}
	if created.CreatedAt == "" || created.UpdatedAt == "" {
		t.Fatalf("时间字段缺失: %+v", created)
	}

	// 详情：按 ID 读回。
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/coupons/"+itoa(created.ID), token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("详情请求失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	detail := decodeData[couponView](t, envelope)
	if detail.ID != created.ID || detail.Code != "Welcome10" || detail.Comment != "新人优惠" {
		t.Fatalf("详情与创建不一致: %+v", detail)
	}

	// 列表：默认分页，新建在前。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/coupons", token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("列表请求失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	list := decodeData[couponListView](t, envelope)
	if list.Total != 1 || len(list.Items) != 1 || list.Items[0].Code != "Welcome10" {
		t.Fatalf("列表结果异常: %+v", list)
	}
}

func TestCouponCreateDefaults(t *testing.T) {
	gdb := testDatabase(t)
	engine := newCouponEngine(t, gdb)
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)

	// 最小请求体：只给 code/type/value。
	created := createCoupon(t, engine, token, map[string]any{
		"code": "MINIMAL", "type": "fixed", "value": "20",
	})
	if created.Status != model.CouponStatusOn || created.MaxUses != 0 || created.UsedCount != 0 {
		t.Fatalf("缺省值异常: %+v", created)
	}
	if len(created.Cycles) != 0 {
		t.Fatalf("缺省 cycles 应为空数组（全部周期），实际 %v", created.Cycles)
	}
	if created.StartsAt != nil || created.ExpiresAt != nil {
		t.Fatalf("缺省时间应为 null，实际 %v / %v", created.StartsAt, created.ExpiresAt)
	}

	// 响应 JSON 里 cycles 必须是 []，不是 null。
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/coupons/"+itoa(created.ID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("详情请求失败: HTTP %d", rec.Code)
	}
	if body := envelope.Data; !strings.Contains(string(body), `"cycles":[]`) {
		t.Fatalf("cycles 应输出空数组，实际 data=%s", body)
	}
}

func TestCouponCreateValidationMatrix(t *testing.T) {
	gdb := testDatabase(t)
	engine := newCouponEngine(t, gdb)
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)

	base := map[string]any{"code": "GOODCODE", "type": "percent", "value": "10"}

	cases := []struct {
		name    string
		mutate  func(map[string]any)
		status  int
		code    int
		message string
	}{
		{"code 太短", func(b map[string]any) { b["code"] = "ab" }, http.StatusBadRequest, response.CodeInvalidParam, "优惠码需为"},
		{"code 含非法字符", func(b map[string]any) { b["code"] = "bad code" }, http.StatusBadRequest, response.CodeInvalidParam, "优惠码需为"},
		{"type 非法", func(b map[string]any) { b["type"] = "half" }, http.StatusBadRequest, response.CodeInvalidParam, "type 只能是"},
		{"value 格式非法", func(b map[string]any) { b["value"] = "10%" }, http.StatusBadRequest, response.CodeInvalidParam, "金额格式不正确"},
		{"percent value 为 0", func(b map[string]any) { b["value"] = "0" }, http.StatusBadRequest, response.CodeValidationFailed, "percent 的 value"},
		{"percent value 超 100", func(b map[string]any) { b["value"] = "100.01" }, http.StatusBadRequest, response.CodeValidationFailed, "percent 的 value"},
		{"fixed value 为 0", func(b map[string]any) {
			b["type"], b["value"] = "fixed", "0"
		}, http.StatusBadRequest, response.CodeValidationFailed, "fixed 的 value"},
		{"cycles 含未知周期", func(b map[string]any) { b["cycles"] = []string{"weekly"} }, http.StatusBadRequest, response.CodeInvalidParam, "不支持的周期"},
		{"cycles 含上游字段名", func(b map[string]any) { b["cycles"] = []string{"annually"} }, http.StatusBadRequest, response.CodeInvalidParam, "不支持的周期"},
		{"时间格式非法", func(b map[string]any) { b["starts_at"] = "2026-10-08 12:00:00" }, http.StatusBadRequest, response.CodeInvalidParam, "RFC3339"},
		{"starts 晚于 expires", func(b map[string]any) {
			b["starts_at"] = "2026-10-09T00:00:00Z"
			b["expires_at"] = "2026-10-08T00:00:00Z"
		}, http.StatusBadRequest, response.CodeValidationFailed, "starts_at 必须早于 expires_at"},
		{"starts 等于 expires", func(b map[string]any) {
			b["starts_at"] = "2026-10-08T00:00:00Z"
			b["expires_at"] = "2026-10-08T00:00:00Z"
		}, http.StatusBadRequest, response.CodeValidationFailed, "starts_at 必须早于 expires_at"},
		{"max_uses 为负", func(b map[string]any) { b["max_uses"] = -1 }, http.StatusBadRequest, response.CodeValidationFailed, "max_uses"},
		{"status 非法", func(b map[string]any) { b["status"] = "paused" }, http.StatusBadRequest, response.CodeInvalidParam, "status 只能是 on 或 off"},
		{"comment 超长", func(b map[string]any) { b["comment"] = strings.Repeat("备", 256) }, http.StatusBadRequest, response.CodeInvalidParam, "comment"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			body := map[string]any{}
			for key, value := range base {
				body[key] = value
			}
			testCase.mutate(body)

			rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/admin/coupons", token, body)
			if rec.Code != testCase.status || envelope.Code != testCase.code {
				t.Fatalf("HTTP %d/code %d，期望 %d/%d（body=%s）",
					rec.Code, envelope.Code, testCase.status, testCase.code, rec.Body.String())
			}
			if !strings.Contains(envelope.Message, testCase.message) {
				t.Fatalf("message = %q，期望包含 %q", envelope.Message, testCase.message)
			}
		})
	}

	// 全部用例失败后不应留下任何记录。
	var total int64
	if err := gdb.Model(&model.Coupon{}).Count(&total).Error; err != nil {
		t.Fatalf("统计优惠码失败: %v", err)
	}
	if total != 0 {
		t.Fatalf("校验失败的请求不应写库，实际 %d 条", total)
	}
}

func TestCouponCreateConflictIsCaseInsensitive(t *testing.T) {
	gdb := testDatabase(t)
	engine := newCouponEngine(t, gdb)
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)

	createCoupon(t, engine, token, map[string]any{"code": "Welcome10", "type": "percent", "value": "10"})

	for _, code := range []string{"Welcome10", "welcome10", "WELCOME10"} {
		rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/admin/coupons", token,
			map[string]any{"code": code, "type": "fixed", "value": "5"})
		if rec.Code != http.StatusConflict || envelope.Code != response.CodeConflict {
			t.Fatalf("code=%s 期望 409，实际 HTTP %d/code %d（body=%s）",
				code, rec.Code, envelope.Code, rec.Body.String())
		}
	}
}

func TestCouponListFilters(t *testing.T) {
	gdb := testDatabase(t)
	engine := newCouponEngine(t, gdb)
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)

	seedCoupon(t, gdb, "ALPHA10", pricing.CouponTypePercent, "10", `[]`, nil)
	seedCoupon(t, gdb, "ALPHA20", pricing.CouponTypeFixed, "20", `[]`, nil)
	seedCoupon(t, gdb, "BETA5", pricing.CouponTypeFixed, "5", `[]`, func(c *model.Coupon) {
		c.Status = model.CouponStatusOff
	})

	// status 过滤。
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/coupons?status=off", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("列表请求失败: HTTP %d", rec.Code)
	}
	list := decodeData[couponListView](t, envelope)
	if list.Total != 1 || list.Items[0].Code != "BETA5" {
		t.Fatalf("status 过滤异常: %+v", list)
	}

	// keyword 过滤（不区分大小写，走列的 ci 排序规则）。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/coupons?keyword=alpha", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("列表请求失败: HTTP %d", rec.Code)
	}
	list = decodeData[couponListView](t, envelope)
	if list.Total != 2 {
		t.Fatalf("keyword 过滤异常: %+v", list)
	}

	// 分页：page_size=1 时每页 1 条，新建在前（BETA5 最后创建）。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/coupons?page=1&page_size=1", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("列表请求失败: HTTP %d", rec.Code)
	}
	list = decodeData[couponListView](t, envelope)
	if list.Total != 3 || len(list.Items) != 1 || list.Items[0].Code != "BETA5" {
		t.Fatalf("分页异常: %+v", list)
	}

	// 非法参数。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/coupons?status=paused", token, nil)
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("非法 status 期望 40001，实际 HTTP %d/code %d", rec.Code, envelope.Code)
	}
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/coupons?page=0", token, nil)
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("非法分页期望 40001，实际 HTTP %d/code %d", rec.Code, envelope.Code)
	}
}

func TestCouponGetNotFound(t *testing.T) {
	gdb := testDatabase(t)
	engine := newCouponEngine(t, gdb)
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)

	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/coupons/999", token, nil)
	if rec.Code != http.StatusNotFound || envelope.Code != response.CodeNotFound {
		t.Fatalf("期望 404，实际 HTTP %d/code %d", rec.Code, envelope.Code)
	}
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/coupons/abc", token, nil)
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("非正整数字段期望 40001，实际 HTTP %d/code %d", rec.Code, envelope.Code)
	}
}

// ---------------------------------------------------------------------------
// 管理端：更新
// ---------------------------------------------------------------------------

func TestCouponUpdateFields(t *testing.T) {
	gdb := testDatabase(t)
	engine := newCouponEngine(t, gdb)
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)

	seedCoupon(t, gdb, "UPDATEME", pricing.CouponTypePercent, "10", `["annual"]`, func(c *model.Coupon) {
		startsAt := time.Now().UTC().Add(time.Hour)
		c.StartsAt = &startsAt
		c.MaxUses = 5
		c.Comment = "旧备注"
	})

	// 改 value / cycles / 时间（清空）/ max_uses / status / comment / code。
	rec, envelope := updateCouponRequest(t, engine, token, 1, map[string]any{
		"code":      "RENAMED",
		"value":     "25.5",
		"cycles":    nil,
		"starts_at": nil,
		"max_uses":  0,
		"status":    "off",
		"comment":   "新备注",
	})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("更新失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	updated := decodeData[couponView](t, envelope)
	if updated.Code != "RENAMED" || updated.Value != "25.50" || updated.MaxUses != 0 ||
		updated.Status != model.CouponStatusOff || updated.Comment != "新备注" {
		t.Fatalf("更新结果异常: %+v", updated)
	}
	if len(updated.Cycles) != 0 {
		t.Fatalf("cycles 传 null 应清空为全部周期，实际 %v", updated.Cycles)
	}
	if updated.StartsAt != nil {
		t.Fatalf("starts_at 传 null 应清空，实际 %v", *updated.StartsAt)
	}
	if updated.Type != pricing.CouponTypePercent {
		t.Fatalf("type 不应被修改，实际 %q", updated.Type)
	}

	// 只改时间：未提供的字段保持原值。
	rec, envelope = updateCouponRequest(t, engine, token, 1, map[string]any{
		"expires_at": "2027-01-01T00:00:00Z",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("更新失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	updated = decodeData[couponView](t, envelope)
	if updated.ExpiresAt == nil || *updated.ExpiresAt != "2027-01-01T00:00:00Z" {
		t.Fatalf("expires_at 更新异常: %+v", updated)
	}
	if updated.Code != "RENAMED" || updated.Comment != "新备注" {
		t.Fatalf("未提供的字段不应变化: %+v", updated)
	}
}

func TestCouponUpdateValidationAndConflicts(t *testing.T) {
	gdb := testDatabase(t)
	engine := newCouponEngine(t, gdb)
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)

	seedCoupon(t, gdb, "FIRST", pricing.CouponTypePercent, "10", `[]`, nil)
	seedCoupon(t, gdb, "SECOND", pricing.CouponTypeFixed, "20", `[]`, nil)

	cases := []struct {
		name    string
		id      uint64
		body    any
		status  int
		code    int
		message string
	}{
		{"空请求体", 1, map[string]any{}, http.StatusBadRequest, response.CodeInvalidParam, "至少提供一个字段"},
		{"code 冲突（大小写不敏感）", 1, map[string]any{"code": "second"}, http.StatusConflict, response.CodeConflict, "优惠码已存在"},
		{"code 非法", 1, map[string]any{"code": "x"}, http.StatusBadRequest, response.CodeInvalidParam, "优惠码需为"},
		{"value 格式非法", 1, map[string]any{"value": "abc"}, http.StatusBadRequest, response.CodeInvalidParam, "金额格式不正确"},
		{"percent value 超范围", 1, map[string]any{"value": "101"}, http.StatusBadRequest, response.CodeValidationFailed, "percent 的 value"},
		{"fixed value 为 0", 2, map[string]any{"value": "0"}, http.StatusBadRequest, response.CodeValidationFailed, "fixed 的 value"},
		{"cycles 非法", 1, map[string]any{"cycles": []string{"weekly"}}, http.StatusBadRequest, response.CodeInvalidParam, "不支持的周期"},
		{"starts 晚于 expires", 1, map[string]any{
			"starts_at": "2026-12-01T00:00:00Z", "expires_at": "2026-11-01T00:00:00Z",
		}, http.StatusBadRequest, response.CodeValidationFailed, "starts_at 必须早于 expires_at"},
		{"max_uses 为负", 1, map[string]any{"max_uses": -3}, http.StatusBadRequest, response.CodeValidationFailed, "max_uses"},
		{"status 非法", 1, map[string]any{"status": "paused"}, http.StatusBadRequest, response.CodeInvalidParam, "status 只能是 on 或 off"},
		{"不存在的优惠码", 999, map[string]any{"value": "5"}, http.StatusNotFound, response.CodeNotFound, "优惠码不存在"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rec, envelope := updateCouponRequest(t, engine, token, testCase.id, testCase.body)
			if rec.Code != testCase.status || envelope.Code != testCase.code {
				t.Fatalf("HTTP %d/code %d，期望 %d/%d（body=%s）",
					rec.Code, envelope.Code, testCase.status, testCase.code, rec.Body.String())
			}
			if !strings.Contains(envelope.Message, testCase.message) {
				t.Fatalf("message = %q，期望包含 %q", envelope.Message, testCase.message)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 角色权限
// ---------------------------------------------------------------------------

func TestCouponRoleGuard(t *testing.T) {
	gdb := testDatabase(t)
	engine := newCouponEngine(t, gdb)
	support := adminTokenFor(t, engine, gdb, model.RoleSupport)
	finance := adminTokenFor(t, engine, gdb, model.RoleFinance)
	seedCoupon(t, gdb, "GUARDED", pricing.CouponTypePercent, "10", `[]`, nil)

	// support：读可以，写 403。
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/coupons", support, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("support 查看列表应成功，实际 HTTP %d/code %d", rec.Code, envelope.Code)
	}
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/coupons/1", support, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("support 查看详情应成功，实际 HTTP %d", rec.Code)
	}
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/admin/coupons", support,
		map[string]any{"code": "NOPE", "type": "fixed", "value": "1"})
	if rec.Code != http.StatusForbidden || envelope.Code != response.CodeForbidden {
		t.Fatalf("support 创建应 403，实际 HTTP %d/code %d", rec.Code, envelope.Code)
	}
	rec, envelope = updateCouponRequest(t, engine, support, 1, map[string]any{"status": "off"})
	if rec.Code != http.StatusForbidden || envelope.Code != response.CodeForbidden {
		t.Fatalf("support 更新应 403，实际 HTTP %d/code %d", rec.Code, envelope.Code)
	}

	// finance：可写。
	createCoupon(t, engine, finance, map[string]any{"code": "FINANCEOK", "type": "fixed", "value": "1"})
}

// ---------------------------------------------------------------------------
// 公开校验接口
// ---------------------------------------------------------------------------

func TestCouponValidateHappyPaths(t *testing.T) {
	gdb := testDatabase(t)
	engine := newCouponEngine(t, gdb)
	productID := seedCouponProduct(t, gdb, 9001, model.ProductStatusOn)

	seedCoupon(t, gdb, "WELCOME10", pricing.CouponTypePercent, "10", `["annual"]`, nil)
	seedCoupon(t, gdb, "CASH20", pricing.CouponTypeFixed, "20", `[]`, nil)

	// percent：年付 220.00 × 10% = 22.00 → 198.00；code 大小写不敏感，回带库内原样。
	result := validateOK(t, engine, "welcome10", fmt.Sprintf("?product_id=%d&cycle=annual", productID))
	if !result.Valid || result.Code != "WELCOME10" || result.Type != pricing.CouponTypePercent || result.Value != "10.00" {
		t.Fatalf("percent 校验结果异常: %+v", result)
	}
	if result.Price != "220.00" || result.DiscountAmount != "22.00" || result.FinalAmount != "198.00" {
		t.Fatalf("percent 金额异常: %+v", result)
	}
	if result.Reason != "" {
		t.Fatalf("有效时不应带 reason: %+v", result)
	}

	// fixed：年付 220.00 − 20.00 = 200.00。
	result = validateOK(t, engine, "CASH20", fmt.Sprintf("?product_id=%d&cycle=annual", productID))
	if !result.Valid || result.Price != "220.00" || result.DiscountAmount != "20.00" || result.FinalAmount != "200.00" {
		t.Fatalf("fixed 年付金额异常: %+v", result)
	}

	// fixed：月付 33.00 − 20.00 = 13.00。
	result = validateOK(t, engine, "CASH20", fmt.Sprintf("?product_id=%d&cycle=monthly", productID))
	if !result.Valid || result.Price != "33.00" || result.DiscountAmount != "20.00" || result.FinalAmount != "13.00" {
		t.Fatalf("fixed 月付金额异常: %+v", result)
	}
}

func TestCouponValidateParameterErrors(t *testing.T) {
	gdb := testDatabase(t)
	engine := newCouponEngine(t, gdb)
	productID := seedCouponProduct(t, gdb, 9001, model.ProductStatusOn)
	seedCoupon(t, gdb, "PARAMS", pricing.CouponTypePercent, "10", `[]`, nil)

	cases := []struct {
		name    string
		code    string
		query   string
		status  int
		bizCode int
	}{
		{"缺 product_id", "PARAMS", "?cycle=annual", http.StatusBadRequest, response.CodeInvalidParam},
		{"product_id 非数字", "PARAMS", "?product_id=abc&cycle=annual", http.StatusBadRequest, response.CodeInvalidParam},
		{"product_id 为 0", "PARAMS", "?product_id=0&cycle=annual", http.StatusBadRequest, response.CodeInvalidParam},
		{"缺 cycle", "PARAMS", fmt.Sprintf("?product_id=%d", productID), http.StatusBadRequest, response.CodeInvalidParam},
		{"cycle 非法", "PARAMS", fmt.Sprintf("?product_id=%d&cycle=weekly", productID), http.StatusBadRequest, response.CodeInvalidParam},
		{"cycle 用上游字段名", "PARAMS", fmt.Sprintf("?product_id=%d&cycle=annually", productID), http.StatusBadRequest, response.CodeInvalidParam},
		{"code 不存在", "NOPE", fmt.Sprintf("?product_id=%d&cycle=annual", productID), http.StatusNotFound, response.CodeNotFound},
		{"商品不存在", "PARAMS", "?product_id=99999&cycle=annual", http.StatusNotFound, response.CodeNotFound},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rec, envelope := validateRequest(t, engine, testCase.code, testCase.query)
			if rec.Code != testCase.status || envelope.Code != testCase.bizCode {
				t.Fatalf("HTTP %d/code %d，期望 %d/%d（body=%s）",
					rec.Code, envelope.Code, testCase.status, testCase.bizCode, rec.Body.String())
			}
		})
	}

	// 下架商品与不存在商品同样 404。
	offID := seedCouponProduct(t, gdb, 9002, model.ProductStatusOff)
	rec, envelope := validateRequest(t, engine, "PARAMS", fmt.Sprintf("?product_id=%d&cycle=annual", offID))
	if rec.Code != http.StatusNotFound || envelope.Code != response.CodeNotFound {
		t.Fatalf("下架商品期望 404，实际 HTTP %d/code %d", rec.Code, envelope.Code)
	}
}

func TestCouponValidateReasonMatrix(t *testing.T) {
	gdb := testDatabase(t)
	engine := newCouponEngine(t, gdb)
	productID := seedCouponProduct(t, gdb, 9001, model.ProductStatusOn)
	now := time.Now().UTC()

	seedCoupon(t, gdb, "DISABLED", pricing.CouponTypePercent, "10", `[]`, func(c *model.Coupon) {
		c.Status = model.CouponStatusOff
	})
	seedCoupon(t, gdb, "NOTSTARTED", pricing.CouponTypePercent, "10", `[]`, func(c *model.Coupon) {
		start := now.Add(time.Hour)
		c.StartsAt = &start
	})
	seedCoupon(t, gdb, "EXPIRED", pricing.CouponTypePercent, "10", `[]`, func(c *model.Coupon) {
		end := now.Add(-time.Hour)
		c.ExpiresAt = &end
	})
	seedCoupon(t, gdb, "USEDUP", pricing.CouponTypePercent, "10", `[]`, func(c *model.Coupon) {
		c.MaxUses, c.UsedCount = 3, 3
	})
	seedCoupon(t, gdb, "ANNUALONLY", pricing.CouponTypePercent, "10", `["annual"]`, nil)
	seedCoupon(t, gdb, "OKPERCENT", pricing.CouponTypePercent, "10", `[]`, nil)

	cases := []struct {
		name   string
		code   string
		cycle  string
		reason string
	}{
		{"停用", "DISABLED", "annual", ReasonCouponDisabled},
		{"未到生效时间", "NOTSTARTED", "annual", ReasonCouponNotStarted},
		{"已过期", "EXPIRED", "annual", ReasonCouponExpired},
		{"次数用尽", "USEDUP", "annual", ReasonCouponUsedUp},
		{"周期不适用（码限定 annual，请求 monthly）", "ANNUALONLY", "monthly", ReasonCycleNotApplicable},
		{"周期不适用（两年付上游不售，price=null）", "OKPERCENT", "biennial", ReasonCycleNotApplicable},
		{"周期不适用（三年付上游不售，price=null）", "OKPERCENT", "triennial", ReasonCycleNotApplicable},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := validateOK(t, engine, testCase.code, fmt.Sprintf("?product_id=%d&cycle=%s", productID, testCase.cycle))
			if result.Valid {
				t.Fatalf("期望 valid=false，实际 %+v", result)
			}
			if result.Reason != testCase.reason {
				t.Fatalf("reason = %q，期望 %q", result.Reason, testCase.reason)
			}
			// 无效响应只带 valid + reason。
			if result.Price != "" || result.DiscountAmount != "" || result.Code != "" {
				t.Fatalf("无效响应不应带折扣明细: %+v", result)
			}
		})
	}
}
