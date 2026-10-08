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
// 阶段 5b：实例操作接口（电源/重装/改密 + 管理端暂停/恢复/同步 + 审计）
// ---------------------------------------------------------------------------

// TestInstancePowerOperations 验证五个电源操作的完整链路：
// 上游 func 参数、假上游状态变化、审计落库（含开通 audit）与操作记录接口。
func TestInstancePowerOperations(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, member := memberTokenFor(t, engine, "poweruser")
	instance := newInstanceFor(t, engine, gateway, gdb, token, product.ID)

	cases := []struct {
		op         string
		wantAction string
		wantFunc   string
		wantPower  string
	}{
		{"soft_on", model.ActionPowerOn, "on", "on"},
		{"soft_off", model.ActionPowerOff, "off", "off"},
		{"reboot", model.ActionReboot, "reboot", "on"},
		{"hard_off", model.ActionHardOff, "hard_off", "off"},
		{"hard_reboot", model.ActionHardReboot, "hard_reboot", "on"},
	}
	for _, tc := range cases {
		path := "/api/v1/instances/" + itoa(instance.ID) + "/power"
		rec, envelope := doAPI(t, engine, http.MethodPost, path, token, map[string]any{"op": tc.op})
		if rec.Code != http.StatusOK || envelope.Code != 0 {
			t.Fatalf("%s 失败: HTTP %d, body=%s", tc.op, rec.Code, rec.Body.String())
		}
		result := decodeData[operationResultView](t, envelope)
		if result.Action != tc.wantAction || result.InstanceID != instance.ID {
			t.Fatalf("%s 返回 = %+v，期望 action=%s", tc.op, result, tc.wantAction)
		}
		if form := host.lastForm(pathProvisionDef); form.Get("func") != tc.wantFunc ||
			form.Get("id") != itoa(uint64(instance.HostID)) {
			t.Fatalf("%s 上游参数错误: func=%q id=%q", tc.op, form.Get("func"), form.Get("id"))
		}
		if state := host.state(instance.HostID); state == nil || state.powerState != tc.wantPower {
			t.Fatalf("%s 后假上游电源状态 = %+v，期望 %s", tc.op, state, tc.wantPower)
		}
	}

	// 审计：开通 1 条（system create）+ 5 条电源操作（member success）。
	logs := instanceLogsFromDB(t, gdb, instance.ID)
	if len(logs) != 6 {
		t.Fatalf("操作记录条数 = %d，期望 6（1 开通 + 5 电源）: %+v", len(logs), logs)
	}
	if logs[len(logs)-1].Action != model.ActionCreate || logs[len(logs)-1].ActorType != model.ActorTypeSystem {
		t.Fatalf("最早一条应为开通审计（system/create）: %+v", logs[len(logs)-1])
	}
	for _, lg := range logs[:5] {
		if lg.Status != model.InstanceOpSuccess || lg.ActorType != model.ActorTypeMember || lg.ActorID != member.ID {
			t.Fatalf("电源操作审计异常: %+v", lg)
		}
	}

	// 会员端操作记录接口：本人可查、最新在前。
	rec, envelope := doAPI(t, engine, http.MethodGet,
		"/api/v1/instances/"+itoa(instance.ID)+"/logs?page=1&page_size=10", token, nil)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("操作记录接口失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	list := decodeData[instanceLogListView](t, envelope)
	if list.Total != 6 || len(list.Items) != 6 || list.Items[0].Action != model.ActionHardReboot {
		t.Fatalf("操作记录内容错误: %+v", list)
	}
}

// TestInstanceOperationGuardsAndIsolation 验证状态约束、鉴权与会员隔离。
func TestInstanceOperationGuardsAndIsolation(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)
	admin := stage4AdminToken(t, engine, gdb, "opsadmin", model.RoleAdmin)

	tokenA, _ := memberTokenFor(t, engine, "opsusera")
	tokenB, _ := memberTokenFor(t, engine, "opsuserb")
	instance := newInstanceFor(t, engine, gateway, gdb, tokenA, product.ID)
	powerPath := "/api/v1/instances/" + itoa(instance.ID) + "/power"

	// 会员 B 操作会员 A 的实例 → 404（与不存在同口径）。
	rec, envelope := doAPI(t, engine, http.MethodPost, powerPath, tokenB, map[string]any{"op": "soft_on"})
	if rec.Code != http.StatusNotFound || envelope.Code != response.CodeNotFound {
		t.Fatalf("跨会员操作应 404：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	// 无 token → 401；管理员 token 调会员接口 → 401（aud 不匹配）。
	rec, _ = doAPI(t, engine, http.MethodPost, powerPath, "", map[string]any{"op": "soft_on"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("无凭证应 401，得到 %d", rec.Code)
	}
	rec, _ = doAPI(t, engine, http.MethodPost, powerPath, admin, map[string]any{"op": "soft_on"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("管理员 token 调会员接口应 401，得到 %d", rec.Code)
	}
	// 非法 op → 40001。
	rec, envelope = doAPI(t, engine, http.MethodPost, powerPath, tokenA, map[string]any{"op": "explode"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("非法 op 应 40001：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// suspended 实例不可操作 → 40002，且审计记 fail（不调上游）。
	setInstanceStatus(t, gdb, instance.ID, model.InstanceStatusSuspended)
	rec, envelope = doAPI(t, engine, http.MethodPost, powerPath, tokenA, map[string]any{"op": "soft_on"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("suspended 实例操作应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(envelope.Message, "仅在 active 状态可用") {
		t.Fatalf("提示文案 = %q，期望说明仅 active 可操作", envelope.Message)
	}
	last := lastInstanceLog(t, gdb, instance.ID)
	if last.Status != model.InstanceOpFail || last.Action != model.ActionPowerOn {
		t.Fatalf("失败尝试应留痕（fail）：%+v", last)
	}

	// 重装与改密在 suspended 下同样拒绝。
	for _, path := range []string{"/reinstall", "/reset-password"} {
		rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/instances/"+itoa(instance.ID)+path,
			tokenA, map[string]any{"os_id": 9, "password": "Abcd1234"})
		if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
			t.Fatalf("suspended 实例 %s 应 40002：HTTP %d, body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

// TestInstanceReinstallFlow 验证重装系统列表（上游需 os_config_option_id）与重装发起、失败路径。
func TestInstanceReinstallFlow(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, _ := memberTokenFor(t, engine, "reinstalluser")
	instance := newInstanceFor(t, engine, gateway, gdb, token, product.ID)

	// 可选系统列表：必须带上商品的 os 配置项 id（商品 config_json 中 `os|操作系统` → id=12）。
	rec, envelope := doAPI(t, engine, http.MethodGet,
		"/api/v1/instances/"+itoa(instance.ID)+"/reinstall-options", token, nil)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("重装列表失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	options := decodeData[reinstallOptionsView](t, envelope)
	if len(options.OS) != 2 || options.OS[0].ID != 9 {
		t.Fatalf("可选系统 = %+v，期望 2 条且首条 id=9", options.OS)
	}
	form := host.lastForm(pathHostCloudOS)
	if form.Get("productid") != itoa(uint64(product.UpstreamPID)) || form.Get("os_config_option_id") != "12" {
		t.Fatalf("cloudos 参数错误: productid=%q os_config_option_id=%q",
			form.Get("productid"), form.Get("os_config_option_id"))
	}

	// 发起重装：os_id 与 port 原样提交上游，假上游记录当前系统。
	rec, envelope = doAPI(t, engine, http.MethodPost,
		"/api/v1/instances/"+itoa(instance.ID)+"/reinstall", token,
		map[string]any{"os_id": 12, "port": 2200})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("重装发起失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	form = host.lastForm(pathProvisionDef)
	if form.Get("func") != "reinstall" || form.Get("os") != "12" || form.Get("port") != "2200" {
		t.Fatalf("重装参数错误: func=%q os=%q port=%q", form.Get("func"), form.Get("os"), form.Get("port"))
	}
	if state := host.state(instance.HostID); state == nil || state.osID != 12 {
		t.Fatalf("假上游系统未更新: %+v", state)
	}
	if last := lastInstanceLog(t, gdb, instance.ID); last.Action != model.ActionReinstall || last.Status != model.InstanceOpSuccess {
		t.Fatalf("重装审计异常: %+v", last)
	}

	// 参数校验：os_id 必须为正整数。
	rec, envelope = doAPI(t, engine, http.MethodPost,
		"/api/v1/instances/"+itoa(instance.ID)+"/reinstall", token, map[string]any{"os_id": 0})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("os_id=0 应 40001：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 上游失败 → 50003 + 审计 fail（错误脱敏透传）。
	host.setProvisionFail("reinstall", "目标系统暂不可用")
	rec, envelope = doAPI(t, engine, http.MethodPost,
		"/api/v1/instances/"+itoa(instance.ID)+"/reinstall", token, map[string]any{"os_id": 9})
	if rec.Code != http.StatusInternalServerError || envelope.Code != response.CodeUpstreamFailed {
		t.Fatalf("上游失败应 50003：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(envelope.Message, "目标系统暂不可用") {
		t.Fatalf("错误应透传上游原因: %q", envelope.Message)
	}
	if last := lastInstanceLog(t, gdb, instance.ID); last.Status != model.InstanceOpFail {
		t.Fatalf("上游失败应审计 fail: %+v", last)
	}
}

// TestInstanceResetPassword 验证改密口径：自动生成、指定密码、弱密码拒绝、审计不含密码。
func TestInstanceResetPassword(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, _ := memberTokenFor(t, engine, "passuser")
	instance := newInstanceFor(t, engine, gateway, gdb, token, product.ID)
	path := "/api/v1/instances/" + itoa(instance.ID) + "/reset-password"

	// 自动生成：16 位，落库到实例记录，上游收到同一密码。
	rec, envelope := doAPI(t, engine, http.MethodPost, path, token, map[string]any{})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("自动生成密码失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	first := decodeData[operationResultView](t, envelope)
	if len([]rune(first.Password)) != 16 {
		t.Fatalf("自动密码长度 = %d，期望 16", len([]rune(first.Password)))
	}
	if form := host.lastForm(pathProvisionDef); form.Get("func") != "crack_pass" || form.Get("password") != first.Password {
		t.Fatalf("上游改密参数错误: func=%q", form.Get("func"))
	}
	if got := instanceFromDB(t, gdb, instance.ID); got.Password != first.Password {
		t.Fatalf("新密码未落库: %q != %q", got.Password, first.Password)
	}

	// 指定密码：合法强度通过。
	rec, envelope = doAPI(t, engine, http.MethodPost, path, token, map[string]any{"password": "Abcd1234"})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("指定密码失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := instanceFromDB(t, gdb, instance.ID); got.Password != "Abcd1234" {
		t.Fatalf("指定密码未落库: %q", got.Password)
	}

	// 弱密码拒绝：纯数字 / 过短。
	for _, weak := range []string{"12345678", "abc"} {
		rec, envelope = doAPI(t, engine, http.MethodPost, path, token, map[string]any{"password": weak})
		if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
			t.Fatalf("弱密码 %q 应 40002：HTTP %d, body=%s", weak, rec.Code, rec.Body.String())
		}
	}

	// 审计与响应不含密码明文。
	logs := instanceLogsFromDB(t, gdb, instance.ID)
	for _, lg := range logs {
		if strings.Contains(lg.Message, first.Password) || strings.Contains(lg.Message, "Abcd1234") {
			t.Fatalf("审计泄露密码: %+v", lg)
		}
	}
	if strings.Contains(rec.Body.String(), first.Password) {
		t.Fatal("弱密码失败响应不应包含历史密码")
	}
}

// TestAdminSuspendUnsuspendAndPermissions 验证管理端暂停/恢复的状态机、角色矩阵与审计。
func TestAdminSuspendUnsuspendAndPermissions(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	admin := stage4AdminToken(t, engine, gdb, "susadmin", model.RoleAdmin)
	finance := stage4AdminToken(t, engine, gdb, "susfin", model.RoleFinance)
	support := stage4AdminToken(t, engine, gdb, "sussup", model.RoleSupport)
	token, _ := memberTokenFor(t, engine, "sususer")
	instance := newInstanceFor(t, engine, gateway, gdb, token, product.ID)

	suspendPath := "/api/v1/admin/instances/" + itoa(instance.ID) + "/suspend"
	unsuspendPath := "/api/v1/admin/instances/" + itoa(instance.ID) + "/unsuspend"

	// 角色矩阵：suspend / unsuspend 仅 admin（finance / support 403，会员 401）。
	for name, at := range map[string]string{"finance": finance, "support": support} {
		rec, envelope := doAPI(t, engine, http.MethodPost, suspendPath, at, map[string]any{"reason": "x"})
		if rec.Code != http.StatusForbidden || envelope.Code != response.CodeForbidden {
			t.Fatalf("%s 暂停应 403：HTTP %d, body=%s", name, rec.Code, rec.Body.String())
		}
	}
	rec, _ := doAPI(t, engine, http.MethodPost, suspendPath, token, map[string]any{"reason": "x"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("会员调管理端接口应 401，得到 %d", rec.Code)
	}

	// reason 必填。
	rec, envelope := doAPI(t, engine, http.MethodPost, suspendPath, admin, map[string]any{"reason": "  "})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("空原因应 40001：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 暂停成功：上游收到 func=suspend + reason，本地置 suspended，审计 actor=admin。
	rec, envelope = doAPI(t, engine, http.MethodPost, suspendPath, admin, map[string]any{"reason": "涉嫌滥用资源"})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("暂停失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	result := decodeData[operationResultView](t, envelope)
	if result.Status != model.InstanceStatusSuspended {
		t.Fatalf("暂停返回状态 = %s，期望 suspended", result.Status)
	}
	form := host.lastForm(pathProvisionDef)
	if form.Get("func") != "suspend" || form.Get("reason") != "涉嫌滥用资源" {
		t.Fatalf("上游暂停参数错误: func=%q reason=%q", form.Get("func"), form.Get("reason"))
	}
	if got := instanceFromDB(t, gdb, instance.ID); got.Status != model.InstanceStatusSuspended {
		t.Fatalf("本地状态 = %s，期望 suspended", got.Status)
	}
	last := lastInstanceLog(t, gdb, instance.ID)
	if last.Action != model.ActionSuspend || last.ActorType != model.ActorTypeAdmin || !strings.Contains(last.Message, "涉嫌滥用资源") {
		t.Fatalf("暂停审计异常: %+v", last)
	}

	// 重复暂停 → 40002；恢复非暂停实例 → 40002（先恢复再试）。
	rec, envelope = doAPI(t, engine, http.MethodPost, suspendPath, admin, map[string]any{"reason": "再次"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("重复暂停应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 恢复成功：上游 func=unsuspend，本地回到 active。
	rec, envelope = doAPI(t, engine, http.MethodPost, unsuspendPath, admin, nil)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("恢复失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if form := host.lastForm(pathProvisionDef); form.Get("func") != "unsuspend" {
		t.Fatalf("上游恢复参数错误: %q", form.Get("func"))
	}
	if got := instanceFromDB(t, gdb, instance.ID); got.Status != model.InstanceStatusActive {
		t.Fatalf("恢复后本地状态 = %s，期望 active", got.Status)
	}
	rec, envelope = doAPI(t, engine, http.MethodPost, unsuspendPath, admin, nil)
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("恢复非暂停实例应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
}

// TestAdminInstanceSync 验证同步接口：回写同步字段、电源状态、按上游状态收敛本地状态。
func TestAdminInstanceSync(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	admin := stage4AdminToken(t, engine, gdb, "syncadmin", model.RoleAdmin)
	finance := stage4AdminToken(t, engine, gdb, "syncfin", model.RoleFinance)
	support := stage4AdminToken(t, engine, gdb, "syncsup", model.RoleSupport)
	token, _ := memberTokenFor(t, engine, "syncuser")
	instance := newInstanceFor(t, engine, gateway, gdb, token, product.ID)

	// 调整假上游：到期时间 +1 小时、电源 off、IP 变化。
	newDue := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	host.mu.Lock()
	state := host.hosts[instance.HostID]
	state.nextDueUnix = newDue.Unix()
	state.powerState = "off"
	state.dedicatedIP = "198.51.100.77"
	host.mu.Unlock()

	syncPath := "/api/v1/admin/instances/" + itoa(instance.ID) + "/sync"

	// 角色矩阵：sync 所有角色可调用；会员 401。
	rec, _ := doAPI(t, engine, http.MethodPost, syncPath, token, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("会员调同步应 401，得到 %d", rec.Code)
	}
	for name, at := range map[string]string{"admin": admin, "finance": finance, "support": support} {
		rec, envelope := doAPI(t, engine, http.MethodPost, syncPath, at, nil)
		if rec.Code != http.StatusOK || envelope.Code != 0 {
			t.Fatalf("%s 同步失败: HTTP %d, body=%s", name, rec.Code, rec.Body.String())
		}
		result := decodeData[syncResultView](t, envelope)
		if result.Action != model.ActionSync || result.PowerState != "off" {
			t.Fatalf("%s 同步返回 = %+v，期望 action=sync power_state=off", name, result)
		}
	}

	got := instanceFromDB(t, gdb, instance.ID)
	if got.NextDueDate == nil || got.NextDueDate.UTC().Unix() != newDue.Unix() {
		t.Fatalf("到期时间 = %v，期望 %v", got.NextDueDate, newDue)
	}
	if got.DedicatedIP != "198.51.100.77" || got.UpstreamStatus != "Active" {
		t.Fatalf("同步字段未回写: %+v", got)
	}

	// 上游暂停而本地仍 active → 同步收敛本地状态为 suspended（status_changed=true）。
	host.mu.Lock()
	host.hosts[instance.HostID].suspended = true
	host.mu.Unlock()
	rec, envelope := doAPI(t, engine, http.MethodPost, syncPath, admin, nil)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("同步失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	result := decodeData[syncResultView](t, envelope)
	if !result.StatusChanged || result.Status != model.InstanceStatusSuspended {
		t.Fatalf("期望状态收敛为 suspended：%+v", result)
	}
	if got := instanceFromDB(t, gdb, instance.ID); got.Status != model.InstanceStatusSuspended {
		t.Fatalf("本地状态 = %s，期望 suspended", got.Status)
	}

	// 上游主机不存在（回读失败）→ 50003 + 审计 fail。
	host.setHostinfoMissing(true)
	rec, envelope = doAPI(t, engine, http.MethodPost, syncPath, admin, nil)
	if rec.Code != http.StatusInternalServerError || envelope.Code != response.CodeUpstreamFailed {
		t.Fatalf("回读失败应 50003：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if last := lastInstanceLog(t, gdb, instance.ID); last.Action != model.ActionSync || last.Status != model.InstanceOpFail {
		t.Fatalf("同步失败应审计 fail: %+v", last)
	}
}

// TestInstanceLogsEndpoints 验证操作记录接口的隔离、角色与分页校验。
func TestInstanceLogsEndpoints(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)
	admin := stage4AdminToken(t, engine, gdb, "logsadmin", model.RoleAdmin)

	tokenA, _ := memberTokenFor(t, engine, "logsusera")
	tokenB, _ := memberTokenFor(t, engine, "logsuserb")
	instance := newInstanceFor(t, engine, gateway, gdb, tokenA, product.ID)

	// 会员 B 查会员 A 的实例记录 → 404。
	rec, envelope := doAPI(t, engine, http.MethodGet,
		"/api/v1/instances/"+itoa(instance.ID)+"/logs", tokenB, nil)
	if rec.Code != http.StatusNotFound || envelope.Code != response.CodeNotFound {
		t.Fatalf("跨会员查询记录应 404：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 管理端可查（所有角色）。
	for name, at := range map[string]string{"admin": admin} {
		rec, envelope = doAPI(t, engine, http.MethodGet,
			"/api/v1/admin/instances/"+itoa(instance.ID)+"/logs", at, nil)
		if rec.Code != http.StatusOK || envelope.Code != 0 {
			t.Fatalf("%s 查询记录失败: HTTP %d, body=%s", name, rec.Code, rec.Body.String())
		}
		list := decodeData[instanceLogListView](t, envelope)
		if list.Total != 1 || list.Items[0].Action != model.ActionCreate {
			t.Fatalf("管理端记录内容错误: %+v", list)
		}
	}

	// 管理端查询不存在的实例 → 404；分页参数非法 → 40001。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/instances/999999/logs", admin, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在实例的记录应 404，得到 %d", rec.Code)
	}
	rec, envelope = doAPI(t, engine, http.MethodGet,
		"/api/v1/instances/"+itoa(instance.ID)+"/logs?page=0", tokenA, nil)
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("非法分页应 40001：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
}
