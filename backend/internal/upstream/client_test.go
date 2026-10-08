package upstream

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"
)

// 测试用上游账号常量（非真实密钥，仅用于断言请求内容）。
const (
	testUsername = "13800000000"
	testAPIKey   = "TestKey1234567"
)

// recordedRequest 记录一次到达假上游的请求，便于断言鉴权头与参数。
type recordedRequest struct {
	Method string
	Path   string
	Query  url.Values
	Form   url.Values
	Auth   string
}

// handlerFunc 是假上游对某个路径的处理函数：callCount 从 1 开始计数。
type handlerFunc func(callCount int, r *http.Request, form url.Values) (httpStatus int, body string)

// fakeUpstream 是一个可编程的假上游（httptest），用于离线单测。
type fakeUpstream struct {
	server   *httptest.Server
	handlers map[string]handlerFunc

	mu    sync.Mutex
	calls map[string]int
	seen  []recordedRequest
}

// newFakeUpstream 创建假上游；未显式登记的路径默认返回 404，
// /zjmf_api_login 默认返回鉴权成功（可在 handlers 里覆盖）。
func newFakeUpstream(t *testing.T, handlers map[string]handlerFunc) *fakeUpstream {
	t.Helper()

	fake := &fakeUpstream{handlers: handlers, calls: map[string]int{}}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()

		fake.mu.Lock()
		fake.calls[r.URL.Path]++
		count := fake.calls[r.URL.Path]
		fake.seen = append(fake.seen, recordedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.Query(),
			Form:   r.PostForm,
			Auth:   r.Header.Get("Authorization"),
		})
		fake.mu.Unlock()

		handler, ok := fake.handlers[r.URL.Path]
		if !ok {
			if r.URL.Path == pathLogin {
				writer := w
				writeJSON(writer, http.StatusOK, `{"status":200,"msg":"鉴权成功","jwt":"fake-jwt-token","is_aff":"1"}`)
				return
			}
			writeJSON(w, http.StatusNotFound, `<html>404</html>`)
			return
		}

		httpStatus, body := handler(count, r, r.PostForm)
		writeJSON(w, httpStatus, body)
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

// client 基于假上游创建客户端。
func (f *fakeUpstream) client(t *testing.T, mutate func(*Config)) *Client {
	t.Helper()
	cfg := Config{
		BaseURL:      f.server.URL,
		Username:     testUsername,
		APIKey:       testAPIKey,
		Timeout:      2 * time.Second,
		RetryBackoff: time.Millisecond,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if mutate != nil {
		mutate(&cfg)
	}
	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New() 失败: %v", err)
	}
	return client
}

// count 返回某路径被调用的次数。
func (f *fakeUpstream) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[path]
}

// requests 返回某路径收到的全部请求。
func (f *fakeUpstream) requests(path string) []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []recordedRequest
	for _, item := range f.seen {
		if item.Path == path {
			out = append(out, item)
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// okBody 构造成功响应（上游包格式）。
func okBody(data string) string {
	return `{"status":200,"msg":"请求成功","data":` + data + `,"is_aff":"1"}`
}

// failBody 构造业务失败响应。
func failBody(status int, msg string) string {
	return `{"status":` + strconv.Itoa(status) + `,"msg":"` + msg + `","is_aff":"1"}`
}

func TestNewRejectsInvalidBaseURL(t *testing.T) {
	cases := []string{"lyew.com", "ftp://lyew.com", "http://"}
	for _, base := range cases {
		if _, err := New(Config{BaseURL: base}); err == nil {
			t.Errorf("New(BaseURL=%q) 期望报错，实际通过", base)
		}
	}
}

func TestEnabledRequiresFullConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"齐全", Config{BaseURL: "https://lyew.com", Username: testUsername, APIKey: testAPIKey}, true},
		{"缺地址", Config{Username: testUsername, APIKey: testAPIKey}, false},
		{"缺账号", Config{BaseURL: "https://lyew.com", APIKey: testAPIKey}, false},
		{"缺密钥", Config{BaseURL: "https://lyew.com", Username: testUsername}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, err := New(tc.cfg)
			if err != nil {
				t.Fatalf("New() 失败: %v", err)
			}
			if got := client.Enabled(); got != tc.want {
				t.Errorf("Enabled() = %v, 期望 %v", got, tc.want)
			}
		})
	}
}

func TestCallWithoutConfigReturnsNotConfigured(t *testing.T) {
	client, err := New(Config{BaseURL: "https://lyew.com"})
	if err != nil {
		t.Fatalf("New() 失败: %v", err)
	}

	var out ProductCatalog
	_, err = client.Get(context.Background(), "/cart/all", nil, &out)
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("未配置上游时 Get() 错误 = %v, 期望 ErrNotConfigured", err)
	}
}

func TestLoginSendsFormCredentialsAndCachesJWT(t *testing.T) {
	fake := newFakeUpstream(t, map[string]handlerFunc{
		pathCartCredit: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, okBody(`{"credit":"100.00","currency":{"id":1,"code":"CNY"}}`)
		},
	})
	client := fake.client(t, nil)

	credit, err := client.Credit(context.Background())
	if err != nil {
		t.Fatalf("Credit() 失败: %v", err)
	}
	if credit.Credit != "100.00" || credit.Currency.Code != "CNY" {
		t.Errorf("Credit() = %+v, 期望 credit=100.00 code=CNY", credit)
	}

	logins := fake.requests(pathLogin)
	if len(logins) != 1 {
		t.Fatalf("登录次数 = %d, 期望 1（JWT 应被缓存复用）", len(logins))
	}
	if got := logins[0].Form.Get("username"); got != testUsername {
		t.Errorf("登录 username = %q, 期望 %q", got, testUsername)
	}
	if got := logins[0].Form.Get("password"); got != testAPIKey {
		t.Errorf("登录 password = %q, 期望 %q", got, testAPIKey)
	}
	if logins[0].Auth != "" {
		t.Errorf("登录请求不应携带 Authorization，实际 = %q", logins[0].Auth)
	}

	// 第二次业务调用只加一次登录。
	if _, err := client.Credit(context.Background()); err != nil {
		t.Fatalf("第二次 Credit() 失败: %v", err)
	}
	if got := fake.count(pathLogin); got != 1 {
		t.Errorf("登录次数 = %d, 期望仍为 1", got)
	}

	calls := fake.requests(pathCartCredit)
	if len(calls) != 2 {
		t.Fatalf("业务调用次数 = %d, 期望 2", len(calls))
	}
	for _, call := range calls {
		if call.Auth != "Bearer fake-jwt-token" {
			t.Errorf("业务请求 Authorization = %q, 期望 Bearer fake-jwt-token", call.Auth)
		}
		if call.Method != http.MethodGet {
			t.Errorf("Credit 应使用 GET，实际 %s", call.Method)
		}
	}
}

func TestLoginFailureMapsToAuthError(t *testing.T) {
	fake := newFakeUpstream(t, map[string]handlerFunc{
		pathLogin: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, failBody(400, "鉴权失败")
		},
	})
	client := fake.client(t, nil)

	var out ProductCatalog
	_, err := client.Get(context.Background(), pathCartAll, nil, &out)
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("鉴权失败时错误 = %v, 期望 ErrAuth", err)
	}

	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("错误类型 = %T, 期望 *AuthError", err)
	}
	if authErr.Msg != "鉴权失败" || authErr.API != pathLogin {
		t.Errorf("AuthError = %+v, 期望 保留上游提示与接口路径", authErr)
	}
}

func TestExpiredTokenTriggersReloginAndReplay(t *testing.T) {
	fake := newFakeUpstream(t, map[string]handlerFunc{
		pathCartCredit: func(call int, _ *http.Request, _ url.Values) (int, string) {
			if call == 1 {
				return http.StatusOK, failBody(statusNotLogged, "请登陆后再试")
			}
			return http.StatusOK, okBody(`{"credit":"20.00","currency":{"id":1,"code":"CNY"}}`)
		},
	})
	client := fake.client(t, nil)

	credit, err := client.Credit(context.Background())
	if err != nil {
		t.Fatalf("登录态失效后未自动重登: %v", err)
	}
	if credit.Credit != "20.00" {
		t.Errorf("Credit() = %+v, 期望 credit=20.00", credit)
	}
	if got := fake.count(pathLogin); got != 2 {
		t.Errorf("登录次数 = %d, 期望 2（首次登录 + 失效后重登）", got)
	}
	if got := fake.count(pathCartCredit); got != 2 {
		t.Errorf("业务调用次数 = %d, 期望 2（原请求 + 重放）", got)
	}
}

func TestReloginAlsoExpiredReturnsErrNotLoggedIn(t *testing.T) {
	fake := newFakeUpstream(t, map[string]handlerFunc{
		pathCartCredit: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, failBody(statusNotLogged, "请登陆后再试")
		},
	})
	client := fake.client(t, nil)

	_, err := client.Credit(context.Background())
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("连续失效时错误 = %v, 期望 ErrNotLoggedIn", err)
	}
	if got := fake.count(pathLogin); got != 2 {
		t.Errorf("登录次数 = %d, 期望 2（不应无限重试）", got)
	}
	if got := fake.count(pathCartCredit); got != 2 {
		t.Errorf("业务调用次数 = %d, 期望 2（不应无限重放）", got)
	}
}

func TestBusinessErrorMapping(t *testing.T) {
	fake := newFakeUpstream(t, map[string]handlerFunc{
		pathCartSummary: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, failBody(400, "暂未开通API功能")
		},
		pathProvisionDef: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, failBody(406, "ID错误")
		},
	})
	client := fake.client(t, nil)

	_, err := client.Summary(context.Background())
	if !errors.Is(err, ErrBusiness) {
		t.Fatalf("Summary() 错误 = %v, 期望 ErrBusiness", err)
	}
	var bizErr *BusinessError
	if !errors.As(err, &bizErr) {
		t.Fatalf("错误类型 = %T, 期望 *BusinessError", err)
	}
	if bizErr.Status != 400 || bizErr.Msg != "暂未开通API功能" || bizErr.API != pathCartSummary {
		t.Errorf("BusinessError = %+v, 期望 status=400 且保留上游提示", bizErr)
	}

	// 406 同样归类为业务失败。
	_, err = client.On(context.Background(), 1)
	if !errors.Is(err, ErrBusiness) {
		t.Fatalf("On() 错误 = %v, 期望 ErrBusiness", err)
	}
	if got := fake.count(pathProvisionDef); got != 1 {
		t.Errorf("写操作调用次数 = %d, 期望 1（业务失败不重试）", got)
	}
}

func TestHTTPStatusAndNonJSONMapToUpstreamError(t *testing.T) {
	fake := newFakeUpstream(t, map[string]handlerFunc{
		pathCartAll: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusBadGateway, "<html>502</html>"
		},
	})
	client := fake.client(t, func(cfg *Config) { cfg.MaxRetries = 0 })

	var out ProductCatalog
	_, err := client.Get(context.Background(), pathCartAll, nil, &out)
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("HTTP 502 时错误 = %v, 期望 ErrUpstream", err)
	}

	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.HTTPStatus != http.StatusBadGateway {
		t.Fatalf("错误类型 = %T (%v), 期望 *HTTPError 且状态码 502", err, err)
	}
}

func TestDecodeErrorOnMalformedJSON(t *testing.T) {
	fake := newFakeUpstream(t, map[string]handlerFunc{
		pathCartCredit: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, `{"status":200,"msg":"ok","data":{`
		},
	})
	client := fake.client(t, func(cfg *Config) { cfg.MaxRetries = 0 })

	_, err := client.Credit(context.Background())
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("响应非合法 JSON 时错误 = %v, 期望 ErrDecode", err)
	}
}

func TestTimeoutMapsToErrTimeout(t *testing.T) {
	fake := newFakeUpstream(t, map[string]handlerFunc{
		pathCartCredit: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			time.Sleep(150 * time.Millisecond)
			return http.StatusOK, okBody(`{"credit":"1.00"}`)
		},
	})
	client := fake.client(t, func(cfg *Config) {
		cfg.Timeout = 20 * time.Millisecond
		cfg.MaxRetries = 0
	})

	_, err := client.Credit(context.Background())
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("超时错误 = %v, 期望 ErrTimeout", err)
	}
}

func TestIdempotentGetRetriesNetworkErrorButPostDoesNot(t *testing.T) {
	// GET 第一次返回 503（上游异常），配置重试后应自愈。
	getFake := newFakeUpstream(t, map[string]handlerFunc{
		pathCartCredit: func(call int, _ *http.Request, _ url.Values) (int, string) {
			if call == 1 {
				return http.StatusServiceUnavailable, "boom"
			}
			return http.StatusOK, okBody(`{"credit":"5.00"}`)
		},
	})
	getClient := getFake.client(t, func(cfg *Config) {
		cfg.MaxRetries = 1
		cfg.RetryBackoff = time.Millisecond
	})

	if _, err := getClient.Credit(context.Background()); err != nil {
		t.Fatalf("幂等 GET 应自动重试成功，实际错误: %v", err)
	}
	if got := getFake.count(pathCartCredit); got != 2 {
		t.Errorf("GET 调用次数 = %d, 期望 2", got)
	}

	// 写操作（POST）绝不自动重试。
	postFake := newFakeUpstream(t, map[string]handlerFunc{
		pathProvisionDef: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusServiceUnavailable, "boom"
		},
	})
	postClient := postFake.client(t, func(cfg *Config) {
		cfg.MaxRetries = 2
		cfg.RetryBackoff = time.Millisecond
	})

	if _, err := postClient.On(context.Background(), 8); err == nil {
		t.Fatal("写操作遇到上游异常应返回错误")
	}
	if got := postFake.count(pathProvisionDef); got != 1 {
		t.Errorf("写操作调用次数 = %d, 期望 1（不自动重试）", got)
	}
}

func TestPostUsesFormEncodingAndBearer(t *testing.T) {
	fake := newFakeUpstream(t, map[string]handlerFunc{
		pathProvisionDef: func(_ int, _ *http.Request, _ url.Values) (int, string) {
			return http.StatusOK, okBody(`{"hostid":12}`)
		},
	})
	client := fake.client(t, nil)

	if _, err := client.On(context.Background(), 7); err != nil {
		t.Fatalf("On() 失败: %v", err)
	}

	calls := fake.requests(pathProvisionDef)
	if len(calls) != 1 {
		t.Fatalf("调用次数 = %d, 期望 1", len(calls))
	}
	call := calls[0]
	if call.Method != http.MethodPost {
		t.Errorf("方法 = %s, 期望 POST", call.Method)
	}
	if call.Form.Get("func") != string(OpOn) || call.Form.Get("id") != "7" {
		t.Errorf("表单 = %v, 期望 func=on&id=7", call.Form)
	}
	if call.Auth != "Bearer fake-jwt-token" {
		t.Errorf("Authorization = %q, 期望 Bearer fake-jwt-token", call.Auth)
	}
}

func TestMaskAPIKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"abc", "****"},
		{"1234567", "****"},
		{"TestKey1234567", "Test****567"},
	}
	for _, tc := range cases {
		if got := MaskAPIKey(tc.in); got != tc.want {
			t.Errorf("MaskAPIKey(%q) = %q, 期望 %q", tc.in, got, tc.want)
		}
	}
}

func TestHostIDsParsesUpstreamVariants(t *testing.T) {
	cases := []struct {
		raw  string
		want []int
	}{
		{`1`, []int{1}},
		{`"2"`, []int{2}},
		{`[3,4]`, []int{3, 4}},
		{`["5","6"]`, []int{5, 6}},
		{`null`, nil},
	}
	for _, tc := range cases {
		resp := &Response{HostID: []byte(tc.raw)}
		got := resp.HostIDs()
		if len(got) != len(tc.want) {
			t.Errorf("HostIDs(%s) = %v, 期望 %v", tc.raw, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("HostIDs(%s)[%d] = %d, 期望 %d", tc.raw, i, got[i], tc.want[i])
			}
		}
	}
}
