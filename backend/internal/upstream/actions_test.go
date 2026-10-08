package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"testing"
)

// jsonNumber 构造测试用的 json.Number（上游账单 ID 有时以字符串返回）。
func jsonNumber(value string) json.Number { return json.Number(value) }

func TestProvisionOperationsSendExpectedParams(t *testing.T) {
	cases := []struct {
		name     string
		call     func(context.Context, *Client) (*ProvisionResult, error)
		wantFunc string
		wantForm map[string]string
	}{
		{"开机", func(ctx context.Context, c *Client) (*ProvisionResult, error) { return c.On(ctx, 88) }, "on", nil},
		{"关机", func(ctx context.Context, c *Client) (*ProvisionResult, error) { return c.Off(ctx, 88) }, "off", nil},
		{"重启", func(ctx context.Context, c *Client) (*ProvisionResult, error) { return c.Reboot(ctx, 88) }, "reboot", nil},
		{"强制关机", func(ctx context.Context, c *Client) (*ProvisionResult, error) { return c.HardOff(ctx, 88) }, "hard_off", nil},
		{"强制重启", func(ctx context.Context, c *Client) (*ProvisionResult, error) { return c.HardReboot(ctx, 88) }, "hard_reboot", nil},
		{"状态", func(ctx context.Context, c *Client) (*ProvisionResult, error) { return c.Status(ctx, 88) }, "status", nil},
		{"VNC", func(ctx context.Context, c *Client) (*ProvisionResult, error) { return c.VNC(ctx, 88) }, "vnc", nil},
		{"暂停", func(ctx context.Context, c *Client) (*ProvisionResult, error) { return c.Suspend(ctx, 88, "欠费") }, "suspend", map[string]string{"reason": "欠费"}},
		{"恢复", func(ctx context.Context, c *Client) (*ProvisionResult, error) { return c.Unsuspend(ctx, 88) }, "unsuspend", nil},
		{"重装", func(ctx context.Context, c *Client) (*ProvisionResult, error) { return c.Reinstall(ctx, 88, 301, 22) }, "reinstall", map[string]string{"os": "301", "port": "22"}},
		{"重置密码", func(ctx context.Context, c *Client) (*ProvisionResult, error) {
			return c.ResetPassword(ctx, 88, "P@ssw0rd!")
		}, "crack_pass", map[string]string{"password": "P@ssw0rd!"}},
		{"救援系统", func(ctx context.Context, c *Client) (*ProvisionResult, error) { return c.RescueSystem(ctx, 88, 2) }, "rescue_system", map[string]string{"system": "2"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeUpstream(t, map[string]handlerFunc{
				pathProvisionDef: func(_ int, _ *http.Request, _ url.Values) (int, string) {
					return http.StatusOK, okBody(`{"status":"on"}`)
				},
			})
			client := fake.client(t, nil)

			result, err := tc.call(context.Background(), client)
			if err != nil {
				t.Fatalf("%s 失败: %v", tc.name, err)
			}
			if result.Status != statusOK {
				t.Errorf("返回状态 = %d, 期望 200", result.Status)
			}
			// 内层 data.status 是电源状态文案（"on"），与外层业务状态码互不干扰。
			if got := result.DataField("status"); got != "on" {
				t.Errorf("DataField(status) = %q, 期望 on", got)
			}

			calls := fake.requests(pathProvisionDef)
			if len(calls) != 1 {
				t.Fatalf("调用次数 = %d, 期望 1", len(calls))
			}
			if got := calls[0].Form.Get("func"); got != tc.wantFunc {
				t.Errorf("func = %q, 期望 %q", got, tc.wantFunc)
			}
			if got := calls[0].Form.Get("id"); got != "88" {
				t.Errorf("id = %q, 期望 88", got)
			}
			for key, want := range tc.wantForm {
				if got := calls[0].Form.Get(key); got != want {
					t.Errorf("%s = %q, 期望 %q", key, got, want)
				}
			}
		})
	}
}

func TestProvisionRejectsInvalidArguments(t *testing.T) {
	fake := newFakeUpstream(t, nil)
	client := fake.client(t, nil)
	ctx := context.Background()

	if _, err := client.On(ctx, 0); !errors.Is(err, ErrBusiness) {
		t.Errorf("On(0) 错误 = %v, 期望 ErrBusiness", err)
	}
	if _, err := client.Reinstall(ctx, 88, 0, 0); !errors.Is(err, ErrBusiness) {
		t.Errorf("Reinstall(os=0) 错误 = %v, 期望 ErrBusiness", err)
	}
	if _, err := client.ResetPassword(ctx, 88, ""); !errors.Is(err, ErrBusiness) {
		t.Errorf("ResetPassword(空密码) 错误 = %v, 期望 ErrBusiness", err)
	}
	if _, err := client.CustomButton(ctx, 88, ""); !errors.Is(err, ErrBusiness) {
		t.Errorf("CustomButton(空 func) 错误 = %v, 期望 ErrBusiness", err)
	}
	if got := fake.count(pathProvisionDef); got != 0 {
		t.Errorf("非法参数不应发起请求，实际 %d 次", got)
	}
}

func TestCustomButtonSendsIDAndFunc(t *testing.T) {
	fake := newFakeUpstream(t, map[string]handlerFunc{
		pathProvisionBtn: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, okBody(`{}`)
		},
	})
	client := fake.client(t, nil)

	if _, err := client.CustomButton(context.Background(), 88, "hard_reboot"); err != nil {
		t.Fatalf("CustomButton() 失败: %v", err)
	}

	calls := fake.requests(pathProvisionBtn)
	if len(calls) != 1 {
		t.Fatalf("调用次数 = %d, 期望 1", len(calls))
	}
	if calls[0].Method != http.MethodPost {
		t.Errorf("方法 = %s, 期望 POST", calls[0].Method)
	}
	if got := calls[0].Form.Get("func"); got != "hard_reboot" {
		t.Errorf("func = %q, 期望 hard_reboot", got)
	}
}

func TestCreateHostRunsOrderingPipelineInOrder(t *testing.T) {
	fake := newFakeUpstream(t, map[string]handlerFunc{
		pathCartClear: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, `{"status":200,"msg":"请求成功","user":{"id":9,"currency":1}}`
		},
		pathCartAddShop: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, okBody(`{}`)
		},
		pathCartSettle: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, okBody(`{"invoiceid":5678,"hostid":[0]}`)
		},
		pathApplyCredit: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, `{"status":1001,"msg":"支付成功","data":{"hostid":[88]},"hostid":[88]}`
		},
	})
	client := fake.client(t, nil)

	result, err := client.CreateHost(context.Background(), CreateHostRequest{
		ProductID:     1,
		BillingCycle:  "monthly",
		Hostname:      "hk-a.example.com",
		Password:      "P@ssw0rd!",
		ConfigOptions: map[int]string{21: "301", 22: "2"},
		CustomFields:  map[int]string{3: "备注"},
		DownstreamURL: "https://lyidc.example.com",
		DownstreamID:  66,
	})
	if err != nil {
		t.Fatalf("CreateHost() 失败: %v", err)
	}
	if !result.Paid || result.HostID != 88 || result.InvoiceID != 5678 {
		t.Errorf("结果 = %+v, 期望 paid=true hostID=88 invoiceID=5678", result)
	}

	// 各步骤都应被调用一次。
	for _, path := range []string{pathCartClear, pathCartAddShop, pathCartSettle, pathApplyCredit} {
		if got := fake.count(path); got != 1 {
			t.Errorf("%s 调用次数 = %d, 期望 1", path, got)
		}
	}

	// 下单参数：商品、周期、主机名、密码、货币、数量与可配置项。
	order := fake.requests(pathCartAddShop)[0].Form
	expects := map[string]string{
		"pid": "1", "billingcycle": "monthly", "host": "hk-a.example.com",
		"password": "P@ssw0rd!", "currencyid": "1", "qty": "1",
		"configoption[21]": "301", "configoption[22]": "2", "customfield[3]": "备注",
		"downstream_url": "https://lyidc.example.com", "downstream_id": "66",
	}
	for key, want := range expects {
		if got := order.Get(key); got != want {
			t.Errorf("add_to_shop %s = %q, 期望 %q", key, got, want)
		}
	}

	// 结算参数：上游 /cart/settle 读的是 cart_data 数组（配置项键名 configoptions）。
	settle := fake.requests(pathCartSettle)[0].Form
	settleExpects := map[string]string{
		"cart_data[pid]": "1", "cart_data[billingcycle]": "monthly",
		"cart_data[host]": "hk-a.example.com", "cart_data[currencyid]": "1",
		"cart_data[qty]": "1", "cart_data[configoptions][21]": "301",
	}
	for key, want := range settleExpects {
		if got := settle.Get(key); got != want {
			t.Errorf("settle %s = %q, 期望 %q", key, got, want)
		}
	}

	// 支付参数：账单 ID + 余额支付。
	pay := fake.requests(pathApplyCredit)[0].Form
	if got := pay.Get("invoiceid"); got != "5678" {
		t.Errorf("apply_credit invoiceid = %q, 期望 5678", got)
	}
	if got := pay.Get("use_credit"); got != "1" {
		t.Errorf("apply_credit use_credit = %q, 期望 1", got)
	}
	// 本次下单生成的账单：enough=0（与上游官方下游实现一致）。
	if got := pay.Get("enough"); got != "0" {
		t.Errorf("apply_credit enough = %q, 期望 0", got)
	}
}

func TestCreateHostPaysExistingInvoiceWithoutReordering(t *testing.T) {
	fake := newFakeUpstream(t, map[string]handlerFunc{
		pathCartClear: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, `{"status":200,"msg":"请求成功","invoiceid":4321,
				"user":{"id":9,"currency":2}}`
		},
		pathApplyCredit: func(_ int, _ *http.Request, form url.Values) (int, string) {
			if got := form.Get("enough"); got != "1" {
				t.Errorf("已有账单时应带 enough=1，实际 %q", got)
			}
			return http.StatusOK, `{"status":1001,"msg":"支付成功","data":{"hostid":[77]}}`
		},
	})
	client := fake.client(t, nil)

	result, err := client.CreateHost(context.Background(), CreateHostRequest{
		ProductID: 1, BillingCycle: "monthly",
	})
	if err != nil {
		t.Fatalf("CreateHost() 失败: %v", err)
	}
	if !result.Paid || result.HostID != 77 || result.InvoiceID != 4321 {
		t.Errorf("结果 = %+v, 期望 paid=true hostID=77 invoiceID=4321", result)
	}
	if got := fake.count(pathCartAddShop); got != 0 {
		t.Errorf("已有待支付账单时不应重新下单，add_to_shop 调用 %d 次", got)
	}
	if got := fake.count(pathCartSettle); got != 0 {
		t.Errorf("已有待支付账单时不应重新结算，settle 调用 %d 次", got)
	}
}

func TestCreateHostFailsWhenUpstreamBalanceNotPaid(t *testing.T) {
	fake := newFakeUpstream(t, map[string]handlerFunc{
		pathCartClear: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, `{"status":200,"msg":"请求成功","user":{"id":9,"currency":1}}`
		},
		pathCartAddShop: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, okBody(`{}`)
		},
		pathCartSettle: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, okBody(`{"invoiceid":5678}`)
		},
		// 上游余额不足时 /apply_credit 返回 status=200（不是 1001）。
		pathApplyCredit: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, okBody(`{"msg":"余额不足"}`)
		},
	})
	client := fake.client(t, nil)

	result, err := client.CreateHost(context.Background(), CreateHostRequest{
		ProductID: 1, BillingCycle: "monthly",
	})
	if err == nil {
		t.Fatal("上游未支付成功时应返回错误")
	}
	if !errors.Is(err, ErrBusiness) {
		t.Errorf("错误 = %v, 期望 ErrBusiness", err)
	}
	if result.Paid {
		t.Errorf("Paid = true, 期望 false")
	}
}

func TestCreateHostValidatesRequest(t *testing.T) {
	fake := newFakeUpstream(t, nil)
	client := fake.client(t, nil)

	if _, err := client.CreateHost(context.Background(), CreateHostRequest{BillingCycle: "monthly"}); !errors.Is(err, ErrBusiness) {
		t.Errorf("缺 ProductID 错误 = %v, 期望 ErrBusiness", err)
	}
	if _, err := client.CreateHost(context.Background(), CreateHostRequest{ProductID: 1}); !errors.Is(err, ErrBusiness) {
		t.Errorf("缺 BillingCycle 错误 = %v, 期望 ErrBusiness", err)
	}
	if got := fake.count(pathCartClear); got != 0 {
		t.Errorf("参数非法不应发起请求，实际 %d 次", got)
	}
}

func TestRenewHostOrdersInvoiceThenPays(t *testing.T) {
	fake := newFakeUpstream(t, map[string]handlerFunc{
		pathHostRenew: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, okBody(`{"invoiceid":9012}`)
		},
		pathApplyCredit: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, `{"status":1001,"msg":"支付成功"}`
		},
	})
	client := fake.client(t, nil)

	result, err := client.RenewHost(context.Background(), 88, "monthly")
	if err != nil {
		t.Fatalf("RenewHost() 失败: %v", err)
	}
	if result.InvoiceID != 9012 || !result.Paid {
		t.Errorf("结果 = %+v, 期望 invoiceID=9012 paid=true", result)
	}

	renew := fake.requests(pathHostRenew)[0].Form
	if renew.Get("hostid") != "88" || renew.Get("billingcycles") != "monthly" {
		t.Errorf("续费参数 = %v, 期望 hostid=88 billingcycles=monthly", renew)
	}
	pay := fake.requests(pathApplyCredit)[0].Form
	if pay.Get("invoiceid") != "9012" || pay.Get("use_credit") != "1" {
		t.Errorf("支付参数 = %v, 期望 invoiceid=9012 use_credit=1", pay)
	}
}

func TestRenewHostFailsWhenNotPaid(t *testing.T) {
	fake := newFakeUpstream(t, map[string]handlerFunc{
		pathHostRenew: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, okBody(`{"invoiceid":9012}`)
		},
		pathApplyCredit: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, okBody(`{"msg":"余额不足"}`)
		},
	})
	client := fake.client(t, nil)

	result, err := client.RenewHost(context.Background(), 88, "")
	if err == nil || !errors.Is(err, ErrBusiness) {
		t.Fatalf("续费未支付错误 = %v, 期望 ErrBusiness", err)
	}
	if result.InvoiceID != 9012 || result.Paid {
		t.Errorf("结果 = %+v, 期望 保留账单 ID 且 Paid=false", result)
	}

	// billingCycle 为空时不发送 billingcycles，由上游沿用当前周期。
	renew := fake.requests(pathHostRenew)[0].Form
	if _, ok := renew["billingcycles"]; ok {
		t.Errorf("billingcycles 不应出现，实际 %v", renew)
	}
}

func TestInvoiceIDFromTopLevelIsAccepted(t *testing.T) {
	resp := &Response{InvoiceID: jsonNumber("321")}
	if got := invoiceIDOf(resp); got != 321 {
		t.Errorf("invoiceIDOf(顶层 invoiceid) = %d, 期望 321", got)
	}

	resp = &Response{Data: []byte(`{"invoiceid":"654"}`)}
	if got := invoiceIDOf(resp); got != 654 {
		t.Errorf("invoiceIDOf(data.invoiceid) = %d, 期望 654", got)
	}

	if got := invoiceIDOf(&Response{Data: []byte(`{}`)}); got != 0 {
		t.Errorf("invoiceIDOf(无账单) = %d, 期望 0", got)
	}
}

func TestRequestCancelSendsTypeAndReason(t *testing.T) {
	fake := newFakeUpstream(t, map[string]handlerFunc{
		pathHostCancel: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, okBody(`{"cancel_id":3}`)
		},
	})
	client := fake.client(t, nil)

	result, err := client.RequestCancel(context.Background(), 88, CancelImmediate, "阶段 2 联调结束")
	if err != nil {
		t.Fatalf("RequestCancel() 失败: %v", err)
	}
	if result.Status != statusOK {
		t.Errorf("status = %d, 期望 200", result.Status)
	}

	calls := fake.requests(pathHostCancel)
	if len(calls) != 1 || calls[0].Method != http.MethodPost {
		t.Fatalf("调用 = %+v, 期望 1 次 POST", calls)
	}
	form := calls[0].Form
	if form.Get("id") != "88" || form.Get("type") != CancelImmediate {
		t.Errorf("表单 = %v, 期望 id=88 type=Immediate", form)
	}
	if form.Get("reason") != "阶段 2 联调结束" {
		t.Errorf("reason = %q, 期望原样带上中文原因", form.Get("reason"))
	}
}

func TestRequestCancelValidatesArguments(t *testing.T) {
	fake := newFakeUpstream(t, nil)
	client := fake.client(t, nil)
	ctx := context.Background()

	if _, err := client.RequestCancel(ctx, 0, CancelImmediate, "r"); !errors.Is(err, ErrBusiness) {
		t.Errorf("hostID=0 错误 = %v, 期望 ErrBusiness", err)
	}
	if _, err := client.RequestCancel(ctx, 88, "now", "r"); !errors.Is(err, ErrBusiness) {
		t.Errorf("非法 type 错误 = %v, 期望 ErrBusiness", err)
	}
	if _, err := client.RequestCancel(ctx, 88, CancelImmediate, ""); !errors.Is(err, ErrBusiness) {
		t.Errorf("空 reason 错误 = %v, 期望 ErrBusiness", err)
	}
	if got := fake.count(pathHostCancel); got != 0 {
		t.Errorf("非法参数不应发起请求，实际 %d 次", got)
	}
}

func TestRequestCancelAcceptsUpstreamStatus202(t *testing.T) {
	fake := newFakeUpstream(t, map[string]handlerFunc{
		pathHostCancel: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			// 实测：上游以 202 + 该 msg 表示「终止申请待处理」。
			return http.StatusOK, `{"status":202,"msg":"mf_cloud_finance_termination_pending","is_aff":"1"}`
		},
	})
	client := fake.client(t, nil)

	result, err := client.RequestCancel(context.Background(), 10919, CancelImmediate, "清理测试资源")
	if err != nil {
		t.Fatalf("202 应视为已受理，实际错误: %v", err)
	}
	if result.Status != 202 || result.Msg != "mf_cloud_finance_termination_pending" {
		t.Errorf("结果 = %+v, 期望 status=202 且保留上游 msg 供调用方判断", result)
	}
}

// TestRequestCancelParsesRequestID 验证取消申请 ID 的四种回带写法（阶段 5c）：
// data/顶层 × cancel_request_id/cancel_id（实测与文档字段名不一致，解析需全部兼容）。
func TestRequestCancelParsesRequestID(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"data.cancel_request_id", okBody(`{"pending":true,"cancel_request_id":433}`), 433},
		{"顶层 cancel_request_id", `{"status":202,"msg":"pending","cancel_request_id":"434"}`, 434},
		{"data.cancel_id（文档口径）", okBody(`{"cancel_id":435}`), 435},
		{"顶层 cancel_id", `{"status":202,"msg":"pending","cancel_id":436}`, 436},
		{"无申请 ID", okBody(`{"pending":true}`), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.body
			fake := newFakeUpstream(t, map[string]handlerFunc{
				pathHostCancel: func(_ int, _ *http.Request, _ url.Values) (int, string) {
					return http.StatusOK, body
				},
			})
			client := fake.client(t, nil)

			result, err := client.RequestCancel(context.Background(), 10919, CancelEndOfBilling, "到期取消")
			if err != nil {
				t.Fatalf("RequestCancel() 失败: %v", err)
			}
			if result.CancelRequestID != tc.want {
				t.Errorf("CancelRequestID = %d, 期望 %d", result.CancelRequestID, tc.want)
			}
		})
	}
}
