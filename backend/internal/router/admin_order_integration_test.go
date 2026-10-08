package router

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
)

// ---------------------------------------------------------------------------
// 管理端订单列表与详情（阶段 8b，契约 12.6）
//
// 覆盖：筛选（status / type / member_id / trade_no 模糊）与分页口径、视图（会员端订单视图
// 原样 + 会员概要）、权限矩阵（查看类三角色可读、会员与匿名 401、重试交付仍仅 admin 由
// 5a 的用例覆盖）、详情交付信息（host_id / provision_error / delivered_at）与 404 / 40001 负例。
// ---------------------------------------------------------------------------

// adminOrderWorld 是管理端订单用例的公共夹具：真库 + 假上游（同步交付）+ 假支付网关 +
// 一个已上架商品 + 两位会员；订单覆盖「待支付 / 已交付 / 续费（renew）」三种形态。
type adminOrderWorld struct {
	engine  *gin.Engine
	gdb     *gorm.DB
	host    *fakeHostServer
	gateway *fakeEpayGateway

	tokenA  string
	tokenB  string
	memberA memberView
	memberB memberView

	// pendingA 是 A 的待支付订单；deliveredA 是 A 的已交付订单；
	// deliveredB 是 B 的已交付订单（续费单据此创建）；renewOrder 是 B 的续费单（type=renew）。
	pendingA   orderView
	deliveredA orderView
	deliveredB orderView
	renewOrder orderView
}

// newAdminOrderWorld 准备夹具（默认商品价：monthly 100.00 / annual 200.00）。
func newAdminOrderWorld(t *testing.T) *adminOrderWorld {
	t.Helper()

	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	w := &adminOrderWorld{engine: engine, gdb: gdb, host: host, gateway: gateway}
	w.tokenA, w.memberA = memberTokenFor(t, engine, "adminordera")
	w.tokenB, w.memberB = memberTokenFor(t, engine, "adminorderb")

	w.pendingA = createOrder(t, engine, w.tokenA, map[string]any{
		"product_id": product.ID, "cycle": "monthly", "config": map[string]any{"11": 111},
	})
	w.deliveredA = createOrder(t, engine, w.tokenA, map[string]any{
		"product_id": product.ID, "cycle": "annual", "config": map[string]any{},
	})
	payOrderByEpayNotify(t, engine, gateway, w.tokenA, w.deliveredA)
	w.deliveredB = createOrder(t, engine, w.tokenB, map[string]any{
		"product_id": product.ID, "cycle": "monthly", "config": map[string]any{},
	})
	payOrderByEpayNotify(t, engine, gateway, w.tokenB, w.deliveredB)

	// 续费单走真实链路（已交付实例 → POST /instances/:id/renew），用于 type=renew 与 instance_id 断言。
	instance := instanceByOrderID(t, gdb, w.deliveredB.ID)
	rec, envelope := doAPI(t, engine, http.MethodPost,
		"/api/v1/instances/"+itoa(instance.ID)+"/renew", w.tokenB, map[string]any{"cycle": "monthly"})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("续费下单失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	w.renewOrder = decodeData[orderView](t, envelope)
	return w
}

// adminOrders 调用管理端订单列表接口并断言成功（query 形如 "?status=paid"）。
func adminOrders(t *testing.T, engine http.Handler, token, query string) adminOrderListView {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/orders"+query, token, nil)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("管理端订单列表失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[adminOrderListView](t, envelope)
}

// TestAdminOrderListFiltersAndPaging 验证管理端订单列表的筛选、分页与视图口径。
func TestAdminOrderListFiltersAndPaging(t *testing.T) {
	w := newAdminOrderWorld(t)
	admin := stage4AdminToken(t, w.engine, w.gdb, "ordlist", model.RoleAdmin)

	// 无筛选：全站 4 单，新建在前（id 降序），回带会员概要。
	all := adminOrders(t, w.engine, admin, "")
	if all.Total != 4 || len(all.Items) != 4 {
		t.Fatalf("全站订单 total=%d len=%d，期望 4/4", all.Total, len(all.Items))
	}
	if all.Page != defaultPage || all.PageSize != defaultPageSize {
		t.Fatalf("分页缺省值错误: page=%d page_size=%d", all.Page, all.PageSize)
	}
	for i := 1; i < len(all.Items); i++ {
		if all.Items[i-1].ID < all.Items[i].ID {
			t.Fatalf("列表未按 id 降序: %d 在 %d 之前", all.Items[i-1].ID, all.Items[i].ID)
		}
	}

	// 最新一单是续费单：type / instance_id / 会员概要齐备（视图 = 会员端订单视图 + member）。
	newest := all.Items[0]
	if newest.ID != w.renewOrder.ID {
		t.Fatalf("最新订单 = %d，期望续费单 %d", newest.ID, w.renewOrder.ID)
	}
	if newest.Type != model.OrderTypeRenew || newest.InstanceID == nil {
		t.Fatalf("续费单字段错误: type=%q instance_id=%v", newest.Type, newest.InstanceID)
	}
	if newest.Amount != "100.00" || newest.FinalAmount != "100.00" || newest.CreatedAt == "" {
		t.Fatalf("订单视图金额/时间字段缺失: %+v", newest)
	}
	if newest.Member == nil || newest.Member.ID != w.memberB.ID ||
		newest.Member.Username != "adminorderb" || newest.Member.Nickname == "" ||
		newest.Member.Email != "adminorderb@example.com" || newest.Member.Status != model.StatusActive {
		t.Fatalf("订单会员概要错误: %+v", newest.Member)
	}

	// status 筛选（6 态枚举内）。
	if got := adminOrders(t, w.engine, admin, "?status=pending"); got.Total != 2 {
		t.Fatalf("status=pending total=%d，期望 2（A 的待支付单 + B 的续费单）", got.Total)
	}
	active := adminOrders(t, w.engine, admin, "?status=active")
	if active.Total != 2 {
		t.Fatalf("status=active total=%d，期望 2", active.Total)
	}
	for _, item := range active.Items {
		if item.Status != model.OrderStatusActive || item.HostID == nil ||
			item.DeliveredAt == nil || item.ProvisionError != "" || item.PayTime == nil {
			t.Fatalf("已交付订单的交付信息不完整: %+v", item)
		}
	}

	// member_id 筛选（只含指定会员）。
	byMember := adminOrders(t, w.engine, admin, "?member_id="+itoa(w.memberA.ID))
	if byMember.Total != 2 || len(byMember.Items) != 2 {
		t.Fatalf("member_id 筛选 total=%d len=%d，期望 2/2", byMember.Total, len(byMember.Items))
	}
	for _, item := range byMember.Items {
		if item.MemberID != w.memberA.ID || item.Member == nil || item.Member.Username != "adminordera" {
			t.Fatalf("会员筛选结果错误: %+v", item)
		}
	}

	// type 筛选。
	if got := adminOrders(t, w.engine, admin, "?type=renew"); got.Total != 1 || got.Items[0].ID != w.renewOrder.ID {
		t.Fatalf("type=renew 筛选错误: total=%d items=%+v", got.Total, got.Items)
	}
	if got := adminOrders(t, w.engine, admin, "?type=new"); got.Total != 3 {
		t.Fatalf("type=new total=%d，期望 3", got.Total)
	}

	// trade_no：完整单号命中；子串按 LIKE 包含匹配且大小写不敏感（表排序规则 utf8mb4_general_ci）。
	full := adminOrders(t, w.engine, admin, "?trade_no="+w.deliveredA.TradeNo)
	if full.Total != 1 || full.Items[0].ID != w.deliveredA.ID {
		t.Fatalf("trade_no 完整单号查询错误: total=%d items=%+v", full.Total, full.Items)
	}
	fragment := strings.ToLower(w.deliveredA.TradeNo[len(w.deliveredA.TradeNo)-4:])
	partial := adminOrders(t, w.engine, admin, "?trade_no="+fragment)
	if partial.Total != 1 || partial.Items[0].ID != w.deliveredA.ID {
		t.Fatalf("trade_no 子串 %q 查询错误: total=%d items=%+v", fragment, partial.Total, partial.Items)
	}

	// 组合筛选：B 的待支付订单只剩续费单。
	combined := adminOrders(t, w.engine, admin,
		"?status=pending&member_id="+itoa(w.memberB.ID)+"&type=renew")
	if combined.Total != 1 || combined.Items[0].ID != w.renewOrder.ID {
		t.Fatalf("组合筛选错误: total=%d items=%+v", combined.Total, combined.Items)
	}

	// 分页：page_size=2 时两页各 2 条且不重叠，total 恒为全量。
	page1 := adminOrders(t, w.engine, admin, "?page=1&page_size=2")
	page2 := adminOrders(t, w.engine, admin, "?page=2&page_size=2")
	if page1.Total != 4 || len(page1.Items) != 2 || page1.PageSize != 2 {
		t.Fatalf("第 1 页错误: %+v", page1)
	}
	if page2.Total != 4 || len(page2.Items) != 2 || page2.Items[0].ID >= page1.Items[1].ID {
		t.Fatalf("第 2 页错误: %+v", page2)
	}

	// 非法筛选与分页参数 → 40001。
	for _, query := range []string{
		"?status=refunded", "?type=upgrade", "?member_id=abc", "?member_id=0", "?page=0", "?page_size=101",
	} {
		rec, envelope := doAPI(t, w.engine, http.MethodGet, "/api/v1/admin/orders"+query, admin, nil)
		if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
			t.Fatalf("%s 应 40001：HTTP %d, body=%s", query, rec.Code, rec.Body.String())
		}
	}
}

// TestAdminOrderDetailPermissionsAndGuards 验证管理端订单详情的权限矩阵、字段口径与负例。
func TestAdminOrderDetailPermissionsAndGuards(t *testing.T) {
	w := newAdminOrderWorld(t)
	admin := stage4AdminToken(t, w.engine, w.gdb, "orddet", model.RoleAdmin)
	finance := stage4AdminToken(t, w.engine, w.gdb, "orddetfin", model.RoleFinance)
	support := stage4AdminToken(t, w.engine, w.gdb, "orddetsup", model.RoleSupport)

	const listPath = "/api/v1/admin/orders"
	detailPath := listPath + "/" + itoa(w.deliveredA.ID)

	// 查看类接口（列表 + 详情）：admin / finance / support 均可读。
	for name, token := range map[string]string{"admin": admin, "finance": finance, "support": support} {
		rec, envelope := doAPI(t, w.engine, http.MethodGet, listPath, token, nil)
		if rec.Code != http.StatusOK || envelope.Code != 0 {
			t.Fatalf("%s 读取订单列表应 200：HTTP %d, body=%s", name, rec.Code, rec.Body.String())
		}
		rec, envelope = doAPI(t, w.engine, http.MethodGet, detailPath, token, nil)
		if rec.Code != http.StatusOK || envelope.Code != 0 {
			t.Fatalf("%s 读取订单详情应 200：HTTP %d, body=%s", name, rec.Code, rec.Body.String())
		}
	}

	// 会员 token 与匿名请求一律 401。
	for name, token := range map[string]string{"会员": w.tokenA, "匿名": ""} {
		rec, envelope := doAPI(t, w.engine, http.MethodGet, detailPath, token, nil)
		if rec.Code != http.StatusUnauthorized || envelope.Code != response.CodeUnauthorized {
			t.Fatalf("%s 读取订单详情应 401：HTTP %d, body=%s", name, rec.Code, rec.Body.String())
		}
	}

	// 详情字段：订单视图与会员端口径**逐字段一致**（reflect 比对），另加会员概要。
	rec, envelope := doAPI(t, w.engine, http.MethodGet, detailPath, support, nil)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("订单详情失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	detail := decodeData[adminOrderView](t, envelope)
	memberSide := getOrderView(t, w.engine, w.tokenA, w.deliveredA.ID)
	if !reflect.DeepEqual(detail.orderView, memberSide) {
		t.Fatalf("管理端订单视图与会员端不一致:\n管理端=%+v\n会员端=%+v", detail.orderView, memberSide)
	}
	if detail.Member == nil || detail.Member.ID != w.memberA.ID ||
		detail.Member.Username != "adminordera" || detail.Member.Email != "adminordera@example.com" {
		t.Fatalf("详情会员概要错误: %+v", detail.Member)
	}
	// 交付信息：成功交付的订单有 host_id / delivered_at 且无失败原因。
	if detail.Status != model.OrderStatusActive || detail.HostID == nil ||
		detail.DeliveredAt == nil || detail.ProvisionError != "" {
		t.Fatalf("已交付订单详情交付信息错误: %+v", detail)
	}

	// 交付失败的订单：provision_error 带脱敏原因、host_id / delivered_at 为空
	// （管理员据此重试交付，契约 14.4）。
	w.host.setFailApplyCredit(true)
	failed := createOrder(t, w.engine, w.tokenA, map[string]any{
		"product_id": w.deliveredA.ProductID, "cycle": "monthly", "config": map[string]any{},
	})
	payOrderByEpayNotify(t, w.engine, w.gateway, w.tokenA, failed)
	rec, envelope = doAPI(t, w.engine, http.MethodGet, listPath+"/"+itoa(failed.ID), admin, nil)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("交付失败订单详情失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	failedDetail := decodeData[adminOrderView](t, envelope)
	if failedDetail.Status != model.OrderStatusFailed || failedDetail.HostID != nil ||
		failedDetail.DeliveredAt != nil || !strings.Contains(failedDetail.ProvisionError, "余额不足") {
		t.Fatalf("交付失败订单的交付信息错误: %+v", failedDetail)
	}

	// 负例：不存在的订单 → 404 订单不存在；ID 非正整数 → 40001。
	rec, envelope = doAPI(t, w.engine, http.MethodGet, listPath+"/999999", admin, nil)
	if rec.Code != http.StatusNotFound || envelope.Code != response.CodeNotFound ||
		envelope.Message != msgOrderMissing {
		t.Fatalf("不存在订单应 404 订单不存在：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	for _, path := range []string{listPath + "/abc", listPath + "/0"} {
		rec, envelope = doAPI(t, w.engine, http.MethodGet, path, admin, nil)
		if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
			t.Fatalf("%s 应 40001：HTTP %d, body=%s", path, rec.Code, rec.Body.String())
		}
	}
}
