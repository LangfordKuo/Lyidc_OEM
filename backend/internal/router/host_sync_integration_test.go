package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

// ---------------------------------------------------------------------------
// 上游状态回推接收端（R16，契约 22.3）：POST /api/host/sync
// ---------------------------------------------------------------------------

// hostSyncResponseBody 是回推接口的响应体测试视图（**上游约定格式，不是统一响应包**）。
type hostSyncResponseBody struct {
	Status int    `json:"status"`
	Msg    string `json:"msg"`
	// Code 只用于回归断言：回推接口的响应里**不得**出现本站响应包的 code 字段
	// （防以后有人把它改回统一响应包）。
	Code *int `json:"code"`
}

// syncToken 是测试用的 downstream_token（32 位十六进制）。
const syncToken = "0123456789abcdef0123456789abcdef"

// postHostSync 按上游口径发一次 form 推送（签名由调用方填入）。
func postHostSync(t *testing.T, engine http.Handler, form url.Values) (*httptest.ResponseRecorder, hostSyncResponseBody) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/host/sync", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	var body hostSyncResponseBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("回推响应不是合法 JSON: %v（body=%s）", err, rec.Body.String())
	}
	return rec, body
}

// signedSyncForm 组装一条带正确签名的推送（字段由调用方按需覆盖）。
func signedSyncForm(instance *model.Instance, randStr string) url.Values {
	form := url.Values{}
	form.Set("id", itoa(instance.ID))
	form.Set("rand_str", randStr)
	form.Set("domainstatus", "Active")
	form.Set("signature", hostSyncSignature(itoa(instance.ID), randStr, instance.DownstreamToken))
	return form
}

// syncSeedSeq 为每次落库的实例分配互不重复的 order_id / host_id（两列都有唯一键）。
var syncSeedSeq atomic.Int64

// seedSyncInstance 直接落一条实例（回推端点的最小前提：id + downstream_token）。
func seedSyncInstance(t *testing.T, gdb *gorm.DB, status, token string) *model.Instance {
	t.Helper()
	seq := syncSeedSeq.Add(1)
	now := time.Now().UTC()
	instance := model.Instance{
		MemberID: 1, OrderID: 70000 + uint64(seq), HostID: int(88000 + seq),
		ProductID: 1, ProductName: "回推测试商品",
		Name: "oem-sync", BillingCycle: "monthly", Status: status, DownstreamToken: token,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := gdb.Create(&instance).Error; err != nil {
		t.Fatalf("落实例失败: %v", err)
	}
	return &instance
}

// instanceRow 读库取实例最新状态（不存在时 Fatal）。
func instanceRow(t *testing.T, gdb *gorm.DB, id uint64) *model.Instance {
	t.Helper()
	var instance model.Instance
	if err := gdb.First(&instance, id).Error; err != nil {
		t.Fatalf("查询实例失败: %v", err)
	}
	return &instance
}

// instanceLogs 读库取实例的审计记录（按 id 升序）。
func instanceLogs(t *testing.T, gdb *gorm.DB, id uint64) []model.InstanceOperationLog {
	t.Helper()
	logs := make([]model.InstanceOperationLog, 0)
	if err := gdb.Where("instance_id = ?", id).Order("id ASC").Find(&logs).Error; err != nil {
		t.Fatalf("查询实例审计失败: %v", err)
	}
	return logs
}

// TestHostSyncAppliesStatusAndFields 验证一条合法推送真的改了本地状态与同步字段、写一条 host_sync 审计，
// 并断言**响应形状**（有 status、没有 code）。
func TestHostSyncAppliesStatusAndFields(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage5Engine(t, gdb, nil, nil)
	instance := seedSyncInstance(t, gdb, model.InstanceStatusActive, syncToken)

	due := time.Now().UTC().AddDate(0, 1, 0).Truncate(time.Second)
	form := signedSyncForm(instance, "aB3xY9")
	form.Set("domainstatus", "Suspended")
	form.Set("host_id", "88001")
	form.Set("domain", "oem-sync.example.com")
	form.Set("username", "root")
	form.Set("password", "UpstreamPass123")
	form.Set("dedicatedip", "203.0.113.10")
	form.Set("assignedips", "203.0.113.11,203.0.113.12")
	form.Set("port", "22022")
	form.Set("nextduedate", strconv.FormatInt(due.Unix(), 10))
	form.Set("type", "create")
	form.Set("signature", hostSyncSignature(itoa(instance.ID), "aB3xY9", syncToken))

	rec, body := postHostSync(t, engine, form)
	if rec.Code != http.StatusOK || body.Status != 200 || body.Msg != "更新成功" {
		t.Fatalf("回推应答 = HTTP %d %+v，期望 200/200/更新成功（body=%s）", rec.Code, body, rec.Body.String())
	}
	// 响应形状回归（契约第 2 节第 5 条）：上游只认 status，绝不能变成本站响应包。
	if body.Code != nil || strings.Contains(rec.Body.String(), `"code"`) {
		t.Fatalf("回推响应不得含统一响应包的 code 字段: %s", rec.Body.String())
	}

	updated := instanceRow(t, gdb, instance.ID)
	if updated.Status != model.InstanceStatusSuspended || updated.UpstreamStatus != "Suspended" {
		t.Fatalf("状态未按推送同步: status=%s upstream_status=%s", updated.Status, updated.UpstreamStatus)
	}
	if updated.NextDueDate == nil || !updated.NextDueDate.Equal(due) {
		t.Fatalf("到期时间 = %v，期望 %v", updated.NextDueDate, due)
	}
	if updated.DedicatedIP != "203.0.113.10" || updated.AssignedIPs != "203.0.113.11,203.0.113.12" ||
		updated.Port != 22022 {
		t.Fatalf("IP/端口未同步: %+v", updated)
	}
	if updated.Username != "root" || updated.Password != "UpstreamPass123" {
		t.Fatalf("账号字段未同步: username=%q password=%q", updated.Username, updated.Password)
	}
	// 回推只改主机信息：id / 占位或真实 host_id / token 都不动。
	if updated.ID != instance.ID || updated.HostID != instance.HostID ||
		updated.DownstreamToken != instance.DownstreamToken {
		t.Fatalf("回推不应改变实例身份与鉴权串: %+v", updated)
	}

	logs := instanceLogs(t, gdb, instance.ID)
	if len(logs) != 1 {
		t.Fatalf("审计条数 = %d，期望 1", len(logs))
	}
	if logs[0].Action != model.ActionHostSync || logs[0].ActorType != model.ActorTypeSystem || logs[0].ActorID != 0 {
		t.Fatalf("审计口径错误: %+v", logs[0])
	}
	if strings.Contains(logs[0].Message, "UpstreamPass123") {
		t.Fatalf("审计描述泄露主机密码: %q", logs[0].Message)
	}
}

// TestHostSyncIdempotent 验证幂等：同状态连推两次，第二次**不写库、不写审计**。
func TestHostSyncIdempotent(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage5Engine(t, gdb, nil, nil)
	instance := seedSyncInstance(t, gdb, model.InstanceStatusActive, syncToken)

	form := signedSyncForm(instance, "aB3xY9")
	if _, body := postHostSync(t, engine, form); body.Status != 200 {
		t.Fatalf("首次推送失败: %+v", body)
	}
	if got := len(instanceLogs(t, gdb, instance.ID)); got != 1 {
		t.Fatalf("首次推送后审计条数 = %d，期望 1", got)
	}

	// 把 updated_at 改成一个可区分的过去值：若第二次真的写了库，这个值会被改写。
	past := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	if err := gdb.Exec("UPDATE instances SET updated_at = ? WHERE id = ?", past, instance.ID).Error; err != nil {
		t.Fatalf("改写 updated_at 失败: %v", err)
	}

	// 第二次推送：rand_str 不同（签名因此不同），但状态与字段完全一致。
	if _, body := postHostSync(t, engine, signedSyncForm(instance, "Zx9Q2m")); body.Status != 200 {
		t.Fatalf("重复推送失败: %+v", body)
	}
	if got := len(instanceLogs(t, gdb, instance.ID)); got != 1 {
		t.Fatalf("无变化的重复推送不应新增审计，条数 = %d", got)
	}
	if after := instanceRow(t, gdb, instance.ID); !after.UpdatedAt.Equal(past) {
		t.Fatalf("无变化的重复推送不应写库（updated_at 被改写为 %v）", after.UpdatedAt)
	}
}

// TestHostSyncRejectsInvalidRequests 验证各类非法请求一律 400 且**不改任何数据**：
// 缺 signature / rand_str、id 非法、实例不存在、downstream_token 为空（存量老实例）、验签失败。
func TestHostSyncRejectsInvalidRequests(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage5Engine(t, gdb, nil, nil)
	instance := seedSyncInstance(t, gdb, model.InstanceStatusActive, syncToken)
	legacy := seedSyncInstance(t, gdb, model.InstanceStatusActive, "") // 存量老实例（未登记 token）

	cases := []struct {
		name string
		form url.Values
		want string
	}{
		{
			name: "缺 signature",
			form: func() url.Values {
				form := signedSyncForm(instance, "aB3xY9")
				form.Del("signature")
				return form
			}(),
			want: msgHostSyncIncomplete,
		},
		{
			name: "缺 rand_str",
			form: func() url.Values {
				form := signedSyncForm(instance, "aB3xY9")
				form.Del("rand_str")
				return form
			}(),
			want: msgHostSyncIncomplete,
		},
		{
			name: "id 非数字",
			form: func() url.Values {
				form := signedSyncForm(instance, "aB3xY9")
				form.Set("id", "abc")
				return form
			}(),
			want: msgHostSyncGeneric,
		},
		{
			name: "实例不存在",
			form: func() url.Values {
				form := signedSyncForm(instance, "aB3xY9")
				form.Set("id", "999999")
				form.Set("signature", hostSyncSignature("999999", "aB3xY9", syncToken))
				return form
			}(),
			want: msgHostSyncGeneric,
		},
		{
			name: "未登记 downstream_token（存量老实例）",
			form: func() url.Values {
				form := url.Values{}
				form.Set("id", itoa(legacy.ID))
				form.Set("rand_str", "aB3xY9")
				form.Set("domainstatus", "Suspended")
				form.Set("signature", hostSyncSignature(itoa(legacy.ID), "aB3xY9", syncToken))
				return form
			}(),
			want: msgHostSyncGeneric,
		},
		{
			name: "验签失败",
			form: func() url.Values {
				form := signedSyncForm(instance, "aB3xY9")
				form.Set("signature", "00000000000000000000000000000000")
				return form
			}(),
			want: msgHostSyncBadSignature,
		},
		{
			name: "用别人的 token 签名（等同于验签失败）",
			form: func() url.Values {
				form := signedSyncForm(legacy, "aB3xY9")
				form.Set("id", itoa(instance.ID))
				form.Set("signature", hostSyncSignature(itoa(instance.ID), "aB3xY9", syncToken+"x"))
				return form
			}(),
			want: msgHostSyncBadSignature,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, body := postHostSync(t, engine, tc.form)
			if rec.Code != http.StatusOK {
				t.Fatalf("回推接口的 HTTP 状态码应恒为 200，得到 %d", rec.Code)
			}
			if body.Status != 400 || body.Msg != tc.want {
				t.Fatalf("回推应答 = %+v，期望 400/%s", body, tc.want)
			}
		})
	}

	// 全部失败请求都不得改动实例、不得写审计。
	if got := instanceRow(t, gdb, instance.ID); got.Status != model.InstanceStatusActive ||
		got.UpstreamStatus != "" || got.Username != "" {
		t.Fatalf("失败请求不应改动实例: %+v", got)
	}
	if got := len(instanceLogs(t, gdb, instance.ID)) + len(instanceLogs(t, gdb, legacy.ID)); got != 0 {
		t.Fatalf("失败请求不应写审计，实际 %d 条", got)
	}
}

// TestHostSyncStatusMappingIntegration 端到端跑一遍状态映射表（契约 22.3）：
// Active/Suspended 互相收敛；Cancelled/Pending/Fraud/未知值**不改本地状态**（只存 upstream_status 原文，
// 其中 Cancelled 见契约 15.8.1 的 5c 定稿）；Deleted 收敛为 terminated，且**终态不可逆**（后续推送不再改状态）。
func TestHostSyncStatusMappingIntegration(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage5Engine(t, gdb, nil, nil)
	instance := seedSyncInstance(t, gdb, model.InstanceStatusActive, syncToken)

	push := func(randStr, domainStatus string) {
		t.Helper()
		form := signedSyncForm(instance, randStr)
		form.Set("domainstatus", domainStatus)
		form.Set("signature", hostSyncSignature(itoa(instance.ID), randStr, syncToken))
		if _, body := postHostSync(t, engine, form); body.Status != 200 {
			t.Fatalf("推送 domainstatus=%s 失败: %+v", domainStatus, body)
		}
	}

	// 已知值：逐个收敛本地状态，且原文照存（最后停在 active，供 Cancelled 场景使用）。
	for i, tc := range []struct{ domainStatus, wantStatus string }{
		{"Suspended", model.InstanceStatusSuspended},
		{"Active", model.InstanceStatusActive},
		{"Suspended", model.InstanceStatusSuspended},
		{"Active", model.InstanceStatusActive},
	} {
		push("rand"+strconv.Itoa(i), tc.domainStatus)
		if got := instanceRow(t, gdb, instance.ID); got.Status != tc.wantStatus || got.UpstreamStatus != tc.domainStatus {
			t.Fatalf("domainstatus=%s → status=%s upstream_status=%s，期望 %s/%s",
				tc.domainStatus, got.Status, got.UpstreamStatus, tc.wantStatus, tc.domainStatus)
		}
	}

	// Cancelled（契约 15.8.1 的 5c 定稿）：本地 `cancelled` 是保留但**不再写入**的预留枚举——
	// 上游推 Cancelled **不改本地状态**，只更新 upstream_status 原文并写一条审计。
	// 关键场景：上游仍在运行（本地 active）时推 Cancelled，会员的操作能力必须原样保留
	// （操作矩阵只有 active 可操作，若映射成 cancelled，会员会在主机还活着时失去操作能力）。
	cancelBeforeLogs := len(instanceLogs(t, gdb, instance.ID))
	push("cancel01", "Cancelled")
	cancelRow := instanceRow(t, gdb, instance.ID)
	if cancelRow.Status != model.InstanceStatusActive {
		t.Fatalf("domainstatus=Cancelled 不应改本地状态（须保持 active），得到 %s", cancelRow.Status)
	}
	if cancelRow.UpstreamStatus != "Cancelled" {
		t.Fatalf("domainstatus=Cancelled 应原文照存 upstream_status，得到 %q", cancelRow.UpstreamStatus)
	}
	cancelLogs := instanceLogs(t, gdb, instance.ID)
	if len(cancelLogs) != cancelBeforeLogs+1 {
		t.Fatalf("Cancelled 推送有变化（upstream_status）应写一条审计，条数 = %d，期望 %d",
			len(cancelLogs), cancelBeforeLogs+1)
	}
	if last := cancelLogs[len(cancelLogs)-1]; last.Action != model.ActionHostSync || last.ActorType != model.ActorTypeSystem {
		t.Fatalf("Cancelled 推送应写 host_sync 审计（actor=system），得到 %+v", last)
	}

	// Pending / Fraud / 未知值：**不改本地状态**，只更新 upstream_status 原文。
	for i, domainStatus := range []string{"Pending", "Fraud", "SomeUnknownStatus"} {
		push("unknown"+strconv.Itoa(i), domainStatus)
		got := instanceRow(t, gdb, instance.ID)
		if got.Status != model.InstanceStatusActive {
			t.Fatalf("domainstatus=%s 不应改本地状态，得到 %s", domainStatus, got.Status)
		}
		if got.UpstreamStatus != domainStatus {
			t.Fatalf("domainstatus=%s 应原文照存，得到 %q", domainStatus, got.UpstreamStatus)
		}
	}

	// Deleted → terminated；终态不可逆：再推 Active 也不复活。
	push("deleted1", "Deleted")
	if got := instanceRow(t, gdb, instance.ID); got.Status != model.InstanceStatusTerminated {
		t.Fatalf("Deleted 应收敛为 terminated，得到 %s", got.Status)
	}
	push("deleted2", "Active")
	got := instanceRow(t, gdb, instance.ID)
	if got.Status != model.InstanceStatusTerminated {
		t.Fatalf("终态 terminated 不应被后续推送改写，得到 %s", got.Status)
	}
	if got.UpstreamStatus != "Active" {
		t.Fatalf("终态下仍应照存 upstream_status 原文，得到 %q", got.UpstreamStatus)
	}
}

// TestHostSyncFieldPresenceSemantics 验证「未出现在表单里的字段不改动、出现的空值即清空」（契约 22.3）：
// 少字段的推送不得清空既有数据（上游部分推送时的数据安全）。
func TestHostSyncFieldPresenceSemantics(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage5Engine(t, gdb, nil, nil)
	instance := seedSyncInstance(t, gdb, model.InstanceStatusActive, syncToken)

	// 第一次：完整的字段推送。
	full := signedSyncForm(instance, "full000")
	full.Set("username", "root")
	full.Set("password", "UpstreamPass123")
	full.Set("dedicatedip", "203.0.113.10")
	full.Set("assignedips", "203.0.113.11")
	full.Set("port", "22022")
	if _, body := postHostSync(t, engine, full); body.Status != 200 {
		t.Fatalf("完整推送失败: %+v", body)
	}

	// 第二次：只推 domainstatus（缺 username / password / IP / 端口）→ 一律保持原值。
	sparse := signedSyncForm(instance, "sparse1")
	sparse.Set("domainstatus", "Suspended")
	sparse.Set("signature", hostSyncSignature(itoa(instance.ID), "sparse1", syncToken))
	if _, body := postHostSync(t, engine, sparse); body.Status != 200 {
		t.Fatalf("稀疏推送失败: %+v", body)
	}
	got := instanceRow(t, gdb, instance.ID)
	if got.Username != "root" || got.Password != "UpstreamPass123" ||
		got.DedicatedIP != "203.0.113.10" || got.AssignedIPs != "203.0.113.11" || got.Port != 22022 {
		t.Fatalf("未出现在表单里的字段不应被清空: %+v", got)
	}
	if got.Status != model.InstanceStatusSuspended {
		t.Fatalf("状态应已收敛为 suspended，得到 %s", got.Status)
	}

	// 第三次：显式给出空值 → 按原文清空（上游确实没有该值）。
	cleared := signedSyncForm(instance, "clear01")
	cleared.Set("domainstatus", "Suspended")
	cleared.Set("dedicatedip", "")
	cleared.Set("signature", hostSyncSignature(itoa(instance.ID), "clear01", syncToken))
	if _, body := postHostSync(t, engine, cleared); body.Status != 200 {
		t.Fatalf("空值推送失败: %+v", body)
	}
	if got := instanceRow(t, gdb, instance.ID); got.DedicatedIP != "" {
		t.Fatalf("出现且为空的字段应清空，得到 %q", got.DedicatedIP)
	}
}

// TestHostSyncURLIsTopLevel 验证路由挂在**顶层**（契约 22.3）：精确路径可 POST，
// 而 /api/v1/host/sync 不存在（回推接口不在业务 API 分组里）。
func TestHostSyncURLIsTopLevel(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage5Engine(t, gdb, nil, nil)

	rec, _ := doAPI(t, engine, http.MethodPost, "/api/v1/host/sync", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/api/v1/host/sync 应 404（回推接口在顶层），得到 HTTP %d", rec.Code)
	}

	rec, body := postHostSync(t, engine, url.Values{})
	if rec.Code != http.StatusOK || body.Status != 400 {
		t.Fatalf("顶层 /api/host/sync 应可达并回 400（缺参数），得到 HTTP %d %+v", rec.Code, body)
	}
}
