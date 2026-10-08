package router

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/notify"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// ---------------------------------------------------------------------------
// 阶段 6b：事件接线（契约 17.4 事件接线表）
// ---------------------------------------------------------------------------

const (
	testAdminEmail       = "ops@oem.example.com"
	testSiteNameForEmail = "示例云主机"
)

// replyMemberTicket 会员回复工单（接口断言成功）。
func replyMemberTicket(t *testing.T, engine http.Handler, token string, ticketID uint64, content string) {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodPost,
		ticketsPath+"/"+itoa(ticketID)+"/reply", token, map[string]any{"content": content})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("会员回复失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
}

// closeTicketFor 关闭工单（会员端或管理端路径）。
func closeTicketFor(t *testing.T, engine http.Handler, path, token string, ticketID uint64) {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodPost, path+"/"+itoa(ticketID)+"/close", token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("关闭工单失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
}

// lastEmailTo 返回发给指定地址的最近一封邮件（不存在时 Fatal）。
func lastEmailTo(t *testing.T, sender *fakeSender, to string) emailMessageView {
	t.Helper()
	messages := sender.messages()
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].To == to {
			return emailMessageView{To: messages[i].To, Subject: messages[i].Subject, Body: messages[i].Body}
		}
	}
	t.Fatalf("没有发给 %s 的邮件（共 %d 封）", to, len(messages))
	return emailMessageView{}
}

// emailMessageView 是测试内对 email.Message 的轻量视图（避免测试直接依赖 email 包字段名）。
type emailMessageView struct {
	To      string
	Subject string
	Body    string
}

// countEmailsTo 统计发给指定地址的邮件数。
func countEmailsTo(sender *fakeSender, to string) int {
	count := 0
	for _, message := range sender.messages() {
		if message.To == to {
			count++
		}
	}
	return count
}

// TestNotifyTicketEventsReachBothSides 覆盖工单四类事件的接线：
// 创建 / 会员回复 → 管理端（admin + support 扇出，finance 不接收）；
// 客服公开回复 / 客服关闭 → 会员；内部备注与会员自行关闭不产生通知。
func TestNotifyTicketEventsReachBothSides(t *testing.T) {
	gdb := testDatabase(t)
	engine, _, sender := newTicketNotifyEngine(t, gdb)
	seedSiteSetting(t, gdb, testSiteNameForEmail, testAdminEmail)

	admin := seedAdmin(t, gdb, "cs6b", "password123", model.RoleAdmin, model.StatusActive)
	support := seedAdmin(t, gdb, "cs6b-cs", "password123", model.RoleSupport, model.StatusActive)
	seedAdmin(t, gdb, "cs6b-fin", "password123", model.RoleFinance, model.StatusActive)

	token, member := memberTokenFor(t, engine, "notify6b")
	adminToken, _ := loginAdmin(t, engine, "cs6b", "password123")
	supportToken, _ := loginAdmin(t, engine, "cs6b-cs", "password123")

	created := createTicket(t, engine, token, map[string]any{
		"subject": testTicketSubject, "content": testTicketContent, "category": "technical",
	})
	ticketID := created.Ticket.ID

	// ① 提单 → 管理员/客服各 1 条 ticket_created；finance 0 条；邮件发站点 admin_email。
	for _, tc := range []struct {
		name      string
		adminID   uint64
		wantCount int
	}{
		{"admin", admin.ID, 1},
		{"support", support.ID, 1},
	} {
		items := notificationsByEvent(t, gdb, model.NotificationRecipientAdmin, tc.adminID,
			model.NotificationEventTicketCreated)
		if len(items) != tc.wantCount {
			t.Fatalf("%s 的 ticket_created = %d 条，期望 %d", tc.name, len(items), tc.wantCount)
		}
		if !strings.Contains(items[0].Content, "notify6b") || !strings.Contains(items[0].Title, testTicketSubject) {
			t.Fatalf("%s 的通知文案异常: %+v", tc.name, items[0])
		}
	}
	if got := countNotifications(t, gdb, model.NotificationRecipientAdmin, 3); got != 0 {
		t.Fatalf("finance 不应收到工单通知，实际 %d 条", got)
	}
	if got := countEmailsTo(sender, testAdminEmail); got != 1 {
		t.Fatalf("管理端提醒邮件 = %d 封，期望 1", got)
	}
	adminMail := lastEmailTo(t, sender, testAdminEmail)
	if !strings.Contains(adminMail.Subject, "新工单") || !strings.Contains(adminMail.Body, testSiteNameForEmail) {
		t.Fatalf("管理端提醒邮件内容异常: %+v", adminMail)
	}

	// ② 会员回复 → 管理端 ticket_replied（会员侧不产生）。
	replyMemberTicket(t, engine, token, ticketID, "补充：重启后仍然无法连接。")
	for _, adminID := range []uint64{admin.ID, support.ID} {
		if items := notificationsByEvent(t, gdb, model.NotificationRecipientAdmin, adminID,
			model.NotificationEventTicketReplied); len(items) != 1 {
			t.Fatalf("会员回复后管理员 %d 的 ticket_replied = %d 条，期望 1", adminID, len(items))
		}
	}
	if items := notificationsByEvent(t, gdb, model.NotificationRecipientMember, member.ID,
		model.NotificationEventTicketReplied); len(items) != 0 {
		t.Fatalf("会员自己的回复不应通知会员，实际 %d 条", len(items))
	}

	// ③ 管理员内部备注 → 不产生任何通知。
	rec, envelope := doAPI(t, engine, http.MethodPost,
		adminTicketsPath+"/"+itoa(ticketID)+"/reply", adminToken,
		map[string]any{"content": "内部备注：先查上游主机状态。", "internal": true})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("内部备注失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if items := notificationsByEvent(t, gdb, model.NotificationRecipientMember, member.ID,
		model.NotificationEventTicketReplied); len(items) != 0 {
		t.Fatalf("内部备注不得产生会员通知，实际 %d 条", len(items))
	}

	// ④ 客服公开回复 → 会员 ticket_replied（含邮件）。
	adminReplyTicket(t, engine, supportToken, ticketID, map[string]any{"content": "已重启上游服务，请重试。"})
	items := notificationsByEvent(t, gdb, model.NotificationRecipientMember, member.ID,
		model.NotificationEventTicketReplied)
	if len(items) != 1 {
		t.Fatalf("客服公开回复后会员通知 = %d 条，期望 1", len(items))
	}
	if !strings.Contains(items[0].Title, "工单已回复") {
		t.Fatalf("会员侧回复通知标题异常: %+v", items[0])
	}
	memberEmail := "notify6b@example.com"
	if got := countEmailsTo(sender, memberEmail); got != 1 {
		t.Fatalf("会员提醒邮件 = %d 封，期望 1", got)
	}

	// ⑤ 客服关闭 → 会员 ticket_closed（含邮件）。
	closeTicketFor(t, engine, adminTicketsPath, adminToken, ticketID)
	if items := notificationsByEvent(t, gdb, model.NotificationRecipientMember, member.ID,
		model.NotificationEventTicketClosed); len(items) != 1 {
		t.Fatalf("客服关闭后会员通知 = %d 条，期望 1", len(items))
	}
	if got := countEmailsTo(sender, memberEmail); got != 2 {
		t.Fatalf("会员提醒邮件 = %d 封，期望 2（回复 + 关闭）", got)
	}
	// 幂等重复关闭不再打扰会员。
	closeTicketFor(t, engine, adminTicketsPath, adminToken, ticketID)
	if items := notificationsByEvent(t, gdb, model.NotificationRecipientMember, member.ID,
		model.NotificationEventTicketClosed); len(items) != 1 {
		t.Fatalf("重复关闭不得重复通知，实际 %d 条", len(items))
	}

	// ⑥ 会员自行关闭 → 不通知客服（契约 17.4 定稿）。
	secondTicket := createTicket(t, engine, token, map[string]any{
		"subject": "第二张工单：账单疑问", "content": "请问本期账单为何增加？", "category": "billing",
	})
	beforeCreated := len(notificationsByEvent(t, gdb, model.NotificationRecipientAdmin, admin.ID,
		model.NotificationEventTicketCreated))
	closeTicketFor(t, engine, ticketsPath, token, secondTicket.Ticket.ID)
	if items := notificationsByEvent(t, gdb, model.NotificationRecipientAdmin, admin.ID,
		model.NotificationEventTicketCreated); len(items) != beforeCreated {
		t.Fatal("会员关闭工单不应产生新的管理端通知")
	}
	if items := notificationsByEvent(t, gdb, model.NotificationRecipientAdmin, admin.ID,
		model.NotificationEventTicketClosed); len(items) != 0 {
		t.Fatalf("会员关闭工单不应通知客服，实际 %d 条", len(items))
	}

	// 邮件留痕：全部成功。
	logs := emailLogsFromDB(t, gdb)
	if len(logs) == 0 {
		t.Fatal("邮件留痕为空")
	}
	for i := range logs {
		if logs[i].Status != model.EmailStatusSuccess || logs[i].ErrorText != "" {
			t.Fatalf("邮件留痕异常: %+v", logs[i])
		}
	}
}

// TestNotifyDeliveryAndRenewEvents 覆盖交付链路的三个事件：
// 新购开通成功（order_delivered）、开通失败（order_failed）、续费成功（renew_succeeded）。
func TestNotifyDeliveryAndRenewEvents(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine, _, sender := newStage6Engine(t, gdb, host.client(t))
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, member := memberTokenFor(t, engine, "notify5b")
	instance := newInstanceFor(t, engine, gateway, gdb, token, product.ID)

	// ① 开通成功 → order_delivered（正文含实例名与到期时间）。
	items := notificationsByEvent(t, gdb, model.NotificationRecipientMember, member.ID,
		model.NotificationEventOrderDelivered)
	if len(items) != 1 {
		t.Fatalf("开通成功后通知 = %d 条，期望 1", len(items))
	}
	if !strings.Contains(items[0].Content, instance.Name) || !strings.Contains(items[0].Content, "到期时间") {
		t.Fatalf("交付通知正文异常: %+v", items[0])
	}
	memberEmail := "notify5b@example.com"
	if got := countEmailsTo(sender, memberEmail); got != 1 {
		t.Fatalf("交付通知邮件 = %d 封，期望 1", got)
	}
	deliveredMail := lastEmailTo(t, sender, memberEmail)
	if deliveredMail.Subject != "订单交付成功" || !strings.Contains(deliveredMail.Body, instance.Name) {
		t.Fatalf("交付邮件异常: %+v", deliveredMail)
	}

	// ② 续费成功 → renew_succeeded。
	rec, envelope := doAPI(t, engine, http.MethodPost,
		"/api/v1/instances/"+itoa(instance.ID)+"/renew", token, map[string]any{"cycle": "monthly"})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("续费下单失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	renewOrder := decodeData[orderView](t, envelope)
	if updated := payOrderByEpayNotify(t, engine, gateway, token, renewOrder); updated.Status != model.OrderStatusActive {
		t.Fatalf("续费交付未完成: %s", updated.Status)
	}
	renewed := notificationsByEvent(t, gdb, model.NotificationRecipientMember, member.ID,
		model.NotificationEventRenewSucceeded)
	if len(renewed) != 1 || !strings.Contains(renewed[0].Content, "新的到期时间") {
		t.Fatalf("续费通知异常: %+v", renewed)
	}

	// ③ 开通失败 → order_failed（正文含已脱敏的失败原因）。
	host.setFailApplyCredit(true)
	failedOrder := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "monthly", "config": map[string]any{},
	})
	failedView := payOrderByEpayNotify(t, engine, gateway, token, failedOrder)
	if failedView.Status != model.OrderStatusFailed {
		t.Fatalf("构造交付失败场景未生效: status=%s", failedView.Status)
	}
	failed := notificationsByEvent(t, gdb, model.NotificationRecipientMember, member.ID,
		model.NotificationEventOrderFailed)
	if len(failed) != 1 {
		t.Fatalf("交付失败通知 = %d 条，期望 1", len(failed))
	}
	if !strings.Contains(failed[0].Content, failedOrder.TradeNo) {
		t.Fatalf("交付失败通知应含订单号: %+v", failed[0])
	}
	if got := countEmailsTo(sender, memberEmail); got != 3 {
		t.Fatalf("会员邮件总数 = %d，期望 3（交付 + 续费 + 失败）", got)
	}
}

// TestNotifyAdminRetryDeliveryReNotifies 固化「管理员重试交付同样接线」（契约 17.4 定稿）：
// 首次交付失败 → order_failed；管理员重试成功 → 补发 order_delivered（不重复失败通知）。
func TestNotifyAdminRetryDeliveryReNotifies(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine, _, sender := newStage6Engine(t, gdb, host.client(t))
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)
	seedAdmin(t, gdb, "retry6b", "password123", model.RoleAdmin, model.StatusActive)
	adminToken, _ := loginAdmin(t, engine, "retry6b", "password123")

	token, member := memberTokenFor(t, engine, "retry6bmember")
	host.setFailApplyCredit(true)
	order := createOrder(t, engine, token, map[string]any{
		"product_id": product.ID, "cycle": "monthly", "config": map[string]any{},
	})
	if failed := payOrderByEpayNotify(t, engine, gateway, token, order); failed.Status != model.OrderStatusFailed {
		t.Fatalf("首次交付应失败: status=%s", failed.Status)
	}
	if items := notificationsByEvent(t, gdb, model.NotificationRecipientMember, member.ID,
		model.NotificationEventOrderFailed); len(items) != 1 {
		t.Fatalf("首次失败通知 = %d 条，期望 1", len(items))
	}

	// 上游恢复 → 管理员重试交付成功 → 补发交付成功通知。
	host.setFailApplyCredit(false)
	rec, envelope := retryDelivery(t, engine, adminToken, order.ID)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("重试交付失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if delivered := notificationsByEvent(t, gdb, model.NotificationRecipientMember, member.ID,
		model.NotificationEventOrderDelivered); len(delivered) != 1 {
		t.Fatalf("重试成功后应补发 order_delivered，实际 %d 条", len(delivered))
	}
	if failed := notificationsByEvent(t, gdb, model.NotificationRecipientMember, member.ID,
		model.NotificationEventOrderFailed); len(failed) != 1 {
		t.Fatalf("重试成功不应重复失败通知，实际 %d 条", len(failed))
	}
	if got := countEmailsTo(sender, "retry6bmember@example.com"); got != 2 {
		t.Fatalf("会员邮件 = %d 封，期望 2（失败 + 交付成功）", got)
	}
}

// TestNotifyExpiryReminderScan 覆盖到期提醒扫描：命中窗口 → 只提醒一次（去重）；
// 续费推进到期时间后重新武装；开关关闭不产生；窗口外不提醒。
func TestNotifyExpiryReminderScan(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine, service, sender := newStage6Engine(t, gdb, host.client(t))
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, member := memberTokenFor(t, engine, "notifyexpiry")
	instance := newInstanceFor(t, engine, gateway, gdb, token, product.ID)

	now := time.Now().UTC()
	scanner := newNotifierScanner(t, gdb, host, now, service)

	// 窗口内（3 天后到期，默认提前 7 天）→ 提醒一次。
	setInstanceDue(t, gdb, instance.ID, now.Add(72*time.Hour))
	report, err := scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	if report.ExpiryScanned != 1 || report.ExpiryReminded != 1 || report.ExpiryFailed != 0 {
		t.Fatalf("提醒统计 = %+v，期望 1/1/0", report)
	}
	items := notificationsByEvent(t, gdb, model.NotificationRecipientMember, member.ID,
		model.NotificationEventExpiryReminder)
	if len(items) != 1 {
		t.Fatalf("到期提醒通知 = %d 条，期望 1", len(items))
	}
	if !strings.Contains(items[0].Content, instance.Name) || !strings.Contains(items[0].Title, "7 天") {
		t.Fatalf("到期提醒文案异常: %+v", items[0])
	}
	if got := countEmailsTo(sender, "notifyexpiry@example.com"); got != 2 { // 开通 + 到期提醒
		t.Fatalf("会员邮件 = %d 封，期望 2（交付 + 提醒）", got)
	}

	// 去重：同一到期周期再扫不重复提醒（去重锚点已写入）。
	stored := instanceFromDB(t, gdb, instance.ID)
	if stored.ExpiryRemindedDue == nil || !stored.ExpiryRemindedDue.Equal(*stored.NextDueDate) {
		t.Fatalf("去重锚点未写入: %+v", stored.ExpiryRemindedDue)
	}
	report, err = scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("第二轮扫描失败: %v", err)
	}
	if report.ExpiryScanned != 0 || report.ExpiryReminded != 0 {
		t.Fatalf("第二轮不应重复提醒: %+v", report)
	}
	if items := notificationsByEvent(t, gdb, model.NotificationRecipientMember, member.ID,
		model.NotificationEventExpiryReminder); len(items) != 1 {
		t.Fatalf("去重后通知 = %d 条，期望仍为 1", len(items))
	}

	// 续费推进到期时间 → 新周期重新武装（5 天后到期，仍在 7 天窗口内）。
	setInstanceDue(t, gdb, instance.ID, now.Add(120*time.Hour))
	report, err = scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("第三轮扫描失败: %v", err)
	}
	if report.ExpiryReminded != 1 {
		t.Fatalf("新到期周期应重新提醒: %+v", report)
	}
	if items := notificationsByEvent(t, gdb, model.NotificationRecipientMember, member.ID,
		model.NotificationEventExpiryReminder); len(items) != 2 {
		t.Fatalf("重新武装后通知 = %d 条，期望 2", len(items))
	}

	// 窗口外（30 天后到期）→ 不提醒。
	setInstanceDue(t, gdb, instance.ID, now.Add(30*24*time.Hour))
	setInstanceRemindedDue(t, gdb, instance.ID, nil)
	report, err = scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("第四轮扫描失败: %v", err)
	}
	if report.ExpiryScanned != 0 {
		t.Fatalf("窗口外不应提醒: %+v", report)
	}

	// 开关关闭 → 窗口内也不再产生提醒（且不认领锚点）。
	seedNotificationSettings(t, gdb, true, true, false, 7)
	setInstanceDue(t, gdb, instance.ID, now.Add(48*time.Hour))
	report, err = scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("第五轮扫描失败: %v", err)
	}
	if report.ExpiryScanned != 0 || report.ExpiryReminded != 0 {
		t.Fatalf("开关关闭时不应提醒: %+v", report)
	}
	if stored := instanceFromDB(t, gdb, instance.ID); stored.ExpiryRemindedDue != nil {
		t.Fatalf("开关关闭时不应认领锚点: %+v", stored.ExpiryRemindedDue)
	}
	if items := notificationsByEvent(t, gdb, model.NotificationRecipientMember, member.ID,
		model.NotificationEventExpiryReminder); len(items) != 2 {
		t.Fatalf("开关关闭后通知数 = %d，期望仍为 2", len(items))
	}

	// 提醒天数可配置：改为 1 天后，2 天后到期的实例不再命中。
	seedNotificationSettings(t, gdb, true, true, true, 1)
	report, err = scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("第六轮扫描失败: %v", err)
	}
	if report.ExpiryScanned != 0 {
		t.Fatalf("提醒天数=1 时 2 天后到期不应命中: %+v", report)
	}
}

// TestNotifyScanSuspendedAndTerminatedEvents 覆盖扫描器的另两个接线点：
// 到期自动暂停 → instance_suspended；取消申请收敛 → instance_terminated（幂等重复收敛不再通知）。
func TestNotifyScanSuspendedAndTerminatedEvents(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine, service, sender := newStage6Engine(t, gdb, host.client(t))
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, member := memberTokenFor(t, engine, "notifyscan")
	now := time.Now().UTC()

	expired := newInstanceFor(t, engine, gateway, gdb, token, product.ID)
	setInstanceDue(t, gdb, expired.ID, now.Add(-time.Hour))

	terminating := newInstanceFor(t, engine, gateway, gdb, token, product.ID)
	setInstanceCancel(t, gdb, terminating.ID, model.CancelTypeImmediate, now.Add(-time.Hour))
	host.deleteHost(terminating.HostID)

	sender.reset() // 忽略开通环节的通知邮件，只看扫描事件

	scanner := newNotifierScanner(t, gdb, host, now, service)
	report, err := scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	if report.Suspended != 1 || report.CancelConverged != 1 {
		t.Fatalf("扫描统计 = %+v，期望 suspended=1 converged=1", report)
	}

	suspended := notificationsByEvent(t, gdb, model.NotificationRecipientMember, member.ID,
		model.NotificationEventInstanceSuspended)
	if len(suspended) != 1 || !strings.Contains(suspended[0].Content, expired.Name) {
		t.Fatalf("到期暂停通知异常: %+v", suspended)
	}
	terminated := notificationsByEvent(t, gdb, model.NotificationRecipientMember, member.ID,
		model.NotificationEventInstanceTerminated)
	if len(terminated) != 1 || !strings.Contains(terminated[0].Content, terminating.Name) {
		t.Fatalf("终止收敛通知异常: %+v", terminated)
	}
	if got := countEmailsTo(sender, "notifyscan@example.com"); got != 2 {
		t.Fatalf("会员邮件 = %d 封，期望 2（暂停 + 终止）", got)
	}

	// 幂等：再次扫描不再产生通知（实例已 suspended / 已 terminated 且已收敛过一次）。
	sender.reset()
	if _, err := scanner.ScanOnce(context.Background()); err != nil {
		t.Fatalf("第二轮扫描失败: %v", err)
	}
	if items := notificationsByEvent(t, gdb, model.NotificationRecipientMember, member.ID,
		model.NotificationEventInstanceSuspended); len(items) != 1 {
		t.Fatalf("重复扫描不得重复通知暂停: %d 条", len(items))
	}
	if items := notificationsByEvent(t, gdb, model.NotificationRecipientMember, member.ID,
		model.NotificationEventInstanceTerminated); len(items) != 1 {
		t.Fatalf("重复扫描不得重复通知终止: %d 条", len(items))
	}
}

// TestNotifySwitchesGateInAppAndEmail 覆盖两个总开关：站内关 → 无通知行（邮件照发）；
// 邮件关 → 无邮件（通知行照写）；发送失败 → email_logs 记 fail 且不影响业务。
func TestNotifySwitchesGateInAppAndEmail(t *testing.T) {
	gdb := testDatabase(t)
	engine, _, sender := newTicketNotifyEngine(t, gdb)
	seedSiteSetting(t, gdb, testSiteNameForEmail, testAdminEmail)
	seedAdmin(t, gdb, "sw6b", "password123", model.RoleAdmin, model.StatusActive)
	token, _ := memberTokenFor(t, engine, "switch6b")

	// 站内关、邮件开：只有邮件。
	seedNotificationSettings(t, gdb, false, true, true, 7)
	createTicket(t, engine, token, map[string]any{
		"subject": "开关用例一", "content": "站内关闭时只发邮件。", "category": "other",
	})
	if got := countNotifications(t, gdb, model.NotificationRecipientAdmin, 0); got != 0 {
		t.Fatalf("站内开关关闭时不应写通知，实际 %d 条", got)
	}
	if got := countEmailsTo(sender, testAdminEmail); got != 1 {
		t.Fatalf("邮件应照发，实际 %d 封", got)
	}

	// 站内开、邮件关：只有通知。
	seedNotificationSettings(t, gdb, true, false, true, 7)
	createTicket(t, engine, token, map[string]any{
		"subject": "开关用例二", "content": "邮件关闭时只写站内通知。", "category": "other",
	})
	if got := countNotifications(t, gdb, model.NotificationRecipientAdmin, 0); got != 1 {
		t.Fatalf("站内开关开启时应写通知，实际 %d 条", got)
	}
	if got := countEmailsTo(sender, testAdminEmail); got != 1 {
		t.Fatalf("邮件开关关闭时不应发信，实际 %d 封", got)
	}

	// 邮件发送失败：email_logs 记 fail（含脱敏原因），站内通知照常。
	seedNotificationSettings(t, gdb, true, true, true, 7)
	sender.setFail(errors.New("SMTP 认证失败（用户名 mailer）：535 credentials invalid"))
	createTicket(t, engine, token, map[string]any{
		"subject": "开关用例三", "content": "邮件失败时站内通知照常。", "category": "other",
	})
	if got := countNotifications(t, gdb, model.NotificationRecipientAdmin, 0); got != 2 {
		t.Fatalf("邮件失败不应影响站内通知，实际 %d 条", got)
	}
	logs := emailLogsFromDB(t, gdb)
	if len(logs) == 0 || logs[0].Status != model.EmailStatusFail {
		t.Fatalf("失败邮件未留痕: %+v", logs)
	}
	if !strings.Contains(logs[0].ErrorText, "认证失败") {
		t.Fatalf("失败原因未写入留痕: %+v", logs[0])
	}
	if !strings.Contains(logs[0].ToAddr, testAdminEmail) {
		t.Fatalf("留痕收件人不符: %+v", logs[0])
	}
}

// TestNotifyWithoutSMTPConfigSkipsEmail 验证 SMTP 未配置时静默跳过邮件
// （不写失败留痕、不影响站内通知）。此处**不注入发送器**，走真实判定分支
// （Usable()=false 时在连接前就返回，不会真的连 SMTP）。
func TestNotifyWithoutSMTPConfigSkipsEmail(t *testing.T) {
	gdb := testDatabase(t)
	service := notify.New(store.New(gdb), settings.NewReader(store.New(gdb)), notify.Options{
		Async: false, Logger: silentLogger(),
	})
	engine := New(Options{
		Logger:   silentLogger(),
		DB:       gdb,
		JWT:      config.JWTConfig{Secret: testJWTSecret, ExpireHours: 168},
		Notifier: service,
	})
	seedSiteSetting(t, gdb, testSiteNameForEmail, testAdminEmail)
	seedAdmin(t, gdb, "nosmtp6b", "password123", model.RoleAdmin, model.StatusActive)
	token, _ := memberTokenFor(t, engine, "nosmtp6b")

	createTicket(t, engine, token, map[string]any{
		"subject": "未配置 SMTP", "content": "邮件应被静默跳过。", "category": "other",
	})
	if got := countNotifications(t, gdb, model.NotificationRecipientAdmin, 0); got != 1 {
		t.Fatalf("站内通知应照常，实际 %d 条", got)
	}
	if logs := emailLogsFromDB(t, gdb); len(logs) != 0 {
		t.Fatalf("未配置 SMTP 不应写失败留痕: %+v", logs)
	}
}

// TestNotifyMemberClosedTicketDoesNotNotifyAdmins 与 TestNotifyTicketEventsReachBothSides 的第 ⑥ 步
// 呼应：单独固化「会员关闭不通知客服」这一条定稿口径（防止回归）。
func TestNotifyMemberClosedTicketDoesNotNotifyAdmins(t *testing.T) {
	gdb := testDatabase(t)
	engine, _, _ := newTicketNotifyEngine(t, gdb)
	seedAdmin(t, gdb, "closeonly", "password123", model.RoleAdmin, model.StatusActive)
	token, _ := memberTokenFor(t, engine, "closeonly")

	created := createTicket(t, engine, token, map[string]any{
		"subject": "会员自行关闭", "content": "不需要客服处理了。", "category": "other",
	})
	closeTicketFor(t, engine, ticketsPath, token, created.Ticket.ID)

	if items := notificationsByEvent(t, gdb, model.NotificationRecipientAdmin, 1,
		model.NotificationEventTicketClosed); len(items) != 0 {
		t.Fatalf("会员关闭不应通知客服: %+v", items)
	}
}

// setInstanceRemindedDue 直接改库设置/清空到期提醒去重锚点（构造前置条件）。
func setInstanceRemindedDue(t *testing.T, gdb *gorm.DB, instanceID uint64, due any) {
	t.Helper()
	if err := gdb.Model(&model.Instance{}).Where("id = ?", instanceID).
		Update("expiry_reminded_due", due).Error; err != nil {
		t.Fatalf("更新到期提醒锚点失败: %v", err)
	}
}
