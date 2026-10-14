package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
)

// ── 邀请人自建券集成测试（契约 18.5，R10-B）──────────────────────────────
//
// 折扣上限：上限 = min(settings.referral.max_coupon_percent, 邀请人的生效返佣比例)。
// 生效返佣比例 = 会员级 rebate_percent_override 优先，否则全局 default_percent（商品级不参与）。

// refCouponSetting 写 referral 设置（含自建券折扣上限）。
func refCouponSetting(t *testing.T, gdb *gorm.DB, enabled bool, defaultPercent, maxCouponPercent string) {
	t.Helper()
	seedSetting(t, gdb, settings.KeyReferral, refSettingsJSON(enabled, defaultPercent, maxCouponPercent))
}

// refSetOverride 直接写库设会员级特批比例（空串 = 清空）。
func refSetOverride(t *testing.T, gdb *gorm.DB, memberID uint64, percent string) {
	t.Helper()
	columns := map[string]any{"updated_at": timeNowUTC()}
	if percent == "" {
		columns["rebate_percent_override"] = nil
	} else {
		columns["rebate_percent_override"] = percent
	}
	if err := gdb.Model(&model.Member{}).Where("id = ?", memberID).Updates(columns).Error; err != nil {
		t.Fatalf("改会员特批比例失败: %v", err)
	}
}

// refSetRebateBalance 直接写库设邀请返现余额（可为负）。
func refSetRebateBalance(t *testing.T, gdb *gorm.DB, memberID uint64, amount string) {
	t.Helper()
	if err := gdb.Model(&model.Member{}).Where("id = ?", memberID).
		UpdateColumn("rebate_balance", amount).Error; err != nil {
		t.Fatalf("改返现余额失败: %v", err)
	}
}

// refCreateCoupon 调 POST /api/v1/referral/coupons，返回响应记录器与响应包（不做成功断言）。
func refCreateCoupon(t *testing.T, engine http.Handler, token string, body map[string]any) (*httptest.ResponseRecorder, apiEnvelope) {
	t.Helper()
	return doAPI(t, engine, http.MethodPost, "/api/v1/referral/coupons", token, body)
}

// refCouponBody 组装自建券请求体（type 恒 percent，value 由调用方给）。
func refCouponBody(code, value string) map[string]any {
	return map[string]any{
		"code":  code,
		"type":  "percent",
		"value": value,
	}
}

// refCreateCouponOK 创建自建券并断言成功。
func refCreateCouponOK(t *testing.T, engine http.Handler, token string, body map[string]any) couponView {
	t.Helper()
	rec, envelope := refCreateCoupon(t, engine, token, body)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("创建自建券失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[couponView](t, envelope)
}

// ── 前置与上限 ──────────────────────────────────────────────────────────

func TestReferralCouponRequiresEnabledReferral(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	refCouponSetting(t, gdb, true, "20.00", "100.00")

	token, member := memberTokenFor(t, engine, "noref")

	// 未开通邀请返现 → 40002。
	rec, envelope := refCreateCoupon(t, engine, token, refCouponBody("NOCAP001", "10.00"))
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("未开通返现建券 = HTTP %d code %d，期望 400/40002（body=%s）",
			rec.Code, envelope.Code, rec.Body.String())
	}

	// 开通后即可建券。
	refEnableReferral(t, engine, token)
	view := refCreateCouponOK(t, engine, token, refCouponBody("NOCAP001", "10.00"))
	if view.OwnerMemberID != member.ID {
		t.Fatalf("owner_member_id = %d，期望 %d", view.OwnerMemberID, member.ID)
	}
}

func TestReferralCouponPercentCapFromSettings(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	// 全局默认比例 50.00 很松，自建券折扣上限被设置压到 10.00 → 上限 = min(10, 50) = 10。
	refCouponSetting(t, gdb, true, "50.00", "10.00")

	token, _ := memberTokenFor(t, engine, "capsetting")
	refEnableReferral(t, engine, token)

	// 超上限（15.00 > 10.00）→ 40002，message 含当前上限值。
	rec, envelope := refCreateCoupon(t, engine, token, refCouponBody("CAPSET01", "15.00"))
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("超上限建券 = HTTP %d code %d，期望 400/40002（body=%s）",
			rec.Code, envelope.Code, rec.Body.String())
	}
	if !containsAll(envelope.Message, "10.00", "50.00") {
		t.Fatalf("拒绝 message %q 应含上限 10.00 与设置来源 50.00", envelope.Message)
	}

	// 等于上限（10.00）→ 放行。
	view := refCreateCouponOK(t, engine, token, refCouponBody("CAPSET02", "10.00"))
	if string(view.Value) != "10.00" {
		t.Fatalf("value = %s，期望 10.00", view.Value)
	}
}

func TestReferralCouponPercentCapFromMemberOverride(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	// 设置很松（80.00），会员特批 20.00 → 上限 = min(80, 20) = 20。
	refCouponSetting(t, gdb, true, "50.00", "80.00")

	token, member := memberTokenFor(t, engine, "capmember")
	refEnableReferral(t, engine, token)
	refSetOverride(t, gdb, member.ID, "20.00")

	// 超上限（25.00 > 20.00）→ 40002，message 含两个来源。
	rec, envelope := refCreateCoupon(t, engine, token, refCouponBody("CAPMEM01", "25.00"))
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("超上限建券 = HTTP %d code %d，期望 400/40002（body=%s）",
			rec.Code, envelope.Code, rec.Body.String())
	}
	if !containsAll(envelope.Message, "20.00", "80.00", "会员级特批") {
		t.Fatalf("拒绝 message %q 应含上限 20.00、设置 80.00 与来源「会员级特批」", envelope.Message)
	}

	// 等于上限 → 放行。
	refCreateCouponOK(t, engine, token, refCouponBody("CAPMEM02", "20.00"))
}

func TestReferralCouponRejectsFixedTypeAndBadValue(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	refCouponSetting(t, gdb, true, "20.00", "100.00")

	token, _ := memberTokenFor(t, engine, "fixedtype")
	refEnableReferral(t, engine, token)

	// fixed 无法用比例卡倒挂 → 40002。
	body := map[string]any{"code": "FIXEDCUP", "type": "fixed", "value": "10.00"}
	rec, envelope := refCreateCoupon(t, engine, token, body)
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("fixed 自建券 = HTTP %d code %d，期望 400/40002（body=%s）",
			rec.Code, envelope.Code, rec.Body.String())
	}

	// value = 0 / > 100 → 40002（格式合法但违反范围）。
	for _, value := range []string{"0", "100.01"} {
		rec, envelope = refCreateCoupon(t, engine, token, refCouponBody("BADVAL01", value))
		if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
			t.Fatalf("value=%s = HTTP %d code %d，期望 400/40002（body=%s）",
				value, rec.Code, envelope.Code, rec.Body.String())
		}
	}
	// 写法非法（负号 / 非十进制）→ 40001。
	for _, value := range []string{"-1.00", "abc"} {
		rec, envelope = refCreateCoupon(t, engine, token, refCouponBody("BADVAL02", value))
		if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
			t.Fatalf("value=%s = HTTP %d code %d，期望 400/40001（body=%s）",
				value, rec.Code, envelope.Code, rec.Body.String())
		}
	}
}

func TestReferralCouponCreateSetsOwnerAndSource(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	refCouponSetting(t, gdb, true, "20.00", "100.00")

	token, member := memberTokenFor(t, engine, "ownerchk")
	refEnableReferral(t, engine, token)

	view := refCreateCouponOK(t, engine, token, refCouponBody("OWNERSRC", "8.00"))
	if view.OwnerMemberID != member.ID {
		t.Fatalf("owner_member_id = %d，期望 %d（强制 = 调用者）", view.OwnerMemberID, member.ID)
	}
	if view.Source != "member" {
		t.Fatalf("source = %q，期望 member", view.Source)
	}
	if view.Type != "percent" || view.Status != "on" {
		t.Fatalf("type/status = %s/%s，期望 percent/on", view.Type, view.Status)
	}

	// 请求体里的 owner_member_id 必须被忽略（强制 = 调用者）。
	body := refCouponBody("OWNERSRC2", "8.00")
	body["owner_member_id"] = 999999
	view = refCreateCouponOK(t, engine, token, body)
	if view.OwnerMemberID != member.ID {
		t.Fatalf("请求体 owner_member_id 被采纳为 %d，期望强制 %d", view.OwnerMemberID, member.ID)
	}
}

// ── 列表与更新 ──────────────────────────────────────────────────────────

func TestReferralCouponListOnlyOwn(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	refCouponSetting(t, gdb, true, "20.00", "100.00")

	token, member := memberTokenFor(t, engine, "listown")
	otherToken, _ := memberTokenFor(t, engine, "listother")
	refEnableReferral(t, engine, token)
	refEnableReferral(t, engine, otherToken)

	refCreateCouponOK(t, engine, token, refCouponBody("LISTOWN1", "5.00"))
	refCreateCouponOK(t, engine, token, refCouponBody("LISTOWN2", "6.00"))
	// 别人的自建券 + 平台券都不该出现在我的列表里。
	refCreateCouponOK(t, engine, otherToken, refCouponBody("LISTOTH1", "5.00"))
	seedCoupon(t, gdb, "LISTPLAT", "percent", "5.00", "[]", nil)

	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/referral/coupons", token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("查询自建券列表失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	list := decodeData[couponListView](t, envelope)
	if list.Total != 2 || len(list.Items) != 2 {
		t.Fatalf("自建券列表 total=%d items=%d，期望 2/2", list.Total, len(list.Items))
	}
	for _, item := range list.Items {
		if item.OwnerMemberID != member.ID {
			t.Fatalf("列表混入他人/平台券：id=%d owner=%d", item.ID, item.OwnerMemberID)
		}
	}
}

func TestReferralCouponUpdateOnlyStatusAndExpiresAt(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	refCouponSetting(t, gdb, true, "20.00", "100.00")

	token, member := memberTokenFor(t, engine, "updself")
	otherToken, _ := memberTokenFor(t, engine, "updother")
	refEnableReferral(t, engine, token)
	refEnableReferral(t, engine, otherToken)

	own := refCreateCouponOK(t, engine, token, refCouponBody("UPDSELF1", "5.00"))

	// 1) 改 status → 允许。
	rec, envelope := doAPI(t, engine, http.MethodPut, "/api/v1/referral/coupons/"+itoa(own.ID),
		token, map[string]any{"status": "off"})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("改 status 失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if view := decodeData[couponView](t, envelope); view.Status != "off" {
		t.Fatalf("status = %s，期望 off", view.Status)
	}

	// 2) 改 expires_at（含 null 清空）→ 允许。
	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/referral/coupons/"+itoa(own.ID),
		token, map[string]any{"expires_at": "2027-01-01T00:00:00Z"})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("改 expires_at 失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/referral/coupons/"+itoa(own.ID),
		token, map[string]any{"expires_at": nil})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("清空 expires_at 失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 3) 改 value / code / type / owner → 一律 40001 明确拒绝（不静默忽略）。
	for _, body := range []map[string]any{
		{"value": "50.00"},
		{"code": "HIJACKED"},
		{"type": "fixed"},
		{"owner_member_id": 1},
		{"max_uses": 99},
		{"comment": "改备注"},
		{"cycles": []string{"annual"}},
		{"starts_at": "2026-01-01T00:00:00Z"},
	} {
		rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/referral/coupons/"+itoa(own.ID),
			token, body)
		if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
			t.Fatalf("改不可改字段 %v = HTTP %d code %d，期望 400/40001（body=%s）",
				body, rec.Code, envelope.Code, rec.Body.String())
		}
	}

	// 4) 他人券 → 404（含平台券）。
	other := refCreateCouponOK(t, engine, otherToken, refCouponBody("UPDOTH01", "5.00"))
	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/referral/coupons/"+itoa(other.ID),
		token, map[string]any{"status": "off"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("改他人券 = HTTP %d，期望 404（body=%s）", rec.Code, rec.Body.String())
	}
	platform := seedCoupon(t, gdb, "UPDPLAT", "percent", "5.00", "[]", nil)
	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/referral/coupons/"+itoa(platform.ID),
		token, map[string]any{"status": "off"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("改平台券 = HTTP %d，期望 404（body=%s）", rec.Code, rec.Body.String())
	}

	// 5) 空更新 → 40001。
	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/referral/coupons/"+itoa(own.ID),
		token, map[string]any{})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("空更新 = HTTP %d code %d，期望 400/40001", rec.Code, envelope.Code)
	}

	// 库内归属与 value 全程未变。
	row, err := storeCouponByID(t, gdb, own.ID)
	if err != nil {
		t.Fatalf("回读优惠码失败: %v", err)
	}
	if row.OwnerMemberID != member.ID || string(row.Value) != "5.00" || row.Code != "UPDSELF1" {
		t.Fatalf("自建券被篡改：owner=%d value=%s code=%s", row.OwnerMemberID, row.Value, row.Code)
	}
}

// ── 管理端可见性与 value 保护 ────────────────────────────────────────────

func TestAdminCouponViewExposesOwnerAndBlocksSelfOwnedValue(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	refCouponSetting(t, gdb, true, "20.00", "100.00")
	seedAdmin(t, gdb, "capadmin", "admin123456", model.RoleAdmin, model.StatusActive)
	adminToken, _ := loginAdmin(t, engine, "capadmin", "admin123456")

	token, member := memberTokenFor(t, engine, "selfowned")
	refEnableReferral(t, engine, token)
	self := refCreateCouponOK(t, engine, token, refCouponBody("SELFCUP1", "5.00"))
	platform := seedCoupon(t, gdb, "PLATCUP1", "percent", "5.00", "[]", nil)

	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/coupons", adminToken, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("管理端券列表失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	list := decodeData[couponListView](t, envelope)
	byID := map[uint64]couponView{}
	for _, item := range list.Items {
		byID[item.ID] = item
	}
	if got := byID[self.ID]; got.OwnerMemberID != member.ID || got.Source != "member" {
		t.Fatalf("自建券视图 owner/source = %d/%q，期望 %d/member", got.OwnerMemberID, got.Source, member.ID)
	}
	if got := byID[platform.ID]; got.OwnerMemberID != 0 || got.Source != "platform" {
		t.Fatalf("平台券视图 owner/source = %d/%q，期望 0/platform", got.OwnerMemberID, got.Source)
	}

	// 管理端不得代改自建券的 value。
	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/admin/coupons/"+itoa(self.ID),
		adminToken, map[string]any{"value": "50.00"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("管理端改自建券 value = HTTP %d code %d，期望 400/40002（body=%s）",
			rec.Code, envelope.Code, rec.Body.String())
	}
	// 平台券可以改 value。
	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/admin/coupons/"+itoa(platform.ID),
		adminToken, map[string]any{"value": "12.00"})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("管理端改平台券 value 失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
}

// ── 使用规则回归（契约 18.5，与 R10-A TestInviteCouponBindingRules 同口径）──

func TestSelfCouponUseRulesWithApiCreatedCoupon(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	refCouponSetting(t, gdb, true, "20.00", "100.00")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	ownerToken, owner := memberTokenFor(t, engine, "apiowner")
	refEnableReferral(t, engine, ownerToken)
	ownerCoupon := refCreateCouponOK(t, engine, ownerToken, refCouponBody("APICUP01", "10.00"))

	// 1) 未绑定买家用自建券 → 首绑到 owner。
	unboundToken, unbound := memberTokenFor(t, engine, "apifresh")
	createOrder(t, engine, unboundToken, refMonthOrder(product.ID, ownerCoupon.Code))
	if got := refMemberByID(t, gdb, unbound.ID).InviterID; got != owner.ID {
		t.Fatalf("用自建券下单后 inviter_id = %d，期望绑定到 %d", got, owner.ID)
	}

	// 2) 已绑定买家再用自建券 → 拒绝 40002，且不改绑。
	otherToken, _ := memberTokenFor(t, engine, "apiother")
	refEnableReferral(t, engine, otherToken)
	otherCoupon := refCreateCouponOK(t, engine, otherToken, refCouponBody("APICUP02", "10.00"))
	rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/orders", unboundToken,
		refMonthOrder(product.ID, otherCoupon.Code))
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("已绑定买家用他人自建券 = HTTP %d code %d，期望 400/40002（body=%s）",
			rec.Code, envelope.Code, rec.Body.String())
	}
	if got := refMemberByID(t, gdb, unbound.ID).InviterID; got != owner.ID {
		t.Fatalf("首绑被改绑为 %d，期望保持 %d", got, owner.ID)
	}

	// 3) 邀请人自己用自己的自建券 → 拒绝（否则自邀）。
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/orders", ownerToken,
		refMonthOrder(product.ID, ownerCoupon.Code))
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("邀请人用自己自建券 = HTTP %d code %d，期望 400/40002（body=%s）",
			rec.Code, envelope.Code, rec.Body.String())
	}

	// 4) 平台券任何人可用、不改绑。
	platform := seedCoupon(t, gdb, "APIPLAT", "percent", "5.00", "[]", nil)
	boundToken, bound := refSeedBoundBuyer(t, engine, gdb, "apibound", owner.ID)
	createOrder(t, engine, boundToken, refMonthOrder(product.ID, platform.Code))
	if got := refMemberByID(t, gdb, bound.ID).InviterID; got != owner.ID {
		t.Fatalf("平台券改绑为 %d，期望保持 %d", got, owner.ID)
	}
}

// ── 小工具 ──────────────────────────────────────────────────────────────

// storeCouponByID 直接读库取优惠码。
func storeCouponByID(t *testing.T, gdb *gorm.DB, id uint64) (*model.Coupon, error) {
	t.Helper()
	var coupon model.Coupon
	err := gdb.First(&coupon, id).Error
	if err != nil {
		return nil, err
	}
	return &coupon, nil
}

// containsAll 判断 message 是否包含全部子串。
func containsAll(message string, wants ...string) bool {
	for _, want := range wants {
		if !strings.Contains(message, want) {
			return false
		}
	}
	return true
}

// timeNowUTC 返回当前 UTC 时间（写库用）。
func timeNowUTC() time.Time { return time.Now().UTC() }
