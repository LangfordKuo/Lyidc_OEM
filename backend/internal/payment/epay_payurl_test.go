package payment

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// P5 安全整改 finding#07（契约 12.2.1）：渠道返回的 payurl 必须过协议白名单。
//
// 背景：网关被指向攻击者（或网关方 / 链路中间人）时返回 `javascript:…`，浏览器会在**本站源**
// 执行它，直接读到 localStorage 里的会员 JWT 并外带；`data:` / `vbscript:` / `//evil` /
// 相对地址同样有害。拒绝一律沿用 ErrGatewayResponse 语义（上层 50002，**不新增错误码**）。

func TestValidatePayurlMatrix(t *testing.T) {
	allowed := []string{
		"https://gw.example.com/pay/x",
		"http://gw.example.com/pay/x",
		"https://gw.example.com/pay/x?a=1&sign=deadbeef",
		"HTTPS://GW.EXAMPLE.COM/pay/x",
		"http://127.0.0.1:8080/pay/x",
		"https://gw.example.com", // 无路径
	}
	for _, payurl := range allowed {
		if err := validatePayurl(payurl); err != nil {
			t.Errorf("validatePayurl(%q) = %v，期望放行", payurl, err)
		}
	}

	rejected := []struct {
		name   string
		payurl string
	}{
		{"javascript 协议", "javascript:alert(1)"},
		{"javascript 大小写变体", "JavaScript:alert(document.cookie)"},
		{"data 协议", "data:text/html,<script>fetch('//evil')</script>"},
		{"vbscript 协议", "vbscript:msgbox(1)"},
		{"file 协议", "file:///C:/Windows/System32/calc.exe"},
		{"协议相对地址", "//evil.example.com/pay"},
		{"绝对路径", "/pay/relative"},
		{"相对地址", "pay/relative"},
		{"opaque 写法", "http:evil.example.com/pay"},
		{"缺主机名", "https:///pay/x"},
		{"空串", ""},
		{"纯空白", "   "},
		{"无法解析", "https://gw.example.com/\x7f\x00"},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePayurl(tc.payurl)
			if err == nil {
				t.Fatalf("validatePayurl(%q) 放行了非法地址", tc.payurl)
			}
			if !errors.Is(err, ErrGatewayResponse) {
				t.Fatalf("错误 = %v，期望包装 ErrGatewayResponse（沿用既有错误码，不新增）", err)
			}
			// 错误文案会被拼进接口 message（50002），**不得回显 payurl 本身**。
			if tc.payurl != "" && strings.Contains(err.Error(), tc.payurl) {
				t.Fatalf("错误文案回显了 payurl：%q", err.Error())
			}
		})
	}
}

func TestParseEpayCreateResponsePayurlWhitelist(t *testing.T) {
	t.Run("合法 https 放行", func(t *testing.T) {
		result, err := parseEpayCreateResponse([]byte(`{"code":1,"trade_no":"t1","payurl":"https://gw.example.com/pay/x"}`))
		if err != nil {
			t.Fatalf("下单结果解析失败: %v", err)
		}
		if result.PayURL != "https://gw.example.com/pay/x" {
			t.Fatalf("payurl = %q", result.PayURL)
		}
	})

	t.Run("合法 http 放行", func(t *testing.T) {
		if _, err := parseEpayCreateResponse([]byte(`{"code":1,"trade_no":"t1","payurl":"http://gw.example.com/pay/x"}`)); err != nil {
			t.Fatalf("http 绝对地址应放行: %v", err)
		}
	})

	// 渗透场景：`{"code":1,"payurl":"javascript:alert(1)"}`。
	rejected := []string{
		"javascript:alert(1)",
		"data:text/html,<script>1</script>",
		"vbscript:msgbox(1)",
		"//evil.example.com/pay",
		"/relative/pay",
		"",
	}
	for _, payurl := range rejected {
		t.Run(fmt.Sprintf("拒绝 %q", payurl), func(t *testing.T) {
			body := fmt.Sprintf(`{"code":1,"trade_no":"t1","payurl":%q}`, payurl)
			_, err := parseEpayCreateResponse([]byte(body))
			if err == nil {
				t.Fatalf("payurl=%q 应被拒绝", payurl)
			}
			if !errors.Is(err, ErrGatewayResponse) {
				t.Fatalf("错误 = %v，期望 ErrGatewayResponse", err)
			}
		})
	}
}

// TestCreateOrderRejectsIllegalPayurlWithoutLeakingIt 端到端走一遍 CreateOrder：
// 非法 payurl 必须让下单失败，且 **WARN 日志里不含 payurl 原文与查询串**。
func TestCreateOrderRejectsIllegalPayurlWithoutLeakingIt(t *testing.T) {
	cases := []struct {
		name     string
		response string
		leak     string
	}{
		{
			name:     "javascript 协议",
			response: `{"code":1,"trade_no":"t1","payurl":"javascript:alert(document.cookie)"}`,
			leak:     "javascript:alert(document.cookie)",
		},
		{
			name:     "https 但带签名查询串",
			response: `{"code":1,"trade_no":"t1","payurl":"https://gw.example.com/pay/x?sign=SUPERSECRET&pid=1000"}`,
			leak:     "SUPERSECRET",
		},
		{
			name:     "非 JSON 响应里的查询串",
			response: `<html>502 <a href="https://gw.example.com/x?sign=SUPERSECRET">retry</a></html>`,
			leak:     "SUPERSECRET",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 「https 但带签名查询串」这一条是合法地址，用它验证的是**日志脱敏**而非拒绝；
			// 因此只有前两条与非 JSON 那条期望下单失败。
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()

			var logs bytes.Buffer
			provider := NewEpay(EpayConfig{Gateway: server.URL, PID: "1000", Key: "k"},
				server.Client(), slog.New(slog.NewTextHandler(&logs, nil)))

			_, err := provider.CreateOrder(context.Background(), CreateRequest{
				OutTradeNo: "O1", Amount: "1.00", Subject: "测试",
			})

			isLegal := strings.Contains(tc.response, `"payurl":"https://gw.example.com/pay/x?sign=`)
			if isLegal {
				if err != nil {
					t.Fatalf("合法 payurl 不应让下单失败: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("非法响应应让下单失败")
				}
				if !errors.Is(err, ErrGatewayResponse) {
					t.Fatalf("错误 = %v，期望 ErrGatewayResponse", err)
				}
			}

			if strings.Contains(logs.String(), tc.leak) {
				t.Fatalf("日志泄漏了 payurl 内容 %q：%s", tc.leak, logs.String())
			}
		})
	}
}

func TestRedactEpayBody(t *testing.T) {
	t.Run("JSON 响应只留协议与主机", func(t *testing.T) {
		summary := redactEpayBody([]byte(`{"code":1,"msg":"ok","trade_no":"t1","payurl":"javascript:alert(1)","qrcode":"https://gw/x.png"}`))
		if strings.Contains(summary, "alert(1)") || strings.Contains(summary, "qrcode") {
			t.Fatalf("摘要泄漏了 payurl 原文或无关字段：%s", summary)
		}
		if !strings.Contains(summary, `"payurl_target":"javascript:"`) {
			t.Fatalf("摘要应只留协议与主机：%s", summary)
		}
		if !strings.Contains(summary, `"code":"1"`) || !strings.Contains(summary, `"trade_no":"t1"`) {
			t.Fatalf("摘要应保留排查所需的 code / trade_no：%s", summary)
		}
	})

	t.Run("JSON 响应的 https payurl 也只留协议与主机", func(t *testing.T) {
		summary := redactEpayBody([]byte(`{"code":1,"payurl":"https://gw.example.com/pay/x?sign=SUPERSECRET"}`))
		if strings.Contains(summary, "SUPERSECRET") || strings.Contains(summary, "/pay/x") {
			t.Fatalf("摘要泄漏了路径或查询串：%s", summary)
		}
		if !strings.Contains(summary, `"payurl_target":"https://gw.example.com"`) {
			t.Fatalf("摘要应保留协议与主机：%s", summary)
		}
	})

	t.Run("非 JSON 响应抹掉 URL 查询串", func(t *testing.T) {
		summary := redactEpayBody([]byte(`<html><a href="https://gw.example.com/x?sign=SUPERSECRET">retry</a></html>`))
		if strings.Contains(summary, "SUPERSECRET") {
			t.Fatalf("非 JSON 摘要泄漏了查询串：%s", summary)
		}
		if !strings.Contains(summary, "https://gw.example.com/x?") {
			t.Fatalf("非 JSON 摘要应保留可读的地址前缀：%s", summary)
		}
	})
}
