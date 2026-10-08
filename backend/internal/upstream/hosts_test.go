package upstream

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
)

// realHostInfoBody 是 GET /cart/hostinfo 的结构样例（字段取自上游 hostInfo 返回定义）。
const realHostInfoBody = `{
  "hosts": [
    {"id": 88, "productid": 1, "domain": "hk-a.example.com", "dedicatedip": "203.0.113.10",
     "assignedips": ["203.0.113.11"], "create_time": 1791448202, "nextduedate": 1796718606,
     "billingcycle": "monthly", "billingcycle_zh": "月付",
     "firstpaymentamount": "20.00", "amount": "20.00", "port": 22,
     "username": "root", "password": "secret", "initiative_renew": 1,
     "domainstatus": "Active", "domainstatus_zh": {"name": "已激活", "color": "#3fbf70"}}
  ],
  "currency": "¥"
}`

func TestHostsSendsHostIDArrayAndParsesRealPayload(t *testing.T) {
	fake := newFakeUpstream(t, map[string]handlerFunc{
		pathCartHostInfo: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, okBody(realHostInfoBody)
		},
	})
	client := fake.client(t, nil)

	list, err := client.Hosts(context.Background(), []int{88, 99}, true)
	if err != nil {
		t.Fatalf("Hosts() 失败: %v", err)
	}
	if len(list.Hosts) != 1 {
		t.Fatalf("主机数量 = %d, 期望 1", len(list.Hosts))
	}

	host := list.Hosts[0]
	if host.ID != 88 || host.ProductID != 1 || host.DomainStatus != "Active" {
		t.Errorf("主机 = %+v, 期望 id=88 productid=1 domainstatus=Active", host)
	}
	if host.DedicatedIP != "203.0.113.10" || len(host.AssignedIPs) != 1 {
		t.Errorf("IP = (%s, %v), 期望 dedicatedip=203.0.113.10 且 assignedips 展开为 1 项",
			host.DedicatedIP, host.AssignedIPs)
	}
	// 实测：create_time / nextduedate 都是 unix 秒（不是日期字符串）。
	if host.CreateTime != 1791448202 || host.NextDueDate != 1796718606 {
		t.Errorf("时间字段 = (%d, %d), 期望 (1791448202, 1796718606)", host.CreateTime, host.NextDueDate)
	}
	if list.Currency != "¥" {
		t.Errorf("货币符号 = %q, 期望 ¥", list.Currency)
	}

	calls := fake.requests(pathCartHostInfo)
	if len(calls) != 1 {
		t.Fatalf("调用次数 = %d, 期望 1", len(calls))
	}
	if got := calls[0].Query["hostid[]"]; len(got) != 2 || got[0] != "88" || got[1] != "99" {
		t.Errorf("hostid[] = %v, 期望 [88 99]", got)
	}
	if got := calls[0].Query.Get("all"); got != "1" {
		t.Errorf("all = %q, 期望 1", got)
	}
}

func TestHostReturnsMatchingHostOrBusinessError(t *testing.T) {
	fake := newFakeUpstream(t, map[string]handlerFunc{
		pathCartHostInfo: func(call int, _ *http.Request, _ url.Values) (int, string) {
			if call == 1 {
				return http.StatusOK, okBody(realHostInfoBody)
			}
			return http.StatusOK, okBody(`{"hosts":[],"currency":"¥"}`)
		},
	})
	client := fake.client(t, nil)

	host, err := client.Host(context.Background(), 88)
	if err != nil {
		t.Fatalf("Host(88) 失败: %v", err)
	}
	if host.ID != 88 {
		t.Errorf("Host(88).ID = %d, 期望 88", host.ID)
	}

	if _, err := client.Host(context.Background(), 77); !errors.Is(err, ErrHostNotFound) {
		t.Fatalf("主机不存在时错误 = %v, 期望 ErrHostNotFound", err)
	}
	// 兼容口径：主机不存在同时命中历史哨兵 ErrBusiness（既有调用方与日志文案不变）。
	if _, err := client.Host(context.Background(), 77); !errors.Is(err, ErrBusiness) {
		t.Fatalf("主机不存在时错误 = %v, 期望同时命中 ErrBusiness", err)
	}
	if _, err := client.Host(context.Background(), 0); !errors.Is(err, ErrBusiness) {
		t.Fatalf("hostID=0 时错误 = %v, 期望 ErrBusiness", err)
	}
}

func TestCreditParsesNullAndAmount(t *testing.T) {
	fake := newFakeUpstream(t, map[string]handlerFunc{
		pathCartCredit: func(call int, _ *http.Request, _ url.Values) (int, string) {
			if call == 1 {
				// 未登录时上游返回 credit=null（实测行为）。
				return http.StatusOK, okBody(`{"credit":null,"currency":{"id":1,"code":"CNY","prefix":"¥","suffix":"元"}}`)
			}
			return http.StatusOK, okBody(`{"credit":"100.00","currency":{"id":1,"code":"CNY","prefix":"¥","suffix":"元"}}`)
		},
	})
	client := fake.client(t, nil)

	credit, err := client.Credit(context.Background())
	if err != nil {
		t.Fatalf("Credit() 失败: %v", err)
	}
	if credit.Credit != "" {
		t.Errorf("credit=null 应解析为空串，实际 %q", credit.Credit)
	}
	if credit.Currency.Code != "CNY" || credit.Currency.Prefix != "¥" {
		t.Errorf("货币 = %+v, 期望 code=CNY prefix=¥", credit.Currency)
	}

	credit, err = client.Credit(context.Background())
	if err != nil {
		t.Fatalf("第二次 Credit() 失败: %v", err)
	}
	if credit.Credit != "100.00" {
		t.Errorf("credit = %q, 期望 100.00（金额保留原始字符串精度）", credit.Credit)
	}
}

func TestSummaryBusinessErrorWhenAPIClosed(t *testing.T) {
	fake := newFakeUpstream(t, map[string]handlerFunc{
		pathCartSummary: func(call int, _ *http.Request, _ url.Values) (int, string) {
			if call == 1 {
				// 上游未开启资源 API / 账号未开通 API 时的真实返回。
				return http.StatusOK, failBody(400, "暂未开通API功能")
			}
			return http.StatusOK, okBody(`{"api_password":"MaskedKey1234","api_open":1,"agent_count":2,
				"host_count":3,"active_count":2,"api_count":10,"ratio":"50.00%","up":1}`)
		},
	})
	client := fake.client(t, nil)

	if _, err := client.Summary(context.Background()); !errors.Is(err, ErrBusiness) {
		t.Fatalf("API 未开通时错误 = %v, 期望 ErrBusiness", err)
	}

	summary, err := client.Summary(context.Background())
	if err != nil {
		t.Fatalf("Summary() 失败: %v", err)
	}
	if summary.APIOpen != 1 || summary.AgentCount != 2 || summary.APICount != 10 {
		t.Errorf("概览 = %+v, 期望 api_open=1 agent_count=2 api_count=10", summary)
	}
}

func TestCloudOSParsesRealPayloadAndSendsParams(t *testing.T) {
	// 实测响应（2026-10-08，已裁剪）：只有 cloud_os，group 是分组名字符串。
	fake := newFakeUpstream(t, map[string]handlerFunc{
		pathHostCloudOS: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, okBody(`{"cloud_os":[
				{"id":3,"name":"Debian-10.3.3-x64","group":"Debian"},
				{"id":9,"name":"CentOS-7.9.2111-x64","group":"CentOS"}],
				"cloud_os_group":[{"id":"Debian","name":"Debian"},{"id":"CentOS","name":"CentOS"}]}`)
		},
	})
	client := fake.client(t, nil)

	list, err := client.CloudOS(context.Background(), 1, 2)
	if err != nil {
		t.Fatalf("CloudOS() 失败: %v", err)
	}
	if len(list.OS) != 2 || list.OS[1].ID != 9 {
		t.Fatalf("系统列表 = %+v, 期望 2 项且第二项 id=9", list.OS)
	}
	if list.OS[1].Group != "CentOS" {
		t.Errorf("group = %q, 期望字符串分组名 CentOS", list.OS[1].Group)
	}
	if len(list.OSGroup) != 2 || list.OSGroup[1].ID != "CentOS" {
		t.Errorf("分组 = %+v, 期望 2 组且 id 为字符串 CentOS", list.OSGroup)
	}

	calls := fake.requests(pathHostCloudOS)
	if len(calls) != 1 {
		t.Fatalf("调用次数 = %d, 期望 1", len(calls))
	}
	if got := calls[0].Query.Get("productid"); got != "1" {
		t.Errorf("productid = %q, 期望 1", got)
	}
	if got := calls[0].Query.Get("os_config_option_id"); got != "2" {
		t.Errorf("os_config_option_id = %q, 期望 2", got)
	}

	if _, err := client.CloudOS(context.Background(), 0, 2); !errors.Is(err, ErrBusiness) {
		t.Errorf("productid=0 错误 = %v, 期望 ErrBusiness", err)
	}
}
