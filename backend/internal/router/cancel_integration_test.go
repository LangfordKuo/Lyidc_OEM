package router

import (
	"net/http"
	"strings"
	"testing"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
)

// ---------------------------------------------------------------------------
// 阶段 5c：取消/终止申请（申请 → 上游受理 → 幂等 → 同步收敛 terminated → 操作矩阵）
// ---------------------------------------------------------------------------

// TestMemberCancelRequestFlow 验证会员取消申请的完整链路：
// 上游参数口径、本地取消标记、审计、幂等重复申请、详情/列表字段、续费收紧与状态约束。
func TestMemberCancelRequestFlow(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, member := memberTokenFor(t, engine, "canceluser")
	tokenB, _ := memberTokenFor(t, engine, "canceluserb")
	instance := newInstanceFor(t, engine, gateway, gdb, token, product.ID)
	cancelPath := "/api/v1/instances/" + itoa(instance.ID) + "/cancel"

	// 跨会员申请 → 404（与不存在同口径）。
	rec, envelope := doAPI(t, engine, http.MethodPost, cancelPath, tokenB,
		map[string]any{"type": "immediate"})
	if rec.Code != http.StatusNotFound || envelope.Code != response.CodeNotFound {
		t.Fatalf("跨会员申请应 404：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 取消方式非法 → 40001（不调上游）。
	rec, envelope = doAPI(t, engine, http.MethodPost, cancelPath, token, map[string]any{"type": "now"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("非法 type 应 40001：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 正常申请：上游收到 type=Immediate + reason，本地记 pending，status 保持 active。
	rec, envelope = doAPI(t, engine, http.MethodPost, cancelPath, token,
		map[string]any{"type": "immediate", "reason": "不再需要该主机"})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("取消申请失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	result := decodeData[cancelResultView](t, envelope)
	if result.CancelStatus != model.InstanceCancelPending || result.CancelType != model.CancelTypeImmediate ||
		result.CancelRequestID <= 0 || result.Duplicate {
		t.Fatalf("申请返回 = %+v，期望 pending / immediate / 申请号 / duplicate=false", result)
	}
	if result.Status != model.InstanceStatusActive {
		t.Fatalf("申请后实例状态 = %s，期望保持 active（契约 15.8.1）", result.Status)
	}
	form := host.lastForm(pathHostCancel)
	if form.Get("id") != itoa(uint64(instance.HostID)) || form.Get("type") != "Immediate" ||
		form.Get("reason") != "不再需要该主机" {
		t.Fatalf("上游取消参数错误: id=%q type=%q reason=%q", form.Get("id"), form.Get("type"), form.Get("reason"))
	}

	got := instanceFromDB(t, gdb, instance.ID)
	if got.CancelStatus != model.InstanceCancelPending || got.CancelRequestedAt == nil ||
		got.CancelRequestID != result.CancelRequestID || got.Status != model.InstanceStatusActive {
		t.Fatalf("本地取消标记异常: %+v", got)
	}
	last := lastInstanceLog(t, gdb, instance.ID)
	if last.Action != model.ActionCancel || last.Status != model.InstanceOpSuccess ||
		last.ActorType != model.ActorTypeMember || last.ActorID != member.ID ||
		!strings.Contains(last.Message, "不再需要该主机") {
		t.Fatalf("取消申请审计异常: %+v", last)
	}

	// 幂等重复申请：返回现状、不再调上游、审计留痕（duplicate=true）。
	rec, envelope = doAPI(t, engine, http.MethodPost, cancelPath, token,
		map[string]any{"type": "end_of_billing", "reason": "改主意了"})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("重复申请应幂等成功: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	duplicate := decodeData[cancelResultView](t, envelope)
	if !duplicate.Duplicate || duplicate.CancelRequestID != result.CancelRequestID ||
		duplicate.CancelType != model.CancelTypeImmediate {
		t.Fatalf("重复申请应返回原申请现状: %+v", duplicate)
	}
	if calls, _, _ := host.cancelCalls(); calls != 1 {
		t.Fatalf("/host/cancel 调用次数 = %d，期望 1（幂等不重复提交）", calls)
	}
	if last := lastInstanceLog(t, gdb, instance.ID); last.Action != model.ActionCancel ||
		last.Status != model.InstanceOpSuccess || !strings.Contains(last.Message, "幂等") {
		t.Fatalf("幂等重复申请应留痕: %+v", last)
	}

	// 详情与列表输出取消字段。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/instances/"+itoa(instance.ID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("详情查询失败: HTTP %d", rec.Code)
	}
	detail := decodeData[instanceDetailView](t, envelope)
	if detail.CancelStatus != model.InstanceCancelPending || detail.CancelType != model.CancelTypeImmediate ||
		detail.CancelRequestID != result.CancelRequestID || detail.CancelRequestedAt == nil {
		t.Fatalf("详情取消字段异常: %+v", detail)
	}

	// 有在途取消申请 → 不可续费（40002）。
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/instances/"+itoa(instance.ID)+"/renew",
		token, map[string]any{"cycle": "monthly"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("在途取消申请时续费应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(envelope.Message, "取消申请") {
		t.Fatalf("续费拒绝提示 = %q，期望说明在途取消申请", envelope.Message)
	}

	// 上游失败（业务拒绝）→ 50003 + 审计 fail，本地不留申请标记。
	other := newInstanceFor(t, engine, gateway, gdb, token, product.ID)
	host.setCancelFail("该产品不支持取消")
	rec, envelope = doAPI(t, engine, http.MethodPost,
		"/api/v1/instances/"+itoa(other.ID)+"/cancel", token, map[string]any{"type": "immediate"})
	if rec.Code != http.StatusInternalServerError || envelope.Code != response.CodeUpstreamFailed {
		t.Fatalf("上游拒绝应 50003：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := instanceFromDB(t, gdb, other.ID); got.CancelStatus != model.InstanceCancelNone {
		t.Fatalf("上游失败后不应留下申请标记: %+v", got)
	}
	if last := lastInstanceLog(t, gdb, other.ID); last.Action != model.ActionCancel || last.Status != model.InstanceOpFail {
		t.Fatalf("上游失败应审计 cancel/fail: %+v", last)
	}
	host.setCancelFail("")

	// suspended 实例仍可申请终止（契约 15.8.1）。
	setInstanceStatus(t, gdb, other.ID, model.InstanceStatusSuspended)
	rec, envelope = doAPI(t, engine, http.MethodPost,
		"/api/v1/instances/"+itoa(other.ID)+"/cancel", token, map[string]any{"type": "end_of_billing"})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("suspended 实例申请失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if _, types, _ := host.cancelCalls(); len(types) != 2 || types[1] != "Endofbilling" {
		t.Fatalf("到期取消应提交 Endofbilling：%v", types)
	}
	if got := instanceFromDB(t, gdb, other.ID); got.Status != model.InstanceStatusSuspended ||
		got.CancelType != model.CancelTypeEndOfBilling {
		t.Fatalf("suspended 实例申请后状态异常: %+v", got)
	}

	// 已 terminated 实例不可申请（40002）。
	terminated := newInstanceFor(t, engine, gateway, gdb, token, product.ID)
	setInstanceStatus(t, gdb, terminated.ID, model.InstanceStatusTerminated)
	rec, envelope = doAPI(t, engine, http.MethodPost,
		"/api/v1/instances/"+itoa(terminated.ID)+"/cancel", token, map[string]any{"type": "immediate"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("已终止实例申请应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 上游主机已被终止（status=200 + domainstatus=Deleted，实测口径）→ 受理并标注「即时生效」，
	// 随后同步立即收敛为 terminated。
	deletedBefore := newInstanceFor(t, engine, gateway, gdb, token, product.ID)
	host.setCancelAlreadyDeleted(true)
	rec, envelope = doAPI(t, engine, http.MethodPost,
		"/api/v1/instances/"+itoa(deletedBefore.ID)+"/cancel", token, map[string]any{"type": "immediate"})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("已删除主机的申请应被受理: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	result = decodeData[cancelResultView](t, envelope)
	if result.CancelStatus != model.InstanceCancelPending || result.CancelRequestID != 0 ||
		!strings.Contains(result.Message, "上游主机已删除") {
		t.Fatalf("已删除主机的受理结果异常: %+v", result)
	}
	host.setCancelAlreadyDeleted(false)
	host.deleteHost(deletedBefore.HostID)
	admin := stage4AdminToken(t, engine, gdb, "canceldeleted", model.RoleAdmin)
	rec, envelope = doAPI(t, engine, http.MethodPost,
		"/api/v1/admin/instances/"+itoa(deletedBefore.ID)+"/sync", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("同步失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := instanceFromDB(t, gdb, deletedBefore.ID); got.Status != model.InstanceStatusTerminated ||
		got.CancelStatus != model.InstanceCancelDone {
		t.Fatalf("即时生效后应收敛为 terminated: %+v", got)
	}
}

// TestAdminCancelPermissionsAndReason 验证管理端代客终止的角色矩阵、原因必填与审计。
func TestAdminCancelPermissionsAndReason(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)
	admin := stage4AdminToken(t, engine, gdb, "canceladmin", model.RoleAdmin)
	finance := stage4AdminToken(t, engine, gdb, "canceladminfin", model.RoleFinance)
	support := stage4AdminToken(t, engine, gdb, "canceladminsup", model.RoleSupport)

	token, _ := memberTokenFor(t, engine, "canceladminuser")
	instance := newInstanceFor(t, engine, gateway, gdb, token, product.ID)
	path := "/api/v1/admin/instances/" + itoa(instance.ID) + "/cancel"

	for name, at := range map[string]string{"finance": finance, "support": support} {
		rec, envelope := doAPI(t, engine, http.MethodPost, path, at,
			map[string]any{"type": "immediate", "reason": "滥用资源"})
		if rec.Code != http.StatusForbidden || envelope.Code != response.CodeForbidden {
			t.Fatalf("%s 调强制终止应 403：HTTP %d, body=%s", name, rec.Code, rec.Body.String())
		}
	}
	rec, _ := doAPI(t, engine, http.MethodPost, path, token, map[string]any{"type": "immediate", "reason": "x"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("会员调管理端终止应 401，得到 %d", rec.Code)
	}

	// reason 必填 / type 校验。
	rec, envelope := doAPI(t, engine, http.MethodPost, path, admin, map[string]any{"type": "immediate", "reason": " "})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("空原因应 40001：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	rec, envelope = doAPI(t, engine, http.MethodPost, path, admin, map[string]any{"type": "later", "reason": "x"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("非法 type 应 40001：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 正常代客终止：审计 actor=admin，本地 pending。
	rec, envelope = doAPI(t, engine, http.MethodPost, path, admin,
		map[string]any{"type": "immediate", "reason": "会员工单要求终止"})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("管理员终止失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	result := decodeData[cancelResultView](t, envelope)
	if result.CancelStatus != model.InstanceCancelPending || result.Action != model.ActionCancel {
		t.Fatalf("管理员终止返回异常: %+v", result)
	}
	last := lastInstanceLog(t, gdb, instance.ID)
	if last.ActorType != model.ActorTypeAdmin || last.Action != model.ActionCancel ||
		!strings.Contains(last.Message, "会员工单要求终止") {
		t.Fatalf("管理员终止审计异常: %+v", last)
	}
}

// TestSyncConvergesTerminated 验证上游终止后的状态收敛：
// 主机已删除 / domainstatus=Deleted → sync 收敛 terminated（幂等）、审计 cancel_sync + sync、
// 终止后全部操作被拒。
func TestSyncConvergesTerminated(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)
	admin := stage4AdminToken(t, engine, gdb, "termadmin", model.RoleAdmin)

	token, _ := memberTokenFor(t, engine, "termuser")
	instance := newInstanceFor(t, engine, gateway, gdb, token, product.ID)
	// 先提交取消申请（真实链路），再模拟上游处理完成删除主机。
	rec, envelope := doAPI(t, engine, http.MethodPost,
		"/api/v1/instances/"+itoa(instance.ID)+"/cancel", token, map[string]any{"type": "immediate"})
	if rec.Code != http.StatusOK {
		t.Fatalf("取消申请失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	host.deleteHost(instance.HostID)

	syncPath := "/api/v1/admin/instances/" + itoa(instance.ID) + "/sync"
	rec, envelope = doAPI(t, engine, http.MethodPost, syncPath, admin, nil)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("同步收敛失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	result := decodeData[syncResultView](t, envelope)
	if !result.Terminated || !result.StatusChanged || result.Status != model.InstanceStatusTerminated {
		t.Fatalf("同步应收敛为 terminated：%+v", result)
	}
	got := instanceFromDB(t, gdb, instance.ID)
	if got.Status != model.InstanceStatusTerminated || got.CancelStatus != model.InstanceCancelDone {
		t.Fatalf("本地收敛结果异常: %+v", got)
	}
	logs := instanceLogsFromDB(t, gdb, instance.ID)
	if logs[0].Action != model.ActionSync || logs[1].Action != model.ActionCancelSync ||
		logs[1].Status != model.InstanceOpSuccess || logs[1].ActorType != model.ActorTypeAdmin {
		t.Fatalf("收敛审计异常: %+v", logs[:2])
	}

	// 幂等：再次同步不报错、状态保持 terminated、审计标注幂等。
	rec, envelope = doAPI(t, engine, http.MethodPost, syncPath, admin, nil)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("重复同步应成功: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	result = decodeData[syncResultView](t, envelope)
	if !result.Terminated || result.StatusChanged || result.Status != model.InstanceStatusTerminated {
		t.Fatalf("重复同步应幂等：%+v", result)
	}
	// 收敛审计（cancel_sync 由 converge 写入，其后是本次 sync 的审计；两者标注幂等）。
	logs = instanceLogsFromDB(t, gdb, instance.ID)
	if logs[0].Action != model.ActionSync || logs[1].Action != model.ActionCancelSync ||
		!strings.Contains(logs[1].Message, "幂等") {
		t.Fatalf("重复收敛应标注幂等: %+v", logs[:2])
	}

	// 已终止实例：电源/重装/改密/续费/再次取消一律 40002。
	powerRec, _ := doAPI(t, engine, http.MethodPost,
		"/api/v1/instances/"+itoa(instance.ID)+"/power", token, map[string]any{"op": "soft_on"})
	if powerRec.Code != http.StatusBadRequest {
		t.Fatalf("已终止实例开机应 40002，得到 %d", powerRec.Code)
	}
	renewRec, renewEnv := doAPI(t, engine, http.MethodPost,
		"/api/v1/instances/"+itoa(instance.ID)+"/renew", token, map[string]any{"cycle": "monthly"})
	if renewRec.Code != http.StatusBadRequest || renewEnv.Code != response.CodeValidationFailed {
		t.Fatalf("已终止实例续费应 40002：HTTP %d, body=%s", renewRec.Code, renewRec.Body.String())
	}
	cancelRec, cancelEnv := doAPI(t, engine, http.MethodPost,
		"/api/v1/instances/"+itoa(instance.ID)+"/cancel", token, map[string]any{"type": "immediate"})
	if cancelRec.Code != http.StatusBadRequest || cancelEnv.Code != response.CodeValidationFailed {
		t.Fatalf("已终止实例再次申请应 40002：HTTP %d, body=%s", cancelRec.Code, cancelRec.Body.String())
	}

	// 另一台主机保留记录但 domainstatus=Deleted（上游形态二）→ 同样收敛。
	other := newInstanceFor(t, engine, gateway, gdb, token, product.ID)
	host.setDomainStatus(other.HostID, "Deleted")
	rec, envelope = doAPI(t, engine, http.MethodPost,
		"/api/v1/admin/instances/"+itoa(other.ID)+"/sync", admin, nil)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("Deleted 状态同步失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	result = decodeData[syncResultView](t, envelope)
	if !result.Terminated {
		t.Fatalf("domainstatus=Deleted 应触发收敛：%+v", result)
	}
	if got := instanceFromDB(t, gdb, other.ID); got.Status != model.InstanceStatusTerminated ||
		got.UpstreamStatus != "Deleted" || got.CancelStatus != model.InstanceCancelDone {
		t.Fatalf("Deleted 收敛结果异常: %+v", got)
	}
}

// TestSyncKeepsActiveWhenUpstreamReadFails 验证回读故障（非「主机不存在」）不触发误收敛。
func TestSyncKeepsActiveWhenUpstreamReadFails(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)
	admin := stage4AdminToken(t, engine, gdb, "readfailadmin", model.RoleAdmin)

	token, _ := memberTokenFor(t, engine, "readfailuser")
	instance := newInstanceFor(t, engine, gateway, gdb, token, product.ID)
	if rec, _ := doAPI(t, engine, http.MethodPost,
		"/api/v1/instances/"+itoa(instance.ID)+"/cancel", token, map[string]any{"type": "immediate"}); rec.Code != http.StatusOK {
		t.Fatalf("取消申请失败: HTTP %d", rec.Code)
	}

	host.setHostinfoFail("主机回读接口暂时不可用")
	rec, envelope := doAPI(t, engine, http.MethodPost,
		"/api/v1/admin/instances/"+itoa(instance.ID)+"/sync", admin, nil)
	if rec.Code != http.StatusInternalServerError || envelope.Code != response.CodeUpstreamFailed {
		t.Fatalf("回读故障应 50003：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	got := instanceFromDB(t, gdb, instance.ID)
	if got.Status != model.InstanceStatusActive || got.CancelStatus != model.InstanceCancelPending {
		t.Fatalf("回读故障不得改动状态: %+v", got)
	}
}
