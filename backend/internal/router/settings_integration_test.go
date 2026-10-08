package router

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
)

// ---------------------------------------------------------------------------
// 假上游（只读：登录 + 查余额；记录调用次数以断言设置生效与 JWT 缓存失效）
// ---------------------------------------------------------------------------

// fakeUpstreamServer 是假上游：/zjmf_api_login 返回固定 JWT，/cart/credit 返回固定余额。
type fakeUpstreamServer struct {
	server *httptest.Server
	jwt    string

	mu     sync.Mutex
	logins int
	credit int
}

func newFakeUpstreamServer(t *testing.T, jwt string) *fakeUpstreamServer {
	t.Helper()
	fake := &fakeUpstreamServer{jwt: jwt}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handle))
	t.Cleanup(fake.server.Close)
	return fake
}

func (s *fakeUpstreamServer) baseURL() string { return s.server.URL }

func (s *fakeUpstreamServer) handle(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/zjmf_api_login":
		s.mu.Lock()
		s.logins++
		s.mu.Unlock()
		_, _ = fmt.Fprintf(w, `{"jwt":%q,"status":200,"msg":"鉴权成功"}`, s.jwt)
	case "/cart/credit", "/cart/all":
		if r.URL.Path == "/cart/credit" {
			s.mu.Lock()
			s.credit++
			s.mu.Unlock()
		}
		// 业务调用必须带正确的 Bearer 头；否则按上游口径返回 405（触发客户端重新登录）。
		if r.Header.Get("Authorization") != "Bearer "+s.jwt {
			_, _ = io.WriteString(w, `{"status":405,"msg":"请登陆后再试"}`)
			return
		}
		if r.URL.Path == "/cart/all" {
			// 空目录：导入不产生任何商品，但会走完「登录 → 业务调用」全过程。
			_, _ = io.WriteString(w, `{"status":200,"msg":"请求成功","data":{"products":[],"count":0,"currency":"CNY"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"status":200,"msg":"请求成功","data":{"credit":"100.00"}}`)
	default:
		http.NotFound(w, r)
	}
}

func (s *fakeUpstreamServer) loginCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logins
}

func (s *fakeUpstreamServer) creditCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.credit
}

// upstreamHealth 调用探活接口并返回数据体。
func upstreamHealth(t *testing.T, engine http.Handler, token string) upstreamHealthData {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/upstream/health", token, nil)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("上游探活失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[upstreamHealthData](t, envelope)
}

// putEpaySettings 调用管理端设置接口更新易支付参数。
func putEpaySettings(t *testing.T, engine http.Handler, token string, body any) (*httptest.ResponseRecorder, apiEnvelope) {
	t.Helper()
	return doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/payment/epay", token, body)
}

// putUpstreamSettings 调用管理端设置接口更新上游参数。
func putUpstreamSettings(t *testing.T, engine http.Handler, token string, body any) (*httptest.ResponseRecorder, apiEnvelope) {
	t.Helper()
	return doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/upstream", token, body)
}

// ---------------------------------------------------------------------------
// 易支付设置
// ---------------------------------------------------------------------------

// TestEpaySettingsPermission 验证设置接口仅 admin 可用（含读取）。
func TestEpaySettingsPermission(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, nil)

	for _, role := range []string{model.RoleFinance, model.RoleSupport} {
		token := stage4AdminToken(t, engine, gdb, role, role)
		for _, request := range []struct {
			method string
			path   string
			body   any
		}{
			{http.MethodGet, "/api/v1/admin/settings/payment/epay", nil},
			{http.MethodPut, "/api/v1/admin/settings/payment/epay", map[string]any{"enabled": false}},
			{http.MethodGet, "/api/v1/admin/settings/upstream", nil},
			{http.MethodPut, "/api/v1/admin/settings/upstream", map[string]any{"timeout_seconds": 5}},
		} {
			rec, envelope := doAPI(t, engine, request.method, request.path, token, request.body)
			if rec.Code != http.StatusForbidden || envelope.Code != response.CodeForbidden {
				t.Fatalf("%s %s（角色 %s）应 403：HTTP %d, body=%s",
					request.method, request.path, role, rec.Code, rec.Body.String())
			}
		}
	}

	// 未认证 → 401。
	rec, _ := doAPI(t, engine, http.MethodGet, "/api/v1/admin/settings/payment/epay", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未认证应 401：HTTP %d", rec.Code)
	}
}

// TestEpaySettingsLifecycle 验证三态 key、脱敏、校验与生效（含审计字段）。
func TestEpaySettingsLifecycle(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, nil)
	adminToken := stage4AdminToken(t, engine, gdb, "admin", model.RoleAdmin)

	// 初始未配置。
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/settings/payment/epay", adminToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("读取设置失败: HTTP %d", rec.Code)
	}
	initial := decodeData[epaySettingsView](t, envelope)
	if initial.Enabled || initial.KeyConfigured || initial.KeyMasked != "" || initial.UpdatedAt != nil {
		t.Fatalf("初始设置应为空: %+v", initial)
	}
	if !strings.HasSuffix(initial.NotifyURLRecommended, "/api/v1/payments/epay/notify") {
		t.Fatalf("推荐回调地址 = %q", initial.NotifyURLRecommended)
	}

	// enabled=true 但缺字段 → 40002 且 message 列出缺失项。
	rec, envelope = putEpaySettings(t, engine, adminToken, map[string]any{"enabled": true})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("缺字段应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	for _, field := range []string{"gateway", "pid", "key", "notify_url"} {
		if !strings.Contains(envelope.Message, field) {
			t.Fatalf("message 未列出缺失项 %s：%s", field, envelope.Message)
		}
	}

	// URL 格式非法 → 40001。
	rec, envelope = putEpaySettings(t, engine, adminToken, map[string]any{"gateway": "pay.example.com"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("URL 非法应 40001：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 完整配置 + 启用。
	rec, envelope = putEpaySettings(t, engine, adminToken, map[string]any{
		"enabled":    true,
		"gateway":    testEpayGateway,
		"pid":        testEpayPID,
		"key":        testEpayKey,
		"notify_url": "https://oem.example.com/api/v1/payments/epay/notify",
	})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("启用失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	saved := decodeData[epaySettingsView](t, envelope)
	if !saved.Enabled || !saved.KeyConfigured {
		t.Fatalf("保存后视图错误: %+v", saved)
	}
	if saved.UpdatedAt == nil || saved.UpdatedBy == nil {
		t.Fatalf("缺少审计字段: %+v", saved)
	}
	// 脱敏：响应绝不含明文密钥，掩码只暴露前 4 位。
	if strings.Contains(rec.Body.String(), testEpayKey) {
		t.Fatalf("响应泄露商户密钥: %s", rec.Body.String())
	}
	if saved.KeyMasked != "test****" {
		t.Fatalf("掩码 = %q，期望 test****", saved.KeyMasked)
	}

	// 省略 key 的更新：保持不变（仍可发起支付 = 密钥未被清掉）。
	seedOrderProduct(t, gdb, model.ProductStatusOn)
	memberToken, _ := memberTokenFor(t, engine, "settinguser")
	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/payment/epay", adminToken,
		map[string]any{"return_url": "https://oem.example.com/pay/result"})
	if rec.Code != http.StatusOK {
		t.Fatalf("更新 return_url 失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	afterUpdate := decodeData[epaySettingsView](t, envelope)
	if !afterUpdate.KeyConfigured || afterUpdate.KeyMasked != saved.KeyMasked {
		t.Fatalf("省略 key 应保持原值: %+v", afterUpdate)
	}
	if afterUpdate.ReturnURL != "https://oem.example.com/pay/result" {
		t.Fatalf("return_url 未更新: %+v", afterUpdate)
	}

	// 提供空串：清空 key（同时停用，否则会被完整性校验拒绝）。
	rec, envelope = putEpaySettings(t, engine, adminToken, map[string]any{"enabled": false, "key": ""})
	if rec.Code != http.StatusOK {
		t.Fatalf("清空 key 失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	cleared := decodeData[epaySettingsView](t, envelope)
	if cleared.KeyConfigured || cleared.KeyMasked != "" {
		t.Fatalf("key 未被清空: %+v", cleared)
	}

	// 清空 key 后渠道不可用：发起支付返回明确业务错误（其余功能不受影响）。
	productID := productIDFromSeed(t, gdb)
	order := createOrder(t, engine, memberToken, map[string]any{
		"product_id": productID, "cycle": "annual", "config": map[string]any{},
	})
	rec, envelope = payOrder(t, engine, memberToken, order.ID, map[string]any{"channel": "epay"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("渠道不可用应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	// 其余功能不受影响：订单详情可读。
	if got := getOrderView(t, engine, memberToken, order.ID); got.Status != model.OrderStatusPending {
		t.Fatalf("订单状态 = %s，期望 pending", got.Status)
	}
}

// TestEpaySettingsEmptyBodyRejected 验证空请求体被拒绝（至少提供一个字段）。
func TestEpaySettingsEmptyBodyRejected(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, nil)
	adminToken := stage4AdminToken(t, engine, gdb, "admin", model.RoleAdmin)

	rec, envelope := putEpaySettings(t, engine, adminToken, map[string]any{})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("空请求体应 40001：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	rec, envelope = putUpstreamSettings(t, engine, adminToken, map[string]any{})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("空请求体应 40001：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
}

// TestEpayReturnRedirect 验证同步跳转：302 到设置的 return_url；未配置时输出提示页。
func TestEpayReturnRedirect(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, nil)
	adminToken := stage4AdminToken(t, engine, gdb, "admin", model.RoleAdmin)

	// 未配置 → 提示页（HTTP 200 + HTML）。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/payments/epay/return", nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "支付已完成") {
		t.Fatalf("未配置应输出提示页：HTTP %d, body=%s", recorder.Code, recorder.Body.String())
	}

	// 配置 return_url → 302。
	if _, envelope := putEpaySettings(t, engine, adminToken, map[string]any{
		"return_url": "https://oem.example.com/pay/result",
	}); envelope.Code != 0 {
		t.Fatalf("设置 return_url 失败: %s", toJSON(t, envelope))
	}
	recorder = httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/payments/epay/return", nil))
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "https://oem.example.com/pay/result" {
		t.Fatalf("同步跳转错误：HTTP %d, Location=%q", recorder.Code, recorder.Header().Get("Location"))
	}
}

// ---------------------------------------------------------------------------
// 上游设置：脱敏 + 生效（客户端与 JWT 缓存失效）
// ---------------------------------------------------------------------------

// TestUpstreamSettingsMaskingAndValidation 验证上游设置的读取脱敏与写入校验。
func TestUpstreamSettingsMaskingAndValidation(t *testing.T) {
	gdb := testDatabase(t)
	var logs bytes.Buffer
	engine := newStage4Engine(t, gdb, &logs)
	adminToken := stage4AdminToken(t, engine, gdb, "admin", model.RoleAdmin)

	// 初始：未配置，超时为缺省 5。
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/settings/upstream", adminToken, nil)
	if initial := decodeData[upstreamSettingsView](t, envelope); initial.APIKeyConfigured ||
		initial.TimeoutSeconds != settings.DefaultUpstreamTimeoutSeconds {
		t.Fatalf("初始上游设置错误: %+v", initial)
	}

	// 超时越界 → 40002；URL 非法 → 40001。
	rec, envelope = putUpstreamSettings(t, engine, adminToken, map[string]any{"timeout_seconds": 121})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("超时越界应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	rec, envelope = putUpstreamSettings(t, engine, adminToken, map[string]any{"base_url": "lyew.com"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("URL 非法应 40001：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 正常写入：回显不含明文密钥。
	rec, envelope = putUpstreamSettings(t, engine, adminToken, map[string]any{
		"base_url": "https://lyew.example.com", "username": "13800000000",
		"api_key": testUpstreamKey, "timeout_seconds": 30,
	})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("写入上游设置失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	saved := decodeData[upstreamSettingsView](t, envelope)
	if !saved.APIKeyConfigured || saved.APIKeyMasked != "test****" || saved.TimeoutSeconds != 30 {
		t.Fatalf("上游设置视图错误: %+v", saved)
	}
	if strings.Contains(rec.Body.String(), testUpstreamKey) {
		t.Fatalf("响应泄露上游密钥: %s", rec.Body.String())
	}
	if strings.Contains(logs.String(), testUpstreamKey) {
		t.Fatal("日志泄露上游密钥")
	}

	// 省略 api_key → 保持不变；空串 → 清空。
	rec, envelope = putUpstreamSettings(t, engine, adminToken, map[string]any{"username": "13900000000"})
	if !decodeData[upstreamSettingsView](t, envelope).APIKeyConfigured {
		t.Fatal("省略 api_key 应保持原值")
	}
	rec, envelope = putUpstreamSettings(t, engine, adminToken, map[string]any{"api_key": ""})
	cleared := decodeData[upstreamSettingsView](t, envelope)
	if cleared.APIKeyConfigured || cleared.APIKeyMasked != "" {
		t.Fatalf("api_key 未清空: %+v", cleared)
	}

	// 清空后探活报告未配置（不发起请求）。
	health := upstreamHealth(t, engine, adminToken)
	if health.Connected || health.Error != upstreamErrorNotConfigured {
		t.Fatalf("未配置探活 = %+v", health)
	}
}

// TestUpstreamSettingsTakesEffect 验证改设置后立即生效：
//   - 换地址后下一次调用就走新上游（旧上游不再被调用）；
//   - 只改密钥也会触发客户端重建，**旧 JWT 缓存一并失效**（业务调用重新登录）；
//   - 设置没变时复用客户端与缓存中的 JWT（不重复登录）。
//
// 说明：探活（/upstream/health）本身每次都主动登录以验证鉴权，因此这里用
// 商品导入（走 ensureJWT 的普通业务调用）来观察 JWT 缓存的复用与失效。
func TestUpstreamSettingsTakesEffect(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, nil)
	adminToken := stage4AdminToken(t, engine, gdb, "admin", model.RoleAdmin)

	upstreamA := newFakeUpstreamServer(t, "jwt-for-a")
	upstreamB := newFakeUpstreamServer(t, "jwt-for-b")

	writeUpstream := func(baseURL, apiKey string) {
		t.Helper()
		if _, envelope := putUpstreamSettings(t, engine, adminToken, map[string]any{
			"base_url": baseURL, "username": "13800000000", "api_key": apiKey, "timeout_seconds": 5,
		}); envelope.Code != 0 {
			t.Fatalf("写入上游设置失败: %s", toJSON(t, envelope))
		}
	}
	importOnce := func() {
		t.Helper()
		rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/admin/products/import", adminToken, nil)
		if rec.Code != http.StatusOK || envelope.Code != 0 {
			t.Fatalf("商品导入失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
		}
	}

	// 指向 A：探活成功（只读，走新客户端），随后一次导入完成首次登录。
	writeUpstream(upstreamA.baseURL(), testUpstreamKey)
	health := upstreamHealth(t, engine, adminToken)
	if !health.Connected || health.BaseURL != upstreamA.baseURL() {
		t.Fatalf("探活 A 失败: %+v", health)
	}
	importOnce()
	if upstreamA.loginCount() != 1 {
		t.Fatalf("A 登录次数 = %d，期望 1", upstreamA.loginCount())
	}

	// 设置未变：再次导入复用客户端与缓存中的 JWT（不再登录）。
	importOnce()
	if upstreamA.loginCount() != 1 {
		t.Fatalf("设置未变时不应重新登录：A 登录次数 = %d，期望 1", upstreamA.loginCount())
	}

	// 换地址到 B：下一次探活必须走 B，A 不再被调用。
	writeUpstream(upstreamB.baseURL(), testUpstreamKey)
	health = upstreamHealth(t, engine, adminToken)
	if !health.Connected || health.BaseURL != upstreamB.baseURL() {
		t.Fatalf("探活 B 失败: %+v", health)
	}
	creditCallsOnA := upstreamA.creditCount()
	importOnce()
	if upstreamB.loginCount() != 1 {
		t.Fatalf("B 登录次数 = %d，期望 1（新客户端应重新登录）", upstreamB.loginCount())
	}
	if upstreamA.creditCount() != creditCallsOnA {
		t.Fatalf("换地址后旧上游仍被调用：A credit=%d", upstreamA.creditCount())
	}

	// 只改 api_key（地址仍是 B）：同样触发重建，旧 JWT 缓存失效 → 重新登录一次。
	writeUpstream(upstreamB.baseURL(), "another-upstream-key")
	importOnce()
	if upstreamB.loginCount() != 2 {
		t.Fatalf("改密钥后应重新登录：B 登录次数 = %d，期望 2", upstreamB.loginCount())
	}
	health = upstreamHealth(t, engine, adminToken)
	// 探活沿用阶段 2 的掩码口径（首 4 位 + **** + 末 3 位，契约第 9 节），
	// 与设置接口的 MaskSecret（首 4 位 + ****，契约 12.1）不同：两者都不含明文。
	if !health.Connected || health.APIKeyMasked != "anot****key" {
		t.Fatalf("探活结果错误: %+v", health)
	}
	if strings.Contains(toJSON(t, health), "another-upstream-key") {
		t.Fatal("探活响应泄露上游密钥")
	}
}

// TestUpstreamSettingsHealthWhenNotConfigured 验证完全未配置时探活不发起请求。
func TestUpstreamSettingsHealthWhenNotConfigured(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, nil)
	adminToken := stage4AdminToken(t, engine, gdb, "admin", model.RoleAdmin)

	health := upstreamHealth(t, engine, adminToken)
	if health.Connected || health.Error != upstreamErrorNotConfigured || health.BaseURL != "" {
		t.Fatalf("未配置探活 = %+v", health)
	}
}

// TestUpstreamSettingsPartialConfigReportsMissing 验证「配一半」时探活报告未配置，
// 但设置接口仍能看到已填字段（便于管理员分步填写）。
func TestUpstreamSettingsPartialConfigReportsMissing(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, nil)
	adminToken := stage4AdminToken(t, engine, gdb, "admin", model.RoleAdmin)

	if _, envelope := putUpstreamSettings(t, engine, adminToken, map[string]any{
		"base_url": "https://lyew.example.com", "username": "13800000000",
	}); envelope.Code != 0 {
		t.Fatalf("写入失败: %s", toJSON(t, envelope))
	}

	health := upstreamHealth(t, engine, adminToken)
	if health.Connected || health.Error != upstreamErrorNotConfigured {
		t.Fatalf("配一半应视为未配置: %+v", health)
	}
	if health.BaseURL != "https://lyew.example.com" {
		t.Fatalf("探活应回带已配置的地址便于排查: %+v", health)
	}

	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/settings/upstream", adminToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("读取设置失败: HTTP %d", rec.Code)
	}
	view := decodeData[upstreamSettingsView](t, envelope)
	if view.BaseURL == "" || view.APIKeyConfigured {
		t.Fatalf("设置视图错误: %+v", view)
	}
}

// TestSettingsCorruptValueReportsInternal 验证库内设置值损坏时返回 500（而不是静默忽略）。
func TestSettingsCorruptValueReportsInternal(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, nil)
	adminToken := stage4AdminToken(t, engine, gdb, "admin", model.RoleAdmin)
	seedSetting(t, gdb, settings.KeyPaymentEpay, "{not-json")

	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/settings/payment/epay", adminToken, nil)
	if rec.Code != http.StatusInternalServerError || envelope.Code != response.CodeInternalError {
		t.Fatalf("损坏值应 500：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
}

// TestSettingsUpdatedByAudit 验证审计字段记录操作管理员 ID。
func TestSettingsUpdatedByAudit(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, nil)
	adminToken := stage4AdminToken(t, engine, gdb, "admin", model.RoleAdmin)

	var admin model.Admin
	if err := gdb.Where("username = ?", "stage4-admin").Take(&admin).Error; err != nil {
		t.Fatalf("读取管理员失败: %v", err)
	}

	if _, envelope := putUpstreamSettings(t, engine, adminToken, map[string]any{"timeout_seconds": 12}); envelope.Code != 0 {
		t.Fatalf("写入失败: %s", toJSON(t, envelope))
	}
	_, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/settings/upstream", adminToken, nil)
	view := decodeData[upstreamSettingsView](t, envelope)
	if view.UpdatedBy == nil || *view.UpdatedBy != admin.ID {
		t.Fatalf("updated_by = %v，期望 %d", view.UpdatedBy, admin.ID)
	}
	if view.UpdatedAt == nil {
		t.Fatal("缺少 updated_at")
	} else if _, err := time.Parse(time.RFC3339, *view.UpdatedAt); err != nil {
		t.Fatalf("updated_at 不是 RFC3339: %q", *view.UpdatedAt)
	}
}

// productIDFromSeed 取最近写入的测试商品 ID。
func productIDFromSeed(t *testing.T, gdb *gorm.DB) uint64 {
	t.Helper()
	var product model.Product
	if err := gdb.Order("id DESC").Take(&product).Error; err != nil {
		t.Fatalf("读取测试商品失败: %v", err)
	}
	return product.ID
}
