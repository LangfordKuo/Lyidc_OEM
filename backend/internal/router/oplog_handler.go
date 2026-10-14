package router

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/oplog"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// oplogClearAll 是「清空全部通道」的 channel 取值。
const oplogClearAll = "all"

// oplogFlushTimeout 是清空前冲出在途缓冲的等待上限。
const oplogFlushTimeout = 5 * time.Second

// operationLogHandler 处理管理端操作日志接口（R18 契约 23.4）：
// 列表（高级搜索）与清空。**没有**按条删除或修改的接口（只允许整体清空）。
type operationLogHandler struct {
	store  *store.Store
	writer *oplog.Writer
	logger *slog.Logger
}

// operationLogView 是列表项视图（时间按 RFC3339 UTC 输出）。
type operationLogView struct {
	ID         uint64 `json:"id"`
	Channel    string `json:"channel"`
	ActorID    uint64 `json:"actor_id"`
	ActorName  string `json:"actor_name"`
	Action     string `json:"action"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	HTTPStatus uint16 `json:"http_status"`
	Status     string `json:"status"`
	IP         string `json:"ip"`
	DurationMS uint64 `json:"duration_ms"`
	Summary    string `json:"summary"`
	CreatedAt  string `json:"created_at"`
}

// operationLogListView 是 GET /api/v1/admin/operation-logs 的 data。
type operationLogListView struct {
	Items    []operationLogView `json:"items"`
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
	Total    int64              `json:"total"`
}

// clearOperationLogsView 是 DELETE /api/v1/admin/operation-logs 的 data。
type clearOperationLogsView struct {
	Channel string `json:"channel"`
	Deleted int64  `json:"deleted"`
	// RecordChannel 是清空动作留痕写入的通道（契约 23.6：留痕写进**未被清空**的通道）。
	RecordChannel string `json:"record_channel"`
}

// listOperationLogs 处理 GET /api/v1/admin/operation-logs：分页 + 高级搜索（关键字 / 时间范围 /
// 通道 / 结果 / 操作人），一律新记录在前（契约 23.4）。
func (h *operationLogHandler) listOperationLogs(c *gin.Context) {
	page, pageSize, ok := pagingParams(c)
	if !ok {
		return
	}

	channel := strings.TrimSpace(c.Query("channel"))
	if channel != "" && !model.IsValidOplogChannel(channel) {
		response.Fail(c, response.CodeInvalidParam, "channel 只能是 admin / member / scheduler")
		return
	}
	status := strings.TrimSpace(c.Query("status"))
	if status != "" && !model.IsValidOplogStatus(status) {
		response.Fail(c, response.CodeInvalidParam, "status 只能是 success 或 failed")
		return
	}

	var actorID *uint64
	if raw := strings.TrimSpace(c.Query("actor_id")); raw != "" {
		value, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			response.Fail(c, response.CodeInvalidParam, "actor_id 必须为非负整数")
			return
		}
		actorID = &value
	}

	keyword := strings.TrimSpace(c.Query("keyword"))
	if keyword != "" {
		// 前置校验：单字符关键字无法被 ngram 索引命中，直接 40001（契约 23.5）。
		if _, err := store.OperationLogKeywordExpr(keyword); err != nil {
			response.Fail(c, response.CodeInvalidParam, err.Error())
			return
		}
	}

	start, ok := oplogTimeQuery(c, "start", false)
	if !ok {
		return
	}
	end, ok := oplogTimeQuery(c, "end", true)
	if !ok {
		return
	}
	if start != nil && end != nil && start.After(*end) {
		response.Fail(c, response.CodeValidationFailed, "start 不能晚于 end")
		return
	}

	items, total, err := h.store.ListOperationLogs(c.Request.Context(), store.OperationLogFilter{
		Channel:  channel,
		ActorID:  actorID,
		Status:   status,
		Keyword:  keyword,
		Start:    start,
		End:      end,
		Page:     page,
		PageSize: pageSize,
	})
	switch {
	case errors.Is(err, store.ErrOperationLogKeyword):
		response.Fail(c, response.CodeInvalidParam, err.Error())
		return
	case errors.Is(err, store.ErrOperationLogKeywordTimeout):
		// 命中过多导致关键字检索被 MySQL 中断（契约 23.5 第 5 条）：回**业务错误** 40002 +
		// 可读的收窄建议，而不是 500——请求本身没错，是命中集合太大没有参考价值。
		response.Fail(c, response.CodeValidationFailed, err.Error())
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	views := make([]operationLogView, 0, len(items))
	for i := range items {
		views = append(views, newOperationLogView(&items[i]))
	}
	response.Success(c, operationLogListView{
		Items:    views,
		Page:     page,
		PageSize: pageSize,
		Total:    total,
	})
}

// clearOperationLogs 处理 DELETE /api/v1/admin/operation-logs?channel=admin|member|scheduler|all。
//
// 语义（契约 23.6）：
//  1. 先冲出在途缓冲（否则刚入队未落库的条目会在清空后落回表里）；
//  2. 按通道清空走分批 DELETE（每批 1000，避免长事务锁表），清空全部走 TRUNCATE（自增 ID 归零）；
//  3. 清空动作**同步写一条留痕**（保证接口返回时已落库），写入**未被清空**的通道：
//     默认 admin，被清空范围包含 admin（清 admin 或清全部）时改写 scheduler；
//  4. 该请求本身不再由中间件重复入队（skipOplog），保证留痕只有一条。
//
// 权限：**仅 admin**（路由注册处挂 requireAdminRole(RoleAdmin)，finance / support 一律 403）。
func (h *operationLogHandler) clearOperationLogs(c *gin.Context) {
	admin, ok := adminFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}

	channel := strings.TrimSpace(c.Query("channel"))
	switch channel {
	case model.OplogChannelAdmin, model.OplogChannelMember, model.OplogChannelScheduler, oplogClearAll:
	default:
		response.Fail(c, response.CodeInvalidParam, "channel 必填，取值为 admin / member / scheduler / all")
		return
	}

	start := time.Now()
	ctx := c.Request.Context()

	flushCtx, cancelFlush := context.WithTimeout(ctx, oplogFlushTimeout)
	flushErr := h.writer.Flush(flushCtx)
	cancelFlush()
	if flushErr != nil {
		// 冲出缓冲超时不阻断清空（日志是尽力而为的留痕）：只记服务日志，继续删。
		h.logger.Warn("清空前冲出操作日志缓冲超时（在途条目可能晚于清空落库）", "error", flushErr)
	}

	var deleted int64
	var err error
	if channel == oplogClearAll {
		deleted, err = h.store.TruncateOperationLogs(ctx)
	} else {
		deleted, err = h.store.ClearOperationLogsByChannel(ctx, channel)
	}
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	// 留痕通道：默认 admin；被清空的范围包含 admin（含清空全部）时改写 scheduler。
	recordChannel := model.OplogChannelAdmin
	if channel == model.OplogChannelAdmin || channel == oplogClearAll {
		recordChannel = model.OplogChannelScheduler
	}
	summary := "清空通道 " + channel + "，删除 " + strconv.FormatInt(deleted, 10) + " 条日志"
	if channel == oplogClearAll {
		summary = "清空全部通道，删除 " + strconv.FormatInt(deleted, 10) + " 条日志（TRUNCATE，自增 ID 归零）"
	}
	if err := h.store.AppendOperationLogs(ctx, []store.OperationLogInput{{
		Channel:    recordChannel,
		ActorID:    admin.ID,
		ActorName:  admin.Username,
		Action:     "清空操作日志",
		Method:     c.Request.Method,
		Path:       c.FullPath(),
		HTTPStatus: 200,
		Status:     model.OplogStatusSuccess,
		IP:         c.ClientIP(),
		DurationMS: time.Since(start).Milliseconds(),
		Summary:    summary,
		CreatedAt:  time.Now().UTC(),
	}}); err != nil {
		// 留痕写入失败不回滚清空（清空已生效），只记服务日志并把事实返回给调用方。
		h.logger.Error("清空操作日志的留痕写入失败", "error", err, "channel", channel)
	}

	skipOplog(c)
	response.Success(c, clearOperationLogsView{
		Channel:       channel,
		Deleted:       deleted,
		RecordChannel: recordChannel,
	})
}

// newOperationLogView 组装列表项视图。
func newOperationLogView(log *model.OperationLog) operationLogView {
	return operationLogView{
		ID:         log.ID,
		Channel:    log.Channel,
		ActorID:    log.ActorID,
		ActorName:  log.ActorName,
		Action:     log.Action,
		Method:     log.Method,
		Path:       log.Path,
		HTTPStatus: log.HTTPStatus,
		Status:     log.Status,
		IP:         log.IP,
		DurationMS: log.DurationMS,
		Summary:    log.Summary,
		CreatedAt:  formatTime(log.CreatedAt),
	}
}

// oplogTimeQuery 解析时间范围参数：接受 RFC3339（2026-10-14T00:00:00Z）或纯日期（2026-10-14）；
// 纯日期按 UTC 当日解释，endOfDay 为 true 时（end 参数）取当日 23:59:59.999999999（含当天）。
// 非法时写出 40001 并返回 ok=false；参数缺席返回 (nil, true)。
func oplogTimeQuery(c *gin.Context, name string, endOfDay bool) (*time.Time, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return nil, true
	}
	if value, err := time.Parse(time.RFC3339, raw); err == nil {
		utc := value.UTC()
		return &utc, true
	}
	if value, err := time.Parse("2006-01-02", raw); err == nil {
		utc := value.UTC()
		if endOfDay {
			utc = utc.Add(24*time.Hour - time.Nanosecond)
		}
		return &utc, true
	}
	response.Fail(c, response.CodeInvalidParam,
		name+" 需为 RFC3339 时间（如 2026-10-14T00:00:00Z）或日期（如 2026-10-14）")
	return nil, false
}
