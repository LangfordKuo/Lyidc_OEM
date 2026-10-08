package router

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
)

// ---------------------------------------------------------------------------
// 阶段 6a：工单系统（会员提单 → 管理员/客服处理 → 关闭；内部备注不泄漏；权限矩阵；限流）
// ---------------------------------------------------------------------------

const (
	ticketsPath       = "/api/v1/tickets"
	adminTicketsPath  = "/api/v1/admin/tickets"
	testTicketSubject = "主机无法连接，请协助排查"
	testTicketContent = "从今天早上开始 SSH 就一直连不上，麻烦帮忙看看。"
)

// newTicketEngine 构造工单用例引擎：工单是纯本地域（不需要上游与交付注入）。
func newTicketEngine(t *testing.T, gdb *gorm.DB) *gin.Engine {
	t.Helper()
	return New(Options{
		Logger: silentLogger(),
		DB:     gdb,
		JWT:    config.JWTConfig{Secret: testJWTSecret, ExpireHours: 168},
	})
}

// seedTicketInstance 直接写库造一台实例（工单只把实例当引用，不需要走开通链路）。
func seedTicketInstance(t *testing.T, gdb *gorm.DB, memberID uint64, seq int, status string) *model.Instance {
	t.Helper()
	now := time.Now().UTC()
	instance := model.Instance{
		MemberID:     memberID,
		OrderID:      uint64(900000 + seq),
		HostID:       20000 + seq,
		ProductID:    1,
		ProductName:  "工单测试商品",
		Name:         "oem-ticket-" + strconv.Itoa(seq),
		BillingCycle: "monthly",
		Status:       status,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := gdb.Create(&instance).Error; err != nil {
		t.Fatalf("写入测试实例失败: %v", err)
	}
	return &instance
}

// createTicket 调用提单接口并断言成功，返回响应视图。
func createTicket(t *testing.T, engine http.Handler, token string, body map[string]any) ticketReplyView {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodPost, ticketsPath, token, body)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("提单失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[ticketReplyView](t, envelope)
}

// getTicketDetail 调用会员端详情接口并断言成功。
func getTicketDetail(t *testing.T, engine http.Handler, token string, ticketID uint64) ticketDetailView {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodGet, ticketsPath+"/"+itoa(ticketID), token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("工单详情失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[ticketDetailView](t, envelope)
}

// adminTicketDetail 调用管理端详情接口并断言成功。
func adminTicketDetail(t *testing.T, engine http.Handler, token string, ticketID uint64) adminTicketDetailView {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodGet, adminTicketsPath+"/"+itoa(ticketID), token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("管理端工单详情失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[adminTicketDetailView](t, envelope)
}

// adminReplyTicket 调用管理端回复接口并断言成功。
func adminReplyTicket(t *testing.T, engine http.Handler, token string, ticketID uint64, body map[string]any) adminTicketReplyView {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodPost,
		adminTicketsPath+"/"+itoa(ticketID)+"/reply", token, body)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("管理端回复失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[adminTicketReplyView](t, envelope)
}

// ticketTradeNoTime 解析工单号里的 UTC 时间部分（T + yyyyMMddHHmmss + 6 位随机）。
func ticketTradeNoTime(t *testing.T, tradeNo string) time.Time {
	t.Helper()
	if len(tradeNo) != 1+14+6 {
		t.Fatalf("工单号长度 = %d（%q），期望 21", len(tradeNo), tradeNo)
	}
	at, err := time.ParseInLocation("20060102150405", tradeNo[1:15], time.UTC)
	if err != nil {
		t.Fatalf("工单号时间部分解析失败（%q）: %v", tradeNo, err)
	}
	for _, ch := range tradeNo[15:] {
		if !strings.ContainsRune(tradeNoAlphabet, ch) {
			t.Fatalf("工单号随机部分含非法字符 %q（%q）", ch, tradeNo)
		}
	}
	return at
}

// TestTicketMemberLifecycle 覆盖会员端全流程：提单（关联本人实例）→ 列表/详情 →
// 会员回复 → 管理端公开回复 → 内部备注 → 关闭 → 关闭后拒绝 → 重复关闭幂等。
func TestTicketMemberLifecycle(t *testing.T) {
	gdb := testDatabase(t)
	engine := newTicketEngine(t, gdb)
	token, member := memberTokenFor(t, engine, "ticketuser")
	instance := seedTicketInstance(t, gdb, member.ID, 1, model.InstanceStatusActive)

	// —— 提单：中文内容 + 关联自己的实例 ——
	created := createTicket(t, engine, token, map[string]any{
		"subject": testTicketSubject, "content": testTicketContent,
		"category": model.TicketCategoryTechnical, "instance_id": instance.ID,
	})
	ticket := created.Ticket
	if !strings.HasPrefix(ticket.TradeNo, model.TicketTradeNoPrefix) {
		t.Fatalf("工单号前缀 = %q，期望 T", ticket.TradeNo)
	}
	if at := ticketTradeNoTime(t, ticket.TradeNo); time.Since(at) > 5*time.Minute {
		t.Fatalf("工单号时间部分 = %s，与当前 UTC 时间相差过大", at)
	}
	if ticket.Status != model.TicketStatusOpen || ticket.ClosedAt != nil {
		t.Fatalf("新工单状态 = %s / closed_at=%v，期望 open / null", ticket.Status, ticket.ClosedAt)
	}
	if ticket.Subject != testTicketSubject || ticket.Category != model.TicketCategoryTechnical {
		t.Fatalf("工单快照错误: %+v", ticket)
	}
	if ticket.LastReplyAt != ticket.CreatedAt {
		t.Fatalf("last_reply_at = %s，期望等于 created_at（%s）", ticket.LastReplyAt, ticket.CreatedAt)
	}
	if ticket.InstanceID == nil || *ticket.InstanceID != instance.ID || ticket.Instance == nil ||
		ticket.Instance.ID != instance.ID || ticket.Instance.ProductName != "工单测试商品" {
		t.Fatalf("关联实例概要错误: %+v", ticket.Instance)
	}
	// 首条消息 = 会员提交的正文（审计必要项：作者类型/作者 ID/可见性/时间）。
	if created.Message.AuthorType != model.TicketAuthorMember || created.Message.AuthorID != member.ID ||
		created.Message.Internal || created.Message.Content != testTicketContent ||
		created.Message.CreatedAt == "" {
		t.Fatalf("首条消息错误: %+v", created.Message)
	}
	if created.Message.AuthorName != "ticketuser" {
		t.Fatalf("首条消息作者名 = %q，期望 ticketuser", created.Message.AuthorName)
	}

	// —— 列表：status 过滤 + 关联实例概要 ——
	rec, envelope := doAPI(t, engine, http.MethodGet, ticketsPath+"?status=open", token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("工单列表失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	list := decodeData[ticketListView](t, envelope)
	if list.Total != 1 || len(list.Items) != 1 || list.Items[0].ID != ticket.ID {
		t.Fatalf("工单列表 = %+v，期望 1 条（id=%d）", list, ticket.ID)
	}
	if list.Items[0].Instance == nil || list.Items[0].Instance.ID != instance.ID {
		t.Fatalf("列表项实例概要错误: %+v", list.Items[0].Instance)
	}

	// —— 详情：首条消息即会员正文 ——
	detail := getTicketDetail(t, engine, token, ticket.ID)
	if len(detail.Messages) != 1 || detail.Messages[0].Content != testTicketContent ||
		detail.Messages[0].Internal {
		t.Fatalf("详情消息流错误: %+v", detail.Messages)
	}

	// —— 会员回复 → 状态回 open ——
	rec, envelope = doAPI(t, engine, http.MethodPost, ticketsPath+"/"+itoa(ticket.ID)+"/reply", token,
		map[string]any{"content": "补充：控制台也进不去。"})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("会员回复失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	reply := decodeData[ticketReplyView](t, envelope)
	if reply.Ticket.Status != model.TicketStatusOpen || reply.Message.Content != "补充：控制台也进不去。" {
		t.Fatalf("会员回复结果错误: %+v", reply)
	}

	// —— 管理端：客服登录（support 角色即可全权处理）——
	seedAdmin(t, gdb, "cs01", "password123", model.RoleSupport, model.StatusActive)
	tokenAdmin, _ := loginAdmin(t, engine, "cs01", "password123")

	// 内部备注：状态**不变**（仍是 open），会员端不可见。
	internalContent := "内部备注：该客户上月也报过同类问题，注意核对宿主机负载。"
	internalReply := adminReplyTicket(t, engine, tokenAdmin, ticket.ID,
		map[string]any{"content": internalContent, "internal": true})
	if internalReply.Ticket.Status != model.TicketStatusOpen {
		t.Fatalf("内部备注后状态 = %s，期望保持 open", internalReply.Ticket.Status)
	}
	if !internalReply.Message.Internal || internalReply.Message.AuthorName != "cs01" {
		t.Fatalf("内部备注消息错误: %+v", internalReply.Message)
	}
	if internalReply.Ticket.MemberID != member.ID || internalReply.Ticket.Member.Username != "ticketuser" {
		t.Fatalf("管理端视图会员概要错误: %+v", internalReply.Ticket.Member)
	}

	// 内部备注不泄漏：会员端详情仍然只有 2 条（首条 + 会员回复）。
	memberDetail := getTicketDetail(t, engine, token, ticket.ID)
	if len(memberDetail.Messages) != 2 {
		t.Fatalf("会员端消息条数 = %d，期望 2（内部备注不返回）", len(memberDetail.Messages))
	}
	if body := toJSON(t, memberDetail); strings.Contains(body, "内部备注") {
		t.Fatalf("会员端详情泄漏内部备注: %s", body)
	}

	// 管理端详情能看到内部备注（internal=true）。
	adminDetail := adminTicketDetail(t, engine, tokenAdmin, ticket.ID)
	if len(adminDetail.Messages) != 3 || !adminDetail.Messages[2].Internal ||
		adminDetail.Messages[2].Content != internalContent {
		t.Fatalf("管理端消息流错误: %+v", adminDetail.Messages)
	}
	if adminDetail.Ticket.Member.ID != member.ID || adminDetail.Ticket.Member.Nickname == "" {
		t.Fatalf("管理端详情会员概要错误: %+v", adminDetail.Ticket.Member)
	}

	// 公开回复 → 状态 replied（待会员）。
	publicReply := adminReplyTicket(t, engine, tokenAdmin, ticket.ID,
		map[string]any{"content": "已为您重启主机，请再试一次。"})
	if publicReply.Ticket.Status != model.TicketStatusReplied || publicReply.Message.Internal {
		t.Fatalf("公开回复结果错误: %+v", publicReply)
	}
	// 状态流转留痕：updated_at 与 last_reply_at 均已推进。
	if publicReply.Ticket.LastReplyAt == "" || publicReply.Ticket.UpdatedAt == "" {
		t.Fatalf("公开回复后时间字段为空: %+v", publicReply.Ticket)
	}

	// 会员回复 → 再次回到 open。
	rec, envelope = doAPI(t, engine, http.MethodPost, ticketsPath+"/"+itoa(ticket.ID)+"/reply", token,
		map[string]any{"content": "还是不行，能换一台吗？"})
	backToOpen := decodeData[ticketReplyView](t, envelope)
	if rec.Code != http.StatusOK || backToOpen.Ticket.Status != model.TicketStatusOpen {
		t.Fatalf("会员回复后状态 = %s（HTTP %d），期望 open", backToOpen.Ticket.Status, rec.Code)
	}

	// —— 关闭（会员关自己的单）——
	closePath := ticketsPath + "/" + itoa(ticket.ID) + "/close"
	rec, envelope = doAPI(t, engine, http.MethodPost, closePath, token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("关闭工单失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	closed := decodeData[ticketCloseView](t, envelope)
	if closed.Ticket.Status != model.TicketStatusClosed || closed.Ticket.ClosedAt == nil || closed.AlreadyClosed {
		t.Fatalf("关闭结果错误: %+v", closed)
	}

	// 关闭后回复：会员与管理员都 40002。
	rec, envelope = doAPI(t, engine, http.MethodPost, ticketsPath+"/"+itoa(ticket.ID)+"/reply", token,
		map[string]any{"content": "再补一句"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("关闭后会员回复应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	rec, envelope = doAPI(t, engine, http.MethodPost,
		adminTicketsPath+"/"+itoa(ticket.ID)+"/reply", tokenAdmin, map[string]any{"content": "客服再补一句"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("关闭后客服回复应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 重复关闭：幂等（200 + already_closed=true，closed_at 保持首次值）。
	rec, envelope = doAPI(t, engine, http.MethodPost, closePath, token, nil)
	again := decodeData[ticketCloseView](t, envelope)
	if rec.Code != http.StatusOK || !again.AlreadyClosed || *again.Ticket.ClosedAt != *closed.Ticket.ClosedAt {
		t.Fatalf("重复关闭应幂等：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 关闭后仍可在列表按 status=closed 查到。
	rec, envelope = doAPI(t, engine, http.MethodGet, ticketsPath+"?status=closed", token, nil)
	closedList := decodeData[ticketListView](t, envelope)
	if rec.Code != http.StatusOK || closedList.Total != 1 || closedList.Items[0].Status != model.TicketStatusClosed {
		t.Fatalf("已关闭列表错误: HTTP %d, %+v", rec.Code, closedList)
	}

	// 会话审计链路：管理端详情的消息流含全部 5 条（会员×3、管理员×2），作者名可解析。
	finalDetail := adminTicketDetail(t, engine, tokenAdmin, ticket.ID)
	if len(finalDetail.Messages) != 5 {
		t.Fatalf("最终消息条数 = %d，期望 5", len(finalDetail.Messages))
	}
	// 作者名解析：会员消息为会员账号名、管理员消息为客服账号名（含内部备注）。
	wantAuthors := []string{"ticketuser", "ticketuser", "cs01", "cs01", "ticketuser"}
	for i, want := range wantAuthors {
		if finalDetail.Messages[i].AuthorName != want {
			t.Fatalf("第 %d 条消息作者 = %q，期望 %q", i+1, finalDetail.Messages[i].AuthorName, want)
		}
	}
}

// TestTicketInternalNoteIsolation 专项验证内部备注不泄漏：会员端的**任何**响应都不含内部备注内容。
func TestTicketInternalNoteIsolation(t *testing.T) {
	gdb := testDatabase(t)
	engine := newTicketEngine(t, gdb)
	token, _ := memberTokenFor(t, engine, "leakuser")
	seedAdmin(t, gdb, "cs02", "password123", model.RoleAdmin, model.StatusActive)
	tokenAdmin, _ := loginAdmin(t, engine, "cs02", "password123")

	created := createTicket(t, engine, token, map[string]any{
		"subject": "账单金额对不上", "content": "本月账单多了 20 元。", "category": model.TicketCategoryBilling,
	})
	ticketID := created.Ticket.ID
	internalContent := "内部：该会员上月有退款未走完流程，勿直接承诺补差价。"
	adminReplyTicket(t, engine, tokenAdmin, ticketID, map[string]any{"content": internalContent, "internal": true})

	// 管理端看得到，且 internal=true（此时消息流 = 首条 + 内部备注）。
	adminDetail := adminTicketDetail(t, engine, tokenAdmin, ticketID)
	if len(adminDetail.Messages) != 2 || !adminDetail.Messages[1].Internal ||
		adminDetail.Messages[1].Content != internalContent {
		t.Fatalf("管理端消息流错误: %+v", adminDetail.Messages)
	}

	// 会员端：详情、列表、回复响应都不含内部备注内容。
	checks := map[string]string{}
	detail := getTicketDetail(t, engine, token, ticketID)
	checks["详情"] = toJSON(t, detail)

	rec, envelope := doAPI(t, engine, http.MethodGet, ticketsPath, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("列表失败: HTTP %d", rec.Code)
	}
	checks["列表"] = toJSON(t, decodeData[ticketListView](t, envelope))

	rec, envelope = doAPI(t, engine, http.MethodPost, ticketsPath+"/"+itoa(ticketID)+"/reply", token,
		map[string]any{"content": "再确认一下金额。"})
	if rec.Code != http.StatusOK {
		t.Fatalf("会员回复失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	checks["回复"] = toJSON(t, decodeData[ticketReplyView](t, envelope))

	for name, body := range checks {
		if strings.Contains(body, "内部") || strings.Contains(body, "补差价") {
			t.Fatalf("会员端%s响应泄漏内部备注: %s", name, body)
		}
	}
	if len(detail.Messages) != 1 {
		t.Fatalf("会员端消息条数 = %d，期望 1", len(detail.Messages))
	}

}

// TestTicketPermissionMatrix 验证权限矩阵：未登录 401、跨类型 token 401、会员隔离 404、
// support 全权、finance 一律 403。
func TestTicketPermissionMatrix(t *testing.T) {
	gdb := testDatabase(t)
	engine := newTicketEngine(t, gdb)
	tokenA, _ := memberTokenFor(t, engine, "alice6a")
	tokenB, _ := memberTokenFor(t, engine, "bob6a")
	seedAdmin(t, gdb, "cs03", "password123", model.RoleSupport, model.StatusActive)
	seedAdmin(t, gdb, "fin6a", "password123", model.RoleFinance, model.StatusActive)
	tokenSupport, _ := loginAdmin(t, engine, "cs03", "password123")
	tokenFinance, _ := loginAdmin(t, engine, "fin6a", "password123")

	created := createTicket(t, engine, tokenA, map[string]any{
		"subject": "测试权限矩阵的工单", "content": "内容", "category": model.TicketCategoryOther,
	})
	ticketID := created.Ticket.ID
	memberPaths := []struct {
		method string
		path   string
		body   map[string]any
	}{
		{http.MethodGet, ticketsPath, nil},
		{http.MethodGet, ticketsPath + "/" + itoa(ticketID), nil},
		{http.MethodPost, ticketsPath + "/" + itoa(ticketID) + "/reply", map[string]any{"content": "x"}},
		{http.MethodPost, ticketsPath + "/" + itoa(ticketID) + "/close", nil},
	}
	adminPaths := []struct {
		method string
		path   string
		body   map[string]any
	}{
		{http.MethodGet, adminTicketsPath, nil},
		{http.MethodGet, adminTicketsPath + "/" + itoa(ticketID), nil},
		{http.MethodPost, adminTicketsPath + "/" + itoa(ticketID) + "/reply", map[string]any{"content": "x"}},
		{http.MethodPost, adminTicketsPath + "/" + itoa(ticketID) + "/close", nil},
	}

	// 未登录 → 401；跨类型 token → 401。
	for _, tc := range memberPaths {
		rec, envelope := doAPI(t, engine, tc.method, tc.path, "", tc.body)
		if rec.Code != http.StatusUnauthorized || envelope.Code != response.CodeUnauthorized {
			t.Fatalf("未登录访问 %s 应 401：HTTP %d, body=%s", tc.path, rec.Code, rec.Body.String())
		}
		rec, envelope = doAPI(t, engine, tc.method, tc.path, tokenSupport, tc.body)
		if rec.Code != http.StatusUnauthorized || envelope.Code != response.CodeUnauthorized {
			t.Fatalf("管理员 token 访问会员端 %s 应 401：HTTP %d, body=%s", tc.path, rec.Code, rec.Body.String())
		}
	}
	for _, tc := range adminPaths {
		rec, envelope := doAPI(t, engine, tc.method, tc.path, "", tc.body)
		if rec.Code != http.StatusUnauthorized || envelope.Code != response.CodeUnauthorized {
			t.Fatalf("未登录访问 %s 应 401：HTTP %d, body=%s", tc.path, rec.Code, rec.Body.String())
		}
		rec, envelope = doAPI(t, engine, tc.method, tc.path, tokenA, tc.body)
		if rec.Code != http.StatusUnauthorized || envelope.Code != response.CodeUnauthorized {
			t.Fatalf("会员 token 访问管理端 %s 应 401：HTTP %d, body=%s", tc.path, rec.Code, rec.Body.String())
		}
	}

	// 会员隔离：他人（含不存在的）工单统一 404，且不改动工单状态。
	for _, tc := range memberPaths[1:] {
		rec, envelope := doAPI(t, engine, tc.method, tc.path, tokenB, tc.body)
		if rec.Code != http.StatusNotFound || envelope.Code != response.CodeNotFound {
			t.Fatalf("跨会员访问 %s 应 404：HTTP %d, body=%s", tc.path, rec.Code, rec.Body.String())
		}
	}
	if detail := adminTicketDetail(t, engine, tokenSupport, ticketID); detail.Ticket.Status != model.TicketStatusOpen ||
		len(detail.Messages) != 1 {
		t.Fatalf("越权尝试改动了工单: %+v", detail)
	}
	// 会员身份也不能用他人实例提单（防越权关联）：404。
	instanceB := seedTicketInstance(t, gdb, 999999, 77, model.InstanceStatusActive)
	rec, envelope := doAPI(t, engine, http.MethodPost, ticketsPath, tokenA, map[string]any{
		"subject": "尝试关联他人实例", "content": "内容", "category": model.TicketCategoryOther,
		"instance_id": instanceB.ID,
	})
	if rec.Code != http.StatusNotFound || envelope.Code != response.CodeNotFound {
		t.Fatalf("关联他人实例应 404：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// support：四个接口全权（读 + 写）。
	if detail := adminTicketDetail(t, engine, tokenSupport, ticketID); detail.Ticket.ID != ticketID {
		t.Fatalf("support 读取详情失败: %+v", detail)
	}
	reply := adminReplyTicket(t, engine, tokenSupport, ticketID, map[string]any{"content": "客服已受理。"})
	if reply.Ticket.Status != model.TicketStatusReplied {
		t.Fatalf("support 回复后状态 = %s，期望 replied", reply.Ticket.Status)
	}
	rec, envelope = doAPI(t, engine, http.MethodPost, adminTicketsPath+"/"+itoa(ticketID)+"/close", tokenSupport, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("support 关闭失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// finance：四个接口一律 403（含只读）。用另一张 open 工单避免被上一步关闭影响。
	other := createTicket(t, engine, tokenA, map[string]any{
		"subject": "财务角色权限验证单", "content": "内容", "category": model.TicketCategoryBilling,
	})
	financePaths := []struct {
		method string
		path   string
		body   map[string]any
	}{
		{http.MethodGet, adminTicketsPath, nil},
		{http.MethodGet, adminTicketsPath + "/" + itoa(other.Ticket.ID), nil},
		{http.MethodPost, adminTicketsPath + "/" + itoa(other.Ticket.ID) + "/reply", map[string]any{"content": "x"}},
		{http.MethodPost, adminTicketsPath + "/" + itoa(other.Ticket.ID) + "/close", nil},
	}
	for _, tc := range financePaths {
		rec, envelope := doAPI(t, engine, tc.method, tc.path, tokenFinance, tc.body)
		if rec.Code != http.StatusForbidden || envelope.Code != response.CodeForbidden {
			t.Fatalf("finance 访问 %s 应 403：HTTP %d, body=%s", tc.path, rec.Code, rec.Body.String())
		}
	}
	if detail := adminTicketDetail(t, engine, tokenSupport, other.Ticket.ID); detail.Ticket.Status != model.TicketStatusOpen {
		t.Fatalf("finance 的越权尝试改动了工单: %+v", detail)
	}
}

// TestTicketValidationErrors 验证字段校验、枚举与越权关联的错误口径。
func TestTicketValidationErrors(t *testing.T) {
	gdb := testDatabase(t)
	engine := newTicketEngine(t, gdb)
	token, member := memberTokenFor(t, engine, "validuser")
	ownInstance := seedTicketInstance(t, gdb, member.ID, 2, model.InstanceStatusActive)

	base := map[string]any{
		"subject": "标题够五个字", "content": "内容。", "category": model.TicketCategoryOther,
	}
	cases := []struct {
		name    string
		body    map[string]any
		http    int
		code    int
		message string
	}{
		{"标题过短", mergeBody(base, map[string]any{"subject": "四个字啊"}), http.StatusBadRequest,
			response.CodeValidationFailed, "5-100"},
		{"标题过长", mergeBody(base, map[string]any{"subject": strings.Repeat("标", 101)}), http.StatusBadRequest,
			response.CodeValidationFailed, "5-100"},
		{"标题纯空白", mergeBody(base, map[string]any{"subject": "  \t "}), http.StatusBadRequest,
			response.CodeValidationFailed, "不能为空"},
		{"内容为空", mergeBody(base, map[string]any{"content": ""}), http.StatusBadRequest,
			response.CodeValidationFailed, "不能为空"},
		{"内容纯空白", mergeBody(base, map[string]any{"content": " \n  "}), http.StatusBadRequest,
			response.CodeValidationFailed, "不能为空"},
		{"内容过长", mergeBody(base, map[string]any{"content": strings.Repeat("内", 5001)}), http.StatusBadRequest,
			response.CodeValidationFailed, "1-5000"},
		{"分类非法", mergeBody(base, map[string]any{"category": "urgent"}), http.StatusBadRequest,
			response.CodeInvalidParam, "category"},
		{"分类为空", mergeBody(base, map[string]any{"category": ""}), http.StatusBadRequest,
			response.CodeInvalidParam, "category"},
		{"实例 ID 为 0", mergeBody(base, map[string]any{"instance_id": 0}), http.StatusBadRequest,
			response.CodeInvalidParam, "instance_id"},
		{"实例不存在", mergeBody(base, map[string]any{"instance_id": 987654}), http.StatusNotFound,
			response.CodeNotFound, "实例不存在"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, envelope := doAPI(t, engine, http.MethodPost, ticketsPath, token, tc.body)
			if rec.Code != tc.http || envelope.Code != tc.code {
				t.Fatalf("HTTP %d/code %d，期望 %d/%d（body=%s）",
					rec.Code, envelope.Code, tc.http, tc.code, rec.Body.String())
			}
			if !strings.Contains(envelope.Message, tc.message) {
				t.Fatalf("message = %q，期望包含 %q", envelope.Message, tc.message)
			}
		})
	}

	// 合法边界：标题 5 字与 100 字、内容 1 字与 5000 字都可创建。
	for _, subject := range []string{strings.Repeat("标", 5), strings.Repeat("标", 100)} {
		created := createTicket(t, engine, token, map[string]any{
			"subject": subject, "content": "内容", "category": model.TicketCategoryTechnical,
			"instance_id": ownInstance.ID,
		})
		if created.Ticket.Subject != subject {
			t.Fatalf("标题边界值未原样保存: %q", created.Ticket.Subject)
		}
	}
	created := createTicket(t, engine, token, map[string]any{
		"subject": "内容长度边界验证单", "content": strings.Repeat("内", 5000), "category": model.TicketCategoryOther,
	})
	if len([]rune(created.Message.Content)) != 5000 {
		t.Fatalf("内容边界值长度 = %d", len([]rune(created.Message.Content)))
	}

	// 首尾空白被裁剪（存储与回显一致）。
	trimmed := createTicket(t, engine, token, map[string]any{
		"subject": "  首尾空白标题  ", "content": "\n 正文首尾空白 \n", "category": model.TicketCategoryOther,
	})
	if trimmed.Ticket.Subject != "首尾空白标题" || trimmed.Message.Content != "正文首尾空白" {
		t.Fatalf("首尾空白未裁剪: %q / %q", trimmed.Ticket.Subject, trimmed.Message.Content)
	}

	// 回复内容校验。
	rec, envelope := doAPI(t, engine, http.MethodPost, ticketsPath+"/"+itoa(created.Ticket.ID)+"/reply", token,
		map[string]any{"content": "   "})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("空白回复应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	rec, envelope = doAPI(t, engine, http.MethodPost, ticketsPath+"/"+itoa(created.Ticket.ID)+"/reply", token,
		map[string]any{"content": strings.Repeat("回", 5001)})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("超长回复应 40002：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	// 路径与查询参数：ID 非法 / status 非法 / 分页越界。
	rec, envelope = doAPI(t, engine, http.MethodGet, ticketsPath+"/abc", token, nil)
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("非法工单 ID 应 40001：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	rec, envelope = doAPI(t, engine, http.MethodGet, ticketsPath+"?status=pending", token, nil)
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("非法 status 应 40001：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	rec, envelope = doAPI(t, engine, http.MethodGet, ticketsPath+"?page=0", token, nil)
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("分页越界应 40001：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	// 请求体不是合法 JSON → 40001。
	rec, envelope = doAPI(t, engine, http.MethodPost, ticketsPath, token, "not-an-object")
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("非法 JSON 应 40001：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
}

// TestTicketOpenLimit 验证未关闭工单上限（20）：第 21 单拒绝，关闭一单后可继续创建。
func TestTicketOpenLimit(t *testing.T) {
	gdb := testDatabase(t)
	engine := newTicketEngine(t, gdb)
	token, _ := memberTokenFor(t, engine, "limituser")

	for i := 0; i < maxOpenTicketsPerMember; i++ {
		createTicket(t, engine, token, map[string]any{
			"subject": fmt.Sprintf("限流验证工单第 %d 单", i+1), "content": "内容",
			"category": model.TicketCategoryOther,
		})
	}

	rec, envelope := doAPI(t, engine, http.MethodPost, ticketsPath, token, map[string]any{
		"subject": "第 21 单应被拒绝", "content": "内容", "category": model.TicketCategoryOther,
	})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("第 %d 单应 40002：HTTP %d, body=%s", maxOpenTicketsPerMember+1, rec.Code, rec.Body.String())
	}
	if !strings.Contains(envelope.Message, strconv.Itoa(maxOpenTicketsPerMember)) {
		t.Fatalf("超限提示未含上限值: %q", envelope.Message)
	}

	// 关闭一单后即可继续创建（closed 不计入上限）。
	rec, envelope = doAPI(t, engine, http.MethodGet, ticketsPath+"?page_size=1", token, nil)
	list := decodeData[ticketListView](t, envelope)
	if rec.Code != http.StatusOK || len(list.Items) != 1 {
		t.Fatalf("列表失败: HTTP %d, %+v", rec.Code, list)
	}
	rec, envelope = doAPI(t, engine, http.MethodPost,
		ticketsPath+"/"+itoa(list.Items[0].ID)+"/close", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("关闭失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	createTicket(t, engine, token, map[string]any{
		"subject": "关闭一单后可以继续提单", "content": "内容", "category": model.TicketCategoryOther,
	})
}

// TestTicketAdminFilters 验证管理端筛选：status / category / member_id / keyword（含 LIKE 转义）。
func TestTicketAdminFilters(t *testing.T) {
	gdb := testDatabase(t)
	engine := newTicketEngine(t, gdb)
	tokenA, memberA := memberTokenFor(t, engine, "filtera")
	tokenB, memberB := memberTokenFor(t, engine, "filterb")
	seedAdmin(t, gdb, "cs04", "password123", model.RoleAdmin, model.StatusActive)
	tokenAdmin, _ := loginAdmin(t, engine, "cs04", "password123")

	a1 := createTicket(t, engine, tokenA, map[string]any{
		"subject": "香港主机磁盘占用 100% 告警", "content": "内容", "category": model.TicketCategoryTechnical,
	})
	b1 := createTicket(t, engine, tokenB, map[string]any{
		"subject": "发票抬头需要修改", "content": "内容", "category": model.TicketCategoryBilling,
	})
	// A 的第二个工单：被客服关闭，用于验证 status 过滤。
	a2 := createTicket(t, engine, tokenA, map[string]any{
		"subject": "备案信息咨询", "content": "内容", "category": model.TicketCategoryOther,
	})
	rec, envelope := doAPI(t, engine, http.MethodPost,
		adminTicketsPath+"/"+itoa(a2.Ticket.ID)+"/close", tokenAdmin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("关闭失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	cases := []struct {
		name     string
		query    string
		expected []uint64
	}{
		{"全部", "", []uint64{a2.Ticket.ID, b1.Ticket.ID, a1.Ticket.ID}},
		{"按状态 open", "?status=open", []uint64{b1.Ticket.ID, a1.Ticket.ID}},
		{"按状态 closed", "?status=closed", []uint64{a2.Ticket.ID}},
		{"按分类 technical", "?category=technical", []uint64{a1.Ticket.ID}},
		{"按会员 A", fmt.Sprintf("?member_id=%d", memberA.ID), []uint64{a2.Ticket.ID, a1.Ticket.ID}},
		{"按会员 B", fmt.Sprintf("?member_id=%d", memberB.ID), []uint64{b1.Ticket.ID}},
		{"关键词命中标题", "?keyword=" + url.QueryEscape("磁盘"), []uint64{a1.Ticket.ID}},
		{"关键词命中工单号", "?keyword=" + b1.Ticket.TradeNo, []uint64{b1.Ticket.ID}},
		{"关键词无命中", "?keyword=不存在的关键词", nil},
		// LIKE 通配符按字面量匹配：% 只命中标题里真的含 % 的那一单，_ 不匹配任意字符。
		{"关键词为字面 %", "?keyword=" + url.QueryEscape("%"), []uint64{a1.Ticket.ID}},
		{"关键词为字面 _", "?keyword=" + url.QueryEscape("_"), nil},
		{"组合筛选", fmt.Sprintf("?member_id=%d&status=closed", memberA.ID), []uint64{a2.Ticket.ID}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, envelope := doAPI(t, engine, http.MethodGet, adminTicketsPath+tc.query, tokenAdmin, nil)
			if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
				t.Fatalf("筛选失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
			}
			list := decodeData[adminTicketListView](t, envelope)
			if len(list.Items) != len(tc.expected) || int(list.Total) != len(tc.expected) {
				t.Fatalf("命中 %d 条（total=%d），期望 %d 条：%s",
					len(list.Items), list.Total, len(tc.expected), rec.Body.String())
			}
			for i, id := range tc.expected {
				if list.Items[i].ID != id {
					t.Fatalf("第 %d 条 id = %d，期望 %d", i, list.Items[i].ID, id)
				}
			}
		})
	}

	// 管理端列表带会员概要；关键词为工单号时能定位到会员。
	rec, envelope = doAPI(t, engine, http.MethodGet, adminTicketsPath+"?keyword="+b1.Ticket.TradeNo, tokenAdmin, nil)
	list := decodeData[adminTicketListView](t, envelope)
	if rec.Code != http.StatusOK || list.Items[0].MemberID != memberB.ID ||
		list.Items[0].Member.Username != "filterb" {
		t.Fatalf("管理端会员概要错误: HTTP %d, %+v", rec.Code, list.Items[0])
	}

	// 管理端筛选参数非法：status / category / member_id。
	for _, query := range []string{"?status=done", "?category=urgent", "?member_id=abc"} {
		rec, envelope := doAPI(t, engine, http.MethodGet, adminTicketsPath+query, tokenAdmin, nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s 应 400：HTTP %d, body=%s", query, rec.Code, rec.Body.String())
		}
		if envelope.Code != response.CodeInvalidParam {
			t.Fatalf("%s code = %d，期望 40001", query, envelope.Code)
		}
	}

	// 管理端对不存在/已删除工单的 404：ID 合法但记录不存在。
	rec, envelope = doAPI(t, engine, http.MethodGet, adminTicketsPath+"/987654", tokenAdmin, nil)
	if rec.Code != http.StatusNotFound || envelope.Code != response.CodeNotFound {
		t.Fatalf("不存在工单应 404：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
}

// TestTicketSortedByLastActivity 验证列表按最近活动倒序（last_reply_at DESC, id DESC）。
func TestTicketSortedByLastActivity(t *testing.T) {
	gdb := testDatabase(t)
	engine := newTicketEngine(t, gdb)
	token, _ := memberTokenFor(t, engine, "sortuser")

	first := createTicket(t, engine, token, map[string]any{
		"subject": "最早创建的工单", "content": "内容", "category": model.TicketCategoryOther,
	})
	second := createTicket(t, engine, token, map[string]any{
		"subject": "较晚创建的工单", "content": "内容", "category": model.TicketCategoryOther,
	})
	// 同一秒内创建时以 id DESC 兜底：列表应为「较晚创建的工单」在前。
	rec, envelope := doAPI(t, engine, http.MethodGet, ticketsPath, token, nil)
	list := decodeData[ticketListView](t, envelope)
	if rec.Code != http.StatusOK || len(list.Items) != 2 {
		t.Fatalf("列表失败: HTTP %d, %+v", rec.Code, list)
	}
	if list.Items[0].ID != second.Ticket.ID || list.Items[1].ID != first.Ticket.ID {
		t.Fatalf("排序错误: %d, %d", list.Items[0].ID, list.Items[1].ID)
	}

	// 对最早那单追加回复（活动时间最新），它应排到最前。
	// last_reply_at 是秒级 DATETIME：跨过一个整秒再回复，避免与创建同秒而落到 id 兜底。
	time.Sleep(1100 * time.Millisecond)
	rec, envelope = doAPI(t, engine, http.MethodPost,
		ticketsPath+"/"+itoa(first.Ticket.ID)+"/reply", token, map[string]any{"content": "有进展了"})
	if rec.Code != http.StatusOK {
		t.Fatalf("回复失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	rec, envelope = doAPI(t, engine, http.MethodGet, ticketsPath, token, nil)
	list = decodeData[ticketListView](t, envelope)
	if list.Items[0].ID != first.Ticket.ID {
		t.Fatalf("回复后排序错误: 首条 = %d，期望 %d", list.Items[0].ID, first.Ticket.ID)
	}
}

// mergeBody 复制基础请求体并覆盖部分字段。
func mergeBody(base, override map[string]any) map[string]any {
	merged := make(map[string]any, len(base)+len(override))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range override {
		merged[key] = value
	}
	return merged
}
