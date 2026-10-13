package router

import (
	"strings"
	"testing"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

// TestHostSyncSignatureGoldenVectors 是签名算法的 golden 向量测试（契约 22.3）：
// **逐字符断言**签名字符串与期望签名——这些值与上游 PHP 侧
// `upper(md5(json_encode(ksort(["id"=>…,"token"=>…,"rand_str"=>…], SORT_STRING))))` 一致，
// 改动算法必然让本用例失败（这就是它的意义）。
func TestHostSyncSignatureGoldenVectors(t *testing.T) {
	cases := []struct {
		name        string
		id          string
		randStr     string
		token       string
		wantPayload string
		wantSign    string
	}{
		{
			name:        "算例一（文档样例）",
			id:          "42",
			randStr:     "aB3xY9",
			token:       "0123456789abcdef0123456789abcdef",
			wantPayload: `{"id":"42","rand_str":"aB3xY9","token":"0123456789abcdef0123456789abcdef"}`,
			wantSign:    "9840FF4B28D19ED233E2228471ABFBB1",
		},
		{
			name:        "算例二（短 id 与全 f token）",
			id:          "7",
			randStr:     "Zx9Q2m",
			token:       "ffffffffffffffffffffffffffffffff",
			wantPayload: `{"id":"7","rand_str":"Zx9Q2m","token":"ffffffffffffffffffffffffffffffff"}`,
			wantSign:    "FE4C0CF8F11C9455B68C4D7503D71034",
		},
		{
			name:        "算例三（四位 id）",
			id:          "1001",
			randStr:     "A1b2C3",
			token:       "5d41402abc4b2a76b9719d911017c592",
			wantPayload: `{"id":"1001","rand_str":"A1b2C3","token":"5d41402abc4b2a76b9719d911017c592"}`,
			wantSign:    "D3D475B73F78EFE8785A783E5539D714",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hostSyncSignPayload(tc.id, tc.randStr, tc.token); got != tc.wantPayload {
				t.Fatalf("签名字符串 = %q，期望 %q", got, tc.wantPayload)
			}
			got := hostSyncSignature(tc.id, tc.randStr, tc.token)
			if got != tc.wantSign {
				t.Fatalf("签名 = %q，期望 %q", got, tc.wantSign)
			}
			if got != strings.ToUpper(got) {
				t.Fatalf("签名必须是大写十六进制：%q", got)
			}
			// 大小写不敏感比对（handler 用 EqualFold）：小写写法必须与期望签名等价。
			if !strings.EqualFold(got, strings.ToLower(tc.wantSign)) {
				t.Fatalf("签名比对应当大小写不敏感：%q / %q", got, tc.wantSign)
			}
		})
	}
}

// TestHostSyncSignPayloadEscaping 验证签名 JSON **不做 HTML 转义**（契约 22.3 第 3 条）：
// Go 默认会把 `<` / `>` / `&` 写成 Unicode 转义形式，而 PHP 的 json_encode 不转义，
// 一旦回退成默认行为，含这些字符的 rand_str 会算出与上游不一致的签名。
func TestHostSyncSignPayloadEscaping(t *testing.T) {
	payload := hostSyncSignPayload("42", "a<b>&c", "0123456789abcdef0123456789abcdef")
	want := `{"id":"42","rand_str":"a<b>&c","token":"0123456789abcdef0123456789abcdef"}`
	if payload != want {
		t.Fatalf("签名字符串 = %q，期望 %q（不得 HTML 转义）", payload, want)
	}
	if strings.Contains(payload, `\\u003c`) || strings.Contains(payload, `\\u0026`) {
		t.Fatalf("签名字符串被 HTML 转义：%q", payload)
	}
}

// TestHostSyncStatusMapping 逐个断言 domainstatus → 本地状态（契约 22.3 映射表）：
// Active/Suspended/Deleted 三个已知值映射；Cancelled/Pending/Fraud 与一切未知值**不改本地状态**。
// `Cancelled` 不映射是 5c 定稿（契约 15.8.1）：`cancelled` 是保留但不再写入的预留枚举，
// 本用例锁死这条口径，防以后有人按上游枚举照抄改回映射。
func TestHostSyncStatusMapping(t *testing.T) {
	cases := []struct {
		domainStatus string
		want         string
		wantOK       bool
	}{
		{domainStatus: "Active", want: model.InstanceStatusActive, wantOK: true},
		{domainStatus: "Suspended", want: model.InstanceStatusSuspended, wantOK: true},
		{domainStatus: "Deleted", want: model.InstanceStatusTerminated, wantOK: true},
		// Cancelled 与 Pending/Fraud 同一条分支：只更新 upstream_status 原文，不改本地状态。
		{domainStatus: "Cancelled", want: "", wantOK: false},
		{domainStatus: "Pending", want: "", wantOK: false},
		{domainStatus: "Fraud", want: "", wantOK: false},
		{domainStatus: "Terminated", want: "", wantOK: false},
		{domainStatus: "unknown-status", want: "", wantOK: false},
		{domainStatus: "", want: "", wantOK: false},
		// 大小写与空白容忍（上游首字母大写、本地枚举小写）。
		{domainStatus: "active", want: model.InstanceStatusActive, wantOK: true},
		{domainStatus: "  Suspended ", want: model.InstanceStatusSuspended, wantOK: true},
		{domainStatus: "cancelled", want: "", wantOK: false},
		{domainStatus: "  Cancelled ", want: "", wantOK: false},
	}

	for _, tc := range cases {
		got, ok := hostSyncStatus(tc.domainStatus)
		if ok != tc.wantOK || got != tc.want {
			t.Fatalf("domainstatus=%q → (%q, %v)，期望 (%q, %v)", tc.domainStatus, got, ok, tc.want, tc.wantOK)
		}
	}
}

// TestParseHostSyncInput 验证 form → 落库输入的字段口径（契约 22.3）：
// 出现的字段才写、nextduedate 额外要求非空、port 非法值忽略、password 原样保留。
func TestParseHostSyncInput(t *testing.T) {
	form := map[string][]string{
		"id":           {"42"},
		"rand_str":     {"aB3xY9"},
		"signature":    {"9840FF4B28D19ED233E2228471ABFBB1"},
		"domainstatus": {"Active"},
		"nextduedate":  {"1796718606"},
		"dedicatedip":  {"203.0.113.10"},
		"assignedips":  {"203.0.113.11,203.0.113.12"},
		"port":         {"22022"},
		"username":     {"root"},
		"password":     {"UpstreamPass123"},
	}

	in, status := parseHostSyncInput(form)
	if status != model.InstanceStatusActive {
		t.Fatalf("目标状态 = %q，期望 active", status)
	}
	if in.UpstreamStatus == nil || *in.UpstreamStatus != "Active" {
		t.Fatalf("upstream_status = %v，期望 Active", in.UpstreamStatus)
	}
	if in.NextDueDate == nil || in.NextDueDate.Unix() != 1796718606 {
		t.Fatalf("next_due_date = %v，期望 unix 1796718606", in.NextDueDate)
	}
	if in.DedicatedIP == nil || *in.DedicatedIP != "203.0.113.10" ||
		in.AssignedIPs == nil || *in.AssignedIPs != "203.0.113.11,203.0.113.12" {
		t.Fatalf("IP 字段解析错误: %+v", in)
	}
	if in.Port == nil || *in.Port != 22022 {
		t.Fatalf("port = %v，期望 22022", in.Port)
	}
	if in.Username == nil || *in.Username != "root" ||
		in.Password == nil || *in.Password != "UpstreamPass123" {
		t.Fatalf("账号字段解析错误: %+v", in)
	}

	// 只出现 id 的场景：除 id 外一律「不改动」（含 nextduedate 空值与非法 port）。
	sparse := map[string][]string{"id": {"42"}, "nextduedate": {""}, "port": {"abc"}, "password": {""}}
	in, status = parseHostSyncInput(sparse)
	if status != "" {
		t.Fatalf("没有 domainstatus 时目标状态 = %q，期望空串（不改状态）", status)
	}
	if in.UpstreamStatus != nil || in.NextDueDate != nil || in.Port != nil || in.Username != nil {
		t.Fatalf("未出现的字段应保持 nil: %+v", in)
	}
	if in.Password == nil || *in.Password != "" {
		t.Fatalf("password 出现在表单里（即使为空）应按原文写入，得到 %v", in.Password)
	}

	// Pending / Fraud：不改本地状态，但 upstream_status 原文照存。
	pending := map[string][]string{"domainstatus": {"Pending"}}
	in, status = parseHostSyncInput(pending)
	if status != "" || in.UpstreamStatus == nil || *in.UpstreamStatus != "Pending" {
		t.Fatalf("Pending 应当只存原文不改状态：status=%q in=%+v", status, in)
	}
}
