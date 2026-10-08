package router

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
)

// ---------------------------------------------------------------------------
// 阶段 5b：续费链路（下单 → 支付 → RenewHost → 到期顺延 → 幂等 / 失败重试 / 恢复）
// ---------------------------------------------------------------------------

// TestRenewOrderAndDelivery 跑通续费核心链路并核对上游参数与到期时间顺延。
func TestRenewOrderAndDelivery(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, member := memberTokenFor(t, engine, "renewuser")
	instance := newInstanceFor(t, engine, gateway, gdb, token, product.ID)
	dueBefore := instanceFromDB(t, gdb, instance.ID).NextDueDate
	if dueBefore == nil {
		t.Fatal("开通后应有到期时间")
	}

	// 续费下单：金额按当前商品该周期售价；type=renew + instance_id 落库；不接受优惠码字段。
	rec, envelope := doAPI(t, engine, http.MethodPost,
		"/api/v1/instances/"+itoa(instance.ID)+"/renew", token,
		map[string]any{"cycle": "monthly", "coupon_code": "IGNORED"})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("续费下单失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	order := decodeData[orderView](t, envelope)
	if order.Type != model.OrderTypeRenew || order.InstanceID == nil || *order.InstanceID != instance.ID {
		t.Fatalf("续费单字段错误: type=%s instance_id=%v", order.Type, order.InstanceID)
	}
	if order.FinalAmount != "100.00" || order.DiscountAmount != "0.00" {
		t.Fatalf("续费金额 = %s（折扣 %s），期望 100.00/0.00", order.FinalAmount, order.DiscountAmount)
	}
	if order.Status != model.OrderStatusPending {
		t.Fatalf("续费单初始状态 = %s，期望 pending", order.Status)
	}

	// 支付 → 自动续费交付。
	updated := payOrderByEpayNotify(t, engine, gateway, token, order)
	if updated.Status != model.OrderStatusActive {
		t.Fatalf("续费交付未完成: status=%s error=%s", updated.Status, updated.ProvisionError)
	}
	if updated.HostID == nil || *updated.HostID != instance.HostID {
		t.Fatalf("续费单 host_id = %v，期望实例 host_id %d", updated.HostID, instance.HostID)
	}

	form := host.lastForm(pathHostRenew)
	if form.Get("hostid") != itoa(uint64(instance.HostID)) || form.Get("billingcycles") != "monthly" {
		t.Fatalf("上游续费参数错误: hostid=%q billingcycles=%q", form.Get("hostid"), form.Get("billingcycles"))
	}
	if got := host.callCount(pathHostRenew); got != 1 {
		t.Fatalf("上游续费次数 = %d，期望 1", got)
	}

	// 到期时间顺延一个周期（假上游按月顺延，本地应与回读一致）。
	wantDue := time.Unix(dueBefore.Unix(), 0).UTC().AddDate(0, 1, 0)
	after := instanceFromDB(t, gdb, instance.ID)
	if after.NextDueDate == nil || !after.NextDueDate.UTC().Equal(wantDue) {
		t.Fatalf("到期时间 = %v，期望 %v", after.NextDueDate, wantDue)
	}
	if after.Status != model.InstanceStatusActive {
		t.Fatalf("续费后实例状态 = %s，期望 active", after.Status)
	}

	// 审计：续费成功（system renew）。
	last := lastInstanceLog(t, gdb, instance.ID)
	if last.Action != model.ActionRenew || last.Status != model.InstanceOpSuccess || last.ActorType != model.ActorTypeSystem {
		t.Fatalf("续费审计异常: %+v", last)
	}

	// 订单列表按类型可区分（会员端视图 type 字段）。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/orders?page=1&page_size=10", token, nil)
	list := decodeData[orderListView](t, envelope)
	var newCount, renewCount int
	for _, item := range list.Items {
		switch item.Type {
		case model.OrderTypeNew:
			newCount++
		case model.OrderTypeRenew:
			renewCount++
		}
	}
	if newCount != 1 || renewCount != 1 {
		t.Fatalf("订单类型分布错误: new=%d renew=%d（共 %d）", newCount, renewCount, list.Total)
	}
	if member.ID == 0 {
		t.Fatal("会员 ID 异常")
	}
}

// TestRenewIdempotentOnDuplicateNotify 验证重复回调不重复续费（到期时间只顺延一次）。
func TestRenewIdempotentOnDuplicateNotify(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, _ := memberTokenFor(t, engine, "renewidem")
	instance := newInstanceFor(t, engine, gateway, gdb, token, product.ID)
	dueBefore := instanceFromDB(t, gdb, instance.ID).NextDueDate

	rec, envelope := doAPI(t, engine, http.MethodPost,
		"/api/v1/instances/"+itoa(instance.ID)+"/renew", token, map[string]any{"cycle": "monthly"})
	if rec.Code != http.StatusOK {
		t.Fatalf("续费下单失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	order := decodeData[orderView](t, envelope)

	// 支付发起 + 三次回调（幂等）。
	if _, envelope := payOrder(t, engine, token, order.ID, map[string]any{"channel": "epay"}); envelope.Code != 0 {
		t.Fatalf("发起支付失败: %+v", envelope)
	}
	notify := gateway.notifyValues(order.TradeNo, order.FinalAmount)
	for i := 0; i < 3; i++ {
		if ack := notifyAck(t, engine, notify); ack != "success" {
			t.Fatalf("第 %d 次回调应答 = %q", i+1, ack)
		}
	}

	if got := host.callCount(pathHostRenew); got != 1 {
		t.Fatalf("上游续费次数 = %d，期望 1（重复回调不得重复续费）", got)
	}
	wantDue := time.Unix(dueBefore.Unix(), 0).UTC().AddDate(0, 1, 0)
	after := instanceFromDB(t, gdb, instance.ID)
	if after.NextDueDate == nil || !after.NextDueDate.UTC().Equal(wantDue) {
		t.Fatalf("到期时间 = %v，期望 %v（只顺延一次）", after.NextDueDate, wantDue)
	}
	if logs := instanceLogsFromDB(t, gdb, instance.ID); len(logs) != 2 { // create + renew
		t.Fatalf("审计条数 = %d，期望 2（不重复续费）: %+v", len(logs), logs)
	}
}

// TestRenewFailureAndAdminRetry 验证续费失败置 failed + 管理员重试同入口恢复。
func TestRenewFailureAndAdminRetry(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)
	admin := stage4AdminToken(t, engine, gdb, "renewretry", model.RoleAdmin)

	token, _ := memberTokenFor(t, engine, "renewretryuser")
	instance := newInstanceFor(t, engine, gateway, gdb, token, product.ID)
	dueBefore := instanceFromDB(t, gdb, instance.ID).NextDueDate

	host.setRenewFail("上游余额不足，续费下单失败")

	rec, envelope := doAPI(t, engine, http.MethodPost,
		"/api/v1/instances/"+itoa(instance.ID)+"/renew", token, map[string]any{"cycle": "monthly"})
	if rec.Code != http.StatusOK {
		t.Fatalf("续费下单失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	order := decodeData[orderView](t, envelope)

	updated := payOrderByEpayNotify(t, engine, gateway, token, order)
	if updated.Status != model.OrderStatusFailed {
		t.Fatalf("上游续费失败后订单状态 = %s，期望 failed", updated.Status)
	}
	if !strings.Contains(updated.ProvisionError, "余额不足") {
		t.Fatalf("provision_error = %q，期望包含上游原因", updated.ProvisionError)
	}
	if got := instanceFromDB(t, gdb, instance.ID); !got.NextDueDate.UTC().Equal(dueBefore.UTC()) {
		t.Fatalf("续费失败不应改变到期时间: %v != %v", got.NextDueDate, dueBefore)
	}

	// 管理员重试（上游恢复）→ 成功顺延。
	host.setRenewFail("")
	rec, envelope = retryDelivery(t, engine, admin, order.ID)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("重试失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	retried := decodeData[orderView](t, envelope)
	if retried.Status != model.OrderStatusActive || retried.ProvisionError != "" {
		t.Fatalf("重试后订单 = %+v，期望 active", retried)
	}
	wantDue := time.Unix(dueBefore.Unix(), 0).UTC().AddDate(0, 1, 0)
	if got := instanceFromDB(t, gdb, instance.ID); got.NextDueDate == nil || !got.NextDueDate.UTC().Equal(wantDue) {
		t.Fatalf("重试成功后到期时间 = %v，期望 %v", got.NextDueDate, wantDue)
	}
}

// TestRenewFromSuspendedResumes 验证 suspended 实例可续费，且续费成功后自动恢复（Unsuspend）。
func TestRenewFromSuspendedResumes(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)
	admin := stage4AdminToken(t, engine, gdb, "renewsus", model.RoleAdmin)

	token, _ := memberTokenFor(t, engine, "renewsususer")
	instance := newInstanceFor(t, engine, gateway, gdb, token, product.ID)

	// 管理员暂停（上游 + 本地同步暂停）。
	rec, envelope := doAPI(t, engine, http.MethodPost,
		"/api/v1/admin/instances/"+itoa(instance.ID)+"/suspend", admin, map[string]any{"reason": "到期未续费"})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("暂停失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if !host.hostState(t).suspended {
		t.Fatal("假上游未进入暂停态")
	}

	// suspended 实例可续费。
	rec, envelope = doAPI(t, engine, http.MethodPost,
		"/api/v1/instances/"+itoa(instance.ID)+"/renew", token, map[string]any{"cycle": "monthly"})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("suspended 实例续费下单失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	order := decodeData[orderView](t, envelope)

	updated := payOrderByEpayNotify(t, engine, gateway, token, order)
	if updated.Status != model.OrderStatusActive {
		t.Fatalf("续费交付未完成: %s（%s）", updated.Status, updated.ProvisionError)
	}

	// 上游续费成功会自行解除暂停（生产实测行为）：回读已是 Active，
	// 本地直接收敛为 active，**不应再调 unsuspend**（调了会被上游以「不能解除该暂停」拒绝）。
	if got := host.funcCallCount("unsuspend"); got != 0 {
		t.Fatalf("上游已自动解除暂停时不应再调 unsuspend，实际调用 %d 次", got)
	}
	if state := host.hostState(t); state.suspended {
		t.Fatal("假上游续费后应已自动解除暂停")
	}
	if got := instanceFromDB(t, gdb, instance.ID); got.Status != model.InstanceStatusActive {
		t.Fatalf("续费恢复后本地状态 = %s，期望 active", got.Status)
	}

	// 审计：续费 success + 恢复 success。
	logs := instanceLogsFromDB(t, gdb, instance.ID)
	actions := map[string]int{}
	for _, lg := range logs {
		actions[lg.Action]++
	}
	// create 1 + suspend 1（管理员）+ renew 1 + unsuspend 1（续费后自动恢复）。
	if actions[model.ActionRenew] != 1 || actions[model.ActionUnsuspend] != 1 || actions[model.ActionSuspend] != 1 {
		t.Fatalf("审计动作分布异常: %+v（全部 %+v）", actions, logs)
	}
}

// TestRenewResumesUpstreamWhenStillSuspended 验证「续费后上游仍暂停」的分支：
// 回读为 Suspended 时由交付链路主动调 Unsuspend，成功后本地回到 active。
func TestRenewResumesUpstreamWhenStillSuspended(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)
	admin := stage4AdminToken(t, engine, gdb, "renewstill", model.RoleAdmin)

	host.setAutoUnsuspendOnRenew(false) // 模拟：续费后上游仍保持暂停

	token, _ := memberTokenFor(t, engine, "renewstilluser")
	instance := newInstanceFor(t, engine, gateway, gdb, token, product.ID)

	rec, envelope := doAPI(t, engine, http.MethodPost,
		"/api/v1/admin/instances/"+itoa(instance.ID)+"/suspend", admin, map[string]any{"reason": "到期未续费"})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("暂停失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	unsuspendBefore := host.funcCallCount("unsuspend")

	rec, envelope = doAPI(t, engine, http.MethodPost,
		"/api/v1/instances/"+itoa(instance.ID)+"/renew", token, map[string]any{"cycle": "monthly"})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("续费下单失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	order := decodeData[orderView](t, envelope)
	updated := payOrderByEpayNotify(t, engine, gateway, token, order)
	if updated.Status != model.OrderStatusActive {
		t.Fatalf("续费交付未完成: %s（%s）", updated.Status, updated.ProvisionError)
	}

	if got := host.funcCallCount("unsuspend"); got != unsuspendBefore+1 {
		t.Fatalf("上游仍暂停时应调用 unsuspend 一次，实际 %d（前值 %d）", got, unsuspendBefore)
	}
	if state := host.hostState(t); state.suspended {
		t.Fatal("unsuspend 成功后假上游应已解除暂停")
	}
	if got := instanceFromDB(t, gdb, instance.ID); got.Status != model.InstanceStatusActive {
		t.Fatalf("本地状态 = %s，期望 active", got.Status)
	}
}

// TestRenewGuardsAndPermissions 验证续费下单的状态守卫、隔离与参数校验。
func TestRenewGuardsAndPermissions(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	tokenA, _ := memberTokenFor(t, engine, "renewguarda")
	tokenB, _ := memberTokenFor(t, engine, "renewguardb")
	instance := newInstanceFor(t, engine, gateway, gdb, tokenA, product.ID)
	path := "/api/v1/instances/" + itoa(instance.ID) + "/renew"

	// 他人实例 → 404；无凭证 → 401。
	rec, envelope := doAPI(t, engine, http.MethodPost, path, tokenB, map[string]any{"cycle": "monthly"})
	if rec.Code != http.StatusNotFound || envelope.Code != response.CodeNotFound {
		t.Fatalf("跨会员续费应 404：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	rec, _ = doAPI(t, engine, http.MethodPost, path, "", map[string]any{"cycle": "monthly"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("无凭证应 401，得到 %d", rec.Code)
	}

	// cycle 非法 → 40001；不可售周期（该商品无 quarterly 售价）→ 40002。
	rec, envelope = doAPI(t, engine, http.MethodPost, path, tokenA, map[string]any{"cycle": "weekly"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("非法 cycle 应 40001：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	rec, envelope = doAPI(t, engine, http.MethodPost, path, tokenA, map[string]any{"cycle": "quarterly"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("不可售周期应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// terminated 实例不可续费 → 40002。
	setInstanceStatus(t, gdb, instance.ID, model.InstanceStatusTerminated)
	rec, envelope = doAPI(t, engine, http.MethodPost, path, tokenA, map[string]any{"cycle": "monthly"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("terminated 实例续费应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
}
