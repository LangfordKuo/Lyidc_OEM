package settings

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// fakeSource 是 Source 的内存实现（未收录的键返回 store.ErrNotFound）。
type fakeSource struct {
	values map[string]string
	err    error
}

func (s fakeSource) Setting(_ context.Context, key string) (*model.Setting, error) {
	if s.err != nil {
		return nil, s.err
	}
	value, ok := s.values[key]
	if !ok {
		return nil, store.ErrNotFound
	}
	updatedBy := uint64(7)
	updatedAt := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	return &model.Setting{Key: key, Value: value, UpdatedBy: &updatedBy, UpdatedAt: updatedAt}, nil
}

func strptr(value string) *string { return &value }
func boolptr(value bool) *bool    { return &value }
func intptr(value int) *int       { return &value }

// TestEpayKeyThreeState 验证 key 的三态语义：省略=保持、新值=替换、空串=清空。
func TestEpayKeyThreeState(t *testing.T) {
	base := Epay{Enabled: true, Gateway: "https://pay.example.com", PID: "1001",
		Key: "secret-key-0001", NotifyURL: "https://oem.example.com/api/v1/payments/epay/notify"}

	// 省略（nil）：保持不变。
	kept, err := base.Apply(EpayUpdate{Enabled: boolptr(true)})
	if err != nil {
		t.Fatalf("Apply 省略 key 失败: %v", err)
	}
	if kept.Key != base.Key {
		t.Fatalf("省略 key 后 = %q，期望保持 %q", kept.Key, base.Key)
	}

	// 提供新值：替换。
	replaced, err := base.Apply(EpayUpdate{Key: strptr("brand-new-key")})
	if err != nil {
		t.Fatalf("Apply 替换 key 失败: %v", err)
	}
	if replaced.Key != "brand-new-key" {
		t.Fatalf("替换后 key = %q", replaced.Key)
	}

	// 空串：清空（清空后 enabled=true 就不完整了，故同时停用）。
	cleared, err := base.Apply(EpayUpdate{Key: strptr(""), Enabled: boolptr(false)})
	if err != nil {
		t.Fatalf("Apply 清空 key 失败: %v", err)
	}
	if cleared.Key != "" {
		t.Fatalf("清空后 key = %q，期望空串", cleared.Key)
	}

	// 空串 + 仍启用：拒绝（40002 口径）。
	if _, err := base.Apply(EpayUpdate{Key: strptr("")}); !errors.Is(err, ErrRule) {
		t.Fatalf("清空 key 且仍启用的 err = %v，期望 ErrRule", err)
	}
}

// TestEpayEnabledRequiresFields 验证 enabled=true 时缺字段拒绝，且 message 列出缺失项。
func TestEpayEnabledRequiresFields(t *testing.T) {
	empty := Epay{}
	if empty.Usable() {
		t.Fatal("未配置的渠道不应可用")
	}

	_, err := empty.Apply(EpayUpdate{Enabled: boolptr(true)})
	if !errors.Is(err, ErrRule) {
		t.Fatalf("err = %v，期望 ErrRule", err)
	}
	for _, field := range []string{"gateway", "pid", "key", "notify_url"} {
		if !strings.Contains(err.Error(), field) {
			t.Fatalf("message 未列出缺失项 %s：%v", field, err)
		}
	}

	// 停用状态下允许只填一部分。
	partial, err := Epay{Gateway: "https://pay.example.com"}.Apply(EpayUpdate{})
	if err != nil {
		t.Fatalf("停用状态下应允许不完整配置: %v", err)
	}
	if partial.Missing() == nil || len(partial.Missing()) == 0 {
		t.Fatal("Missing() 应列出未填字段")
	}

	// 补齐后启用成功。
	full, err := empty.Apply(EpayUpdate{
		Enabled:   boolptr(true),
		Gateway:   strptr("https://pay.example.com"),
		PID:       strptr("1001"),
		Key:       strptr("secret-key-0001"),
		NotifyURL: strptr("https://oem.example.com/api/v1/payments/epay/notify"),
	})
	if err != nil {
		t.Fatalf("完整配置启用失败: %v", err)
	}
	if !full.Usable() {
		t.Fatal("完整配置应可用")
	}
}

// TestEpayURLValidation 验证 URL 字段格式（非 http/https 或缺少主机名 → ErrFormat）。
func TestEpayURLValidation(t *testing.T) {
	cases := []struct {
		name     string
		value    Epay
		wantRule bool
	}{
		{"合法 http", Epay{Gateway: "http://pay.example.com"}, false},
		{"合法 https", Epay{Gateway: "https://pay.example.com"}, false},
		{"缺 scheme", Epay{Gateway: "pay.example.com"}, true},
		{"ftp 协议", Epay{Gateway: "ftp://pay.example.com"}, true},
		{"缺主机名", Epay{NotifyURL: "https:///notify"}, true},
		{"空串合法（未配置）", Epay{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.value.Validate()
			if tc.wantRule {
				if !errors.Is(err, ErrFormat) {
					t.Fatalf("err = %v，期望 ErrFormat", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("不该报错: %v", err)
			}
		})
	}
}

// TestUpstreamApplyAndValidation 验证上游设置：api_key 三态、超时范围、缺省超时。
func TestUpstreamApplyAndValidation(t *testing.T) {
	base := Upstream{Username: "13800000000", APIKey: "upstream-key-1", TimeoutSeconds: 10}

	kept, err := base.Apply(UpstreamUpdate{TimeoutSeconds: intptr(30)})
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if kept.APIKey != base.APIKey || kept.TimeoutSeconds != 30 {
		t.Fatalf("kept = %+v", kept)
	}

	replaced, err := base.Apply(UpstreamUpdate{APIKey: strptr("new-key")})
	if err != nil || replaced.APIKey != "new-key" {
		t.Fatalf("替换 api_key 失败: %v, %+v", err, replaced)
	}

	cleared, err := base.Apply(UpstreamUpdate{APIKey: strptr("")})
	if err != nil || cleared.APIKey != "" {
		t.Fatalf("清空 api_key 失败: %v, %+v", err, cleared)
	}
	if cleared.Configured() {
		t.Fatal("清空密钥后不应视为已配置")
	}

	// 超时越界 → ErrRule。
	if _, err := base.Apply(UpstreamUpdate{TimeoutSeconds: intptr(0)}); !errors.Is(err, ErrRule) {
		t.Fatalf("超时 0 的 err = %v，期望 ErrRule", err)
	}
	if _, err := base.Apply(UpstreamUpdate{TimeoutSeconds: intptr(121)}); !errors.Is(err, ErrRule) {
		t.Fatalf("超时 121 的 err = %v，期望 ErrRule", err)
	}

	// 缺省超时：0 值读出为 5。
	if got := (Upstream{}).TimeoutSecondsOr(); got != DefaultUpstreamTimeoutSeconds {
		t.Fatalf("缺省超时 = %d，期望 %d", got, DefaultUpstreamTimeoutSeconds)
	}
}

// TestUpstreamMissing 验证「配了一半」的缺失项列表：R17 起只看 username / api_key
// （上游地址来自构建期，不参与判定），两者全空返回 nil。
func TestUpstreamMissing(t *testing.T) {
	if missing := (Upstream{}).Missing(); missing != nil {
		t.Fatalf("完全未配置时 Missing() = %v，期望 nil", missing)
	}
	missing := (Upstream{APIKey: "upstream-key-1"}).Missing()
	if len(missing) != 1 || missing[0] != "username" {
		t.Fatalf("缺账号时 Missing() = %v，期望 [username]", missing)
	}
	missing = (Upstream{Username: "13800000000"}).Missing()
	if len(missing) != 1 || missing[0] != "api_key" {
		t.Fatalf("缺密钥时 Missing() = %v，期望 [api_key]", missing)
	}
	if missing := (Upstream{Username: "13800000000", APIKey: "k"}).Missing(); missing != nil {
		t.Fatalf("齐全时 Missing() = %v，期望 nil", missing)
	}
}

// TestUpstreamIgnoresLegacyBaseURL 验证库内遗留的 base_url 键被忽略（不再读、不再写）：
// 解析时不报错、不进入取值结构，重新编码后 JSON 里不再含该键。
func TestUpstreamIgnoresLegacyBaseURL(t *testing.T) {
	legacy := `{"base_url":"https://lyew.com","username":"13800000000","api_key":"k","timeout_seconds":30}`
	decoded, err := decodeUpstream(legacy)
	if err != nil {
		t.Fatalf("解析遗留值失败: %v", err)
	}
	if decoded.Username != "13800000000" || decoded.APIKey != "k" || decoded.TimeoutSeconds != 30 {
		t.Fatalf("遗留值解析错误: %+v", decoded)
	}

	encoded, err := decoded.Encode()
	if err != nil {
		t.Fatalf("Encode 失败: %v", err)
	}
	if strings.Contains(encoded, "base_url") {
		t.Fatalf("重新编码后仍写回 base_url：%s", encoded)
	}
	if want := `{"username":"13800000000","api_key":"k","timeout_seconds":30}`; encoded != want {
		t.Fatalf("Encode = %s，期望 %s", encoded, want)
	}
}

// TestReaderDefaults 验证未配置时返回缺省值（不报错），以及库内值损坏时的错误。
func TestReaderDefaults(t *testing.T) {
	ctx := context.Background()

	empty := NewReader(fakeSource{})
	epay, err := empty.Epay(ctx)
	if err != nil {
		t.Fatalf("未配置读取失败: %v", err)
	}
	if epay.Value.Enabled || epay.Value.Key != "" || epay.Meta.Exists {
		t.Fatalf("未配置时应返回零值: %+v", epay)
	}
	if epay.Fingerprint != "" {
		t.Fatalf("未配置时指纹应为空串，得到 %q", epay.Fingerprint)
	}

	upstream, err := empty.Upstream(ctx)
	if err != nil {
		t.Fatalf("未配置读取失败: %v", err)
	}
	if upstream.Value.TimeoutSeconds != DefaultUpstreamTimeoutSeconds {
		t.Fatalf("未配置时超时应为缺省值，得到 %d", upstream.Value.TimeoutSeconds)
	}

	corrupt := NewReader(fakeSource{values: map[string]string{KeyUpstream: "{not json"}})
	if _, err := corrupt.Upstream(ctx); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("损坏值的 err = %v，期望 ErrCorrupt", err)
	}

	// 库内遗留的 base_url 键被忽略（旧版写入的值属惰性残留，不再读、不再写）。
	legacy := NewReader(fakeSource{values: map[string]string{
		KeyUpstream: `{"base_url":"https://lyew.com","username":"13800000000","api_key":"k","timeout_seconds":30}`,
	}})
	state, err := legacy.Upstream(ctx)
	if err != nil {
		t.Fatalf("含遗留 base_url 的值应能正常读取: %v", err)
	}
	if state.Value.Username != "13800000000" || state.Value.TimeoutSeconds != 30 {
		t.Fatalf("遗留值解析错误: %+v", state.Value)
	}

	// 数据库错误原样回传（不是 ErrNotFound）。
	dbErr := errors.New("db down")
	if _, err := NewReader(fakeSource{err: dbErr}).Epay(ctx); !errors.Is(err, dbErr) {
		t.Fatalf("数据库错误 err = %v", err)
	}
}

// TestReaderFingerprintAndMeta 验证读取结果的指纹与审计字段。
func TestReaderFingerprintAndMeta(t *testing.T) {
	raw := `{"enabled":true,"gateway":"https://pay.example.com","pid":"1001",` +
		`"key":"secret-key-0001","notify_url":"https://oem.example.com/api/v1/payments/epay/notify","return_url":""}`
	reader := NewReader(fakeSource{values: map[string]string{KeyPaymentEpay: raw}})

	state, err := reader.Epay(context.Background())
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if state.Fingerprint != raw {
		t.Fatalf("指纹 = %q，期望库内原文", state.Fingerprint)
	}
	if !state.Meta.Exists || state.Meta.UpdatedBy == nil || *state.Meta.UpdatedBy != 7 {
		t.Fatalf("审计信息 = %+v", state.Meta)
	}
	if !state.Value.Usable() {
		t.Fatalf("应视为可用: %+v", state.Value)
	}
}

// TestEncodeRoundTrip 验证落库 JSON 与解析的往返一致（字段顺序稳定，便于按内容比较）。
func TestEncodeRoundTrip(t *testing.T) {
	value := Upstream{Username: "13800000000", APIKey: "k", TimeoutSeconds: 5}
	encoded, err := value.Encode()
	if err != nil {
		t.Fatalf("Encode 失败: %v", err)
	}
	decoded, err := decodeUpstream(encoded)
	if err != nil {
		t.Fatalf("decode 失败: %v", err)
	}
	if decoded != value {
		t.Fatalf("往返不一致: %+v != %+v", decoded, value)
	}
}

// TestMaskSecret 验证密钥掩码：前 4 位 + ****；过短只给 ****；空串给空串。
func TestMaskSecret(t *testing.T) {
	cases := map[string]string{
		"":             "",
		"abc":          "****",
		"1234567":      "****",
		"1sXR3e8nZZG5": "1sXR****",
	}
	for input, want := range cases {
		if got := MaskSecret(input); got != want {
			t.Fatalf("MaskSecret(%q) = %q，期望 %q", input, got, want)
		}
	}
}
