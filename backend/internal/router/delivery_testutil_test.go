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

// fakeUpstreamHostIDs 为每次开通分配互不重复的上游主机 ID。
var fakeUpstreamHostIDs atomic.Int64

// fakeHostServer 是支持完整开通链路的假上游（httptest）：
// /cart/clear → /cart/add_to_shop → /cart/settle → /apply_credit → /cart/hostinfo，
// 记录各步调用次数与收到的参数，供断言参数拼装与幂等（不重复开通）。
type fakeHostServer struct {
	server *httptest.Server

	mu     sync.Mutex
	counts map[string]int
	forms  map[string]url.Values

	hostID int
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
}

// newFakeHostServer 启动假上游（测试结束自动关闭）。
func newFakeHostServer(t *testing.T) *fakeHostServer {
	t.Helper()
	f := &fakeHostServer{
		counts:       map[string]int{},
		forms:        map[string]url.Values{},
		nextDueUnix:  time.Now().UTC().AddDate(0, 1, 0).Unix(),
		hostPassword: "UpstreamPass123",
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
		f.writeHostInfo(w)
	default:
		_, _ = io.WriteString(w, `{"status":404,"msg":"接口不存在"}`)
	}
}

// writeHostInfo 回带已开通主机的详情（回读来源）。
func (f *fakeHostServer) writeHostInfo(w http.ResponseWriter) {
	f.mu.Lock()
	hostID := f.hostID
	nextDue := f.nextDueUnix
	missing := f.hostinfoMissing
	password := f.hostPassword
	f.mu.Unlock()

	if missing || hostID == 0 {
		_, _ = io.WriteString(w, `{"status":200,"msg":"请求成功","data":{"hosts":[],"currency":"CNY"}}`)
		return
	}
	_, _ = fmt.Fprintf(w,
		`{"status":200,"msg":"请求成功","data":{"hosts":[{"id":%d,"productid":7001,"domain":"oem-host",`+
			`"dedicatedip":"203.0.113.10","assignedips":["203.0.113.11",""],"create_time":%d,`+
			`"nextduedate":%d,"billingcycle":"monthly","domainstatus":"Active","port":22022,`+
			`"username":"root","password":%q}],"currency":"CNY"}}`,
		hostID, time.Now().Unix(), nextDue, password)
}

// allocateHostID 为一次新开通分配上游主机 ID（每次开通都不重复；同一开通内 settle 与
// apply_credit 用同一个 ID，由 hostIDValue 读取）。
func (f *fakeHostServer) allocateHostID() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hostID = int(fakeUpstreamHostIDs.Add(1)) + 90000
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
