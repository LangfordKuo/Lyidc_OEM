package router

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/oplog"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/scheduler"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upstream"
)

// ---------------------------------------------------------------------------
// R18 操作日志（契约第 23 节）：中间件兜底埋点、异步写入、列表与清空接口。
// ---------------------------------------------------------------------------

// newOplogEngineWithWriter 构造带真实数据库 + 操作日志写入器的引擎（测试结束关闭写入器）。
func newOplogEngineWithWriter(t *testing.T, gdb *gorm.DB) (*gin.Engine, *oplog.Writer) {
	t.Helper()
	writer := oplog.NewWriter(oplog.Options{
		Store:         store.New(gdb),
		Logger:        silentLogger(),
		FlushInterval: 20 * time.Millisecond,
	})
	t.Cleanup(writer.Close)
	engine := New(Options{
		Logger: silentLogger(),
		DB:     gdb,
		JWT:    config.JWTConfig{Secret: testJWTSecret, ExpireHours: 168},
		Oplog:  writer,
	})
	return engine, writer
}

// newOplogEngine 是只关心引擎的便捷版本。
func newOplogEngine(t *testing.T, gdb *gorm.DB) *gin.Engine {
	t.Helper()
	engine, _ := newOplogEngineWithWriter(t, gdb)
	return engine
}

// mustFlushOplog 冲出写入器缓冲（断言前调用，消除异步窗口带来的不确定性）。
func mustFlushOplog(t *testing.T, writer *oplog.Writer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := writer.Flush(ctx); err != nil {
		t.Fatalf("冲出操作日志缓冲失败: %v", err)
	}
}

// oplogRows 读取指定通道的日志（id 升序 = 时间升序）。
func oplogRows(t *testing.T, gdb *gorm.DB, channel string) []model.OperationLog {
	t.Helper()
	rows := make([]model.OperationLog, 0)
	if err := gdb.Where("channel = ?", channel).Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("读取操作日志失败: %v", err)
	}
	return rows
}

// seedOperationLogs 直接写库造日志（确定性造数据，不经中间件）。
func seedOperationLogs(t *testing.T, gdb *gorm.DB, entries ...store.OperationLogInput) {
	t.Helper()
	if err := store.New(gdb).AppendOperationLogs(context.Background(), entries); err != nil {
		t.Fatalf("造操作日志失败: %v", err)
	}
}

// truncateOperationLogsForTest 清空日志表（把测试前置的登录留痕清掉，保证计数确定）。
func truncateOperationLogsForTest(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	if err := gdb.Exec("DELETE FROM operation_logs").Error; err != nil {
		t.Fatalf("清空操作日志失败: %v", err)
	}
}

// assertNoSecretInOperationLogs 断言全表任何字段都不包含给定敏感串（契约 23.7）。
func assertNoSecretInOperationLogs(t *testing.T, gdb *gorm.DB, secrets ...string) {
	t.Helper()
	rows := make([]model.OperationLog, 0)
	if err := gdb.Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("读取操作日志失败: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("操作日志表为空，脱敏断言失去意义")
	}
	for _, row := range rows {
		blob := row.ActorName + "\x00" + row.Action + "\x00" + row.Method + "\x00" + row.Path + "\x00" + row.Summary
		for _, secret := range secrets {
			if secret == "" {
				continue
			}
			if strings.Contains(blob, secret) {
				t.Fatalf("操作日志含敏感串 %q（日志 #%d: %+v）", secret, row.ID, row)
			}
		}
	}
}

// listOperationLogsRequest 请求列表接口并解码（query 需自带 "?" 或为空）。
func listOperationLogsRequest(t *testing.T, engine http.Handler, query, token string) operationLogListView {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/operation-logs"+query, token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("日志列表请求失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[operationLogListView](t, envelope)
}

// TestOperationLogCapturesAdminAndMemberWrites 验证：管理端/会员端的写请求都产生日志（含处理器补充的
// 动作名与摘要），GET 不产生。
func TestOperationLogCapturesAdminAndMemberWrites(t *testing.T) {
	gdb := testDatabase(t)
	engine, writer := newOplogEngineWithWriter(t, gdb)
	admin := seedAdmin(t, gdb, "logadmin", "admin-pass-123", model.RoleAdmin, model.StatusActive)
	adminToken, _ := loginAdmin(t, engine, "logadmin", "admin-pass-123")
	mustFlushOplog(t, writer)

	// 管理端写操作：修改站点对外地址（处理器补充摘要）。
	rec, _ := doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/site", adminToken,
		map[string]string{"url": "https://log.example.com"})
	if rec.Code != http.StatusOK {
		t.Fatalf("修改站点设置失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	// 读操作（GET）不产生日志。
	if rec, _ := doAPI(t, engine, http.MethodGet, "/api/v1/admin/profile", adminToken, nil); rec.Code != http.StatusOK {
		t.Fatalf("读取管理员资料失败: HTTP %d", rec.Code)
	}
	mustFlushOplog(t, writer)

	rows := oplogRows(t, gdb, model.OplogChannelAdmin)
	if len(rows) != 2 {
		t.Fatalf("管理员通道日志条数 = %d，期望 2（登录 + 改设置；GET 不记）: %+v", len(rows), rows)
	}
	login, site := rows[0], rows[1]
	if login.Action != "管理员登录" || login.ActorID != admin.ID || login.ActorName != "logadmin" ||
		login.Status != model.OplogStatusSuccess || login.HTTPStatus != 200 {
		t.Fatalf("登录日志异常: %+v", login)
	}
	if site.Action != "修改站点地址设置" || site.Method != http.MethodPut ||
		site.Path != "/api/v1/admin/settings/site" || site.ActorID != admin.ID || site.ActorName != "logadmin" ||
		site.Status != model.OplogStatusSuccess || site.HTTPStatus != 200 {
		t.Fatalf("改设置日志异常: %+v", site)
	}
	if !strings.Contains(site.Summary, "log.example.com") {
		t.Fatalf("改设置日志摘要未带业务补充: %q", site.Summary)
	}
	if site.IP == "" {
		t.Fatalf("日志缺少客户端 IP: %+v", site)
	}

	// 会员端：注册 / 登录 / 改资料都是写操作，各自产生一条；GET /members/me 不产生。
	registerMember(t, engine, "logmember", "logmember@example.com", "member-pass-123")
	memberToken, member := loginMember(t, engine, "logmember", "member-pass-123")
	rec, _ = doAPI(t, engine, http.MethodPut, "/api/v1/members/me", memberToken, map[string]string{"nickname": "日志会员"})
	if rec.Code != http.StatusOK {
		t.Fatalf("更新会员资料失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if rec, _ := doAPI(t, engine, http.MethodGet, "/api/v1/members/me", memberToken, nil); rec.Code != http.StatusOK {
		t.Fatalf("读取会员资料失败: HTTP %d", rec.Code)
	}
	mustFlushOplog(t, writer)

	memberRows := oplogRows(t, gdb, model.OplogChannelMember)
	if len(memberRows) != 3 {
		t.Fatalf("会员通道日志条数 = %d，期望 3（注册 + 登录 + 改资料）: %+v", len(memberRows), memberRows)
	}
	if memberRows[0].Action != "会员注册" || memberRows[0].ActorName != "logmember" ||
		memberRows[0].ActorID != member.ID || !strings.Contains(memberRows[0].Summary, "注册成功") {
		t.Fatalf("注册日志异常: %+v", memberRows[0])
	}
	if memberRows[1].Action != "会员登录" || memberRows[1].ActorID != member.ID {
		t.Fatalf("登录日志异常: %+v", memberRows[1])
	}
	if memberRows[2].Action != "修改会员资料" || !strings.Contains(memberRows[2].Summary, "nickname") {
		t.Fatalf("改资料日志异常: %+v", memberRows[2])
	}
}

// TestOperationLogCapturesRejectedWrites 验证：未登录（401）与角色不足（403）的写请求同样留痕；
// 未注册路径（404）与未注册方法（405）不经过记录中间件，因此不产生日志（也不会被扫描器刷表）。
func TestOperationLogCapturesRejectedWrites(t *testing.T) {
	gdb := testDatabase(t)
	engine, writer := newOplogEngineWithWriter(t, gdb)
	seedAdmin(t, gdb, "logadmin", "admin-pass-123", model.RoleAdmin, model.StatusActive)
	adminToken, _ := loginAdmin(t, engine, "logadmin", "admin-pass-123")

	finance := seedAdmin(t, gdb, "logfinance", "finance-pass-123", model.RoleFinance, model.StatusActive)
	financeToken, _ := loginAdmin(t, engine, "logfinance", "finance-pass-123")

	// 未登录写管理端设置：401，仍留痕（操作人为 0/空串）。
	rec, _ := doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/site", "",
		map[string]string{"url": "https://anon.example.com"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录写请求 = %d，期望 401", rec.Code)
	}
	// finance 角色写仅 admin 的接口：403，仍留痕（操作人 = finance 账号）。
	rec, _ = doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/site", financeToken,
		map[string]string{"url": "https://finance.example.com"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("finance 写请求 = %d，期望 403", rec.Code)
	}
	// 未注册路径的 POST：404，不经过路由组中间件 → 不记。
	rec, _ = doAPI(t, engine, http.MethodPost, "/api/v1/admin/no-such-endpoint", adminToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("未注册路径 = %d，期望 404", rec.Code)
	}
	// 已注册路径 + 未注册方法：gin 的 NoMethod 分发同样不经过路由组中间件（命中 NoRoute 兜底 404）。
	rec, _ = doAPI(t, engine, http.MethodPatch, "/api/v1/admin/operation-logs", adminToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("未注册方法 = %d，期望 404（与既有 TestMethodNotAllowedReturnsEnvelope 同口径）", rec.Code)
	}
	mustFlushOplog(t, writer)

	rows := oplogRows(t, gdb, model.OplogChannelAdmin)
	if len(rows) != 4 {
		t.Fatalf("管理员通道日志条数 = %d，期望 4（两次登录 + 401 + 403；404/405 不记）: %+v", len(rows), rows)
	}
	unauthorized, forbidden := rows[2], rows[3]
	if unauthorized.HTTPStatus != 401 || unauthorized.Status != model.OplogStatusFailed ||
		unauthorized.ActorID != 0 || unauthorized.ActorName != "" ||
		unauthorized.Action != "修改站点地址设置" {
		t.Fatalf("401 留痕异常: %+v", unauthorized)
	}
	if forbidden.HTTPStatus != 403 || forbidden.Status != model.OplogStatusFailed ||
		forbidden.ActorID != finance.ID || forbidden.ActorName != "logfinance" {
		t.Fatalf("403 留痕异常: %+v", forbidden)
	}
}

// TestOperationLogLoginRecordsAttemptedUsernameOnly 验证：管理员登录失败只记尝试的用户名（不记密码），
// 成功则记账号本身。
func TestOperationLogLoginRecordsAttemptedUsernameOnly(t *testing.T) {
	gdb := testDatabase(t)
	engine, writer := newOplogEngineWithWriter(t, gdb)
	admin := seedAdmin(t, gdb, "loguser", "correct-pass-123", model.RoleAdmin, model.StatusActive)

	doAPI(t, engine, http.MethodPost, "/api/v1/admin/auth/login", "", map[string]string{
		"username": "loguser", "password": "wrong-secret-pass",
	})
	doAPI(t, engine, http.MethodPost, "/api/v1/admin/auth/login", "", map[string]string{
		"username": "nobody", "password": "another-secret-pass",
	})
	loginAdmin(t, engine, "loguser", "correct-pass-123")
	mustFlushOplog(t, writer)

	rows := oplogRows(t, gdb, model.OplogChannelAdmin)
	if len(rows) != 3 {
		t.Fatalf("管理员通道日志条数 = %d，期望 3: %+v", len(rows), rows)
	}
	if rows[0].Action != "管理员登录失败" || rows[0].ActorName != "loguser" || rows[0].ActorID != 0 ||
		rows[0].Status != model.OplogStatusFailed || rows[0].HTTPStatus != 401 ||
		rows[0].Summary != "用户名或密码错误" {
		t.Fatalf("失败登录留痕异常: %+v", rows[0])
	}
	if rows[1].Action != "管理员登录失败" || rows[1].ActorName != "nobody" {
		t.Fatalf("不存在账号的失败登录留痕异常: %+v", rows[1])
	}
	if rows[2].Action != "管理员登录" || rows[2].ActorID != admin.ID ||
		rows[2].ActorName != "loguser" || rows[2].Status != model.OplogStatusSuccess {
		t.Fatalf("成功登录留痕异常: %+v", rows[2])
	}
	assertNoSecretInOperationLogs(t, gdb, "wrong-secret-pass", "another-secret-pass", "correct-pass-123")
}

// TestOperationLogListFiltersAndSearch 验证列表的分页、时间范围、通道/结果/操作人过滤与关键字搜索
// （含中文关键字与多词 AND）。
func TestOperationLogListFiltersAndSearch(t *testing.T) {
	gdb := testDatabase(t)
	engine, writer := newOplogEngineWithWriter(t, gdb)
	seedAdmin(t, gdb, "logadmin", "admin-pass-123", model.RoleAdmin, model.StatusActive)
	adminToken, _ := loginAdmin(t, engine, "logadmin", "admin-pass-123")
	mustFlushOplog(t, writer)
	truncateOperationLogsForTest(t, gdb)

	base := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	paged := make([]store.OperationLogInput, 0, 25)
	for i := 0; i < 25; i++ {
		paged = append(paged, store.OperationLogInput{
			Channel: model.OplogChannelMember, ActorID: 42, ActorName: "member42",
			Action: "会员动作", Status: model.OplogStatusSuccess, HTTPStatus: 200,
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
	seedOperationLogs(t, gdb, paged...)
	seedOperationLogs(t, gdb,
		store.OperationLogInput{
			Channel: model.OplogChannelAdmin, ActorID: 201, ActorName: "admin201",
			Action: "修改上游设置", Summary: "已保存上游凭据设置（账号=13900000000）",
			Status: model.OplogStatusSuccess, HTTPStatus: 200, CreatedAt: base,
		},
		store.OperationLogInput{
			Channel: model.OplogChannelAdmin, ActorID: 201, ActorName: "admin201",
			Action: "创建数据库备份", Summary: "已创建数据库备份 lyidc-backup-20261014.sql",
			Status: model.OplogStatusSuccess, HTTPStatus: 200, CreatedAt: base.Add(time.Hour),
		},
		store.OperationLogInput{
			Channel: model.OplogChannelAdmin, ActorID: 202, ActorName: "ops202",
			Action: "清空操作日志", Summary: "清空通道 member，删除 12 条日志",
			Status: model.OplogStatusFailed, HTTPStatus: 500, CreatedAt: base.Add(2 * time.Hour),
		},
		store.OperationLogInput{
			Channel: model.OplogChannelAdmin, ActorID: 202, ActorName: "ops202",
			Action: "回复工单", Summary: "已公开回复工单 T20261014000000",
			Status: model.OplogStatusSuccess, HTTPStatus: 200, CreatedAt: base.Add(3 * time.Hour),
		},
	)

	// 分页：新记录在前，缺省 page_size 是 20、上限 100。
	view := listOperationLogsRequest(t, engine, "?channel=member&page=1&page_size=10", adminToken)
	if view.Total != 25 || len(view.Items) != 10 || view.Page != 1 || view.PageSize != 10 {
		t.Fatalf("分页结果异常: total=%d items=%d page=%d size=%d", view.Total, len(view.Items), view.Page, view.PageSize)
	}
	if view.Items[0].CreatedAt != "2026-10-14T08:24:00Z" || view.Items[9].CreatedAt != "2026-10-14T08:15:00Z" {
		t.Fatalf("新记录在前口径不符: first=%s last=%s", view.Items[0].CreatedAt, view.Items[9].CreatedAt)
	}
	view = listOperationLogsRequest(t, engine, "?channel=member&page=3&page_size=10", adminToken)
	if len(view.Items) != 5 || view.Total != 25 {
		t.Fatalf("末页结果异常: items=%d total=%d", len(view.Items), view.Total)
	}
	// 无 channel：三个通道一起数（此处只有 member 与 admin）。
	view = listOperationLogsRequest(t, engine, "?page_size=100", adminToken)
	if view.Total != 29 {
		t.Fatalf("不带通道的 total = %d，期望 29", view.Total)
	}

	// 操作人过滤。
	view = listOperationLogsRequest(t, engine, "?channel=admin&actor_id=202", adminToken)
	if view.Total != 2 || view.Items[0].ActorName != "ops202" {
		t.Fatalf("actor_id 过滤异常: total=%d items=%+v", view.Total, view.Items)
	}
	// 结果过滤。
	view = listOperationLogsRequest(t, engine, "?channel=admin&status=failed", adminToken)
	if view.Total != 1 || view.Items[0].Action != "清空操作日志" {
		t.Fatalf("status 过滤异常: total=%d items=%+v", view.Total, view.Items)
	}
	// 时间范围（闭区间，RFC3339）。
	view = listOperationLogsRequest(t, engine,
		"?channel=admin&start=2026-10-14T09:00:00Z&end=2026-10-14T09:30:00Z", adminToken)
	if view.Total != 1 || view.Items[0].Action != "创建数据库备份" {
		t.Fatalf("时间范围过滤异常: total=%d items=%+v", view.Total, view.Items)
	}
	// 纯日期形式：end 按当日 23:59:59 解释（含当天）。
	view = listOperationLogsRequest(t, engine, "?channel=admin&start=2026-10-14&end=2026-10-14", adminToken)
	if view.Total != 4 {
		t.Fatalf("纯日期时间窗 total = %d，期望 4", view.Total)
	}
	// 关键字：中文短语命中摘要。
	view = listOperationLogsRequest(t, engine, "?channel=admin&keyword=上游凭据", adminToken)
	if view.Total != 1 || view.Items[0].Action != "修改上游设置" {
		t.Fatalf("中文关键字命中异常: total=%d items=%+v", view.Total, view.Items)
	}
	// 关键字：命中动作名（FULLTEXT 覆盖 action）。
	view = listOperationLogsRequest(t, engine, "?channel=admin&keyword=备份", adminToken)
	if view.Total != 1 || view.Items[0].Action != "创建数据库备份" {
		t.Fatalf("动作名关键字命中异常: total=%d items=%+v", view.Total, view.Items)
	}
	// 关键字：多词 AND（两个词都要命中）。
	view = listOperationLogsRequest(t, engine, "?channel=admin&keyword=备份%20lyidc", adminToken)
	if view.Total != 1 {
		t.Fatalf("多词 AND 命中异常: total=%d", view.Total)
	}
	view = listOperationLogsRequest(t, engine, "?channel=admin&keyword=备份%20工单", adminToken)
	if view.Total != 0 {
		t.Fatalf("多词 AND 不应命中: total=%d", view.Total)
	}
	// 关键字与其它过滤条件叠加。
	view = listOperationLogsRequest(t, engine, "?channel=admin&status=failed&keyword=清空操作日志", adminToken)
	if view.Total != 1 {
		t.Fatalf("关键字 + 状态叠加异常: total=%d", view.Total)
	}

	// 非法参数：一律 40001；start 晚于 end 是 40002。
	badQueries := []string{
		"?channel=xxx",
		"?status=ok",
		"?actor_id=abc",
		"?start=not-a-time",
		"?keyword=备", // 单字符无法被 ngram 命中
		"?page_size=101",
	}
	for _, query := range badQueries {
		rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/operation-logs"+query, adminToken, nil)
		if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
			t.Fatalf("非法查询 %s = HTTP %d/code %d，期望 400/40001", query, rec.Code, envelope.Code)
		}
	}
	rec, envelope := doAPI(t, engine, http.MethodGet,
		"/api/v1/admin/operation-logs?start=2026-10-15T00:00:00Z&end=2026-10-14T00:00:00Z", adminToken, nil)
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("start>end = HTTP %d/code %d，期望 400/40002", rec.Code, envelope.Code)
	}
}

// TestOperationLogClearByChannelAllAndAudit 验证清空语义：按通道清空只清该通道、清空全部走 TRUNCATE，
// 且清空动作本身留痕在**未被清空**的通道（admin 被清空时改写 scheduler）。
func TestOperationLogClearByChannelAllAndAudit(t *testing.T) {
	gdb := testDatabase(t)
	engine, writer := newOplogEngineWithWriter(t, gdb)
	admin := seedAdmin(t, gdb, "logadmin", "admin-pass-123", model.RoleAdmin, model.StatusActive)
	adminToken, _ := loginAdmin(t, engine, "logadmin", "admin-pass-123")
	mustFlushOplog(t, writer)

	seedOperationLogs(t, gdb,
		store.OperationLogInput{Channel: model.OplogChannelAdmin, ActorID: 1, Action: "第一", CreatedAt: time.Now().UTC()},
		store.OperationLogInput{Channel: model.OplogChannelAdmin, ActorID: 1, Action: "第二", CreatedAt: time.Now().UTC()},
		store.OperationLogInput{Channel: model.OplogChannelMember, ActorID: 2, Action: "会员一", CreatedAt: time.Now().UTC()},
		store.OperationLogInput{Channel: model.OplogChannelMember, ActorID: 2, Action: "会员二", CreatedAt: time.Now().UTC()},
		store.OperationLogInput{Channel: model.OplogChannelMember, ActorID: 2, Action: "会员三", CreatedAt: time.Now().UTC()},
		store.OperationLogInput{Channel: model.OplogChannelScheduler, Action: "定时任务一", CreatedAt: time.Now().UTC()},
	)

	clear := func(channel string) clearOperationLogsView {
		t.Helper()
		rec, envelope := doAPI(t, engine, http.MethodDelete,
			"/api/v1/admin/operation-logs?channel="+channel, adminToken, nil)
		if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
			t.Fatalf("清空 %s 失败: HTTP %d, body=%s", channel, rec.Code, rec.Body.String())
		}
		return decodeData[clearOperationLogsView](t, envelope)
	}

	// 1) 按通道清空会员通道：只清会员，留痕写进管理员通道。
	result := clear(model.OplogChannelMember)
	if result.Channel != model.OplogChannelMember || result.Deleted != 3 ||
		result.RecordChannel != model.OplogChannelAdmin {
		t.Fatalf("清空会员通道结果异常: %+v", result)
	}
	if rows := oplogRows(t, gdb, model.OplogChannelMember); len(rows) != 0 {
		t.Fatalf("会员通道清空后仍有日志: %+v", rows)
	}
	adminRows := oplogRows(t, gdb, model.OplogChannelAdmin)
	if len(adminRows) != 4 {
		t.Fatalf("管理员通道日志条数 = %d，期望 4（登录 + 种子 2 条 + 清空留痕）: %+v", len(adminRows), adminRows)
	}
	audit := adminRows[3]
	if audit.Action != "清空操作日志" || audit.ActorID != admin.ID || audit.ActorName != "logadmin" ||
		!strings.Contains(audit.Summary, "member") || !strings.Contains(audit.Summary, "3") {
		t.Fatalf("清空留痕异常: %+v", audit)
	}

	// 2) 清空管理员通道（留痕写进 scheduler，管理员通道必须真的为空）。
	result = clear(model.OplogChannelAdmin)
	if result.Deleted != 4 || result.RecordChannel != model.OplogChannelScheduler {
		t.Fatalf("清空管理员通道结果异常: %+v", result)
	}
	if rows := oplogRows(t, gdb, model.OplogChannelAdmin); len(rows) != 0 {
		t.Fatalf("管理员通道清空后仍有日志: %+v", rows)
	}
	if rows := oplogRows(t, gdb, model.OplogChannelScheduler); len(rows) != 2 {
		t.Fatalf("定时任务通道日志条数 = %d，期望 2（种子 1 条 + 清空留痕）: %+v", len(rows), rows)
	}

	// 3) 按通道清空定时任务通道：留痕回到管理员通道。
	result = clear(model.OplogChannelScheduler)
	if result.Deleted != 2 || result.RecordChannel != model.OplogChannelAdmin {
		t.Fatalf("清空定时任务通道结果异常: %+v", result)
	}
	if rows := oplogRows(t, gdb, model.OplogChannelScheduler); len(rows) != 0 {
		t.Fatalf("定时任务通道清空后仍有日志: %+v", rows)
	}

	// 4) 清空全部：TRUNCATE 掉三通道，只保留一条清空留痕（scheduler，因为范围内含 admin）。
	result = clear(oplogClearAll)
	if result.Deleted != 1 || result.RecordChannel != model.OplogChannelScheduler {
		t.Fatalf("清空全部结果异常: %+v", result)
	}
	if rows := oplogRows(t, gdb, model.OplogChannelAdmin); len(rows) != 0 {
		t.Fatalf("清空全部后管理员通道仍有日志: %+v", rows)
	}
	if rows := oplogRows(t, gdb, model.OplogChannelMember); len(rows) != 0 {
		t.Fatalf("清空全部后会员通道仍有日志: %+v", rows)
	}
	schedulerRows := oplogRows(t, gdb, model.OplogChannelScheduler)
	if len(schedulerRows) != 1 || schedulerRows[0].Action != "清空操作日志" ||
		!strings.Contains(schedulerRows[0].Summary, "全部通道") {
		t.Fatalf("清空全部后的唯一留痕异常: %+v", schedulerRows)
	}

	// 5) channel 必填且取值受限。
	rec, envelope := doAPI(t, engine, http.MethodDelete, "/api/v1/admin/operation-logs", adminToken, nil)
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("缺 channel 的清空 = HTTP %d/code %d，期望 400/40001", rec.Code, envelope.Code)
	}
	rec, envelope = doAPI(t, engine, http.MethodDelete, "/api/v1/admin/operation-logs?channel=member2", adminToken, nil)
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("非法 channel 的清空 = HTTP %d/code %d，期望 400/40001", rec.Code, envelope.Code)
	}
}

// TestOperationLogNoPerEntryRoutes 验证：日志只允许清空，**不存在**按 id 删除/修改/批量删选的路由
// （契约 23.4 硬约束）。
func TestOperationLogNoPerEntryRoutes(t *testing.T) {
	gdb := testDatabase(t)
	engine, writer := newOplogEngineWithWriter(t, gdb)
	seedAdmin(t, gdb, "logadmin", "admin-pass-123", model.RoleAdmin, model.StatusActive)
	adminToken, _ := loginAdmin(t, engine, "logadmin", "admin-pass-123")
	mustFlushOplog(t, writer)
	seedOperationLogs(t, gdb, store.OperationLogInput{
		Channel: model.OplogChannelAdmin, ActorID: 1, Action: "不应被删", CreatedAt: time.Now().UTC(),
	})

	attempts := []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodDelete, "/api/v1/admin/operation-logs/1", http.StatusNotFound},
		{http.MethodPut, "/api/v1/admin/operation-logs/1", http.StatusNotFound},
		{http.MethodPatch, "/api/v1/admin/operation-logs/1", http.StatusNotFound},
		{http.MethodPost, "/api/v1/admin/operation-logs/1", http.StatusNotFound},
		{http.MethodDelete, "/api/v1/admin/operation-logs/1/delete", http.StatusNotFound},
		{http.MethodDelete, "/api/v1/admin/operation-logs/batch", http.StatusNotFound},
		// 未注册方法同样落 NoRoute 兜底（gin 在本项目里的既有口径，见 TestMethodNotAllowedReturnsEnvelope）。
		{http.MethodPost, "/api/v1/admin/operation-logs", http.StatusNotFound},
		{http.MethodPut, "/api/v1/admin/operation-logs", http.StatusNotFound},
		{http.MethodPatch, "/api/v1/admin/operation-logs", http.StatusNotFound},
	}
	for _, attempt := range attempts {
		rec, _ := doAPI(t, engine, attempt.method, attempt.path, adminToken, nil)
		if rec.Code != attempt.want {
			t.Fatalf("%s %s = HTTP %d，期望 %d", attempt.method, attempt.path, rec.Code, attempt.want)
		}
	}

	// 任何一次尝试都没有删掉日志。
	rows := oplogRows(t, gdb, model.OplogChannelAdmin)
	if len(rows) != 2 {
		t.Fatalf("日志条数 = %d，期望 2（登录 + 种子，均未被删除）: %+v", len(rows), rows)
	}
	found := false
	for _, row := range rows {
		if row.Action == "不应被删" {
			found = true
		}
	}
	if !found {
		t.Fatal("种子日志被意外删除")
	}
}

// TestOperationLogPermissions 验证读与清空都仅 admin（finance / support 403，会员 401，匿名 401）。
func TestOperationLogPermissions(t *testing.T) {
	gdb := testDatabase(t)
	engine := newOplogEngine(t, gdb)
	seedAdmin(t, gdb, "logadmin", "admin-pass-123", model.RoleAdmin, model.StatusActive)
	seedAdmin(t, gdb, "logfinance", "finance-pass-123", model.RoleFinance, model.StatusActive)
	seedAdmin(t, gdb, "logsupport", "support-pass-123", model.RoleSupport, model.StatusActive)
	adminToken, _ := loginAdmin(t, engine, "logadmin", "admin-pass-123")
	financeToken, _ := loginAdmin(t, engine, "logfinance", "finance-pass-123")
	supportToken, _ := loginAdmin(t, engine, "logsupport", "support-pass-123")
	memberToken, _ := memberTokenFor(t, engine, "logmember")

	cases := []struct {
		name   string
		method string
		token  string
		want   int
	}{
		{"admin 可读", http.MethodGet, adminToken, http.StatusOK},
		{"finance 不可读", http.MethodGet, financeToken, http.StatusForbidden},
		{"support 不可读", http.MethodGet, supportToken, http.StatusForbidden},
		{"会员不可读", http.MethodGet, memberToken, http.StatusUnauthorized},
		{"匿名不可读", http.MethodGet, "", http.StatusUnauthorized},
		{"finance 不可清空", http.MethodDelete, financeToken, http.StatusForbidden},
		{"support 不可清空", http.MethodDelete, supportToken, http.StatusForbidden},
		{"会员不可清空", http.MethodDelete, memberToken, http.StatusUnauthorized},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			path := "/api/v1/admin/operation-logs"
			if tt.method == http.MethodDelete {
				path += "?channel=member"
			}
			rec, _ := doAPI(t, engine, tt.method, path, tt.token, nil)
			if rec.Code != tt.want {
				t.Fatalf("%s %s = HTTP %d，期望 %d（body=%s）", tt.method, path, rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}

// TestOperationLogSanitizationDropsSecrets 验证：带密码/密钥的请求落库后，全表任何字段都不含这些值
// （请求体整体不落库 + 摘要脱敏）。
func TestOperationLogSanitizationDropsSecrets(t *testing.T) {
	gdb := testDatabase(t)
	engine, writer := newOplogEngineWithWriter(t, gdb)
	seedAdmin(t, gdb, "logadmin", "admin-pass-123", model.RoleAdmin, model.StatusActive)
	adminToken, _ := loginAdmin(t, engine, "logadmin", "admin-pass-123")

	// 管理端：SMTP 口令与易支付密钥都在请求体里。
	doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/email/smtp", adminToken, map[string]any{
		"enabled": true, "host": "smtp.example.com", "port": 587, "username": "mailer",
		"password": "smtp-secret-987", "from": "oem@example.com", "from_name": "OEM",
		"encryption": "starttls",
	})
	doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/payment/epay", adminToken, map[string]any{
		"enabled": true, "gateway": "https://pay.example.com", "pid": "1001",
		"key": "epay-secret-key-123", "notify_url": "https://oem.example.com/api/v1/payments/epay/notify",
	})
	// 会员端：注册密码、登录密码、改密的新旧密码。
	registerMember(t, engine, "secmember", "secmember@example.com", "member-secret-pw-1")
	memberToken, _ := loginMember(t, engine, "secmember", "member-secret-pw-1")
	doAPI(t, engine, http.MethodPost, "/api/v1/members/me/password", memberToken, map[string]string{
		"old_password": "member-secret-pw-1", "new_password": "member-secret-pw-2",
	})
	mustFlushOplog(t, writer)

	assertNoSecretInOperationLogs(t, gdb,
		"smtp-secret-987", "epay-secret-key-123", "member-secret-pw-1", "member-secret-pw-2", "admin-pass-123")

	// 留痕本身仍然可读：管理员两条设置留痕带 host/开关，会员改密留痕只说明动作。
	adminRows := oplogRows(t, gdb, model.OplogChannelAdmin)
	blob := ""
	for _, row := range adminRows {
		blob += row.Action + "|" + row.Summary + "\n"
	}
	if !strings.Contains(blob, "SMTP") || !strings.Contains(blob, "smtp.example.com") {
		t.Fatalf("SMTP 留痕缺少可读信息: %s", blob)
	}
	memberRows := oplogRows(t, gdb, model.OplogChannelMember)
	found := false
	for _, row := range memberRows {
		if row.Action == "修改会员密码" && row.Status == model.OplogStatusSuccess {
			found = true
		}
	}
	if !found {
		t.Fatalf("会员改密留痕缺失: %+v", memberRows)
	}
}

// TestOperationLogWriteDoesNotBlockRequests 验证：落库被卡住时业务请求照常返回
// （写日志不阻塞请求，契约 23.6）。
func TestOperationLogWriteDoesNotBlockRequests(t *testing.T) {
	gdb := testDatabase(t)
	seedAdmin(t, gdb, "logadmin", "admin-pass-123", model.RoleAdmin, model.StatusActive)

	release := make(chan struct{})
	blocked := make(chan struct{})
	var releaseOnce, blockedOnce sync.Once
	writer := oplog.NewWriter(oplog.Options{
		Logger:    silentLogger(),
		BatchSize: 1, // 每条即触发落库，让「卡住落库」在请求返回前必定发生
		Insert: func(context.Context, []store.OperationLogInput) error {
			blockedOnce.Do(func() { close(blocked) })
			<-release
			return nil
		},
	})
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		writer.Close()
	})

	engine := New(Options{
		Logger: silentLogger(),
		DB:     gdb,
		JWT:    config.JWTConfig{Secret: testJWTSecret, ExpireHours: 168},
		Oplog:  writer,
	})
	adminToken, _ := loginAdmin(t, engine, "logadmin", "admin-pass-123")

	start := time.Now()
	rec, _ := doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/site", adminToken,
		map[string]string{"url": "https://blocked.example.com"})
	elapsed := time.Since(start)
	if rec.Code != http.StatusOK {
		t.Fatalf("写请求 = HTTP %d，期望 200", rec.Code)
	}
	if elapsed > time.Second {
		t.Fatalf("写请求耗时 %v：日志落库卡住时不应阻塞业务", elapsed)
	}
	select {
	case <-blocked:
	default:
		t.Fatal("后台落库未被触发（测试前置失效）")
	}
}

// TestOperationLogSchedulerRoundFailure 验证：定时任务每轮都留一条汇总（任务名/结果/耗时/错误）。
func TestOperationLogSchedulerRoundFailure(t *testing.T) {
	gdb := testDatabase(t)
	writer := oplog.NewWriter(oplog.Options{
		Store:         store.New(gdb),
		Logger:        silentLogger(),
		FlushInterval: 20 * time.Millisecond,
	})
	t.Cleanup(writer.Close)

	now := time.Now().UTC()
	scanner := scheduler.New(store.New(gdb), upstream.StaticProvider{C: nil}, scheduler.Options{
		Clock:  func() time.Time { return now },
		Logger: silentLogger(),
		Oplog:  writer,
	})
	if _, err := scanner.ScanOnce(context.Background()); err == nil {
		t.Fatal("上游未配置时扫描应返回错误")
	}
	mustFlushOplog(t, writer)

	rows := oplogRows(t, gdb, model.OplogChannelScheduler)
	if len(rows) != 1 {
		t.Fatalf("定时任务通道日志条数 = %d，期望 1: %+v", len(rows), rows)
	}
	round := rows[0]
	if round.Action != "定时任务：到期扫描" || round.Status != model.OplogStatusFailed ||
		!strings.Contains(round.Summary, "错误：") || round.Method != "" || round.Path != "" ||
		round.ActorID != 0 || round.ActorName != "" {
		t.Fatalf("扫描失败留痕异常: %+v", round)
	}
}

// TestOperationLogSchedulerRoundAndSuspendDetail 验证：定时任务在有实际动作时另写明细
// （到期自动暂停），且汇总为成功。
func TestOperationLogSchedulerRoundAndSuspendDetail(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, _ := memberTokenFor(t, engine, "oplogscan")
	instance := newInstanceFor(t, engine, gateway, gdb, token, product.ID)
	now := time.Now().UTC()
	setInstanceDue(t, gdb, instance.ID, now.Add(-time.Hour))

	writer := oplog.NewWriter(oplog.Options{
		Store:         store.New(gdb),
		Logger:        silentLogger(),
		FlushInterval: 20 * time.Millisecond,
	})
	t.Cleanup(writer.Close)
	scanner := scheduler.New(store.New(gdb), upstream.StaticProvider{C: host.client(t)}, scheduler.Options{
		Clock:  func() time.Time { return now },
		Logger: silentLogger(),
		Oplog:  writer,
	})
	report, err := scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	if report.Suspended != 1 {
		t.Fatalf("扫描统计 = %+v，期望 suspended=1", report)
	}
	mustFlushOplog(t, writer)

	rows := oplogRows(t, gdb, model.OplogChannelScheduler)
	if len(rows) != 2 {
		t.Fatalf("定时任务通道日志条数 = %d，期望 2（暂停明细 + 扫描汇总）: %+v", len(rows), rows)
	}
	// 明细在扫描过程中写入、汇总在整轮结束后写入，因此明细的 id 更小。
	detail, round := rows[0], rows[1]
	if round.Action != "定时任务：到期扫描" || round.Status != model.OplogStatusSuccess ||
		!strings.Contains(round.Summary, "暂停 1") {
		t.Fatalf("扫描汇总留痕异常: %+v", round)
	}
	if detail.Action != "到期自动暂停" || detail.Status != model.OplogStatusSuccess ||
		!strings.Contains(detail.Summary, "实例 #"+itoa(instance.ID)) {
		t.Fatalf("暂停明细留痕异常: %+v", detail)
	}
}
