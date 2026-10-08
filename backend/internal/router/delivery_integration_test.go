package router

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/delivery"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
)

// ---------------------------------------------------------------------------
// 交付链路（阶段 5a）：在线支付/余额支付 → 自动开通 → 实例落库 → 会员/管理端查询
// ---------------------------------------------------------------------------

// TestDeliveryAfterOnlinePay 跑通核心链路：在线支付回调 → 自动开通 → 实例落库 →
// 订单 active + host_id，并核对提交上游的开通参数与会员端实例接口（含敏感字段可见性）。
func TestDeliveryAfterOnlinePay(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, member := memberTokenFor(t, engine, "deliveryuser")
	order := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "monthly", "config": map[string]any{"11": 111},
	})

	updated := payOrderByEpayNotify(t, engine, gateway, token, order)
	if updated.Status != model.OrderStatusActive {
		t.Fatalf("交付后订单状态 = %s，期望 active（provision_error=%q）", updated.Status, updated.ProvisionError)
	}
	if updated.HostID == nil || *updated.HostID != host.hostIDValue() {
		t.Fatalf("订单 host_id = %v，期望 %d", updated.HostID, host.hostIDValue())
	}
	if updated.DeliveredAt == nil || updated.ProvisionError != "" {
		t.Fatalf("交付时间/错误字段异常: delivered_at=%v provision_error=%q", updated.DeliveredAt, updated.ProvisionError)
	}

	// 上游开通参数：pid / billingcycle / host / password / qty / configoption 全部按快照拼装。
	form := host.lastForm(pathCartAdd)
	if form.Get("pid") != strconv.Itoa(product.UpstreamPID) {
		t.Fatalf("add_to_shop pid = %q，期望 %d", form.Get("pid"), product.UpstreamPID)
	}
	if form.Get("billingcycle") != "monthly" || form.Get("qty") != "1" {
		t.Fatalf("add_to_shop 周期/数量错误: billingcycle=%q qty=%q", form.Get("billingcycle"), form.Get("qty"))
	}
	hostname := form.Get("host")
	if !strings.HasPrefix(hostname, "oem-") || hostname != strings.ToLower(hostname) {
		t.Fatalf("主机名 = %q，期望 oem- 前缀且全小写", hostname)
	}
	if wantHost := delivery.HostnameFor(order.TradeNo); hostname != wantHost {
		t.Fatalf("主机名 = %q，期望按订单号派生 %q", hostname, wantHost)
	}
	if form.Get("password") == "" {
		t.Fatalf("未向上游提交主机密码")
	}
	if form.Get("configoption[11]") != "111" {
		t.Fatalf("configoption[11] = %q，期望 111", form.Get("configoption[11]"))
	}
	if settle := host.lastForm(pathCartSettle); settle.Get("cart_data[configoptions][11]") != "111" {
		t.Fatalf("settle 未带配置项: %v", settle)
	}

	// 实例落库：订单快照 + 上游回读字段。
	instance := instanceByOrderID(t, gdb, order.ID)
	if instance.MemberID != member.ID || instance.HostID != host.hostIDValue() ||
		instance.ProductName != "阶段4测试商品" || instance.BillingCycle != "monthly" {
		t.Fatalf("实例落库字段错误: %+v", instance)
	}
	if instance.Status != model.InstanceStatusActive || instance.UpstreamStatus != "Active" {
		t.Fatalf("实例状态错误: %+v", instance)
	}
	wantDue := time.Unix(host.nextDueUnix, 0).UTC().Format(time.RFC3339)
	if instance.NextDueDate == nil || instance.NextDueDate.UTC().Format(time.RFC3339) != wantDue {
		t.Fatalf("到期时间 = %v，期望 %s", instance.NextDueDate, wantDue)
	}
	if instance.DedicatedIP != "203.0.113.10" || instance.AssignedIPs != "203.0.113.11" || instance.Port != 22022 {
		t.Fatalf("上游同步字段错误: %+v", instance)
	}
	if instance.Username != "root" || instance.Password != "UpstreamPass123" {
		t.Fatalf("上游账号字段错误: username=%q password=%q", instance.Username, instance.Password)
	}

	// 会员端列表：仅本人 + 不含敏感字段。
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/instances", token, nil)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("实例列表失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "password") || strings.Contains(rec.Body.String(), "username") {
		t.Fatalf("实例列表泄露敏感字段: %s", rec.Body.String())
	}
	list := decodeData[instanceListView](t, envelope)
	if len(list.Items) != 1 || list.Items[0].HostID != host.hostIDValue() || list.Total != 1 {
		t.Fatalf("实例列表错误: %+v", list)
	}
	if list.Items[0].NextDueDate == nil || *list.Items[0].NextDueDate != wantDue {
		t.Fatalf("列表到期时间 = %v，期望 %s", list.Items[0].NextDueDate, wantDue)
	}

	// 会员端详情：本人可见敏感字段。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/instances/"+itoa(instance.ID), token, nil)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("实例详情失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	detail := decodeData[instanceDetailView](t, envelope)
	if detail.Password != "UpstreamPass123" || detail.Username != "root" || detail.Port != 22022 {
		t.Fatalf("详情敏感字段错误: %+v", detail)
	}
	if len(detail.AssignedIPs) != 1 || detail.AssignedIPs[0] != "203.0.113.11" {
		t.Fatalf("详情附加 IP 错误: %v", detail.AssignedIPs)
	}
}

// TestDeliveryIdempotentOnDuplicateNotify 验证重复回调不重复开通（幂等）。
func TestDeliveryIdempotentOnDuplicateNotify(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, _ := memberTokenFor(t, engine, "idempotentuser")
	order := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "monthly", "config": map[string]any{},
	})

	notify := gateway.notifyValues(order.TradeNo, order.FinalAmount)
	for i := 0; i < 2; i++ {
		if ack := notifyAck(t, engine, notify); ack != "success" {
			t.Fatalf("第 %d 次回调应答 = %q，期望 success", i+1, ack)
		}
	}

	if got := host.callCount(pathCartSettle); got != 1 {
		t.Fatalf("上游下单次数 = %d，期望 1（重复回调不得重复开通）", got)
	}
	if got := host.callCount(pathApplyCred); got != 1 {
		t.Fatalf("上游支付次数 = %d，期望 1", got)
	}
	instance := instanceByOrderID(t, gdb, order.ID)
	if instance.HostID != host.hostIDValue() {
		t.Fatalf("实例 host_id = %d，期望 %d", instance.HostID, host.hostIDValue())
	}

	var count int64
	if err := gdb.Model(&model.Instance{}).Where("order_id = ?", order.ID).Count(&count).Error; err != nil {
		t.Fatalf("统计实例失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("实例数 = %d，期望 1", count)
	}
	if current := orderFromDB(t, gdb, order.ID); current.Status != model.OrderStatusActive {
		t.Fatalf("订单状态 = %s，期望 active", current.Status)
	}
}

// TestDeliveryAfterBalancePay 验证余额支付成功后同样触发自动交付。
func TestDeliveryAfterBalancePay(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, _ := memberTokenFor(t, engine, "balanceuser")
	recharge := createRechargeOK(t, engine, token, "100.00")
	if ack := notifyAck(t, engine, gateway.notifyValues(recharge.Recharge.TradeNo, "100.00")); ack != "success" {
		t.Fatalf("充值回调应答 = %q，期望 success", ack)
	}

	order := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "monthly", "config": map[string]any{"11": 112},
	})
	rec, envelope := payOrder(t, engine, token, order.ID, map[string]any{"channel": "balance"})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("余额支付失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 同步交付模式下：余额支付响应返回时交付已完成。
	current := orderFromDB(t, gdb, order.ID)
	if current.Status != model.OrderStatusActive || current.HostID == nil {
		t.Fatalf("余额支付后交付未完成: status=%s host_id=%v provision_error=%q",
			current.Status, current.HostID, current.ProvisionError)
	}
	if current.PayChannel != model.PayChannelBalance {
		t.Fatalf("支付渠道 = %s，期望 balance", current.PayChannel)
	}
	if instance := instanceByOrderID(t, gdb, order.ID); instance.HostID != *current.HostID {
		t.Fatalf("实例 host_id 与订单不一致: %d / %d", instance.HostID, *current.HostID)
	}
}

// TestDeliveryFailureAndAdminRetry 验证交付失败落库、管理员重试与重试成功后的状态流转。
func TestDeliveryFailureAndAdminRetry(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)
	admin := stage4AdminToken(t, engine, gdb, "retry", model.RoleAdmin)

	host.setFailApplyCredit(true) // 上游余额不足：apply_credit 返回 status=200

	token, _ := memberTokenFor(t, engine, "retryuser")
	order := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "monthly", "config": map[string]any{},
	})
	updated := payOrderByEpayNotify(t, engine, gateway, token, order)
	if updated.Status != model.OrderStatusFailed {
		t.Fatalf("上游失败后订单状态 = %s，期望 failed", updated.Status)
	}
	if !strings.Contains(updated.ProvisionError, "余额不足") {
		t.Fatalf("provision_error = %q，期望包含上游原因", updated.ProvisionError)
	}
	if count := countInstancesForOrder(t, gdb, order.ID); count != 0 {
		t.Fatalf("交付失败不应落实例，实际 %d 条", count)
	}

	// 管理员重试（上游仍失败）：同步执行后返回最新订单视图（failed，仍可再试）。
	rec, envelope := retryDelivery(t, engine, admin, order.ID)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("重试接口失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	stillFailed := decodeData[orderView](t, envelope)
	if stillFailed.Status != model.OrderStatusFailed || !strings.Contains(stillFailed.ProvisionError, "余额不足") {
		t.Fatalf("重试后订单 = %+v，期望仍 failed", stillFailed)
	}

	// 上游恢复后重试成功：订单 active + 实例落库 + 错误信息清空。
	host.setFailApplyCredit(false)
	rec, envelope = retryDelivery(t, engine, admin, order.ID)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("重试接口失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	delivered := decodeData[orderView](t, envelope)
	if delivered.Status != model.OrderStatusActive || delivered.HostID == nil || delivered.ProvisionError != "" {
		t.Fatalf("重试成功后的订单 = %+v", delivered)
	}
	if count := countInstancesForOrder(t, gdb, order.ID); count != 1 {
		t.Fatalf("重试成功后实例数 = %d，期望 1", count)
	}

	// 已交付订单再次重试 → 40002（无需重试）。
	rec, envelope = retryDelivery(t, engine, admin, order.ID)
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("已交付订单重试应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(envelope.Message, "已交付完成") {
		t.Fatalf("message = %q", envelope.Message)
	}
}

// TestRetryDeliveryGuardsAndPermissions 验证重试交付的鉴权与状态守卫。
func TestRetryDeliveryGuardsAndPermissions(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	admin := stage4AdminToken(t, engine, gdb, "guard", model.RoleAdmin)
	finance := stage4AdminToken(t, engine, gdb, "guardfin", model.RoleFinance)
	support := stage4AdminToken(t, engine, gdb, "guardsup", model.RoleSupport)
	token, _ := memberTokenFor(t, engine, "guarduser")

	order := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "monthly", "config": map[string]any{},
	})
	path := "/api/v1/admin/orders/" + itoa(order.ID) + "/retry-delivery"

	// 会员 token → 401；finance / support → 403（仅 admin）。
	rec, _ := doAPI(t, engine, http.MethodPost, path, token, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("会员调用重试应 401，得到 %d", rec.Code)
	}
	for name, at := range map[string]string{"finance": finance, "support": support} {
		rec, envelope := doAPI(t, engine, http.MethodPost, path, at, nil)
		if rec.Code != http.StatusForbidden || envelope.Code != response.CodeForbidden {
			t.Fatalf("%s 调用重试应 403：HTTP %d, body=%s", name, rec.Code, rec.Body.String())
		}
	}

	// 未支付订单 → 40002（提示尚未支付）。
	rec, envelope := retryDelivery(t, engine, admin, order.ID)
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("未支付订单重试应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(envelope.Message, "尚未支付") {
		t.Fatalf("message = %q", envelope.Message)
	}

	// 不存在的订单 → 404。
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/admin/orders/999999/retry-delivery", admin, nil)
	if rec.Code != http.StatusNotFound || envelope.Code != response.CodeNotFound {
		t.Fatalf("不存在订单应 404：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 已取消订单 → 40002。
	cancelled := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "monthly", "config": map[string]any{},
	})
	if rec, _ := doAPI(t, engine, http.MethodPost, "/api/v1/orders/"+itoa(cancelled.ID)+"/cancel", token, nil); rec.Code != http.StatusOK {
		t.Fatalf("取消订单失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	rec, envelope = retryDelivery(t, engine, admin, cancelled.ID)
	if rec.Code != http.StatusBadRequest || !strings.Contains(envelope.Message, "已取消") {
		t.Fatalf("已取消订单重试应 40002 提示已取消：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
}

// TestInstanceIsolationAndAdminList 验证实例的会员隔离与管理端列表（筛选 + 不含敏感字段）。
func TestInstanceIsolationAndAdminList(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)
	admin := stage4AdminToken(t, engine, gdb, "list", model.RoleAdmin)

	tokenA, memberA := memberTokenFor(t, engine, "instancea")
	tokenB, memberB := memberTokenFor(t, engine, "instanceb")

	orderA := createOrder(t, engine, tokenA, map[string]any{
		"product_id": product.ID, "cycle": "monthly", "config": map[string]any{},
	})
	payOrderByEpayNotify(t, engine, gateway, tokenA, orderA)
	orderB := createOrder(t, engine, tokenB, map[string]any{
		"product_id": product.ID, "cycle": "monthly", "config": map[string]any{},
	})
	payOrderByEpayNotify(t, engine, gateway, tokenB, orderB)
	instanceB := instanceByOrderID(t, gdb, orderB.ID)

	// A 的列表只含自己的实例。
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/instances", tokenA, nil)
	list := decodeData[instanceListView](t, envelope)
	if len(list.Items) != 1 || list.Items[0].OrderID != orderA.ID {
		t.Fatalf("A 的实例列表错误: %+v", list)
	}

	// A 访问 B 的实例详情 → 404（与不存在同口径）。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/instances/"+itoa(instanceB.ID), tokenA, nil)
	if rec.Code != http.StatusNotFound || envelope.Code != response.CodeNotFound {
		t.Fatalf("跨会员访问应 404：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 管理端列表：全站 2 条、含 member_id、不含敏感字段。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/instances", admin, nil)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("管理端实例列表失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "password") || strings.Contains(rec.Body.String(), "\"username\"") {
		t.Fatalf("管理端实例列表泄露敏感字段: %s", rec.Body.String())
	}
	adminList := decodeData[adminInstanceListView](t, envelope)
	if adminList.Total != 2 {
		t.Fatalf("管理端实例总数 = %d，期望 2", adminList.Total)
	}
	members := map[uint64]bool{}
	for _, item := range adminList.Items {
		members[item.MemberID] = true
	}
	if !members[memberA.ID] || !members[memberB.ID] {
		t.Fatalf("管理端列表缺少会员归属: %+v", adminList.Items)
	}

	// 按 member 筛选 + 按 status 筛选。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/instances?member_id="+itoa(memberA.ID), admin, nil)
	filtered := decodeData[adminInstanceListView](t, envelope)
	if filtered.Total != 1 || filtered.Items[0].MemberID != memberA.ID {
		t.Fatalf("按会员筛选错误: %+v", filtered)
	}
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/instances?status=active", admin, nil)
	if got := decodeData[adminInstanceListView](t, envelope).Total; got != 2 {
		t.Fatalf("按 status 筛选总数 = %d，期望 2", got)
	}

	// 参数校验：非法 status / member_id。
	for _, query := range []string{"?status=deleted", "?member_id=abc", "?member_id=0"} {
		rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/instances"+query, admin, nil)
		if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
			t.Fatalf("非法查询参数 %q 应 40001：HTTP %d, body=%s", query, rec.Code, rec.Body.String())
		}
	}
}

// TestDeliveryPasswordNotLogged 验证自动生成的主机密码不进入日志（上游未回传密码时）。
func TestDeliveryPasswordNotLogged(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	host.setHostPassword("") // 模拟上游不回传密码：实例记录自动生成的密码
	var logs bytes.Buffer
	engine := newStage5Engine(t, gdb, host.client(t), &logs)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, _ := memberTokenFor(t, engine, "loguser")
	order := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "monthly", "config": map[string]any{},
	})
	updated := payOrderByEpayNotify(t, engine, gateway, token, order)
	if updated.Status != model.OrderStatusActive {
		t.Fatalf("交付未完成: %s（%s）", updated.Status, updated.ProvisionError)
	}

	instance := instanceByOrderID(t, gdb, order.ID)
	if len([]rune(instance.Password)) != delivery.PasswordLength {
		t.Fatalf("自动生成密码长度 = %d，期望 %d", len([]rune(instance.Password)), delivery.PasswordLength)
	}
	if strings.Contains(logs.String(), instance.Password) {
		t.Fatalf("日志泄露主机密码: %s", logs.String())
	}
	if strings.Contains(logs.String(), testEpayKey) {
		t.Fatalf("日志泄露商户密钥")
	}
}

// TestDeliveryUpstreamNotConfigured 验证上游未配置时交付置 failed 且原因可读，
// 其余功能（下单/支付入账）不受影响。
func TestDeliveryUpstreamNotConfigured(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage5Engine(t, gdb, nil, nil) // StaticProvider{C: nil} → 上游未配置
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, _ := memberTokenFor(t, engine, "noupstream")
	order := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "monthly", "config": map[string]any{},
	})
	updated := payOrderByEpayNotify(t, engine, gateway, token, order)
	if updated.Status != model.OrderStatusFailed {
		t.Fatalf("订单状态 = %s，期望 failed", updated.Status)
	}
	if !strings.Contains(updated.ProvisionError, "未配置") {
		t.Fatalf("provision_error = %q，期望提示上游未配置", updated.ProvisionError)
	}
	if updated.PayTime == nil {
		t.Fatal("上游未配置不应影响支付入账（pay_time 应有值）")
	}
}

// TestDeliveryHostReadbackFailureStillDelivers 验证「开通成功但回读失败」仍算交付成功
// （主机已开通并扣费，不重复开通），同步字段留空。
func TestDeliveryHostReadbackFailureStillDelivers(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	host.setHostinfoMissing(true)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, _ := memberTokenFor(t, engine, "readback")
	order := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "monthly", "config": map[string]any{},
	})
	updated := payOrderByEpayNotify(t, engine, gateway, token, order)
	if updated.Status != model.OrderStatusActive || updated.HostID == nil {
		t.Fatalf("回读失败不应阻断交付: status=%s host_id=%v", updated.Status, updated.HostID)
	}

	instance := instanceByOrderID(t, gdb, order.ID)
	if instance.NextDueDate != nil || instance.UpstreamStatus != "" || instance.DedicatedIP != "" {
		t.Fatalf("回读失败时同步字段应留空: %+v", instance)
	}
	if len([]rune(instance.Password)) != delivery.PasswordLength {
		t.Fatalf("回读失败时应保留自动生成的密码: %q", instance.Password)
	}
	if got := host.callCount(pathCartSettle); got != 1 {
		t.Fatalf("上游下单次数 = %d，期望 1", got)
	}
}

// TestDeliveryAsyncTrigger 验证生产默认的异步交付：回调立即应答，随后交付完成（最终一致）。
func TestDeliveryAsyncTrigger(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5EngineAsync(t, gdb, host.client(t))
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, _ := memberTokenFor(t, engine, "asyncuser")
	order := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "monthly", "config": map[string]any{},
	})

	if ack := notifyAck(t, engine, gateway.notifyValues(order.TradeNo, order.FinalAmount)); ack != "success" {
		t.Fatalf("回调应答 = %q，期望 success", ack)
	}
	// 异步交付不阻塞回调：随后轮询等待交付完成。
	final := waitForOrderStatus(t, gdb, order.ID, model.OrderStatusActive)
	if final.HostID == nil {
		t.Fatalf("异步交付完成但无 host_id: %+v", final)
	}
	if count := countInstancesForOrder(t, gdb, order.ID); count != 1 {
		t.Fatalf("异步交付后实例数 = %d，期望 1", count)
	}
}

// ---------------------------------------------------------------------------
// 交付测试辅助
// ---------------------------------------------------------------------------

// payOrderByEpayNotify 走「发起在线支付 → 伪造成功回调」，返回回调后的订单视图。
func payOrderByEpayNotify(t *testing.T, engine http.Handler, gateway *fakeEpayGateway, token string, order orderView) orderView {
	t.Helper()

	rec, envelope := payOrder(t, engine, token, order.ID, map[string]any{"channel": "epay"})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("发起在线支付失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if ack := notifyAck(t, engine, gateway.notifyValues(order.TradeNo, order.FinalAmount)); ack != "success" {
		t.Fatalf("支付回调应答 = %q，期望 success", ack)
	}
	return getOrderView(t, engine, token, order.ID)
}

// retryDelivery 调用管理员重试交付接口。
func retryDelivery(t *testing.T, engine http.Handler, adminToken string, orderID uint64) (*httptest.ResponseRecorder, apiEnvelope) {
	t.Helper()
	return doAPI(t, engine, http.MethodPost, "/api/v1/admin/orders/"+itoa(orderID)+"/retry-delivery", adminToken, nil)
}

// orderFromDB 直接读库取订单。
func orderFromDB(t *testing.T, gdb *gorm.DB, orderID uint64) *model.Order {
	t.Helper()
	var order model.Order
	if err := gdb.First(&order, orderID).Error; err != nil {
		t.Fatalf("查询订单失败: %v", err)
	}
	return &order
}

// instanceByOrderID 读库取订单对应的实例（不存在时 Fatal）。
func instanceByOrderID(t *testing.T, gdb *gorm.DB, orderID uint64) *model.Instance {
	t.Helper()
	var instance model.Instance
	if err := gdb.Where("order_id = ?", orderID).Take(&instance).Error; err != nil {
		t.Fatalf("查询实例失败: %v", err)
	}
	return &instance
}

// countInstancesForOrder 统计订单的实例数。
func countInstancesForOrder(t *testing.T, gdb *gorm.DB, orderID uint64) int64 {
	t.Helper()
	var count int64
	if err := gdb.Model(&model.Instance{}).Where("order_id = ?", orderID).Count(&count).Error; err != nil {
		t.Fatalf("统计实例失败: %v", err)
	}
	return count
}

// waitForOrderStatus 轮询等待订单到达指定状态（异步交付用例用），超时即失败。
func waitForOrderStatus(t *testing.T, gdb *gorm.DB, orderID uint64, want string) *model.Order {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var order model.Order
		if err := gdb.First(&order, orderID).Error; err != nil {
			t.Fatalf("查询订单失败: %v", err)
		}
		if order.Status == want {
			return &order
		}
		time.Sleep(20 * time.Millisecond)
	}
	order := orderFromDB(t, gdb, orderID)
	t.Fatalf("等待订单 %d 到状态 %s 超时，当前 %s（provision_error=%q）", orderID, want, order.Status, order.ProvisionError)
	return nil
}
