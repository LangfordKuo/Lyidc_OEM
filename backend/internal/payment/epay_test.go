package payment

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// md5hex 是测试用的 MD5 小写十六进制实现（与 Sign 的期望原文逐字比对）。
func md5hex(text string) string {
	sum := md5.Sum([]byte(text))
	return hex.EncodeToString(sum[:])
}

func TestSignStandardAlgorithm(t *testing.T) {
	const key = "test-key"
	params := map[string]string{
		"pid":          "1000",
		"type":         "alipay",
		"out_trade_no": "O20261008TEST",
		"name":         "月付套餐",
		"money":        "10.00",
		"notify_url":   "https://api.example.com/api/v1/payments/epay/notify",
		"empty":        "",
	}

	// 参数名 ASCII 升序、剔空值、原值拼接（不做 URL 编码）、末尾直接拼 KEY。
	preimage := "money=10.00" +
		"&name=月付套餐" +
		"&notify_url=https://api.example.com/api/v1/payments/epay/notify" +
		"&out_trade_no=O20261008TEST" +
		"&pid=1000" +
		"&type=alipay" + key
	want := md5hex(preimage)

	if got := Sign(params, key); got != want {
		t.Fatalf("Sign = %q, 期望 %q（原文 %q）", got, want, preimage)
	}

	// sign / sign_type 不参与签名；空值参数不参与签名。
	withSignature := map[string]string{}
	for name, value := range params {
		withSignature[name] = value
	}
	withSignature["sign"] = "abc"
	withSignature["sign_type"] = "MD5"
	if got := Sign(withSignature, key); got != want {
		t.Fatalf("带 sign/sign_type 后 Sign = %q, 期望不变 %q", got, want)
	}

	// 原值拼接：值里的 & 与 = 不转义，直接进签名原文。
	special := map[string]string{"a": "x&y=z"}
	specialPreimage := "a=x&y=z" + key
	if got := Sign(special, key); got != md5hex(specialPreimage) {
		t.Fatalf("特殊字符 Sign 原文不正确: %q", specialPreimage)
	}
}

func TestVerifySign(t *testing.T) {
	const key = "test-key"
	params := map[string]string{
		"pid":          "1000",
		"out_trade_no": "O20261008TEST",
		"trade_no":     "2026100822001",
		"money":        "10.00",
		"trade_status": "TRADE_SUCCESS",
	}
	params["sign"] = Sign(params, key)

	if !VerifySign(params, key) {
		t.Fatal("合法签名校验失败")
	}

	// 大小写不敏感（签名统一小写输出）。
	upper := map[string]string{}
	for name, value := range params {
		upper[name] = value
	}
	upper["sign"] = strings.ToUpper(params["sign"])
	if !VerifySign(upper, key) {
		t.Fatal("大写签名应视为合法")
	}

	// 篡改金额后签名失效。
	tampered := map[string]string{}
	for name, value := range params {
		tampered[name] = value
	}
	tampered["money"] = "0.01"
	if VerifySign(tampered, key) {
		t.Fatal("篡改金额后签名仍通过")
	}

	// 缺失 sign。
	missing := map[string]string{}
	for name, value := range params {
		missing[name] = value
	}
	delete(missing, "sign")
	if VerifySign(missing, key) {
		t.Fatal("缺失 sign 应校验失败")
	}

	// 换 key 后签名失效。
	if VerifySign(params, "other-key") {
		t.Fatal("换 key 后签名仍通过")
	}
}

func TestParseNotify(t *testing.T) {
	const key = "test-key"
	provider := NewEpay(EpayConfig{PID: "1000", Key: key}, nil, nil)

	newNotifyRequest := func(method string, params map[string]string) *http.Request {
		values := url.Values{}
		for name, value := range params {
			values.Set(name, value)
		}
		if method == http.MethodPost {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/payments/epay/notify", strings.NewReader(values.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			return req
		}
		return httptest.NewRequest(http.MethodGet, "/api/v1/payments/epay/notify?"+values.Encode(), nil)
	}

	baseParams := func() map[string]string {
		return map[string]string{
			"pid":          "1000",
			"trade_no":     "2026100822001",
			"out_trade_no": "R20261008TEST",
			"type":         "alipay",
			"name":         "余额充值",
			"money":        "100.00",
			"trade_status": "TRADE_SUCCESS",
		}
	}

	t.Run("POST 成功回调", func(t *testing.T) {
		params := baseParams()
		params["sign"] = Sign(params, key)
		params["sign_type"] = "MD5"

		notification, err := provider.ParseNotify(newNotifyRequest(http.MethodPost, params))
		if err != nil {
			t.Fatalf("解析回调失败: %v", err)
		}
		if notification.OutTradeNo != "R20261008TEST" || notification.TradeNo != "2026100822001" {
			t.Fatalf("单号解析错误: %+v", notification)
		}
		if notification.Amount != "100.00" || notification.PayType != "alipay" {
			t.Fatalf("金额/支付方式解析错误: %+v", notification)
		}
		if !notification.Paid {
			t.Fatal("TRADE_SUCCESS 应判定为支付成功")
		}
	})

	t.Run("GET 成功回调", func(t *testing.T) {
		params := baseParams()
		params["sign"] = Sign(params, key)

		notification, err := provider.ParseNotify(newNotifyRequest(http.MethodGet, params))
		if err != nil {
			t.Fatalf("GET 回调解析失败: %v", err)
		}
		if !notification.Paid {
			t.Fatal("GET 回调应判定为支付成功")
		}
	})

	t.Run("非成功状态", func(t *testing.T) {
		params := baseParams()
		params["trade_status"] = "WAIT_BUYER_PAY"
		params["sign"] = Sign(params, key)

		notification, err := provider.ParseNotify(newNotifyRequest(http.MethodPost, params))
		if err != nil {
			t.Fatalf("解析回调失败: %v", err)
		}
		if notification.Paid {
			t.Fatal("非 TRADE_SUCCESS 不应判定为支付成功")
		}
	})

	t.Run("篡改金额验签失败", func(t *testing.T) {
		params := baseParams()
		params["sign"] = Sign(params, key)
		params["money"] = "0.01"

		if _, err := provider.ParseNotify(newNotifyRequest(http.MethodPost, params)); err == nil {
			t.Fatal("篡改金额后验签应失败")
		}
	})

	t.Run("pid 不一致", func(t *testing.T) {
		params := baseParams()
		params["pid"] = "9999"
		params["sign"] = Sign(params, key)

		if _, err := provider.ParseNotify(newNotifyRequest(http.MethodPost, params)); err == nil {
			t.Fatal("pid 不一致应失败")
		}
	})

	t.Run("缺少必要参数", func(t *testing.T) {
		params := baseParams()
		delete(params, "out_trade_no")
		params["sign"] = Sign(params, key)

		if _, err := provider.ParseNotify(newNotifyRequest(http.MethodPost, params)); err == nil {
			t.Fatal("缺少 out_trade_no 应失败")
		}
	})

	t.Run("不支持的签名类型", func(t *testing.T) {
		params := baseParams()
		params["sign"] = Sign(params, key)
		params["sign_type"] = "RSA"

		if _, err := provider.ParseNotify(newNotifyRequest(http.MethodPost, params)); err == nil {
			t.Fatal("sign_type=RSA 应失败")
		}
	})
}

func TestCreateOrder(t *testing.T) {
	const (
		key        = "test-key"
		notifyURL  = "https://api.example.com/api/v1/payments/epay/notify"
		wantReturn = "https://api.example.com/api/v1/payments/epay/return"
	)

	type captured struct {
		values url.Values
	}
	var last captured

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != epayCreatePath {
			t.Errorf("下单路径 = %s, 期望 %s", r.URL.Path, epayCreatePath)
		}
		if r.Method != http.MethodPost {
			t.Errorf("下单方法 = %s, 期望 POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
			t.Errorf("Content-Type = %q", ct)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("解析表单失败: %v", err)
		}
		last.values = r.PostForm

		params := map[string]string{}
		for name := range r.PostForm {
			params[name] = r.PostForm.Get(name)
		}
		wantSign := md5hex(signPreimageString(params, key))
		if got := params["sign"]; got != wantSign {
			t.Errorf("下单签名 = %q, 期望 %q", got, wantSign)
		}

		_, _ = w.Write([]byte(`{"code":1,"msg":"ok","trade_no":"2026100822002","payurl":"https://pay.example.com/qr/abc","qrcode":"https://pay.example.com/qr/abc.png"}`))
	}))
	defer server.Close()

	provider := NewEpay(EpayConfig{
		Gateway:   server.URL,
		PID:       "1000",
		Key:       key,
		NotifyURL: notifyURL,
	}, server.Client(), nil)

	result, err := provider.CreateOrder(context.Background(), CreateRequest{
		OutTradeNo: "O20261008TEST",
		Amount:     "10.00",
		Subject:    "月付套餐",
		PayType:    "wxpay",
	})
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}
	if result.PayURL != "https://pay.example.com/qr/abc" || result.TradeNo != "2026100822002" {
		t.Fatalf("下单结果解析错误: %+v", result)
	}
	if result.PayType != "wxpay" {
		t.Fatalf("pay_type = %q, 期望 wxpay", result.PayType)
	}
	if result.Extra["qrcode"] != "https://pay.example.com/qr/abc.png" {
		t.Fatalf("qrcode 未透传: %+v", result.Extra)
	}

	// 下单请求参数逐项核对（含 return_url 由 notify_url 推导）。
	want := map[string]string{
		"pid":          "1000",
		"type":         "wxpay",
		"out_trade_no": "O20261008TEST",
		"notify_url":   notifyURL,
		"return_url":   wantReturn,
		"name":         "月付套餐",
		"money":        "10.00",
		"sign_type":    "MD5",
	}
	for name, value := range want {
		if got := last.values.Get(name); got != value {
			t.Errorf("下单参数 %s = %q, 期望 %q", name, got, value)
		}
	}
	if last.values.Get("sign") == "" {
		t.Error("下单请求缺少 sign")
	}
}

// signPreimageString 按标准协议重建签名原文（测试侧独立实现，用于交叉验证）。
func signPreimageString(params map[string]string, key string) string {
	names := make([]string, 0, len(params))
	for name, value := range params {
		if name == "sign" || name == "sign_type" || value == "" {
			continue
		}
		names = append(names, name)
	}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+params[name])
	}
	return strings.Join(parts, "&") + key
}

func TestCreateOrderFailures(t *testing.T) {
	respond := func(body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
	}

	cases := []struct {
		name string
		body string
	}{
		{"渠道返回失败码", `{"code":0,"msg":"商户不存在"}`},
		{"响应缺少 payurl", `{"code":1,"trade_no":"1"}`},
		{"响应不是 JSON", `<html>502</html>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := respond(tc.body)
			defer server.Close()

			provider := NewEpay(EpayConfig{Gateway: server.URL, PID: "1000", Key: "k"}, server.Client(), nil)
			if _, err := provider.CreateOrder(context.Background(), CreateRequest{
				OutTradeNo: "O1", Amount: "1.00", Subject: "测试",
			}); err == nil {
				t.Fatal("异常响应应返回错误")
			}
		})
	}

	t.Run("不支持的支付方式", func(t *testing.T) {
		provider := NewEpay(EpayConfig{Gateway: "http://127.0.0.1:1", PID: "1000", Key: "k"}, nil, nil)
		_, err := provider.CreateOrder(context.Background(), CreateRequest{
			OutTradeNo: "O1", Amount: "1.00", Subject: "测试", PayType: "qqpay",
		})
		if err == nil || !strings.Contains(err.Error(), "支付方式") {
			t.Fatalf("期望支付方式错误，得到 %v", err)
		}
	})
}

func TestParseEpayCreateResponseCodeTypes(t *testing.T) {
	// code 为字符串 "1" 的实现也应被接受。
	result, err := parseEpayCreateResponse([]byte(`{"code":"1","trade_no":"t1","payurl":"https://a/b"}`))
	if err != nil {
		t.Fatalf("字符串 code 解析失败: %v", err)
	}
	if result.PayURL != "https://a/b" {
		t.Fatalf("payurl = %q", result.PayURL)
	}

	var parsed epayCreateResponse
	if err := json.Unmarshal([]byte(`{"code":1}`), &parsed); err != nil {
		t.Fatalf("数字 code 解析失败: %v", err)
	}
	if string(parsed.Code) != "1" {
		t.Fatalf("code = %q", parsed.Code)
	}
}

func TestReturnURLDerivation(t *testing.T) {
	cases := []struct {
		notify string
		want   string
	}{
		{"https://api.example.com/api/v1/payments/epay/notify", "https://api.example.com/api/v1/payments/epay/return"},
		{"https://api.example.com/custom/hook", "https://api.example.com/api/v1/payments/epay/return"},
		{"https://api.example.com/api/v1/payments/epay/notify?x=1", "https://api.example.com/api/v1/payments/epay/return"},
		{"", ""},
	}
	for _, tc := range cases {
		provider := NewEpay(EpayConfig{NotifyURL: tc.notify}, nil, nil)
		if got := provider.returnURL(); got != tc.want {
			t.Errorf("returnURL(%q) = %q, 期望 %q", tc.notify, got, tc.want)
		}
	}
}

func TestEpayConfigMaskedKey(t *testing.T) {
	cases := []struct{ key, want string }{
		{"1sXR3e8nZZG5", "1s****G5"},
		{"abcd", "****"},
		{"", "****"},
	}
	for _, tc := range cases {
		if got := (EpayConfig{Key: tc.key}).MaskedKey(); got != tc.want {
			t.Errorf("MaskedKey(%q) = %q, 期望 %q", tc.key, got, tc.want)
		}
	}
}

func TestEpayAck(t *testing.T) {
	provider := NewEpay(EpayConfig{ReturnURL: "https://www.example.com/pay/result"}, nil, nil)
	if provider.Ack(true) != AckSuccess || provider.Ack(false) != AckFail {
		t.Fatal("Ack 应答不符合协议")
	}
	if provider.Name() != ProviderEpay {
		t.Fatalf("Name = %q", provider.Name())
	}
}
