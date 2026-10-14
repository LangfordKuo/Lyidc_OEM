package router

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// ── 邀请返佣集成测试辅助（契约 18）────────────────────────────────────────
//
// 口径（契约 18.2）：返佣 = max(订单原价 × 生效比例 − 本单优惠券减免, 0)，下取整到分。
// seedOrderProduct 的月付固定价是 100.00，正好对应契约 18.2 的四个验证例子。

// refSettingsJSON 组装 settings.referral 的 JSON 原文。
func refSettingsJSON(enabled bool, defaultPercent, maxCouponPercent string) string {
	enabledLit := "false"
	if enabled {
		enabledLit = "true"
	}
	return `{"enabled":` + enabledLit + `,"default_percent":"` + defaultPercent +
		`","max_coupon_percent":"` + maxCouponPercent + `"}`
}

// seedReferralSetting 直接写库配置返佣设置（总开关 + 全局默认比例）。
func seedReferralSetting(t *testing.T, gdb *gorm.DB, enabled bool, defaultPercent string) {
	t.Helper()
	seedSetting(t, gdb, settings.KeyReferral, refSettingsJSON(enabled, defaultPercent, "100.00"))
}

// refMemberByID 读库取会员（断言用）。
func refMemberByID(t *testing.T, gdb *gorm.DB, id uint64) *model.Member {
	t.Helper()
	var member model.Member
	if err := gdb.First(&member, id).Error; err != nil {
		t.Fatalf("查询会员 %d 失败: %v", id, err)
	}
	return &member
}

// refRebatesOf 查某邀请人的全部返佣流水。
func refRebatesOf(t *testing.T, gdb *gorm.DB, inviterID uint64) []model.ReferralRebate {
	t.Helper()
	var rows []model.ReferralRebate
	if err := gdb.Where("inviter_id = ?", inviterID).Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("查询返佣流水失败: %v", err)
	}
	return rows
}

// refRebateByOrder 按订单查返佣流水；不存在时返回 nil。
func refRebateByOrder(t *testing.T, gdb *gorm.DB, orderID uint64) *model.ReferralRebate {
	t.Helper()
	var row model.ReferralRebate
	err := gdb.Where("order_id = ?", orderID).Take(&row).Error
	if err == gorm.ErrRecordNotFound {
		return nil
	}
	if err != nil {
		t.Fatalf("查询订单 %d 的返佣流水失败: %v", orderID, err)
	}
	return &row
}

// refLedgerOf 查某会员某类型的全部流水。
func refLedgerOf(t *testing.T, gdb *gorm.DB, memberID uint64, kind string) []model.Ledger {
	t.Helper()
	var rows []model.Ledger
	if err := gdb.Where("member_id = ? AND type = ?", memberID, kind).
		Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("查询流水失败: %v", err)
	}
	return rows
}

// refCreatePaidOrder 下单并用余额支付，返回订单（支付后）。
func refCreatePaidOrder(t *testing.T, engine *gin.Engine, gdb *gorm.DB,
	token string, buyerID uint64, body map[string]any) orderView {
	t.Helper()
	order := createOrder(t, engine, token, body)
	setMemberBalance(t, gdb, buyerID, "1000.00")
	rec, envelope := payOrder(t, engine, token, order.ID, map[string]any{"channel": "balance"})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("余额支付失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return getOrderView(t, engine, token, order.ID)
}

// refSeedBoundBuyer 注册买家并绑定到邀请人（直接写库模拟注册时的首绑）。
func refSeedBoundBuyer(t *testing.T, engine *gin.Engine, gdb *gorm.DB,
	username string, inviterID uint64) (string, memberView) {
	t.Helper()
	token, member := memberTokenFor(t, engine, username)
	if err := gdb.Model(&model.Member{}).Where("id = ?", member.ID).
		UpdateColumn("inviter_id", inviterID).Error; err != nil {
		t.Fatalf("绑定邀请人失败: %v", err)
	}
	return token, member
}

// refMonthOrder 是 seedOrderProduct 商品的月付下单请求体（原价 100.00）。
func refMonthOrder(productID uint64, couponCode string) map[string]any {
	body := map[string]any{"product_id": productID, "cycle": "monthly", "config": map[string]any{}}
	if couponCode != "" {
		body["coupon_code"] = couponCode
	}
	return body
}

// ── 注册邀请码 ──────────────────────────────────────────────────────────

func TestRegisterWithInviteCode(t *testing.T) {
	gdb := testDatabase(t)
	engine := newAccountEngine(t, gdb)

	// 邀请人：开通返现拿到邀请码。
	inviterToken, inviter := memberTokenFor(t, engine, "inviter")
	inviteCode := refEnableReferral(t, engine, inviterToken)
	if inviteCode == "" {
		t.Fatalf("开通返现后邀请码为空")
	}

	// 1) 合法邀请码 → 绑定 inviter_id。
	rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"username":    "buyer_ok",
		"email":       "buyer_ok@example.com",
		"password":    "password123",
		"invite_code": inviteCode,
	})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("带邀请码注册失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	created := decodeData[memberView](t, envelope)
	if got := refMemberByID(t, gdb, created.ID).InviterID; got != inviter.ID {
		t.Fatalf("注册后 inviter_id = %d，期望 %d", got, inviter.ID)
	}

	// 2) 不存在的邀请码 → 40002 明确报错（不静默忽略）。
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"username":    "buyer_bad",
		"email":       "buyer_bad@example.com",
		"password":    "password123",
		"invite_code": "NOSUCHCODE",
	})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("非法邀请码注册 = HTTP %d code %d，期望 400/40002（body=%s）",
			rec.Code, envelope.Code, rec.Body.String())
	}

	// 3) 空邀请码（显式传空串）→ 拒绝。
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"username":    "buyer_empty",
		"email":       "buyer_empty@example.com",
		"password":    "password123",
		"invite_code": "   ",
	})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("空邀请码注册 = HTTP %d code %d，期望 400/40002（body=%s）",
			rec.Code, envelope.Code, rec.Body.String())
	}

	// 4) 自邀（同邮箱）→ 拒绝。
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"username":    "inviter_copy",
		"email":       "inviter@example.com",
		"password":    "password123",
		"invite_code": inviteCode,
	})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("自邀注册 = HTTP %d code %d，期望 400/40002（body=%s）",
			rec.Code, envelope.Code, rec.Body.String())
	}

	// 5) 不传邀请码 → 照常注册且 inviter_id=0。
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"username": "buyer_plain",
		"email":    "buyer_plain@example.com",
		"password": "password123",
	})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("无邀请码注册失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	plain := decodeData[memberView](t, envelope)
	if got := refMemberByID(t, gdb, plain.ID).InviterID; got != 0 {
		t.Fatalf("无邀请码注册后 inviter_id = %d，期望 0", got)
	}
}

// ── 开通/概览 ───────────────────────────────────────────────────────────

func TestReferralEnableGeneratesInviteCodeLazily(t *testing.T) {
	gdb := testDatabase(t)
	engine := newAccountEngine(t, gdb)
	seedReferralSetting(t, gdb, true, "20.00")

	token, _ := memberTokenFor(t, engine, "alice")

	// 未开通：开关关、邀请码空。
	overview := refOverview(t, engine, token)
	if overview.Enabled || overview.InviteCode != "" || overview.InviteLink != "" {
		t.Fatalf("未开通概览 = %+v，期望 enabled=false 且邀请码/链接为空", overview)
	}

	// 首次开通 → 惰性生成唯一邀请码。
	enabled := refEnableReferral(t, engine, token)
	if len(enabled) != 8 {
		t.Fatalf("邀请码 = %q，期望 8 位", enabled)
	}
	overview = refOverview(t, engine, token)
	if !overview.Enabled || overview.InviteCode != enabled {
		t.Fatalf("开通后概览 = %+v，期望 enabled=true 且邀请码一致", overview)
	}
	if overview.EffectivePercent != "20.00" {
		t.Fatalf("生效比例 = %q，期望 20.00（全局默认）", overview.EffectivePercent)
	}

	// 再次开/关/开：邀请码保持不变（惰性生成只发生一次）。
	refPutReferral(t, engine, token, false)
	refPutReferral(t, engine, token, true)
	if again := refOverview(t, engine, token); again.InviteCode != enabled {
		t.Fatalf("重复开通后邀请码 = %q，期望保持 %q", again.InviteCode, enabled)
	}

	// 关闭后生效比例仍按全局默认展示（开关是会员自己的，不影响比例取值）。
	// 总开关关闭时 effective_percent 恒为 0.00。
	seedReferralSetting(t, gdb, false, "20.00")
	if got := refOverview(t, engine, token); got.EffectivePercent != "0.00" {
		t.Fatalf("总开关关闭后生效比例 = %q，期望 0.00", got.EffectivePercent)
	}

	// 站点地址已配置时拼出站内邀请链接。
	seedSetting(t, gdb, settings.KeySite, `{"name":"Lyidc","url":"https://shop.example.com","admin_email":""}`)
	overview = refOverview(t, engine, token)
	wantLink := "https://shop.example.com/register?invite=" + enabled
	if overview.InviteLink != wantLink {
		t.Fatalf("邀请链接 = %q，期望 %q", overview.InviteLink, wantLink)
	}
}

// ── 返佣发放：契约 18.2 四个验证例子（端到端）───────────────────────────

func TestReferralRebateFormulaExamplesEndToEnd(t *testing.T) {
	tests := []struct {
		name        string
		couponCode  string // 空串 = 不用券
		couponKind  string
		couponValue string
		wantRebate  string // 期望返佣；"0.00" 表示不发放（不写流水）
	}{
		{"原价 100、比例 20% → 返 20", "", "", "", "20.00"},
		{"用券实付 90（减 10）→ 返 10", "OFF10", "fixed", "10.00", "10.00"},
		{"20% 比例下 8 折券（减 20）→ 返 0", "OFF20PCT", "percent", "20.00", "0.00"},
		{"9 折券（减 10）→ 返 10", "OFF10PCT", "percent", "10.00", "10.00"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gdb := testDatabase(t)
			engine := newStage4Engine(t, gdb, "", nil)
			seedReferralSetting(t, gdb, true, "20.00")
			product := seedOrderProduct(t, gdb, model.ProductStatusOn)

			_, inviter := memberTokenFor(t, engine, "inviter")
			buyerToken, buyer := refSeedBoundBuyer(t, engine, gdb, "buyer", inviter.ID)

			if tc.couponCode != "" {
				seedCoupon(t, gdb, tc.couponCode, tc.couponKind, tc.couponValue, "[]",
					func(c *model.Coupon) { c.OwnerMemberID = 0 })
			}

			order := refCreatePaidOrder(t, engine, gdb, buyerToken, buyer.ID,
				refMonthOrder(product.ID, tc.couponCode))

			rebate := refRebateByOrder(t, gdb, order.ID)
			if tc.wantRebate == "0.00" {
				if rebate != nil {
					t.Fatalf("返佣为 0 时不应发放流水，却得到 %+v", rebate)
				}
				if got := refMemberByID(t, gdb, inviter.ID).RebateBalance; string(got) != "0.00" {
					t.Fatalf("返现余额 = %q，期望 0.00", got)
				}
				return
			}
			if rebate == nil {
				t.Fatalf("未查到返佣流水（订单 %d）", order.ID)
			}
			if string(rebate.RebateAmount) != tc.wantRebate {
				t.Fatalf("返佣金额 = %s，期望 %s", rebate.RebateAmount, tc.wantRebate)
			}
			if rebate.Status != model.RebateStatusGranted {
				t.Fatalf("返佣状态 = %q，期望 granted", rebate.Status)
			}
			if rebate.RatioSource != model.RebateSourceGlobal {
				t.Fatalf("比例来源 = %q，期望 global", rebate.RatioSource)
			}
			if string(rebate.RatioPercent) != "20.00" {
				t.Fatalf("比例快照 = %s，期望 20.00", rebate.RatioPercent)
			}
			if string(rebate.BaseAmount) != "100.00" {
				t.Fatalf("原价快照 = %s，期望 100.00", rebate.BaseAmount)
			}
			if got := refMemberByID(t, gdb, inviter.ID).RebateBalance; string(got) != tc.wantRebate {
				t.Fatalf("返现余额 = %s，期望 %s", got, tc.wantRebate)
			}
		})
	}
}

// ── 比例三级优先级 ──────────────────────────────────────────────────────

func TestReferralRatioPriority(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	seedReferralSetting(t, gdb, true, "5.00")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	_, inviter := memberTokenFor(t, engine, "inviter")
	buyerToken, buyer := refSeedBoundBuyer(t, engine, gdb, "buyer", inviter.ID)

	setProductRebate := func(percent string, disabled bool) {
		t.Helper()
		columns := map[string]any{"rebate_disabled": disabled}
		if percent == "" {
			columns["rebate_percent"] = nil
		} else {
			columns["rebate_percent"] = percent
		}
		if err := gdb.Model(&model.Product{}).Where("id = ?", product.ID).
			Updates(columns).Error; err != nil {
			t.Fatalf("改商品返佣字段失败: %v", err)
		}
	}
	setMemberOverride := func(percent string) {
		t.Helper()
		columns := map[string]any{}
		if percent == "" {
			columns["rebate_percent_override"] = nil
		} else {
			columns["rebate_percent_override"] = percent
		}
		if err := gdb.Model(&model.Member{}).Where("id = ?", inviter.ID).
			Updates(columns).Error; err != nil {
			t.Fatalf("改会员特批比例失败: %v", err)
		}
	}

	// 1) 三级都有值 → 商品级压过一切。
	setProductRebate("20.00", false)
	setMemberOverride("10.00")
	order := refCreatePaidOrder(t, engine, gdb, buyerToken, buyer.ID, refMonthOrder(product.ID, ""))
	rebate := refRebateByOrder(t, gdb, order.ID)
	if rebate == nil {
		t.Fatalf("未查到返佣流水")
	}
	if string(rebate.RatioPercent) != "20.00" || rebate.RatioSource != model.RebateSourceProduct {
		t.Fatalf("三级都有值时 = %s/%s，期望 20.00/product", rebate.RatioPercent, rebate.RatioSource)
	}
	if string(rebate.RebateAmount) != "20.00" {
		t.Fatalf("返佣 = %s，期望 20.00", rebate.RebateAmount)
	}

	// 2) 商品未覆盖 → 会员级特批。
	setProductRebate("", false)
	setMemberOverride("10.00")
	order = refCreatePaidOrder(t, engine, gdb, buyerToken, buyer.ID, refMonthOrder(product.ID, ""))
	rebate = refRebateByOrder(t, gdb, order.ID)
	if rebate == nil {
		t.Fatalf("未查到返佣流水")
	}
	if string(rebate.RatioPercent) != "10.00" || rebate.RatioSource != model.RebateSourceMember {
		t.Fatalf("会员级命中时 = %s/%s，期望 10.00/member", rebate.RatioPercent, rebate.RatioSource)
	}

	// 3) 商品/会员都未覆盖 → 全局默认。
	setMemberOverride("")
	order = refCreatePaidOrder(t, engine, gdb, buyerToken, buyer.ID, refMonthOrder(product.ID, ""))
	rebate = refRebateByOrder(t, gdb, order.ID)
	if rebate == nil {
		t.Fatalf("未查到返佣流水")
	}
	if string(rebate.RatioPercent) != "5.00" || rebate.RatioSource != model.RebateSourceGlobal {
		t.Fatalf("全局默认命中时 = %s/%s，期望 5.00/global", rebate.RatioPercent, rebate.RatioSource)
	}

	// 4) rebate_disabled=1 压过一切（哪怕商品级比例存在）。
	setProductRebate("20.00", true)
	order = refCreatePaidOrder(t, engine, gdb, buyerToken, buyer.ID, refMonthOrder(product.ID, ""))
	if rebate = refRebateByOrder(t, gdb, order.ID); rebate != nil {
		t.Fatalf("禁用返佣后仍发放了流水 %+v", rebate)
	}

	// 5) 总开关关闭 → 一律不发放（哪怕三级比例都有值）。
	setProductRebate("20.00", false)
	seedReferralSetting(t, gdb, false, "5.00")
	order = refCreatePaidOrder(t, engine, gdb, buyerToken, buyer.ID, refMonthOrder(product.ID, ""))
	if rebate = refRebateByOrder(t, gdb, order.ID); rebate != nil {
		t.Fatalf("总开关关闭后仍发放了流水 %+v", rebate)
	}
}

// ── 参与/不参与、幂等 ───────────────────────────────────────────────────

func TestReferralRebateCoversNewRenewAndBalancePayButNotRecharge(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	seedReferralSetting(t, gdb, true, "20.00")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	_, inviter := memberTokenFor(t, engine, "inviter")
	buyerToken, buyer := refSeedBoundBuyer(t, engine, gdb, "buyer", inviter.ID)

	// 1) 新购 + 余额支付 → 返。
	newOrder := refCreatePaidOrder(t, engine, gdb, buyerToken, buyer.ID, refMonthOrder(product.ID, ""))
	if rebate := refRebateByOrder(t, gdb, newOrder.ID); rebate == nil ||
		string(rebate.RebateAmount) != "20.00" {
		t.Fatalf("新购返佣 = %+v，期望 20.00", rebate)
	}

	// 2) 续费单 → 也返（type=renew）。
	st := store.New(gdb)
	instanceID := uint64(1)
	renewOrder, err := st.CreateOrder(context.Background(), store.OrderInput{
		TradeNo:        "O-REF-RENEW-1",
		MemberID:       buyer.ID,
		ProductID:      product.ID,
		ProductName:    product.Name,
		Cycle:          "monthly",
		Qty:            1,
		ConfigJSON:     "{}",
		Amount:         "100.00",
		DiscountAmount: "0.00",
		FinalAmount:    "100.00",
		Type:           model.OrderTypeRenew,
		InstanceID:     &instanceID,
	})
	if err != nil {
		t.Fatalf("创建续费单失败: %v", err)
	}
	setMemberBalance(t, gdb, buyer.ID, "1000.00")
	rec, envelope := payOrder(t, engine, buyerToken, renewOrder.ID, map[string]any{"channel": "balance"})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("续费单余额支付失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	renewRebate := refRebateByOrder(t, gdb, renewOrder.ID)
	if renewRebate == nil || string(renewRebate.RebateAmount) != "20.00" {
		t.Fatalf("续费返佣 = %+v，期望 20.00", renewRebate)
	}
	if renewRebate.OrderType != model.OrderTypeRenew {
		t.Fatalf("返佣订单类型快照 = %q，期望 renew", renewRebate.OrderType)
	}

	// 3) 充值 → 不返。
	before := refMemberByID(t, gdb, inviter.ID).RebateBalance
	recharge := model.Recharge{
		TradeNo:   "R-REF-1",
		MemberID:  buyer.ID,
		Amount:    "50.00",
		Channel:   "epay",
		Status:    model.RechargeStatusPending,
		CreatedAt: time.Now().UTC(),
	}
	if err := gdb.Create(&recharge).Error; err != nil {
		t.Fatalf("写入充值单失败: %v", err)
	}
	if _, err := st.CompleteRechargePayment(context.Background(), store.RechargePaymentInput{
		OutTradeNo: recharge.TradeNo,
		PaidAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("充值入账失败: %v", err)
	}
	if after := refMemberByID(t, gdb, inviter.ID).RebateBalance; after != before {
		t.Fatalf("充值后返现余额 = %s，期望不变 %s", after, before)
	}
	if rows := refRebatesOf(t, gdb, inviter.ID); len(rows) != 2 {
		t.Fatalf("返佣流水条数 = %d，期望 2（充值不产生）", len(rows))
	}
}

func TestReferralNoRebateForUnboundBuyer(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	seedReferralSetting(t, gdb, true, "20.00")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	_, inviter := memberTokenFor(t, engine, "inviter")
	// 买家**没有**绑定邀请人。
	buyerToken, buyer := memberTokenFor(t, engine, "lonely")

	order := refCreatePaidOrder(t, engine, gdb, buyerToken, buyer.ID, refMonthOrder(product.ID, ""))
	if rebate := refRebateByOrder(t, gdb, order.ID); rebate != nil {
		t.Fatalf("未绑定买家产生了返佣 %+v", rebate)
	}
	if rows := refRebatesOf(t, gdb, inviter.ID); len(rows) != 0 {
		t.Fatalf("邀请人流水 = %+v，期望空", rows)
	}
	// 买家自己的返现余额不受影响。
	if got := refMemberByID(t, gdb, buyer.ID).RebateBalance; string(got) != "0.00" {
		t.Fatalf("买家返现余额 = %s，期望 0.00", got)
	}
}

func TestReferralRebateIdempotentOnReplayedCallback(t *testing.T) {
	gdb := testDatabase(t)
	gateway := newFakeEpayGateway(t)
	engine := newStage4Engine(t, gdb, "", nil)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://shop.example.com/api/v1/payments/epay/notify")
	seedReferralSetting(t, gdb, true, "20.00")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	_, inviter := memberTokenFor(t, engine, "inviter")
	buyerToken, _ := refSeedBoundBuyer(t, engine, gdb, "buyer", inviter.ID)

	order := createOrder(t, engine, buyerToken, refMonthOrder(product.ID, ""))
	values := gateway.notifyValues(order.TradeNo, order.FinalAmount)

	if ack := notifyAck(t, engine, values); ack != "success" {
		t.Fatalf("首次回调应答 = %q，期望 success", ack)
	}
	// 渠道重放同一回调（重复入账）。
	if ack := notifyAck(t, engine, values); ack != "success" {
		t.Fatalf("重放回调应答 = %q，期望 success", ack)
	}

	if rows := refRebatesOf(t, gdb, inviter.ID); len(rows) != 1 {
		t.Fatalf("返佣流水条数 = %d，期望 1（order_id 唯一，重复回调只发一次）", len(rows))
	}
	if got := refMemberByID(t, gdb, inviter.ID).RebateBalance; string(got) != "20.00" {
		t.Fatalf("返现余额 = %s，期望 20.00（只发一次）", got)
	}
}

// ── 回滚 ────────────────────────────────────────────────────────────────

func TestReferralRebateRollbackOnOrderCancel(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	seedReferralSetting(t, gdb, true, "20.00")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	_, inviter := memberTokenFor(t, engine, "inviter")
	buyerToken, buyer := refSeedBoundBuyer(t, engine, gdb, "buyer", inviter.ID)

	// 已支付 → 已发放返佣。
	paid := refCreatePaidOrder(t, engine, gdb, buyerToken, buyer.ID, refMonthOrder(product.ID, ""))
	if string(refMemberByID(t, gdb, inviter.ID).RebateBalance) != "20.00" {
		t.Fatalf("发放后返现余额 = %s，期望 20.00", refMemberByID(t, gdb, inviter.ID).RebateBalance)
	}

	// 订单取消回滚：与 CancelOrder 成功路径调用的是同一个钩子
	// （当前取消接口只允许 pending，因此这里直接驱动该钩子——一旦出现已支付取消/退款
	// 路径，复用同一函数即可）。
	st := store.New(gdb)
	if err := st.RollbackReferralRebate(context.Background(), paid.ID); err != nil {
		t.Fatalf("回滚返佣失败: %v", err)
	}

	rebate := refRebateByOrder(t, gdb, paid.ID)
	if rebate == nil || rebate.Status != model.RebateStatusRolledBack {
		t.Fatalf("回滚后流水 = %+v，期望 status=rolled_back", rebate)
	}
	if got := refMemberByID(t, gdb, inviter.ID).RebateBalance; string(got) != "0.00" {
		t.Fatalf("回滚后返现余额 = %s，期望 0.00（冲回）", got)
	}
	// 冲回写负向 ledger。
	ledgerRows := refLedgerOf(t, gdb, inviter.ID, model.LedgerTypeReferralRebate)
	if len(ledgerRows) != 2 {
		t.Fatalf("返佣流水账条数 = %d，期望 2（发放 + 冲回）", len(ledgerRows))
	}
	if string(ledgerRows[1].Amount) != "-20.00" {
		t.Fatalf("冲回金额 = %s，期望 -20.00", ledgerRows[1].Amount)
	}

	// 再次回滚：幂等，不重复冲回。
	if err := st.RollbackReferralRebate(context.Background(), paid.ID); err != nil {
		t.Fatalf("重复回滚失败: %v", err)
	}
	if got := refMemberByID(t, gdb, inviter.ID).RebateBalance; string(got) != "0.00" {
		t.Fatalf("重复回滚后返现余额 = %s，期望 0.00", got)
	}
	if rows := refLedgerOf(t, gdb, inviter.ID, model.LedgerTypeReferralRebate); len(rows) != 2 {
		t.Fatalf("重复回滚后流水账条数 = %d，期望 2（不重复）", len(rows))
	}
}

func TestCancelOrderTriggersRebateRollbackHook(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	seedReferralSetting(t, gdb, true, "20.00")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	_, inviter := memberTokenFor(t, engine, "inviter")
	buyerToken, buyer := refSeedBoundBuyer(t, engine, gdb, "buyer", inviter.ID)

	// pending 订单（可被 CancelOrder 取消）+ 一笔已发放的返佣
	// （模拟「已支付订单被取消」的防御场景：现有取消接口只允许 pending，钩子照常接线）。
	order := createOrder(t, engine, buyerToken, refMonthOrder(product.ID, ""))
	now := time.Now().UTC()
	rebate := model.ReferralRebate{
		OrderID:        order.ID,
		InviterID:      inviter.ID,
		BuyerID:        buyer.ID,
		OrderType:      model.OrderTypeNew,
		BaseAmount:     "100.00",
		DiscountAmount: "0.00",
		RatioPercent:   "20.00",
		RatioSource:    model.RebateSourceGlobal,
		RebateAmount:   "20.00",
		Status:         model.RebateStatusGranted,
		Note:           "订单 " + order.TradeNo + " 返佣",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := gdb.Create(&rebate).Error; err != nil {
		t.Fatalf("写入返佣流水失败: %v", err)
	}
	if err := gdb.Model(&model.Member{}).Where("id = ?", inviter.ID).
		UpdateColumn("rebate_balance", "20.00").Error; err != nil {
		t.Fatalf("写入返现余额失败: %v", err)
	}

	rec, envelope := doAPI(t, engine, http.MethodPost,
		"/api/v1/orders/"+itoa(order.ID)+"/cancel", buyerToken, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("取消订单失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	got := refRebateByOrder(t, gdb, order.ID)
	if got == nil || got.Status != model.RebateStatusRolledBack {
		t.Fatalf("取消后返佣流水 = %+v，期望 status=rolled_back", got)
	}
	if balance := refMemberByID(t, gdb, inviter.ID).RebateBalance; string(balance) != "0.00" {
		t.Fatalf("取消后返现余额 = %s，期望 0.00（冲回）", balance)
	}

	// 本就没有返佣的 pending 单取消：钩子是 no-op，不影响正常取消。
	plain := createOrder(t, engine, buyerToken, refMonthOrder(product.ID, ""))
	rec, envelope = doAPI(t, engine, http.MethodPost,
		"/api/v1/orders/"+itoa(plain.ID)+"/cancel", buyerToken, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("无返佣订单取消失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
}

// ── 邀请人自建券绑定 ────────────────────────────────────────────────────

func TestInviteCouponBindingRules(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	seedReferralSetting(t, gdb, true, "20.00")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	ownerToken, owner := memberTokenFor(t, engine, "owner")
	ownerCoupon := seedCoupon(t, gdb, "OWNERCUP", "fixed", "10.00", "[]",
		func(c *model.Coupon) { c.OwnerMemberID = owner.ID })

	// 1) 未绑定买家用自建券 → 首绑到 owner。
	unboundToken, unbound := memberTokenFor(t, engine, "fresh")
	order := createOrder(t, engine, unboundToken, refMonthOrder(product.ID, ownerCoupon.Code))
	if got := refMemberByID(t, gdb, unbound.ID).InviterID; got != owner.ID {
		t.Fatalf("用自建券下单后 inviter_id = %d，期望绑定到 %d", got, owner.ID)
	}
	// 绑定后支付：返佣发给 owner（用券减 10 → 返 10）。
	setMemberBalance(t, gdb, unbound.ID, "1000.00")
	rec, envelope := payOrder(t, engine, unboundToken, order.ID, map[string]any{"channel": "balance"})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("支付失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	rebate := refRebateByOrder(t, gdb, order.ID)
	if rebate == nil || string(rebate.RebateAmount) != "10.00" {
		t.Fatalf("自建券买家的返佣 = %+v，期望 10.00 给 owner", rebate)
	}

	// 2) 已绑定买家再用自建券 → 拒绝（40002），且不改绑。
	_, other := memberTokenFor(t, engine, "otherowner")
	otherCoupon := seedCoupon(t, gdb, "OTHERCUP", "fixed", "5.00", "[]",
		func(c *model.Coupon) { c.OwnerMemberID = other.ID })
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/orders", unboundToken,
		refMonthOrder(product.ID, otherCoupon.Code))
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("已绑定买家用他人自建券 = HTTP %d code %d，期望 400/40002（body=%s）",
			rec.Code, envelope.Code, rec.Body.String())
	}
	if got := refMemberByID(t, gdb, unbound.ID).InviterID; got != owner.ID {
		t.Fatalf("首绑被改绑为 %d，期望保持 %d（首绑不改）", got, owner.ID)
	}

	// 3) 注册绑定后用他人自建券 → 同样拒绝（首绑不改）。
	regToken, regBuyer := refSeedBoundBuyer(t, engine, gdb, "regbuyer", owner.ID)
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/orders", regToken,
		refMonthOrder(product.ID, otherCoupon.Code))
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("注册绑定买家用他人自建券 = HTTP %d code %d，期望 400/40002（body=%s）",
			rec.Code, envelope.Code, rec.Body.String())
	}
	if got := refMemberByID(t, gdb, regBuyer.ID).InviterID; got != owner.ID {
		t.Fatalf("注册绑定被改绑为 %d，期望保持 %d", got, owner.ID)
	}

	// 4) 邀请人自己用自己的自建券 → 拒绝（否则自邀）。
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/orders",
		ownerToken, refMonthOrder(product.ID, ownerCoupon.Code))
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("邀请人用自己自建券 = HTTP %d code %d，期望 400/40002（body=%s）",
			rec.Code, envelope.Code, rec.Body.String())
	}

	// 5) 平台券（owner=0）任何人都能用，不改绑。
	platformCoupon := seedCoupon(t, gdb, "PLATFORM", "fixed", "5.00", "[]", nil)
	boundToken, bound := refSeedBoundBuyer(t, engine, gdb, "boundbuyer", owner.ID)
	createOrder(t, engine, boundToken, refMonthOrder(product.ID, platformCoupon.Code))
	if got := refMemberByID(t, gdb, bound.ID).InviterID; got != owner.ID {
		t.Fatalf("平台券改绑为 %d，期望保持 %d", got, owner.ID)
	}
}

// refEnableReferral 开通返现并返回邀请码。
func refEnableReferral(t *testing.T, engine http.Handler, token string) string {
	t.Helper()
	view := refPutReferral(t, engine, token, true)
	return view.InviteCode
}

// refPutReferral 调 PUT /api/v1/referral 并返回概览。
func refPutReferral(t *testing.T, engine http.Handler, token string, enabled bool) referralOverviewView {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodPut, "/api/v1/referral", token,
		map[string]any{"enabled": enabled})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("更新邀请返现开关失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[referralOverviewView](t, envelope)
}

// refOverview 调 GET /api/v1/referral 并返回概览。
func refOverview(t *testing.T, engine http.Handler, token string) referralOverviewView {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/referral", token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("查询返佣概览失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[referralOverviewView](t, envelope)
}

// ── 会员端查询接口 ──────────────────────────────────────────────────────

func TestReferralMemberQueryEndpoints(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	seedReferralSetting(t, gdb, true, "20.00")
	seedSetting(t, gdb, settings.KeySite, `{"name":"Lyidc","url":"https://shop.example.com","admin_email":""}`)
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	inviterToken, _ := memberTokenFor(t, engine, "inviter")
	inviteCode := refEnableReferral(t, engine, inviterToken)

	// 两个被邀请人：一个走邀请码注册，一个走自建券绑定。
	rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"username":    "buyer1",
		"email":       "buyer1@example.com",
		"password":    "password123",
		"invite_code": inviteCode,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("buyer1 注册失败: %s", rec.Body.String())
	}
	buyer1 := decodeData[memberView](t, envelope)
	buyer1Token, _ := loginMember(t, engine, "buyer1", "password123")

	buyer2Token, _ := memberTokenFor(t, engine, "buyer2")

	// buyer1 下单支付 → 给 inviter 产生返佣。
	refCreatePaidOrder(t, engine, gdb, buyer1Token, buyer1.ID, refMonthOrder(product.ID, ""))

	// GET /referral：已邀请人数与累计返佣。
	overview := refOverview(t, engine, inviterToken)
	if overview.InviteeCount != 1 {
		t.Fatalf("已邀请人数 = %d，期望 1", overview.InviteeCount)
	}
	if overview.TotalRebate != "20.00" {
		t.Fatalf("累计返佣 = %s，期望 20.00", overview.TotalRebate)
	}
	if overview.InviteLink != "https://shop.example.com/register?invite="+inviteCode {
		t.Fatalf("邀请链接 = %q", overview.InviteLink)
	}

	// GET /referral/rebates：流水含订单号与买家用户名。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/referral/rebates", inviterToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("返佣流水查询失败: %s", rec.Body.String())
	}
	rebates := decodeData[referralRebateListView](t, envelope)
	if rebates.Total != 1 || len(rebates.Items) != 1 {
		t.Fatalf("返佣流水 = %+v，期望 1 条", rebates)
	}
	item := rebates.Items[0]
	if item.BuyerUsername != "buyer1" || item.OrderTradeNo == "" {
		t.Fatalf("流水行缺订单号/买家用户名: %+v", item)
	}
	if item.RebateAmount != "20.00" || item.Status != model.RebateStatusGranted {
		t.Fatalf("流水行金额/状态 = %s/%s", item.RebateAmount, item.Status)
	}

	// GET /referral/invitees：消费/贡献返佣统计。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/referral/invitees", inviterToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("邀请列表查询失败: %s", rec.Body.String())
	}
	invitees := decodeData[inviteeListView](t, envelope)
	if invitees.Total != 1 || len(invitees.Items) != 1 {
		t.Fatalf("邀请列表 = %+v，期望 1 条", invitees)
	}
	row := invitees.Items[0]
	if row.Username != "buyer1" || row.OrderCount != 1 {
		t.Fatalf("邀请行 = %+v，期望 buyer1 / 1 单", row)
	}
	if row.TotalSpent != "100.00" || row.TotalRebate != "20.00" {
		t.Fatalf("邀请行消费/贡献 = %s/%s，期望 100.00/20.00", row.TotalSpent, row.TotalRebate)
	}

	// 只看自己的数据：buyer2 的视角为空。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/referral/rebates", buyer2Token, nil)
	if got := decodeData[referralRebateListView](t, envelope); got.Total != 0 {
		t.Fatalf("buyer2 的返佣流水 = %+v，期望 0 条", got)
	}
}

// ── 管理端设置 ──────────────────────────────────────────────────────────

func TestReferralAdminSettings(t *testing.T) {
	gdb := testDatabase(t)
	engine := newAccountEngine(t, gdb)

	seedAdmin(t, gdb, "root", "admin123456", model.RoleAdmin, model.StatusActive)
	adminToken, _ := loginAdmin(t, engine, "root", "admin123456")
	seedAdmin(t, gdb, "fin", "admin123456", model.RoleFinance, model.StatusActive)
	financeToken, _ := loginAdmin(t, engine, "fin", "admin123456")

	// GET 缺省值：总开关关闭、比例 0.00。
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/settings/referral", adminToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("读取返佣设置失败: %s", rec.Body.String())
	}
	view := decodeData[referralSettingsView](t, envelope)
	if view.Enabled || view.DefaultPercent != "0.00" || view.MaxCouponPercent != "100.00" {
		t.Fatalf("缺省设置 = %+v", view)
	}

	// PUT 更新。
	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/referral", adminToken,
		map[string]any{"enabled": true, "default_percent": "20.00"})
	if rec.Code != http.StatusOK {
		t.Fatalf("更新返佣设置失败: %s", rec.Body.String())
	}
	view = decodeData[referralSettingsView](t, envelope)
	if !view.Enabled || view.DefaultPercent != "20.00" {
		t.Fatalf("更新后设置 = %+v", view)
	}

	// 非法比例 → 40002。
	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/referral", adminToken,
		map[string]any{"default_percent": "150.00"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("越界比例 = HTTP %d code %d，期望 400/40002", rec.Code, envelope.Code)
	}

	// finance / support 角色 → 403（仅 admin）。
	rec, _ = doAPI(t, engine, http.MethodGet, "/api/v1/admin/settings/referral", financeToken, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("finance 读取返佣设置 = HTTP %d，期望 403", rec.Code)
	}
}

// ── 会员端余额流水不含返佣 ──────────────────────────────────────────────

func TestBalanceLedgerExcludesReferralRebate(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	seedReferralSetting(t, gdb, true, "20.00")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	inviterToken, inviter := memberTokenFor(t, engine, "inviter")
	buyerToken, buyer := refSeedBoundBuyer(t, engine, gdb, "buyer", inviter.ID)

	refCreatePaidOrder(t, engine, gdb, buyerToken, buyer.ID, refMonthOrder(product.ID, ""))

	// 邀请人的余额流水里不应出现 referral_rebate（那是返现余额的变动）。
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/finance/ledger", inviterToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("余额流水查询失败: %s", rec.Body.String())
	}
	list := decodeData[ledgerListView](t, envelope)
	if list.Total != 0 {
		t.Fatalf("邀请人余额流水 = %+v，期望 0 条（返佣不进余额流水）", list)
	}

	// 管理端对账仍可按类型查到 referral_rebate。
	seedAdmin(t, gdb, "root", "admin123456", model.RoleAdmin, model.StatusActive)
	adminToken, _ := loginAdmin(t, engine, "root", "admin123456")
	rec, envelope = doAPI(t, engine, http.MethodGet,
		"/api/v1/admin/ledger?type=referral_rebate", adminToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("管理端流水查询失败: %s", rec.Body.String())
	}
	adminList := decodeData[ledgerListView](t, envelope)
	if adminList.Total != 1 {
		t.Fatalf("管理端 referral_rebate 流水 = %+v，期望 1 条", adminList)
	}
}
