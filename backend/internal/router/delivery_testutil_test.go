package router

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/delivery"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upstream"
)

// ---------------------------------------------------------------------------
// 阶段 5a 测试基建：no-op 交付器、同步交付引擎、完整开通链路的假上游
// ---------------------------------------------------------------------------

// noopDeliverer 是不触发任何交付的哑实现：阶段 4 用例（支付后停留在 paid）继续成立，
// 交付链路由 newStage5Engine 的专门用例覆盖。
type noopDeliverer struct{}

// Trigger 什么都不做。
func (noopDeliverer) Trigger(uint64) {}

// Deliver 不应被调用；返回 ErrNotClaimable 以暴露误用。
func (noopDeliverer) Deliver(context.Context, uint64, bool) (*model.Order, error) {
	return nil, delivery.ErrNotClaimable
}

// newStage5Engine 构造阶段 5a 集成测试引擎：真实数据库 + 假上游（StaticProvider）+
// **同步交付**（Async=false：触发点返回前交付已完成，可确定断言落库结果）。
// logs 非 nil 时把服务端日志写入该缓冲（用于断言日志不含主机密码）。
func newStage5Engine(t *testing.T, gdb *gorm.DB, client *upstream.Client, logs *bytes.Buffer) *gin.Engine {
	t.Helper()
	var handler io.Writer = io.Discard
	if logs != nil {
		handler = logs
	}
	logger := slog.New(slog.NewTextHandler(handler, nil))
	provider := upstream.StaticProvider{C: client}
	return New(Options{
		Logger:   logger,
		DB:       gdb,
		JWT:      config.JWTConfig{Secret: testJWTSecret, ExpireHours: 168},
		Upstream: provider,
		Delivery: delivery.New(store.New(gdb), provider, delivery.Options{Async: false, Logger: logger}),
	})
}

// 开通链路相关路径（与 internal/upstream 内部常量一致）。
const (
	pathCartClear  = "/cart/clear"
	pathCartAdd    = "/cart/add_to_shop"
	pathCartSettle = "/cart/settle"
	pathApplyCred  = "/apply_credit"
	pathHostInfo   = "/cart/hostinfo"
)

// 阶段 5b 新增路径：生命周期操作 / 续费 / 可选系统列表。
const (
	pathProvisionDef = "/provision/default"
	pathHostRenew    = "/host/renew"
	pathHostCloudOS  = "/host/cloudos"
)

// fakeUpstreamHostIDs 为每次开通分配互不重复的上游主机 ID。
var fakeUpstreamHostIDs atomic.Int64

// fakeHostServer 是支持完整开通链路与生命周期操作的假上游（httptest）：
// /cart/clear → /cart/add_to_shop → /cart/settle → /apply_credit → /cart/hostinfo，
// 以及阶段 5b 的 /provision/default（on/off/reboot/hard_*/status/reinstall/crack_pass/suspend/unsuspend）、
// /host/renew、/host/cloudos；记录各步调用次数与收到的参数，供断言参数拼装与幂等。
type fakeHostServer struct {
	server *httptest.Server

	mu     sync.Mutex
	counts map[string]int
	forms  map[string]url.Values

	hostID int
	// hosts 是已开通主机的运行态（主机 ID → 状态），支持一个假上游下多台主机。
	hosts map[int]*fakeHostState
	// nextDueUnix 是 hostinfo 回带的到期时间（unix 秒；默认 30 天后）。
	nextDueUnix int64
	// hostPassword 是 hostinfo 回带的主机密码（空串表示上游不回传）。
	hostPassword string

	// failApplyCredit 为 true 时 /apply_credit 返回 status=200（上游余额不足语义）。
	failApplyCredit bool
	// settleNoHostID 为 true 时 settle/apply_credit 都不回带 hostid（上游异常响应）。
	settleNoHostID bool
	// hostinfoMissing 为 true 时 /cart/hostinfo 返回空列表（回读失败场景）。
	hostinfoMissing bool

	// provisionFail 按 func 注入 /provision/default 的业务失败（返回 status=400 + 该文案）。
	provisionFail map[string]string
	// funcCounts 记录 /provision/default 各 func 的调用次数（断言「未调用」用）。
	funcCounts map[string]int
	// autoUnsuspendOnRenew 为 true（默认，与生产上游实测一致）时，续费成功会自动解除暂停。
	autoUnsuspendOnRenew bool
	// renewFail 非空时 /host/renew 返回业务失败（status=400 + 该文案）。
	renewFail string
	// renewDueUnix 是续费后的到期时间（0 表示在现有到期时间上顺延 1 个月）。
	renewDueUnix int64
	// cloudOS 是 /host/cloudos 返回的可选系统（缺省为两个固定系统）。
	cloudOS []fakeCloudOS
}

// fakeHostState 是一台已开通主机的运行态。
type fakeHostState struct {
	powerState  string
	suspended   bool
	osID        int
	password    string
	nextDueUnix int64
	dedicatedIP string
}

// fakeCloudOS 是一条可选系统记录。
type fakeCloudOS struct {
	ID    int
	Name  string
	Group string
}

// newFakeHostServer 启动假上游（测试结束自动关闭）。
func newFakeHostServer(t *testing.T) *fakeHostServer {
	t.Helper()
	f := &fakeHostServer{
		counts:               map[string]int{},
		forms:                map[string]url.Values{},
		funcCounts:           map[string]int{},
		autoUnsuspendOnRenew: true,
		hosts:                map[int]*fakeHostState{},
		nextDueUnix:          time.Now().UTC().AddDate(0, 1, 0).Unix(),
		hostPassword:         "UpstreamPass123",
		cloudOS: []fakeCloudOS{
			{ID: 9, Name: "CentOS-7.9.2111-x64", Group: "CentOS"},
			{ID: 12, Name: "Debian-12.0_x64", Group: "Debian"},
		},
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	return f
}

// client 返回指向假上游的客户端（登录固定成功）。
func (f *fakeHostServer) client(t *testing.T) *upstream.Client {
	t.Helper()
	return fakeUpstreamClient(t, f.handle)
}

// handle 处理开通链路各步请求。
func (f *fakeHostServer) handle(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()

	f.mu.Lock()
	f.counts[r.URL.Path]++
	f.forms[r.URL.Path] = r.Form
	f.mu.Unlock()

	switch r.URL.Path {
	case pathCartClear:
		// 无历史待支付账单：正常走 add_to_shop + settle（客户端据此取 user.currency）。
		_, _ = io.WriteString(w, `{"status":200,"msg":"请求成功","user":{"id":1,"username":"fake","currency":1}}`)
	case pathCartAdd:
		_, _ = io.WriteString(w, `{"status":200,"msg":"加入购物车成功"}`)
	case pathCartSettle:
		hostID := f.allocateHostID()
		if f.settleNoHostID {
			_, _ = io.WriteString(w, `{"status":200,"msg":"结算成功","invoiceid":55001}`)
			return
		}
		_, _ = fmt.Fprintf(w, `{"status":200,"msg":"结算成功","hostid":%d,"invoiceid":55001}`, hostID)
	case pathApplyCred:
		if f.failApplyCredit {
			_, _ = io.WriteString(w, `{"status":200,"msg":"余额不足"}`)
			return
		}
		if f.settleNoHostID {
			_, _ = io.WriteString(w, `{"status":1001,"msg":"支付成功","data":{}}`)
			return
		}
		hostID := f.hostIDValue()
		if hostID == 0 {
			hostID = f.allocateHostID()
		}
		_, _ = fmt.Fprintf(w, `{"status":1001,"msg":"支付成功","data":{"hostid":%d}}`, hostID)
	case pathHostInfo:
		f.writeHostInfo(w, r.Form)
	case pathProvisionDef:
		f.handleProvision(w, r.Form)
	case pathHostRenew:
		f.handleRenew(w, r.Form)
	case pathHostCloudOS:
		f.handleCloudOS(w, r.Form)
	default:
		_, _ = io.WriteString(w, `{"status":404,"msg":"接口不存在"}`)
	}
}

// handleProvision 处理 /provision/default（生命周期操作）。
func (f *fakeHostServer) handleProvision(w http.ResponseWriter, form url.Values) {
	funcName := form.Get("func")
	hostID, _ := strconv.Atoi(form.Get("id"))

	f.mu.Lock()
	if f.funcCounts == nil {
		f.funcCounts = map[string]int{}
	}
	f.funcCounts[funcName]++
	if msg, ok := f.provisionFail[funcName]; ok {
		f.mu.Unlock()
		_, _ = fmt.Fprintf(w, `{"status":400,"msg":%q}`, msg)
		return
	}
	state := f.hosts[hostID]
	if state == nil {
		state = &fakeHostState{powerState: "off", password: f.hostPassword, nextDueUnix: f.nextDueUnix}
		f.hosts[hostID] = state
	}
	switch funcName {
	case "on":
		state.powerState = "on"
	case "off", "hard_off":
		state.powerState = "off"
	case "reboot", "hard_reboot":
		state.powerState = "on"
	case "reinstall":
		if osID, err := strconv.Atoi(form.Get("os")); err == nil {
			state.osID = osID
		}
	case "crack_pass":
		if password := form.Get("password"); password != "" {
			state.password = password
		}
	case "suspend":
		state.suspended = true
	case "unsuspend":
		state.suspended = false
	}
	stateSnapshot := *state
	f.mu.Unlock()

	switch funcName {
	case "status":
		desc := map[string]string{"on": "开机", "off": "关机", "process": "重装中"}[stateSnapshot.powerState]
		_, _ = fmt.Fprintf(w, `{"status":200,"msg":"","data":{"status":%q,"des":%q}}`,
			stateSnapshot.powerState, desc)
	case "on":
		_, _ = io.WriteString(w, `{"status":200,"msg":"开机成功","data":null}`)
	case "off":
		_, _ = io.WriteString(w, `{"status":200,"msg":"关机成功","data":null}`)
	case "hard_off":
		_, _ = io.WriteString(w, `{"status":200,"msg":"强制关机成功","data":null}`)
	case "reboot":
		_, _ = io.WriteString(w, `{"status":200,"msg":"发起重启成功","data":null}`)
	case "hard_reboot":
		_, _ = io.WriteString(w, `{"status":200,"msg":"强制重启成功","data":null}`)
	case "reinstall":
		_, _ = io.WriteString(w, `{"status":200,"msg":"重装发起成功","data":null}`)
	case "crack_pass":
		_, _ = io.WriteString(w, `{"status":200,"msg":"密码修改成功","data":null}`)
	case "suspend":
		_, _ = io.WriteString(w, `{"status":200,"msg":"暂停成功","data":null}`)
	case "unsuspend":
		_, _ = io.WriteString(w, `{"status":200,"msg":"解除暂停成功","data":null}`)
	default:
		_, _ = fmt.Fprintf(w, `{"status":400,"msg":"不支持的方法 %s"}`, funcName)
	}
}

// handleRenew 处理 /host/renew：更新该主机的到期时间并返回账单 ID。
func (f *fakeHostServer) handleRenew(w http.ResponseWriter, form url.Values) {
	hostID, _ := strconv.Atoi(form.Get("hostid"))

	f.mu.Lock()
	if f.renewFail != "" {
		msg := f.renewFail
		f.mu.Unlock()
		_, _ = fmt.Fprintf(w, `{"status":400,"msg":%q}`, msg)
		return
	}
	state := f.hosts[hostID]
	if state == nil {
		state = &fakeHostState{powerState: "on", password: f.hostPassword, nextDueUnix: f.nextDueUnix}
		f.hosts[hostID] = state
	}
	if f.renewDueUnix > 0 {
		state.nextDueUnix = f.renewDueUnix
	} else {
		state.nextDueUnix = time.Unix(state.nextDueUnix, 0).UTC().AddDate(0, 1, 0).Unix()
	}
	if f.autoUnsuspendOnRenew {
		// 与生产上游实测一致：续费成功会自行解除到期暂停。
		state.suspended = false
	}
	f.mu.Unlock()

	_, _ = io.WriteString(w, `{"status":200,"msg":"续费成功","invoiceid":55002}`)
}

// handleCloudOS 处理 /host/cloudos：**必须带 os_config_option_id** 才返回系统列表
// （与上游实测一致：不带该参数的请求返回空列表）。
func (f *fakeHostServer) handleCloudOS(w http.ResponseWriter, form url.Values) {
	if form.Get("os_config_option_id") == "" {
		_, _ = io.WriteString(w, `{"status":200,"data":{"cloud_os":[],"cloud_os_group":[]}}`)
		return
	}
	f.mu.Lock()
	items := append([]fakeCloudOS(nil), f.cloudOS...)
	f.mu.Unlock()

	body := make([]string, 0, len(items))
	for _, item := range items {
		body = append(body, fmt.Sprintf(`{"id":%d,"name":%q,"group":%q}`, item.ID, item.Name, item.Group))
	}
	_, _ = fmt.Fprintf(w, `{"status":200,"data":{"cloud_os":[%s],"cloud_os_group":[]}}`, strings.Join(body, ","))
}

// writeHostInfo 回带已开通主机的详情（回读来源）。
// 请求带 hostid[] 时按该 ID 精确返回；否则返回最近一次开通的主机。
func (f *fakeHostServer) writeHostInfo(w http.ResponseWriter, form url.Values) {
	f.mu.Lock()
	hostID := f.hostID
	if len(form["hostid[]"]) > 0 {
		if id, err := strconv.Atoi(form["hostid[]"][0]); err == nil {
			hostID = id
		}
	}
	nextDue := f.nextDueUnix
	missing := f.hostinfoMissing
	password := f.hostPassword
	state := f.hosts[hostID]
	if state != nil {
		nextDue = state.nextDueUnix
		password = state.password
	}
	suspended := state != nil && state.suspended
	f.mu.Unlock()

	if missing || hostID == 0 {
		_, _ = io.WriteString(w, `{"status":200,"msg":"请求成功","data":{"hosts":[],"currency":"CNY"}}`)
		return
	}
	domainStatus := "Active"
	if suspended {
		domainStatus = "Suspended"
	}
	dedicatedIP := "203.0.113.10"
	if state != nil && state.dedicatedIP != "" {
		dedicatedIP = state.dedicatedIP
	}
	_, _ = fmt.Fprintf(w,
		`{"status":200,"msg":"请求成功","data":{"hosts":[{"id":%d,"productid":7001,"domain":"oem-host",`+
			`"dedicatedip":%q,"assignedips":["203.0.113.11",""],"create_time":%d,`+
			`"nextduedate":%d,"billingcycle":"monthly","domainstatus":%q,"port":22022,`+
			`"username":"root","password":%q}],"currency":"CNY"}}`,
		hostID, dedicatedIP, time.Now().Unix(), nextDue, domainStatus, password)
}

// allocateHostID 为一次新开通分配上游主机 ID（每次开通都不重复；同一开通内 settle 与
// apply_credit 用同一个 ID，由 hostIDValue 读取）。同时初始化该主机的运行态。
func (f *fakeHostServer) allocateHostID() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hostID = int(fakeUpstreamHostIDs.Add(1)) + 90000
	f.hosts[f.hostID] = &fakeHostState{
		powerState:  "on",
		password:    f.hostPassword,
		nextDueUnix: f.nextDueUnix,
		dedicatedIP: "203.0.113.10",
	}
	return f.hostID
}

// hostIDValue 返回本次开通的主机 ID（未开通时为 0）。
func (f *fakeHostServer) hostIDValue() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hostID
}

// callCount 返回某路径被调用的次数。
func (f *fakeHostServer) callCount(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[path]
}

// lastForm 返回某路径最后一次收到的表单参数（未调用过时为空）。
func (f *fakeHostServer) lastForm(path string) url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.forms[path]
}

// setFailApplyCredit 切换 /apply_credit 失败（上游余额不足）注入。
func (f *fakeHostServer) setFailApplyCredit(fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failApplyCredit = fail
}

// setHostinfoMissing 切换回读失败注入（/cart/hostinfo 返回空列表）。
func (f *fakeHostServer) setHostinfoMissing(missing bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hostinfoMissing = missing
}

// setHostPassword 设置 hostinfo 回带的主机密码（空串表示不回传）。
func (f *fakeHostServer) setHostPassword(password string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hostPassword = password
}

// setProvisionFail 注入 /provision/default 某个 func 的业务失败（msg 为空表示清除）。
func (f *fakeHostServer) setProvisionFail(funcName, msg string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.provisionFail == nil {
		f.provisionFail = map[string]string{}
	}
	if msg == "" {
		delete(f.provisionFail, funcName)
		return
	}
	f.provisionFail[funcName] = msg
}

// setRenewFail 注入 /host/renew 的业务失败（空串表示恢复正常）。
func (f *fakeHostServer) setRenewFail(msg string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.renewFail = msg
}

// setAutoUnsuspendOnRenew 设置「续费成功是否自动解除暂停」（默认 true，与生产上游一致；
// false 用于模拟「续费后上游仍暂停」的场景）。
func (f *fakeHostServer) setAutoUnsuspendOnRenew(auto bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.autoUnsuspendOnRenew = auto
}

// funcCallCount 返回 /provision/default 某 func 被调用的次数。
func (f *fakeHostServer) funcCallCount(funcName string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.funcCounts[funcName]
}

// setRenewDueUnix 设置续费后的到期时间（unix 秒；0 表示顺延 1 个月）。
func (f *fakeHostServer) setRenewDueUnix(unix int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.renewDueUnix = unix
}

// state 返回某主机的运行态（不存在时返回 nil）。
func (f *fakeHostServer) state(hostID int) *fakeHostState {
	f.mu.Lock()
	defer f.mu.Unlock()
	state := f.hosts[hostID]
	if state == nil {
		return nil
	}
	snapshot := *state
	return &snapshot
}

// hostState 返回最近一次开通主机的运行态（不存在时 Fatal）。
func (f *fakeHostServer) hostState(t *testing.T) *fakeHostState {
	t.Helper()
	state := f.state(f.hostIDValue())
	if state == nil {
		t.Fatalf("假上游不存在主机 %d 的运行态", f.hostIDValue())
	}
	return state
}

// newStage5EngineAsync 构造异步交付引擎（生产默认路径：触发不阻塞，交付在后台完成）。
func newStage5EngineAsync(t *testing.T, gdb *gorm.DB, client *upstream.Client) *gin.Engine {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	provider := upstream.StaticProvider{C: client}
	return New(Options{
		Logger:   logger,
		DB:       gdb,
		JWT:      config.JWTConfig{Secret: testJWTSecret, ExpireHours: 168},
		Upstream: provider,
		Delivery: delivery.New(store.New(gdb), provider, delivery.Options{Async: true, Logger: logger}),
	})
}
