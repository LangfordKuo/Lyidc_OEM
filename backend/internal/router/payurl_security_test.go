package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
)

// P5 安全整改 finding#07（契约 12.2.1）：渠道返回的 payurl 必须过协议白名单。
//
// 渗透实测场景：网关被指向攻击者（或网关方 / 链路中间人）时返回
// `{"code":1,"payurl":"javascript:alert(1)"}`，前端原样 `location.assign` 即在本站源执行
// 任意 JS 并外带 localStorage 里的会员 JWT。本用例用**真实 HTTP 假网关**跑完整链路：
// 会员下单 → 发起支付 → 渠道返回非法 payurl → 接口回 50002 且不下发 payurl。

// TestPayOrderRejectsIllegalPayurl 覆盖「正向对照 + 非法协议拒绝 + 日志脱敏」三件事。
func TestPayOrderRejectsIllegalPayurl(t *testing.T) {
	gdb := testDatabase(t)
	var logs bytes.Buffer
	engine := newStage4Engine(t, gdb, "", &logs)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, _ := memberTokenFor(t, engine, "payurluser")
	order := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "annual", "config": map[string]any{},
	})

	// ① 正向对照：合法 https payurl 正常下发（白名单不是「一律拒绝」）。
	rec, envelope := payOrder(t, engine, token, order.ID, map[string]any{"channel": "epay"})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("合法 payurl 应正常下发：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	pay := decodeData[payOrderView](t, envelope)
	if pay.Pay.PayURL != gateway.payURL {
		t.Fatalf("payurl = %q，期望 %q", pay.Pay.PayURL, gateway.payURL)
	}

	// ② 渗透场景：网关返回 `javascript:` 载荷 → 50002（渠道响应异常，沿用既有错误码）。
	const payload = "javascript:alert(document.cookie)"
	gateway.setPayURL(payload)
	logs.Reset()

	rec, envelope = payOrder(t, engine, token, order.ID, map[string]any{"channel": "epay"})
	if rec.Code != http.StatusInternalServerError || envelope.Code != response.CodePaymentGateway {
		t.Fatalf("非法 payurl 应 50002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	// 拒绝后**不下发 payurl**：失败响应的 data 为 null，message 里也不回显地址。
	if string(envelope.Data) != "null" {
		t.Fatalf("失败响应不应带 data：%s", envelope.Data)
	}
	if strings.Contains(envelope.Message, "javascript") || strings.Contains(envelope.Message, "alert(") {
		t.Fatalf("错误提示回显了非法 payurl：%q", envelope.Message)
	}

	// ③ 日志脱敏：WARN 里既没有完整载荷，也没有 payurl 原文。
	if logged := logs.String(); strings.Contains(logged, "alert(document.cookie)") || strings.Contains(logged, payload) {
		t.Fatalf("日志泄漏了非法 payurl：%s", logged)
	} else if !strings.Contains(logged, "易支付下单响应异常") {
		t.Fatalf("非法 payurl 应留下一条可排查的 WARN：%s", logged)
	}

	// ④ 订单不受影响：仍停在 pending，会员可改用合法地址重试。
	detail := getOrderView(t, engine, token, order.ID)
	if detail.Status != "pending" {
		t.Fatalf("下单失败不应改动订单状态：%s", detail.Status)
	}
	gateway.setPayURL("https://pay.example.com/pay/retry")
	rec, envelope = payOrder(t, engine, token, order.ID, map[string]any{"channel": "epay"})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("恢复合法地址后应可再次发起支付：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
}

// TestPayurlRejectionLogKeepsOnlyTarget 锁定日志口径：即使 payurl 合法（带签名查询串），
// 日志里也只留「协议 + 主机」，绝不把签名参数写进日志文件。
func TestPayurlRejectionLogKeepsOnlyTarget(t *testing.T) {
	gdb := testDatabase(t)
	var logs bytes.Buffer
	engine := newStage4Engine(t, gdb, "", &logs)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, _ := memberTokenFor(t, engine, "payurlsecret")
	order := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "annual", "config": map[string]any{},
	})

	// 渠道先返回一个 code=0 的失败响应，触发那条带响应摘要的 WARN；
	// 摘要里即便出现 URL，也只允许保留协议与主机。
	gateway.setRejectCode("0")
	rec, _ := payOrder(t, engine, token, order.ID, map[string]any{"channel": "epay"})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("渠道拒绝应 500：HTTP %d", rec.Code)
	}
	if logged := logs.String(); strings.Contains(logged, testEpayKey) {
		t.Fatalf("日志泄漏了商户密钥：%s", logged)
	}
}

// TestEpayGatewaySettingRejectsScriptScheme 补一条设置侧用例：
// `gateway` 本身必须是 http(s) 绝对地址（既有实现已校验，本批只补测试，不改行为）。
func TestEpayGatewaySettingRejectsScriptScheme(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	adminToken := stage4AdminToken(t, engine, gdb, "admin", model.RoleAdmin)

	for _, gatewayURL := range []string{
		"javascript:alert(1)",
		"data:text/html,<script>1</script>",
		"//evil.example.com",
		"pay.example.com",
	} {
		rec, envelope := putEpaySettings(t, engine, adminToken, map[string]any{"gateway": gatewayURL})
		if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
			t.Fatalf("gateway=%q 应 40001：HTTP %d, body=%s", gatewayURL, rec.Code, rec.Body.String())
		}
	}

	// 合法地址照常接受（对照）。
	rec, envelope := putEpaySettings(t, engine, adminToken, map[string]any{
		"enabled": true, "gateway": "https://pay.example.com", "pid": "1001",
		"key": "test-epay-key-0001", "notify_url": "https://oem.example.com/api/v1/payments/epay/notify",
	})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("合法 gateway 应接受：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	var view map[string]any
	if err := json.Unmarshal(envelope.Data, &view); err != nil {
		t.Fatalf("解析设置视图失败: %v", err)
	}
	if view["gateway"] != "https://pay.example.com" {
		t.Fatalf("gateway 回显 = %v", view["gateway"])
	}
}
