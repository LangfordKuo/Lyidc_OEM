package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
)

// stage4AdminToken 建一个指定角色与用户名后缀的管理员并登录，返回 token。
func stage4AdminToken(t *testing.T, engine http.Handler, gdb *gorm.DB, name, role string) string {
	t.Helper()

	const password = "admin123456"
	username := "stage4-" + name
	seedAdmin(t, gdb, username, password, role, model.StatusActive)
	token, _ := loginAdmin(t, engine, username, password)
	return token
}

// createRecharge 调用充值下单接口。
func createRecharge(t *testing.T, engine http.Handler, token string, body map[string]any) (*httptest.ResponseRecorder, apiEnvelope) {
	t.Helper()
	return doAPI(t, engine, http.MethodPost, "/api/v1/recharges", token, body)
}

// createRechargeOK 调用充值下单接口并断言成功，返回视图。
func createRechargeOK(t *testing.T, engine http.Handler, token string, amount string) createRechargeView {
	t.Helper()
	rec, envelope := createRecharge(t, engine, token, map[string]any{"amount": amount, "channel": "epay"})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("创建充值单失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[createRechargeView](t, envelope)
}

// memberBalance 直接读库取会员余额。
func memberBalance(t *testing.T, gdb *gorm.DB, memberID uint64) string {
	t.Helper()
	var member model.Member
	if err := gdb.First(&member, memberID).Error; err != nil {
		t.Fatalf("读取会员失败: %v", err)
	}
	return string(member.Balance)
}

// TestRechargeCreateAndNotify 跑通「充值下单 → 渠道下单 → 回调 → 余额到账 + 流水」，并验证幂等。
func TestRechargeCreateAndNotify(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")

	token, member := memberTokenFor(t, engine, "rechargeuser")
	created := createRechargeOK(t, engine, token, "100.00")

	if created.Recharge.Amount != "100.00" || created.Recharge.Status != model.RechargeStatusPending {
		t.Fatalf("充值单错误: %+v", created.Recharge)
	}
	if created.Recharge.Channel != "epay" || created.Recharge.PaidAt != nil || created.Recharge.ChannelTradeNo != nil {
		t.Fatalf("新充值单字段错误: %+v", created.Recharge)
	}
	if created.Pay.PayURL != gateway.payURL || created.Pay.Channel != "epay" {
		t.Fatalf("渠道返回错误: %+v", created.Pay)
	}

	creates := gateway.created()
	if len(creates) != 1 {
		t.Fatalf("渠道下单次数 = %d，期望 1", len(creates))
	}
	if creates[0].form.Get("money") != "100.00" || creates[0].form.Get("out_trade_no") != created.Recharge.TradeNo {
		t.Fatalf("渠道下单参数错误: %v", creates[0].form)
	}
	if creates[0].form.Get("name") != rechargeSubject {
		t.Fatalf("渠道支付标题 = %q", creates[0].form.Get("name"))
	}

	// 金额不符 → fail，且不入账。
	if ack := notifyAck(t, engine, gateway.notifyValues(created.Recharge.TradeNo, "99.00")); ack != "fail" {
		t.Fatalf("金额不符应应答 fail，得到 %q", ack)
	}
	if balance := memberBalance(t, gdb, member.ID); balance != "0.00" {
		t.Fatalf("金额不符后余额 = %s，期望 0.00", balance)
	}

	// 正常回调 → 入账。
	if ack := notifyAck(t, engine, gateway.notifyValues(created.Recharge.TradeNo, "100.00")); ack != "success" {
		t.Fatalf("回调应答 = %q，期望 success", ack)
	}
	if balance := memberBalance(t, gdb, member.ID); balance != "100.00" {
		t.Fatalf("余额 = %s，期望 100.00", balance)
	}

	entry := ledgerForRef(t, gdb, member.ID, model.LedgerTypeRecharge, created.Recharge.ID)
	if entry.Amount != "100.00" || entry.BalanceBefore != "0.00" || entry.BalanceAfter != "100.00" {
		t.Fatalf("充值流水错误: %+v", entry)
	}
	if entry.RefType != model.LedgerRefRecharge {
		t.Fatalf("流水关联类型 = %q", entry.RefType)
	}

	// 重复回调 → success 且不重复加款。
	if ack := notifyAck(t, engine, gateway.notifyValues(created.Recharge.TradeNo, "100.00")); ack != "success" {
		t.Fatalf("重复回调应答 = %q，期望 success", ack)
	}
	if balance := memberBalance(t, gdb, member.ID); balance != "100.00" {
		t.Fatalf("重复回调后余额 = %s，期望 100.00", balance)
	}

	// 充值单已转 paid 且记录渠道单号与到账时间。
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/recharges", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("充值单列表失败: HTTP %d", rec.Code)
	}
	list := decodeData[rechargeListView](t, envelope)
	if list.Total != 1 || len(list.Items) != 1 {
		t.Fatalf("充值单列表 = %+v", list)
	}
	item := list.Items[0]
	if item.Status != model.RechargeStatusPaid || item.PaidAt == nil ||
		item.ChannelTradeNo == nil || *item.ChannelTradeNo != "GATEWAY-"+created.Recharge.TradeNo {
		t.Fatalf("充值单回读错误: %+v", item)
	}
}

// TestRechargeAmountAndChannelValidation 验证金额边界（1.00 ~ 50000.00）与渠道校验。
func TestRechargeAmountAndChannelValidation(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	token, _ := memberTokenFor(t, engine, "amountuser")

	cases := []struct {
		name     string
		body     map[string]any
		wantHTTP int
		wantCode int
	}{
		{"下界之下", map[string]any{"amount": "0.99", "channel": "epay"}, http.StatusBadRequest, response.CodeValidationFailed},
		{"上界之上", map[string]any{"amount": "50000.01", "channel": "epay"}, http.StatusBadRequest, response.CodeValidationFailed},
		{"金额写法非法", map[string]any{"amount": "100.001", "channel": "epay"}, http.StatusBadRequest, response.CodeInvalidParam},
		{"金额非数字", map[string]any{"amount": "abc", "channel": "epay"}, http.StatusBadRequest, response.CodeInvalidParam},
		{"渠道不支持", map[string]any{"amount": "100.00", "channel": "balance"}, http.StatusBadRequest, response.CodeValidationFailed},
		{"渠道缺失", map[string]any{"amount": "100.00"}, http.StatusBadRequest, response.CodeValidationFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, envelope := createRecharge(t, engine, token, tc.body)
			if rec.Code != tc.wantHTTP || envelope.Code != tc.wantCode {
				t.Fatalf("HTTP %d code=%d，期望 %d/%d（body=%s）",
					rec.Code, envelope.Code, tc.wantHTTP, tc.wantCode, rec.Body.String())
			}
		})
	}

	// 边界内合法（下界与上界都能建单，落库统一两位小数）。
	if view := createRechargeOK(t, engine, token, "1"); view.Recharge.Amount != "1.00" {
		t.Fatalf("下界金额 = %s，期望 1.00", view.Recharge.Amount)
	}
	if view := createRechargeOK(t, engine, token, "50000.00"); view.Recharge.Amount != "50000.00" {
		t.Fatalf("上界金额 = %s", view.Recharge.Amount)
	}

	// 未登录 → 401。
	rec, envelope := createRecharge(t, engine, "", map[string]any{"amount": "100.00", "channel": "epay"})
	if rec.Code != http.StatusUnauthorized || envelope.Code != response.CodeUnauthorized {
		t.Fatalf("未登录应 401：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
}

// TestRechargeChannelUnavailable 验证渠道未配置时充值返回明确业务错误（不影响其余功能）。
func TestRechargeChannelUnavailable(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, nil)
	token, _ := memberTokenFor(t, engine, "nochanneluser")

	rec, envelope := createRecharge(t, engine, token, map[string]any{"amount": "100.00", "channel": "epay"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("渠道未配置应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 其余功能不受影响：余额查询正常。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/finance/balance", token, nil)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("余额查询失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
}

// TestFinanceListsAndIsolation 验证本人流水/充值单隔离与管理端对账列表（含角色守卫）。
func TestFinanceListsAndIsolation(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")

	ownerToken, owner := memberTokenFor(t, engine, "financeowner")
	otherToken, other := memberTokenFor(t, engine, "financeother")

	first := createRechargeOK(t, engine, ownerToken, "100.00")
	second := createRechargeOK(t, engine, ownerToken, "50.00")
	notifyAck(t, engine, gateway.notifyValues(first.Recharge.TradeNo, "100.00"))
	notifyAck(t, engine, gateway.notifyValues(second.Recharge.TradeNo, "50.00"))

	// 本人流水：两笔充值，倒序（新建在前）+ 分页。
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/finance/ledger?page=1&page_size=1", ownerToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("流水列表失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	ledgerList := decodeData[ledgerListView](t, envelope)
	if ledgerList.Total != 2 || len(ledgerList.Items) != 1 {
		t.Fatalf("流水列表错误: %+v", ledgerList)
	}
	if ledgerList.Items[0].Amount != "50.00" || ledgerList.Items[0].BalanceAfter != "150.00" {
		t.Fatalf("流水未按倒序或余额链错误: %+v", ledgerList.Items[0])
	}

	// 类型过滤：order_pay 无记录。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/finance/ledger?type=order_pay", ownerToken, nil)
	if rec.Code != http.StatusOK || decodeData[ledgerListView](t, envelope).Total != 0 {
		t.Fatalf("类型过滤失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	rec, _ = doAPI(t, engine, http.MethodGet, "/api/v1/finance/ledger?type=nope", ownerToken, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 type 应 400：HTTP %d", rec.Code)
	}

	// 他人看不到我的数据。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/finance/ledger", otherToken, nil)
	if other := decodeData[ledgerListView](t, envelope); other.Total != 0 {
		t.Fatalf("他人可见我的流水: %+v", other)
	}
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/recharges", otherToken, nil)
	if rec.Code != http.StatusOK || decodeData[rechargeListView](t, envelope).Total != 0 {
		t.Fatalf("他人可见我的充值单: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 管理端对账：admin 可见全部并按会员过滤。
	adminToken := stage4AdminToken(t, engine, gdb, "admin", model.RoleAdmin)
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/recharges", adminToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("管理端充值单列表失败: HTTP %d", rec.Code)
	}
	if all := decodeData[rechargeListView](t, envelope); all.Total != 2 {
		t.Fatalf("管理端应看到 2 条充值单: %+v", all)
	}
	rec, envelope = doAPI(t, engine, http.MethodGet,
		"/api/v1/admin/recharges?member_id="+itoa(other.ID), adminToken, nil)
	if rec.Code != http.StatusOK || decodeData[rechargeListView](t, envelope).Total != 0 {
		t.Fatalf("会员过滤失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/ledger?type=recharge", adminToken, nil)
	if rec.Code != http.StatusOK || decodeData[ledgerListView](t, envelope).Total != 2 {
		t.Fatalf("管理端流水列表失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// finance 角色可查；support 一律 403。
	financeToken := stage4AdminToken(t, engine, gdb, "finance", model.RoleFinance)
	rec, _ = doAPI(t, engine, http.MethodGet, "/api/v1/admin/recharges", financeToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("finance 应可查充值单：HTTP %d", rec.Code)
	}
	supportToken := stage4AdminToken(t, engine, gdb, "support", model.RoleSupport)
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/recharges", supportToken, nil)
	if rec.Code != http.StatusForbidden || envelope.Code != response.CodeForbidden {
		t.Fatalf("support 应 403：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/ledger", supportToken, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("support 查流水应 403：HTTP %d", rec.Code)
	}
	if balance := memberBalance(t, gdb, owner.ID); balance != "150.00" {
		t.Fatalf("本人余额 = %s，期望 150.00", balance)
	}
}
