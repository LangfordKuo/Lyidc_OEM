package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
)

// 优惠码「下单即原子占用 + 取消释放」的集成用例（P4 安全整改 finding#09，契约 12.5）。
//
// 与 order_integration_test.go 的分工：那边覆盖「下单占用失败即拒绝」这条主链路，
// 本文件覆盖并发抢占、支付不再自增、取消释放、历史遗留单兼容与 max_uses=0 不回归。

// ---------------------------------------------------------------------------
// 并发：两个请求抢同一张 max_uses=1 的券
// ---------------------------------------------------------------------------

// TestOrderCouponConcurrentCreationOnlyOneWins 验证并发口径（契约 12.5 第 5 条）：
// 两个请求同时对同一张 max_uses=1 的券下单，只允许 1 单创建成功，另一个 40002。
//
// 这是本批的硬要求——实现必须靠**条件自增**（`WHERE … used_count < max_uses`）在行锁下完成，
// 「先 SELECT 再 UPDATE」会让两个请求都读到 used_count=0 而双双放行。
func TestOrderCouponConcurrentCreationOnlyOneWins(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)
	coupon := seedCoupon(t, gdb, "RACE1", "percent", "100", `[]`, func(c *model.Coupon) { c.MaxUses = 1 })

	token, _ := memberTokenFor(t, engine, "raceuser")
	body := map[string]any{
		"product_id": product.ID, "cycle": "annual", "config": map[string]any{}, "coupon_code": coupon.Code,
	}

	const racers = 2
	codes := make([]int, racers)
	httpStatus := make([]int, racers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // 尽量让两个请求同时进入下单逻辑
			httpStatus[i], codes[i] = postOrderRaw(t, engine, token, body)
		}(i)
	}
	close(start)
	wg.Wait()

	succeeded := 0
	for i := 0; i < racers; i++ {
		switch {
		case httpStatus[i] == http.StatusOK && codes[i] == response.CodeSuccess:
			succeeded++
		case httpStatus[i] == http.StatusBadRequest && codes[i] == response.CodeValidationFailed:
		default:
			t.Fatalf("第 %d 个并发请求返回意外结果：HTTP %d, code=%d", i, httpStatus[i], codes[i])
		}
	}
	if succeeded != 1 {
		t.Fatalf("并发下单成功数 = %d，期望恰好 1（另一个应 40002）", succeeded)
	}
	if got := orderCount(t, gdb); got != 1 {
		t.Fatalf("订单行数 = %d，期望 1（被拒的那单不落库）", got)
	}
	if got := couponUsedCount(t, gdb, coupon.ID); got != 1 {
		t.Fatalf("used_count = %d，期望 1（只占用一次）", got)
	}
}

// postOrderRaw 是并发用例专用的下单请求：直接发 HTTP，**不在非测试 goroutine 里调用 t.Fatalf**
// （t.Fatalf 只能由测试 goroutine 调用），失败信息通过返回值交给调用方断言。
func postOrderRaw(t *testing.T, engine http.Handler, token string, body map[string]any) (int, int) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Errorf("序列化请求体失败: %v", err)
		return 0, 0
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	var envelope apiEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Errorf("响应不是合法 JSON: %v (body=%s)", err, rec.Body.String())
		return rec.Code, 0
	}
	return rec.Code, envelope.Code
}

// ---------------------------------------------------------------------------
// 支付成功不再自增
// ---------------------------------------------------------------------------

// TestOrderCouponPaidDoesNotIncrement 验证 P4 口径：占用在下单时完成，支付成功**不再自增**，
// 重复发起余额支付也只消耗一次（幂等锚点是订单状态，不是优惠码计数）。
func TestOrderCouponPaidDoesNotIncrement(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)
	// 100% 券 + max_uses=1：渗透实测里「1 次上限成交多单」的形态。
	coupon := seedCoupon(t, gdb, "FREE100", "percent", "100", `[]`, func(c *model.Coupon) { c.MaxUses = 1 })

	token, member := memberTokenFor(t, engine, "p4norecount")
	order := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "annual", "config": map[string]any{}, "coupon_code": coupon.Code,
	})
	if order.FinalAmount != "0.00" {
		t.Fatalf("100%% 券的应付金额 = %s，期望 0.00", order.FinalAmount)
	}
	if got := couponUsedCount(t, gdb, coupon.ID); got != 1 {
		t.Fatalf("下单后 used_count = %d，期望 1", got)
	}

	// 余额支付 0 元单：入账成功、计数不变。
	setMemberBalance(t, gdb, member.ID, "0.00")
	rec, envelope := payOrder(t, engine, token, order.ID, map[string]any{"channel": "balance"})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("0 元单余额支付失败：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if paid := decodeData[payOrderView](t, envelope); paid.Order.Status != model.OrderStatusPaid {
		t.Fatalf("支付后订单状态 = %s，期望 paid", paid.Order.Status)
	}
	if got := couponUsedCount(t, gdb, coupon.ID); got != 1 {
		t.Fatalf("支付后 used_count = %d，期望仍为 1（支付不再自增）", got)
	}

	// 重复发起余额支付：40002 幂等提示，计数仍然不变。
	rec, envelope = payOrder(t, engine, token, order.ID, map[string]any{"channel": "balance"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("重复支付应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := couponUsedCount(t, gdb, coupon.ID); got != 1 {
		t.Fatalf("重复支付后 used_count = %d，期望仍为 1", got)
	}
}

// ---------------------------------------------------------------------------
// 取消释放
// ---------------------------------------------------------------------------

// TestOrderCancelReleasesCouponUsage 验证取消 pending 订单会在同一事务内释放占用
// （used_count 减 1、coupon_counted 置 0），释放后券重新可用。
//
// 已支付/已开通的订单不可取消（状态机拒绝），因此「成交即消耗」不会被动摇。
func TestOrderCancelReleasesCouponUsage(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)
	coupon := seedCoupon(t, gdb, "RELEASE1", "fixed", "20", `["annual"]`, func(c *model.Coupon) { c.MaxUses = 1 })

	token, member := memberTokenFor(t, engine, "releaseuser")
	order := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "annual", "config": map[string]any{}, "coupon_code": coupon.Code,
	})
	if got := couponUsedCount(t, gdb, coupon.ID); got != 1 {
		t.Fatalf("下单后 used_count = %d，期望 1", got)
	}

	// 占用期间第 2 单被拒。
	rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/orders", token, map[string]any{
		"product_id": product.ID, "cycle": "annual", "config": map[string]any{}, "coupon_code": coupon.Code,
	})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("占用期间第 2 单应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 取消 → 释放。
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/orders/"+itoa(order.ID)+"/cancel", token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("取消失败：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := couponUsedCount(t, gdb, coupon.ID); got != 0 {
		t.Fatalf("取消后 used_count = %d，期望 0（释放）", got)
	}
	if orderCouponCounted(t, gdb, order.ID) {
		t.Fatalf("取消后订单 coupon_counted 应为 0")
	}

	// 释放后可以再次下单并支付成功。
	again := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "annual", "config": map[string]any{}, "coupon_code": coupon.Code,
	})
	if got := couponUsedCount(t, gdb, coupon.ID); got != 1 {
		t.Fatalf("再次下单后 used_count = %d，期望 1", got)
	}
	setMemberBalance(t, gdb, member.ID, "1000.00")
	rec, envelope = payOrder(t, engine, token, again.ID, map[string]any{"channel": "balance"})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("再次下单后支付失败：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := couponUsedCount(t, gdb, coupon.ID); got != 1 {
		t.Fatalf("支付后 used_count = %d，期望仍为 1", got)
	}

	// 已支付订单不可取消（状态机拒绝），占用因此不会被误释放。
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/orders/"+itoa(again.ID)+"/cancel", token, nil)
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("已支付订单取消应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := couponUsedCount(t, gdb, coupon.ID); got != 1 {
		t.Fatalf("已支付订单不可取消：used_count = %d，期望 1", got)
	}
}

// ---------------------------------------------------------------------------
// 历史遗留单（迁移 0020 之前下单，coupon_counted 仍为 0）
// ---------------------------------------------------------------------------

// TestOrderLegacyCouponCountedOnPayment 验证迁移前下单的历史 pending 单在支付时按老口径补记占用：
// 券未用尽 → 补记一次并置标记；券已用尽 → 拒绝入账、订单保持 pending、余额不动。
//
// 「历史遗留」在库里的表现就是 coupon_counted=0（迁移刻意不回填），因此用例直接改库构造。
func TestOrderLegacyCouponCountedOnPayment(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)
	coupon := seedCoupon(t, gdb, "LEGACY1", "fixed", "20", `["annual"]`, func(c *model.Coupon) { c.MaxUses = 5 })

	t.Run("券未用尽：支付时补记一次并置标记", func(t *testing.T) {
		token, member := memberTokenFor(t, engine, "legacyok")
		order := createOrder(t, engine, token, map[string]any{
			"product_id": product.ID, "cycle": "annual", "config": map[string]any{}, "coupon_code": coupon.Code,
		})
		// 退化成迁移前的历史行：清掉占用标记并把券计数还原（模拟「占用从未发生」）。
		setOrderCouponCounted(t, gdb, order.ID, false)
		setCouponUsedCount(t, gdb, coupon.ID, 0)

		setMemberBalance(t, gdb, member.ID, "500.00")
		rec, envelope := payOrder(t, engine, token, order.ID, map[string]any{"channel": "balance"})
		if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
			t.Fatalf("历史单支付失败：HTTP %d, body=%s", rec.Code, rec.Body.String())
		}
		if got := couponUsedCount(t, gdb, coupon.ID); got != 1 {
			t.Fatalf("历史单支付后 used_count = %d，期望 1（补记一次）", got)
		}
		if !orderCouponCounted(t, gdb, order.ID) {
			t.Fatalf("历史单支付后 coupon_counted 应为 1")
		}
		if got := getOrderView(t, engine, token, order.ID); got.Status != model.OrderStatusPaid {
			t.Fatalf("历史单状态 = %s，期望 paid", got.Status)
		}
		// 余额按实付金额扣减（200.00 − 20.00 = 180.00）。
		if balance := memberBalance(t, gdb, member.ID); balance != "320.00" {
			t.Fatalf("历史单支付后余额 = %s，期望 320.00", balance)
		}
	})

	t.Run("券已用尽：拒绝入账且订单仍 pending", func(t *testing.T) {
		token, member := memberTokenFor(t, engine, "legacyfull")
		order := createOrder(t, engine, token, map[string]any{
			"product_id": product.ID, "cycle": "annual", "config": map[string]any{}, "coupon_code": coupon.Code,
		})
		// 历史行 + 券被顶到上限（max_uses=5，used_count 也是 5）。
		setOrderCouponCounted(t, gdb, order.ID, false)
		setCouponUsedCount(t, gdb, coupon.ID, 5)

		setMemberBalance(t, gdb, member.ID, "500.00")
		rec, envelope := payOrder(t, engine, token, order.ID, map[string]any{"channel": "balance"})
		if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
			t.Fatalf("历史单券已用尽应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(envelope.Message, "优惠码使用次数已用尽") {
			t.Fatalf("message = %q", envelope.Message)
		}
		// 事务整体回滚：订单仍 pending、未扣余额、计数未变。
		if got := getOrderView(t, engine, token, order.ID); got.Status != model.OrderStatusPending {
			t.Fatalf("拒绝入账后订单状态 = %s，期望 pending", got.Status)
		}
		if balance := memberBalance(t, gdb, member.ID); balance != "500.00" {
			t.Fatalf("拒绝入账后余额 = %s，期望 500.00（不扣款）", balance)
		}
		if got := couponUsedCount(t, gdb, coupon.ID); got != 5 {
			t.Fatalf("拒绝入账后 used_count = %d，期望 5", got)
		}
		if orderCouponCounted(t, gdb, order.ID) {
			t.Fatalf("拒绝入账后 coupon_counted 应仍为 0")
		}
	})
}

// ---------------------------------------------------------------------------
// 不回归：max_uses=0（不限）与未用码
// ---------------------------------------------------------------------------

// TestOrderCouponUnlimitedAndNoCouponUnaffected 验证 max_uses=0（不限次数）与未用码的路径不受影响：
// 不限次数的券可以连续下单（每次都占用一次、计数递增），未用码的订单不写占用标记。
func TestOrderCouponUnlimitedAndNoCouponUnaffected(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)
	coupon := seedCoupon(t, gdb, "UNLIMITED", "fixed", "20", `["annual"]`, nil) // max_uses 默认 0 = 不限

	token, _ := memberTokenFor(t, engine, "unlimiteduser")
	for i := 1; i <= 3; i++ {
		order := createOrder(t, engine, token, map[string]any{
			"product_id": product.ID, "cycle": "annual", "config": map[string]any{}, "coupon_code": coupon.Code,
		})
		if got := couponUsedCount(t, gdb, coupon.ID); got != i {
			t.Fatalf("第 %d 单后 used_count = %d，期望 %d", i, got, i)
		}
		if !orderCouponCounted(t, gdb, order.ID) {
			t.Fatalf("第 %d 单的 coupon_counted 应为 1", i)
		}
	}

	// 未用码：不占用、不写标记。
	plain := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "annual", "config": map[string]any{},
	})
	if orderCouponCounted(t, gdb, plain.ID) {
		t.Fatalf("未用码的订单 coupon_counted 应为 0")
	}
	if got := couponUsedCount(t, gdb, coupon.ID); got != 3 {
		t.Fatalf("未用码下单后 used_count = %d，期望仍为 3", got)
	}
}
