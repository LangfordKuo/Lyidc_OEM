package router

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/payment"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
)

// 阶段 4 集成测试的固定假值（与真实商户参数无关）。
const (
	testEpayGateway = "https://pay.example.com"
	testEpayPID     = "1001"
	testEpayKey     = "test-epay-key-0001"
	// testUpstreamBase 与 testUpstreamKey 是假上游地址与假密钥。
	testUpstreamKey = "test-upstream-key"
)

// ---------------------------------------------------------------------------
// 假易支付网关（httptest；按彩虹易支付标准协议实现地图：下单 + 验签）
// ---------------------------------------------------------------------------

// createCall 是一次收到的下单请求。
type createCall struct {
	form url.Values
}

// fakeEpayGateway 是假易支付网关：POST /mapi.php 下单（校验签名）并返回 payurl。
type fakeEpayGateway struct {
	server  *httptest.Server
	pid     string
	key     string
	payURL  string
	tradeNo string
	// rejectCode 非空时下单返回该 code（非 1 即失败），用于模拟渠道拒绝。
	rejectCode string

	mu      sync.Mutex
	creates []createCall
}

// newFakeEpayGateway 启动假网关（测试结束自动关闭）。
func newFakeEpayGateway(t *testing.T) *fakeEpayGateway {
	t.Helper()
	gateway := &fakeEpayGateway{
		pid:     testEpayPID,
		key:     testEpayKey,
		payURL:  "https://pay.example.com/pay/abc123",
		tradeNo: "GATEWAY-TRADE-1",
	}
	gateway.server = httptest.NewServer(http.HandlerFunc(gateway.handle))
	t.Cleanup(gateway.server.Close)
	return gateway
}

// baseURL 返回假网关地址（写入设置里的 gateway）。
func (g *fakeEpayGateway) baseURL() string { return g.server.URL }

// handle 处理下单请求：校验签名后返回 payurl；签名错误按渠道协议返回 code=-1。
func (g *fakeEpayGateway) handle(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/mapi.php" {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		_, _ = io.WriteString(w, `{"code":-1,"msg":"参数解析失败"}`)
		return
	}
	g.mu.Lock()
	g.creates = append(g.creates, createCall{form: r.PostForm})
	g.mu.Unlock()

	params := map[string]string{}
	for key := range r.PostForm {
		params[key] = r.PostForm.Get(key)
	}
	if r.PostForm.Get("pid") != g.pid || !payment.VerifySign(params, g.key) {
		_, _ = io.WriteString(w, `{"code":-1,"msg":"签名错误"}`)
		return
	}
	if g.rejectCode != "" {
		_, _ = fmt.Fprintf(w, `{"code":%s,"msg":"渠道拒绝下单"}`, g.rejectCode)
		return
	}
	_, _ = fmt.Fprintf(w, `{"code":1,"trade_no":%q,"payurl":%q}`, g.tradeNo, g.payURL)
}

// created 返回收到的下单请求（最近一次在最后）。
func (g *fakeEpayGateway) created() []createCall {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]createCall(nil), g.creates...)
}

// setRejectCode 设置下单失败码（空串表示恢复正常）。
func (g *fakeEpayGateway) setRejectCode(code string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.rejectCode = code
}

// notifyValues 生成一份**签名正确**的异步通知参数（trade_status=TRADE_SUCCESS）。
//
// 渠道单号默认按 out_trade_no 派生，money 由调用方给出（可用于构造金额不符用例）。
func (g *fakeEpayGateway) notifyValues(outTradeNo, money string) url.Values {
	now := time.Now().UTC()
	params := map[string]string{
		"pid":          g.pid,
		"trade_no":     "GATEWAY-" + outTradeNo,
		"out_trade_no": outTradeNo,
		"type":         payment.PayTypeAlipay,
		"name":         "测试商品",
		"money":        money,
		"trade_status": "TRADE_SUCCESS",
		"timestamp":    fmt.Sprintf("%d", now.Unix()),
	}
	params["sign"] = payment.Sign(params, g.key)
	params["sign_type"] = "MD5"

	values := url.Values{}
	for key, value := range params {
		values.Set(key, value)
	}
	return values
}

// signNotify 用商户密钥对参数重新签名（用于篡改用例）。
func (g *fakeEpayGateway) signNotify(params map[string]string) string {
	return payment.Sign(params, g.key)
}

// postEpayNotify 以 form 方式提交异步通知，返回记录器（响应体应为纯文本 success / fail）。
func postEpayNotify(t *testing.T, engine http.Handler, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/payments/epay/notify", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// notifyAck 提交通知并返回响应正文（去掉首尾空白）。
func notifyAck(t *testing.T, engine http.Handler, values url.Values) string {
	t.Helper()
	rec := postEpayNotify(t, engine, values)
	if rec.Code != http.StatusOK {
		t.Fatalf("回调应返回 HTTP 200，得到 %d（body=%s）", rec.Code, rec.Body.String())
	}
	return strings.TrimSpace(rec.Body.String())
}

// ---------------------------------------------------------------------------
// 测试数据辅助
// ---------------------------------------------------------------------------

// newStage4Engine 构造阶段 4 集成测试引擎：真实数据库 + 假支付/假上游（由设置驱动）。
// logs 非 nil 时同时把服务端日志写入该缓冲（用于断言日志不含密钥明文）。
//
// 阶段 5a 起支付成功会触发交付：此处注入 noopDeliverer，保持阶段 4 用例
// 「支付后订单停留在 paid」的原断言（交付链路由 newStage5Engine 的专门用例覆盖）。
func newStage4Engine(t *testing.T, gdb *gorm.DB, logs *bytes.Buffer) *gin.Engine {
	t.Helper()
	var handler io.Writer = io.Discard
	if logs != nil {
		handler = logs
	}
	return New(Options{
		Logger:   slog.New(slog.NewTextHandler(handler, nil)),
		DB:       gdb,
		JWT:      config.JWTConfig{Secret: testJWTSecret, ExpireHours: 168},
		Delivery: noopDeliverer{},
	})
}

// seedSetting 直接写库写入一条设置（绕过管理端接口，用于测试准备）；
// 同键重复调用按「覆盖」处理（与 UpsertSetting 同语义，便于用例改写设置）。
func seedSetting(t *testing.T, gdb *gorm.DB, key, value string) {
	t.Helper()
	now := time.Now().UTC()
	updatedBy := uint64(0)
	setting := model.Setting{Key: key, Value: value, UpdatedBy: &updatedBy, CreatedAt: now, UpdatedAt: now}
	if err := gdb.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_by", "updated_at"}),
	}).Create(&setting).Error; err != nil {
		t.Fatalf("写入测试设置失败: %v", err)
	}
}

// seedEpaySetting 写库配置易支付渠道（指向假网关，密钥为测试假值）。
func seedEpaySetting(t *testing.T, gdb *gorm.DB, gatewayURL, notifyURL string) {
	t.Helper()
	value := fmt.Sprintf(
		`{"enabled":true,"gateway":%q,"pid":%q,"key":%q,"notify_url":%q,"return_url":""}`,
		gatewayURL, testEpayPID, testEpayKey, notifyURL)
	seedSetting(t, gdb, settings.KeyPaymentEpay, value)
}

// seededProductPIDs 为测试商品分配互不重复的上游商品 ID（uk_products_upstream_pid）。
var seededProductPIDs atomic.Int64

// seedOrderProduct 写库创建一个已上架商品：固定价 monthly 100.00 / annual 200.00，
// 含两个会员可见可配置项——区域（配置项 id=11，可见取值 id=111/112、隐藏取值 id=113）与
// 操作系统（配置项 id=12，取值 id=121=CentOS-7.9 / 122=Debian-12；阶段 5b 重装列表按该项取
// `os_config_option_id`）；upstream_id 为 0 或展示值——真实数据恒为 0，仅作透传展示，
// 不参与下单与交付口径（契约 14.5）。
func seedOrderProduct(t *testing.T, gdb *gorm.DB, status string) *model.Product {
	t.Helper()

	upstreamPID := int(seededProductPIDs.Add(1)) + 7000
	now := time.Now().UTC()
	product := model.Product{
		UpstreamPID:     upstreamPID,
		UpstreamGroupID: 800,
		Name:            "阶段4测试商品",
		Type:            "dcimcloud",
		Module:          "idcsmart_common",
		ConfigJSON: `{"products":{"id":7001,"name":"阶段4测试商品","stock_control":0,"qty":0,"ontrial":0},` +
			`"config_groups":[{"id":1,"name":"区域","options":[{"id":11,"gid":1,"option_name":"area|区域",` +
			`"option_type":1,"upstream_id":101,"hidden":0,"sub":[` +
			`{"id":111,"config_id":11,"option_name":"香港","upstream_id":201,"hidden":0,"pricings":[]},` +
			`{"id":112,"config_id":11,"option_name":"美国","upstream_id":202,"hidden":0,"pricings":[]},` +
			`{"id":113,"config_id":11,"option_name":"隐藏机房","upstream_id":203,"hidden":1,"pricings":[]}]}]},` +
			`{"id":2,"name":"系统","options":[{"id":12,"gid":2,"option_name":"os|操作系统",` +
			`"option_type":5,"upstream_id":0,"hidden":0,"sub":[` +
			`{"id":121,"config_id":12,"option_name":"CentOS-7.9","upstream_id":0,"hidden":0,"pricings":[]},` +
			`{"id":122,"config_id":12,"option_name":"Debian-12","upstream_id":0,"hidden":0,"pricings":[]}]}]}],` +
			`"customfields":[]}`,
		UpstreamPricesJSON: `{"code":"CNY","prices":{"monthly":"90.00","annual":"180.00"}}`,
		PricingJSON:        `{"mode":"fixed","fixed":{"monthly":"100.00","annual":"200.00"}}`,
		Status:             status,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := gdb.Create(&product).Error; err != nil {
		t.Fatalf("写入测试商品失败: %v", err)
	}
	return &product
}

// memberTokenFor 注册并登录一个会员，返回 token 与会员视图。
func memberTokenFor(t *testing.T, engine http.Handler, username string) (string, memberView) {
	t.Helper()
	registerMember(t, engine, username, username+"@example.com", "password123")
	return loginMember(t, engine, username, "password123")
}

// createOrder 调用下单接口并断言成功。
func createOrder(t *testing.T, engine http.Handler, token string, body map[string]any) orderView {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/orders", token, body)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("下单失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[orderView](t, envelope)
}

// payOrder 调用发起支付接口。
func payOrder(t *testing.T, engine http.Handler, token string, orderID uint64, body map[string]any) (*httptest.ResponseRecorder, apiEnvelope) {
	t.Helper()
	return doAPI(t, engine, http.MethodPost, "/api/v1/orders/"+itoa(orderID)+"/pay", token, body)
}

// getOrderView 调用订单详情接口并断言成功。
func getOrderView(t *testing.T, engine http.Handler, token string, orderID uint64) orderView {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/orders/"+itoa(orderID), token, nil)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("订单详情失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[orderView](t, envelope)
}
