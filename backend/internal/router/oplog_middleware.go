package router

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/oplog"
)

// contextOplogKey 是请求级操作日志草稿在 gin 上下文中的键。
const contextOplogKey = "lyidc.oplog"

// oplogState 是请求级日志草稿：中间件在写请求开始时放入上下文，处理器可选地补充，
// 请求结束时由中间件统一补齐「方法/路由/状态码/耗时/IP/操作人」并入队（R18 契约 23.3）。
type oplogState struct {
	entry oplog.Entry
	// skip 为 true 时本次请求不入队（清空日志接口自己写留痕，见 skipOplog）。
	skip bool
}

// recordOperation 是管理端/会员端的操作日志中间件（写操作**兜底**埋点）。
//
// 口径（契约 23.3）：
//   - 只记写方法（POST / PUT / PATCH / DELETE），GET / HEAD / OPTIONS 一律不记；
//   - **含被鉴权中间件拒绝的写请求**（401 / 403 同样留痕，操作人取不到时为 0/空串）——
//     未注册路径的 404 不经过本中间件（挂在路由组上，NoRoute 不触发），因此不会被扫描器刷日志；
//   - 动作名优先取处理器补充值，其次查 oplogActionNames 映射表，最后回落「METHOD /path」；
//   - 操作人取鉴权中间件放入上下文的账号（快照用户名）；登录接口由处理器显式补充尝试的用户名；
//   - 入队非阻塞、失败只记服务日志（契约 23.6），绝不影响业务请求。
func recordOperation(writer *oplog.Writer, channel string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isWriteMethod(c.Request.Method) {
			c.Next()
			return
		}

		state := &oplogState{entry: oplog.Entry{Channel: channel, CreatedAt: time.Now().UTC()}}
		c.Set(contextOplogKey, state)
		start := time.Now()
		c.Next()
		if state.skip {
			return
		}

		entry := state.entry
		entry.Method = c.Request.Method
		entry.Path = c.FullPath()
		if entry.Path == "" {
			entry.Path = c.Request.URL.Path
		}
		entry.HTTPStatus = c.Writer.Status()
		if entry.Status == "" {
			entry.Status = oplog.StatusFromHTTPStatus(entry.HTTPStatus)
		}
		entry.IP = c.ClientIP()
		entry.DurationMS = time.Since(start).Milliseconds()
		if entry.Action == "" {
			entry.Action = oplogActionName(entry.Method, entry.Path)
		}
		if entry.ActorID == 0 && entry.ActorName == "" {
			switch {
			case hasAdminContext(c):
				admin, _ := adminFromContext(c)
				entry.ActorID, entry.ActorName = admin.ID, admin.Username
			case hasMemberContext(c):
				member, _ := memberFromContext(c)
				entry.ActorID, entry.ActorName = member.ID, member.Username
			}
		}
		writer.Enqueue(entry)
	}
}

// isWriteMethod 判断是否为需要留痕的写方法。
func isWriteMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// oplogStateFromContext 取出请求级日志草稿；不存在（读请求、或该路由未挂中间件）时返回 nil。
func oplogStateFromContext(c *gin.Context) *oplogState {
	value, ok := c.Get(contextOplogKey)
	if !ok {
		return nil
	}
	state, ok := value.(*oplogState)
	if !ok {
		return nil
	}
	return state
}

// setOplogAction 补充可读动作名（覆盖映射表的兜底值）；非写请求下调用是安全的空操作。
func setOplogAction(c *gin.Context, action string) {
	if state := oplogStateFromContext(c); state != nil {
		state.entry.Action = action
	}
}

// setOplogSummary 补充可读业务摘要（**绝不含密码/密钥**，请求体整体不落库）；非写请求下是空操作。
func setOplogSummary(c *gin.Context, summary string) {
	if state := oplogStateFromContext(c); state != nil {
		state.entry.Summary = summary
	}
}

// setOplogActor 显式指定操作人（登录等没有鉴权中间件的场景：失败记尝试的用户名，成功记账号本身）。
func setOplogActor(c *gin.Context, actorID uint64, actorName string) {
	if state := oplogStateFromContext(c); state != nil {
		state.entry.ActorID, state.entry.ActorName = actorID, actorName
	}
}

// skipOplog 标记本次请求不再由中间件入队（仅清空日志接口使用：它按契约 23.6 自己写一条留痕，
// 且留痕必须落在**未被清空**的通道——中间件默认会写进被清空的通道，故必须跳过）。
func skipOplog(c *gin.Context) {
	if state := oplogStateFromContext(c); state != nil {
		state.skip = true
	}
}

// hasAdminContext / hasMemberContext 是带 ok 的探针（避免 adminFromContext 返回 nil 时误用）。
func hasAdminContext(c *gin.Context) bool {
	_, ok := adminFromContext(c)
	return ok
}

func hasMemberContext(c *gin.Context) bool {
	_, ok := memberFromContext(c)
	return ok
}
