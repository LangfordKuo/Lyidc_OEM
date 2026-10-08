package router

import (
	"bytes"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
)

// ---------------------------------------------------------------------------
// 下单：金额与优惠码
// ---------------------------------------------------------------------------

// TestOrderCreateWithCouponAndPriceSnapshot 验证下单校验与金额明细（含优惠码折扣快照）。
func TestOrderCreateWithCouponAndPriceSnapshot(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, nil)
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)
	seedCoupon(t, gdb, "CASH20", "fixed", "20", `["annual"]`, nil)

	token, _ := memberTokenFor(t, engine, "orderuser")

	// 未知配置项 → 40002。
	rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/orders", token, map[string]any{
		"product_id": product.ID, "cycle": "annual", "config": map[string]any{"999": 111},
	})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("未知配置项应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 隐藏取值（upstream_id=203）不在会员可见范围内 → 40002。
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/orders", token, map[string]any{
		"product_id": product.ID, "cycle": "annual", "config": map[string]any{"11": 113},
	})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("隐藏取值应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 周期不可售：fixed 未覆盖且上游无价（triennial）→ 40002。
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/orders", token, map[string]any{
		"product_id": product.ID, "cycle": "triennial", "config": map[string]any{},
	})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("不可售周期应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 数字写法的配置项也被接受（{"11": 111} 整数）。
	order := createOrder(t, engine, token, map[string]any{
		"product_id":  product.ID,
		"cycle":       "annual",
		"config":      map[string]any{"11": 111},
		"coupon_code": "cash20", // 大小写不敏感
	})
	if order.Amount != "200.00" || order.DiscountAmount != "20.00" || order.FinalAmount != "180.00" {
		t.Fatalf("金额明细错误: %+v", order)
	}
	if order.Status != model.OrderStatusPending || order.PayChannel != "" || order.PayTime != nil {
		t.Fatalf("新订单状态错误: %+v", order)
	}
	if order.CouponCode != "CASH20" || order.ProductName != "阶段4测试商品" || order.Cycle != "annual" || order.Qty != 1 {
		t.Fatalf("订单快照错误: %+v", order)
	}
	if order.Config["11"] != "111" {
		t.Fatalf("配置快照错误: %+v", order.Config)
	}
	if !strings.HasPrefix(order.TradeNo, model.OrderTradeNoPrefix) {
		t.Fatalf("订单号前缀错误: %s", order.TradeNo)
	}
}

// TestOrderCreateCouponRejections 验证优惠码在下单链路的统一 40002 口径。
func TestOrderCreateCouponRejections(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, nil)
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)
	seedCoupon(t, gdb, "OFF10", "percent", "10", `[]`, func(c *model.Coupon) {
		c.Status = model.CouponStatusOff
	})
	seedCoupon(t, gdb, "MONTHLY10", "percent", "10", `["monthly"]`, nil)
	seedCoupon(t, gdb, "USEDUP", "percent", "10", `[]`, func(c *model.Coupon) {
		c.MaxUses = 1
		c.UsedCount = 1
	})

	token, _ := memberTokenFor(t, engine, "couponuser")
	cases := []struct {
		name    string
		code    string
		message string
	}{
		{"不存在", "NOSUCHCODE", "优惠码不存在"},
		{"已停用", "OFF10", "优惠码已停用"},
		{"周期不适用", "MONTHLY10", "优惠码不适用于该周期"},
		{"次数用尽", "USEDUP", "优惠码使用次数已用尽"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/orders", token, map[string]any{
				"product_id": product.ID, "cycle": "annual", "config": map[string]any{},
				"coupon_code": tc.code,
			})
			if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
				t.Fatalf("应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(envelope.Message, tc.message) {
				t.Fatalf("message = %q，期望包含 %q", envelope.Message, tc.message)
			}
		})
	}
}

// TestOrderCreateParameterErrors 验证参数格式类错误（40001）与商品类错误（404）。
func TestOrderCreateParameterErrors(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, nil)
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)
	offProduct := seedOrderProduct(t, gdb, model.ProductStatusOff)

	token, _ := memberTokenFor(t, engine, "paramuser")
	cases := []struct {
		name     string
		body     map[string]any
		wantHTTP int
		wantCode int
	}{
		{"商品 ID 非正整数", map[string]any{"product_id": 0, "cycle": "annual"}, http.StatusBadRequest, response.CodeInvalidParam},
		{"周期非法", map[string]any{"product_id": product.ID, "cycle": "weekly"}, http.StatusBadRequest, response.CodeInvalidParam},
		{"配置值类型非法", map[string]any{"product_id": product.ID, "cycle": "annual", "config": map[string]any{"11": true}},
			http.StatusBadRequest, response.CodeInvalidParam},
		{"商品不存在", map[string]any{"product_id": 999999, "cycle": "annual"}, http.StatusNotFound, response.CodeNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/orders", token, tc.body)
			if rec.Code != tc.wantHTTP || envelope.Code != tc.wantCode {
				t.Fatalf("HTTP %d code=%d，期望 %d/%d（body=%s）", rec.Code, envelope.Code, tc.wantHTTP, tc.wantCode, rec.Body.String())
			}
		})
	}

	// 下架商品与不存在统一 404（与会员端商品接口语义一致）。
	rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/orders", token, map[string]any{
		"product_id": offProduct.ID, "cycle": "annual",
	})
	if rec.Code != http.StatusNotFound || envelope.Code != response.CodeNotFound {
		t.Fatalf("下架商品应 404：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 未登录 → 401。
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/orders", "", map[string]any{
		"product_id": product.ID, "cycle": "annual",
	})
	if rec.Code != http.StatusUnauthorized || envelope.Code != response.CodeUnauthorized {
		t.Fatalf("未登录应 401：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// 在线支付 + 回调
// ---------------------------------------------------------------------------

// TestOrderOnlinePayAndNotify 跑通「下单 → 渠道下单 → 异步回调 → 订单 paid + 优惠码计数」，
// 并验证重复回调的幂等与金额一致性校验。
func TestOrderOnlinePayAndNotify(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)
	coupon := seedCoupon(t, gdb, "CASH20", "fixed", "20", `["annual"]`, nil)

	token, _ := memberTokenFor(t, engine, "payuser")
	order := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "annual", "config": map[string]any{"11": 111},
		"coupon_code": coupon.Code,
	})

	// 发起支付：渠道应收到 money=180.00、out_trade_no=订单号、签名正确的下单请求。
	rec, envelope := payOrder(t, engine, token, order.ID, map[string]any{"channel": "epay", "pay_type": "wxpay"})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("发起支付失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	pay := decodeData[payOrderView](t, envelope)
	if pay.Pay.PayURL != gateway.payURL || pay.Pay.PayType != "wxpay" || pay.Pay.Channel != "epay" {
		t.Fatalf("支付返回错误: %+v", pay.Pay)
	}

	creates := gateway.created()
	if len(creates) != 1 {
		t.Fatalf("渠道下单次数 = %d，期望 1", len(creates))
	}
	form := creates[0].form
	if form.Get("out_trade_no") != order.TradeNo || form.Get("money") != "180.00" || form.Get("type") != "wxpay" {
		t.Fatalf("渠道下单参数错误: %v", form)
	}
	if form.Get("notify_url") != "https://oem.example.com/api/v1/payments/epay/notify" {
		t.Fatalf("notify_url 未按设置下发: %q", form.Get("notify_url"))
	}

	// 金额不符的回调必须被拒绝。
	mismatch := gateway.notifyValues(order.TradeNo, "179.99")
	if ack := notifyAck(t, engine, mismatch); ack != "fail" {
		t.Fatalf("金额不符应应答 fail，得到 %q", ack)
	}
	if got := getOrderView(t, engine, token, order.ID); got.Status != model.OrderStatusPending {
		t.Fatalf("金额不符后订单状态 = %s，期望 pending", got.Status)
	}

	// 验签失败的回调必须被拒绝。
	bad := gateway.notifyValues(order.TradeNo, "180.00")
	bad.Set("sign", strings.Repeat("0", 32))
	if ack := notifyAck(t, engine, bad); ack != "fail" {
		t.Fatalf("验签失败应应答 fail，得到 %q", ack)
	}

	// 正常回调：订单转 paid + 渠道单号 + 优惠码计数。
	if ack := notifyAck(t, engine, gateway.notifyValues(order.TradeNo, "180.00")); ack != "success" {
		t.Fatalf("回调应答 = %q，期望 success", ack)
	}
	paid := getOrderView(t, engine, token, order.ID)
	if paid.Status != model.OrderStatusPaid || paid.PayChannel != "epay" || paid.PayTime == nil {
		t.Fatalf("订单未正确转 paid: %+v", paid)
	}
	if paid.ChannelTradeNo != "GATEWAY-"+order.TradeNo {
		t.Fatalf("渠道单号 = %q", paid.ChannelTradeNo)
	}
	if got := couponUsedCount(t, gdb, coupon.ID); got != 1 {
		t.Fatalf("优惠码 used_count = %d，期望 1", got)
	}

	// 重复回调：应答 success 且不重复处理（计数不变）。
	if ack := notifyAck(t, engine, gateway.notifyValues(order.TradeNo, "180.00")); ack != "success" {
		t.Fatalf("重复回调应答 = %q，期望 success", ack)
	}
	if got := couponUsedCount(t, gdb, coupon.ID); got != 1 {
		t.Fatalf("重复回调后 used_count = %d，期望 1", got)
	}
}

// TestOrderPayReusesTradeNoOnRepeat 验证重复发起支付沿用同一 out_trade_no（契约 12.2）。
func TestOrderPayReusesTradeNoOnRepeat(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, _ := memberTokenFor(t, engine, "repayuser")
	order := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "annual", "config": map[string]any{},
	})

	for i := 0; i < 2; i++ {
		rec, envelope := payOrder(t, engine, token, order.ID, map[string]any{"channel": "epay"})
		if rec.Code != http.StatusOK || envelope.Code != 0 {
			t.Fatalf("第 %d 次发起支付失败: HTTP %d, body=%s", i+1, rec.Code, rec.Body.String())
		}
	}
	creates := gateway.created()
	if len(creates) != 2 {
		t.Fatalf("渠道下单次数 = %d，期望 2", len(creates))
	}
	if creates[0].form.Get("out_trade_no") != order.TradeNo || creates[1].form.Get("out_trade_no") != order.TradeNo {
		t.Fatalf("重复发起支付未复用订单号: %q / %q", creates[0].form.Get("out_trade_no"), creates[1].form.Get("out_trade_no"))
	}
}

// TestOrderChannelFailures 验证渠道拒绝下单与渠道未配置的错误码。
func TestOrderChannelFailures(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, nil)
	gateway := newFakeEpayGateway(t)
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)
	token, _ := memberTokenFor(t, engine, "failuser")
	order := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "annual", "config": map[string]any{},
	})

	// 渠道未配置 → 40002（明确业务错误，不影响其他功能）。
	rec, envelope := payOrder(t, engine, token, order.ID, map[string]any{"channel": "epay"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("渠道未配置应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(envelope.Message, "支付渠道") {
		t.Fatalf("message = %q", envelope.Message)
	}

	// 渠道拒绝下单 → 50002（渠道故障，带脱敏提示）。
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	gateway.setRejectCode("-1")
	rec, envelope = payOrder(t, engine, token, order.ID, map[string]any{"channel": "epay"})
	if rec.Code != http.StatusInternalServerError || envelope.Code != response.CodePaymentGateway {
		t.Fatalf("渠道拒绝应 50002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(envelope.Message, testEpayKey) {
		t.Fatalf("错误信息泄露商户密钥: %s", envelope.Message)
	}

	// 不支持的 pay_type → 40002。
	gateway.setRejectCode("")
	rec, envelope = payOrder(t, engine, token, order.ID, map[string]any{"channel": "epay", "pay_type": "bank"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("非法 pay_type 应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 未知渠道 → 40002。
	rec, envelope = payOrder(t, engine, token, order.ID, map[string]any{"channel": "stripe"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("未知渠道应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
}

// TestOrderNotifyRejections 验证回调的拒绝分支：找不到单 / 非成功状态 / 前缀不可识别。
func TestOrderNotifyRejections(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")

	// 找不到本地单 → fail。
	if ack := notifyAck(t, engine, gateway.notifyValues("O20990101000000AAAAAA", "10.00")); ack != "fail" {
		t.Fatalf("未知单号应应答 fail，得到 %q", ack)
	}

	// 前缀不可识别 → fail。
	if ack := notifyAck(t, engine, gateway.notifyValues("X20990101000000AAAAAA", "10.00")); ack != "fail" {
		t.Fatalf("未知前缀应应答 fail，得到 %q", ack)
	}

	// trade_status 非成功 → success（不处理，也不是错误）。
	params := map[string]string{
		"pid":          testEpayPID,
		"trade_no":     "GATEWAY-NOTPAID",
		"out_trade_no": "O20990101000000AAAAAA",
		"type":         "alipay",
		"name":         "测试商品",
		"money":        "10.00",
		"trade_status": "WAIT_BUYER_PAY",
	}
	params["sign"] = gateway.signNotify(params)
	params["sign_type"] = "MD5"
	waiting := url.Values{}
	for key, value := range params {
		waiting.Set(key, value)
	}
	if ack := notifyAck(t, engine, waiting); ack != "success" {
		t.Fatalf("非成功状态应应答 success，得到 %q", ack)
	}

	// 渠道不可用（设置被清空）→ fail。
	if err := gdb.Exec("DELETE FROM settings").Error; err != nil {
		t.Fatalf("清空设置失败: %v", err)
	}
	if ack := notifyAck(t, engine, gateway.notifyValues("O20990101000000AAAAAA", "10.00")); ack != "fail" {
		t.Fatalf("渠道不可用应应答 fail，得到 %q", ack)
	}
}

// TestOrderCancelThenNotify 验证取消后收到回调：记 WARN、不处理、仍应答 success。
func TestOrderCancelThenNotify(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)
	coupon := seedCoupon(t, gdb, "CASH20", "fixed", "20", `["annual"]`, nil)

	token, _ := memberTokenFor(t, engine, "canceluser")
	order := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "annual", "config": map[string]any{},
		"coupon_code": coupon.Code,
	})

	rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/orders/"+itoa(order.ID)+"/cancel", token, nil)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("取消失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if cancelled := decodeData[orderView](t, envelope); cancelled.Status != model.OrderStatusCancelled {
		t.Fatalf("取消后状态 = %s", cancelled.Status)
	}

	// 重复取消 → 40002。
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/orders/"+itoa(order.ID)+"/cancel", token, nil)
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("重复取消应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 取消后发起支付 → 40002。
	rec, envelope = payOrder(t, engine, token, order.ID, map[string]any{"channel": "epay"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("已取消订单支付应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 回调到达：应答 success 但不改状态、不计数。
	if ack := notifyAck(t, engine, gateway.notifyValues(order.TradeNo, string(order.FinalAmount))); ack != "success" {
		t.Fatalf("已取消订单回调应应答 success，得到 %q", ack)
	}
	if got := getOrderView(t, engine, token, order.ID); got.Status != model.OrderStatusCancelled {
		t.Fatalf("回调后订单状态 = %s，期望 cancelled", got.Status)
	}
	if got := couponUsedCount(t, gdb, coupon.ID); got != 0 {
		t.Fatalf("取消订单不应计数：used_count = %d", got)
	}
}

// TestOrderCouponOveruseDoesNotBlock 验证极端并发超用不阻断入账（used_count 停在 max_uses）。
func TestOrderCouponOveruseDoesNotBlock(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)
	// max_uses=1：第一单支付占用后，第二单支付时条件自增影响行数为 0（不阻断）。
	coupon := seedCoupon(t, gdb, "ONCE", "fixed", "1", `[]`, func(c *model.Coupon) { c.MaxUses = 1 })

	token, _ := memberTokenFor(t, engine, "overuseuser")
	first := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "annual", "config": map[string]any{}, "coupon_code": coupon.Code,
	})
	second := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "annual", "config": map[string]any{}, "coupon_code": coupon.Code,
	})

	for _, order := range []orderView{first, second} {
		if ack := notifyAck(t, engine, gateway.notifyValues(order.TradeNo, order.FinalAmount)); ack != "success" {
			t.Fatalf("回调应答 = %q，期望 success", ack)
		}
	}
	if got := getOrderView(t, engine, token, second.ID); got.Status != model.OrderStatusPaid {
		t.Fatalf("超用订单仍应入账为 paid，得到 %s", got.Status)
	}
	if got := couponUsedCount(t, gdb, coupon.ID); got != 1 {
		t.Fatalf("used_count = %d，期望停在 max_uses=1", got)
	}
}

// ---------------------------------------------------------------------------
// 余额支付
// ---------------------------------------------------------------------------

// TestOrderPayWithBalance 验证余额支付成功与不足两条路径。
func TestOrderPayWithBalance(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, nil)
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, member := memberTokenFor(t, engine, "balanceuser")
	order := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "annual", "config": map[string]any{},
	})

	// 余额不足 → 40002。
	rec, envelope := payOrder(t, engine, token, order.ID, map[string]any{"channel": "balance"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("余额不足应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 充值 250.00（直接写库，充值链路另有专门用例）。
	setMemberBalance(t, gdb, member.ID, "250.00")
	rec, envelope = payOrder(t, engine, token, order.ID, map[string]any{"channel": "balance"})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("余额支付失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	paid := decodeData[payOrderView](t, envelope)
	if !paid.Pay.Paid || paid.Pay.Channel != model.PayChannelBalance || paid.Pay.BalanceAfter != "50.00" {
		t.Fatalf("余额支付返回错误: %+v", paid.Pay)
	}
	if paid.Order.Status != model.OrderStatusPaid || paid.Order.PayChannel != model.PayChannelBalance {
		t.Fatalf("余额支付后订单错误: %+v", paid.Order)
	}

	// 流水：金额为负、前后余额准确。
	entry := ledgerForRef(t, gdb, member.ID, model.LedgerTypeOrderPay, order.ID)
	if entry.Amount != "-200.00" || entry.BalanceBefore != "250.00" || entry.BalanceAfter != "50.00" {
		t.Fatalf("流水金额错误: %+v", entry)
	}

	// 已支付订单再次发起支付 → 40002。
	rec, envelope = payOrder(t, engine, token, order.ID, map[string]any{"channel": "balance"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("已支付订单支付应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 余额实时接口反映扣款结果。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/finance/balance", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("余额查询失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if balance := decodeData[balanceView](t, envelope); balance.Balance != "50.00" {
		t.Fatalf("余额 = %s，期望 50.00", balance.Balance)
	}
}

// ---------------------------------------------------------------------------
// 列表与越权
// ---------------------------------------------------------------------------

// TestOrderListAndIsolation 验证列表分页、状态过滤与他人订单不可见。
func TestOrderListAndIsolation(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, nil)
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	ownerToken, _ := memberTokenFor(t, engine, "owneruser")
	otherToken, _ := memberTokenFor(t, engine, "otheruser")

	first := createOrder(t, engine, ownerToken, map[string]any{
		"product_id": product.ID, "cycle": "annual", "config": map[string]any{},
	})
	createOrder(t, engine, ownerToken, map[string]any{
		"product_id": product.ID, "cycle": "monthly", "config": map[string]any{},
	})

	// 列表：新建在前 + 分页。
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/orders?page=1&page_size=1", ownerToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("订单列表失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	list := decodeData[orderListView](t, envelope)
	if list.Total != 2 || len(list.Items) != 1 || list.PageSize != 1 {
		t.Fatalf("订单列表错误: %+v", list)
	}
	if list.Items[0].Cycle != pricing.CycleMonthly {
		t.Fatalf("列表未按新建在前排序: %+v", list.Items[0])
	}

	// 状态过滤。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/orders?status=paid", ownerToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态过滤失败: HTTP %d", rec.Code)
	}
	if filtered := decodeData[orderListView](t, envelope); filtered.Total != 0 {
		t.Fatalf("paid 过滤应无结果: %+v", filtered)
	}
	rec, _ = doAPI(t, engine, http.MethodGet, "/api/v1/orders?status=unknown", ownerToken, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 status 应 400：HTTP %d", rec.Code)
	}

	// 他人订单：详情 / 支付 / 取消一律 404。
	for _, path := range []string{"/api/v1/orders/" + itoa(first.ID), "/api/v1/orders/" + itoa(first.ID) + "/pay",
		"/api/v1/orders/" + itoa(first.ID) + "/cancel"} {
		method := http.MethodGet
		if strings.HasSuffix(path, "/pay") || strings.HasSuffix(path, "/cancel") {
			method = http.MethodPost
		}
		rec, envelope = doAPI(t, engine, method, path, otherToken, map[string]any{"channel": "balance"})
		if rec.Code != http.StatusNotFound || envelope.Code != response.CodeNotFound {
			t.Fatalf("%s 他人订单应 404：HTTP %d, body=%s", path, rec.Code, rec.Body.String())
		}
	}

	// 未登录 → 401。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/orders", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应 401：HTTP %d", rec.Code)
	}
}

// TestPaymentLogsNeverContainSecrets 验证服务端日志不含任何密钥明文（契约 12.1）。
func TestPaymentLogsNeverContainSecrets(t *testing.T) {
	gdb := testDatabase(t)
	var logs bytes.Buffer
	engine := newStage4Engine(t, gdb, &logs)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, _ := memberTokenFor(t, engine, "loguser")
	order := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "annual", "config": map[string]any{},
	})
	if _, envelope := payOrder(t, engine, token, order.ID, map[string]any{"channel": "epay"}); envelope.Code != 0 {
		t.Fatalf("发起支付失败: %s", toJSON(t, envelope))
	}
	notifyAck(t, engine, gateway.notifyValues(order.TradeNo, order.FinalAmount))

	output := logs.String()
	if strings.Contains(output, testEpayKey) {
		t.Fatal("日志包含商户密钥明文")
	}
	if strings.Contains(output, testUpstreamKey) {
		t.Fatal("日志包含上游密钥明文")
	}
}

// TestSettingsRawValueNotLeakedInOrderFlows 验证订单/支付接口响应里不含密钥（防御性断言）。
func TestSettingsRawValueNotLeakedInOrderFlows(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, _ := memberTokenFor(t, engine, "leakuser")
	order := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "annual", "config": map[string]any{},
	})
	rec, envelope := payOrder(t, engine, token, order.ID, map[string]any{"channel": "epay"})
	if rec.Code != http.StatusOK {
		t.Fatalf("发起支付失败: %s", rec.Body.String())
	}
	if body := rec.Body.String(); strings.Contains(body, testEpayKey) {
		t.Fatalf("支付响应泄露商户密钥: %s", body)
	}
	if raw := toJSON(t, envelope); strings.Contains(raw, testEpayKey) {
		t.Fatal("响应包泄露商户密钥")
	}
}

// ---------------------------------------------------------------------------
// 小工具
// ---------------------------------------------------------------------------

// couponUsedCount 直接读库取优惠码已用次数。
func couponUsedCount(t *testing.T, gdb *gorm.DB, couponID uint64) int {
	t.Helper()
	var coupon model.Coupon
	if err := gdb.First(&coupon, couponID).Error; err != nil {
		t.Fatalf("读取优惠码失败: %v", err)
	}
	return coupon.UsedCount
}

// setMemberBalance 直接写库设置会员余额。
func setMemberBalance(t *testing.T, gdb *gorm.DB, memberID uint64, balance string) {
	t.Helper()
	if err := gdb.Model(&model.Member{}).Where("id = ?", memberID).
		Update("balance", balance).Error; err != nil {
		t.Fatalf("设置余额失败: %v", err)
	}
}

// ledgerForRef 读库取指定关联单据的流水。
func ledgerForRef(t *testing.T, gdb *gorm.DB, memberID uint64, kind string, refID uint64) model.Ledger {
	t.Helper()
	var entry model.Ledger
	if err := gdb.Where("member_id = ? AND type = ? AND ref_id = ?", memberID, kind, refID).
		Take(&entry).Error; err != nil {
		t.Fatalf("读取流水失败: %v", err)
	}
	return entry
}
