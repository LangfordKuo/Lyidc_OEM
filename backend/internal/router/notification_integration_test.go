package router

import (
	"net/http"
	"testing"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
)

// ---------------------------------------------------------------------------
// 阶段 6b：站内通知接口（会员端与管理端同构；仅本人；已读幂等）
// ---------------------------------------------------------------------------

const (
	notificationsPath      = "/api/v1/notifications"
	adminNotificationsPath = "/api/v1/admin/notifications"
)

// listNotifications 请求列表接口并断言成功。
func listNotifications(t *testing.T, engine http.Handler, path, token, query string) notificationListView {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodGet, path+query, token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("通知列表失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[notificationListView](t, envelope)
}

// unreadCount 请求未读计数接口并断言成功。
func unreadCount(t *testing.T, engine http.Handler, path, token string) int64 {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodGet, path+"/unread-count", token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("未读计数失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[notificationUnreadView](t, envelope).Unread
}

// readNotification 请求单条已读接口并断言成功。
func readNotification(t *testing.T, engine http.Handler, path, token string, id uint64) notificationReadView {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodPost, path+"/"+itoa(id)+"/read", token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("通知已读失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[notificationReadView](t, envelope)
}

// readAllNotifications 请求全部已读接口并断言成功。
func readAllNotifications(t *testing.T, engine http.Handler, path, token string) notificationReadAllView {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodPost, path+"/read-all", token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("全部已读失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[notificationReadAllView](t, envelope)
}

// TestNotificationMemberInbox 覆盖会员端收件箱全流程：列表 / 未读筛选 / 未读计数 /
// 单条已读（幂等、不覆盖首次 read_at）/ 全部已读。
func TestNotificationMemberInbox(t *testing.T) {
	gdb := testDatabase(t)
	engine, _, _ := newTicketNotifyEngine(t, gdb)

	token, member := memberTokenFor(t, engine, "notifymember")
	first := seedNotification(t, gdb, model.NotificationRecipientMember, member.ID,
		model.NotificationEventOrderDelivered, "订单交付成功", "您的订单 O2026 已开通完成。", false)
	second := seedNotification(t, gdb, model.NotificationRecipientMember, member.ID,
		model.NotificationEventTicketReplied, "工单已回复：主机无法连接", "客服已回复您的工单。", false)
	read := seedNotification(t, gdb, model.NotificationRecipientMember, member.ID,
		model.NotificationEventTicketClosed, "工单已关闭：主机无法连接", "您的工单已被客服关闭。", true)
	// 他人（同引擎下的另一个会员）的通知不应出现在本人收件箱。
	otherToken, other := memberTokenFor(t, engine, "notifyother")
	seedNotification(t, gdb, model.NotificationRecipientMember, other.ID,
		model.NotificationEventExpiryReminder, "实例即将到期", "请及时续费。", false)

	// 列表：新建在前（id DESC），顺带回带未读数。
	list := listNotifications(t, engine, notificationsPath, token, "")
	if list.Total != 3 || len(list.Items) != 3 || list.Unread != 2 {
		t.Fatalf("会员收件箱 = total %d / items %d / unread %d，期望 3/3/2",
			list.Total, len(list.Items), list.Unread)
	}
	if list.Items[0].ID != read.ID || list.Items[2].ID != first.ID {
		t.Fatalf("列表顺序异常: %d ... %d", list.Items[0].ID, list.Items[2].ID)
	}
	if !list.Items[0].Read || list.Items[0].ReadAt == nil {
		t.Fatalf("已读通知视图异常: %+v", list.Items[0])
	}
	if list.Items[1].Read || list.Items[1].ReadAt != nil {
		t.Fatalf("未读通知不应有 read_at: %+v", list.Items[1])
	}
	if list.Items[1].Event != model.NotificationEventTicketReplied {
		t.Fatalf("事件字段 = %q", list.Items[1].Event)
	}

	// 未读筛选。
	unreadOnly := listNotifications(t, engine, notificationsPath, token, "?unread=true")
	if unreadOnly.Total != 2 || len(unreadOnly.Items) != 2 {
		t.Fatalf("未读筛选 = %d/%d，期望 2/2", unreadOnly.Total, len(unreadOnly.Items))
	}

	// 未读计数。
	if got := unreadCount(t, engine, notificationsPath, token); got != 2 {
		t.Fatalf("未读计数 = %d，期望 2", got)
	}

	// 单条已读：首次 changed=true。
	readView := readNotification(t, engine, notificationsPath, token, first.ID)
	if readView.AlreadyRead || !readView.Notification.Read || readView.Notification.ReadAt == nil {
		t.Fatalf("首次已读响应异常: %+v", readView)
	}
	firstReadAt := *readView.Notification.ReadAt

	// 重复已读：幂等（already_read=true 且不覆盖首次时间）。
	repeat := readNotification(t, engine, notificationsPath, token, first.ID)
	if !repeat.AlreadyRead {
		t.Fatalf("重复已读应返回 already_read=true: %+v", repeat)
	}
	if repeat.Notification.ReadAt == nil || *repeat.Notification.ReadAt != firstReadAt {
		t.Fatalf("重复已读不得覆盖 read_at: %v -> %v", firstReadAt, repeat.Notification.ReadAt)
	}

	if got := unreadCount(t, engine, notificationsPath, token); got != 1 {
		t.Fatalf("已读一条后未读计数 = %d，期望 1", got)
	}

	// 全部已读：本次置为已读 1 条（第二条未读），再调用为 0（幂等）。
	all := readAllNotifications(t, engine, notificationsPath, token)
	if all.Updated != 1 || all.Unread != 0 {
		t.Fatalf("全部已读结果 = %+v，期望 updated=1 unread=0", all)
	}
	again := readAllNotifications(t, engine, notificationsPath, token)
	if again.Updated != 0 {
		t.Fatalf("重复全部已读 = %+v，期望 updated=0", again)
	}
	if got := unreadCount(t, engine, notificationsPath, token); got != 0 {
		t.Fatalf("全部已读后未读计数 = %d", got)
	}
	if remaining := listNotifications(t, engine, notificationsPath, token, "?unread=true"); remaining.Total != 0 {
		t.Fatalf("全部已读后未读列表 = %d", remaining.Total)
	}

	// 库内一致性：三条通知都属于本人，read_at 均已写入。
	stored := notificationsOf(t, gdb, model.NotificationRecipientMember, member.ID)
	if len(stored) != 3 {
		t.Fatalf("库内通知数 = %d，期望 3", len(stored))
	}
	for i := range stored {
		if stored[i].ReadAt == nil {
			t.Fatalf("通知 %d 应已读", stored[i].ID)
		}
	}
	// second 也要能被单独读取（幂等分支之外的正向路径）。
	if view := readNotification(t, engine, notificationsPath, token, second.ID); view.Notification.ID != second.ID {
		t.Fatalf("读取第二条通知异常: %+v", view)
	}

	// 他人通知不泄漏：另一个会员能看到自己的 1 条。
	if got := listNotifications(t, engine, notificationsPath, otherToken, "").Total; got != 1 {
		t.Fatalf("他人收件箱 = %d，期望 1", got)
	}
}

// TestNotificationPermissionIsolation 覆盖权限边界：仅本人可见、跨角色 token 一律 401。
func TestNotificationPermissionIsolation(t *testing.T) {
	gdb := testDatabase(t)
	engine, _, _ := newTicketNotifyEngine(t, gdb)

	tokenA, memberA := memberTokenFor(t, engine, "notifypermA")
	tokenB, _ := memberTokenFor(t, engine, "notifypermB")
	notification := seedNotification(t, gdb, model.NotificationRecipientMember, memberA.ID,
		model.NotificationEventOrderDelivered, "订单交付成功", "内容", false)

	// 会员 B 读会员 A 的通知：404（不暴露存在性）。
	rec, envelope := doAPI(t, engine, http.MethodPost,
		notificationsPath+"/"+itoa(notification.ID)+"/read", tokenB, nil)
	if rec.Code != http.StatusNotFound || envelope.Code != response.CodeNotFound {
		t.Fatalf("越权已读 = %d/%d，期望 404", rec.Code, envelope.Code)
	}
	// 越权已读不得改动状态。
	if stored := notificationsOf(t, gdb, model.NotificationRecipientMember, memberA.ID); stored[0].ReadAt != nil {
		t.Fatal("越权请求不得把他人通知置为已读")
	}
	// 会员 B 的全部已读不影响 A。
	if all := readAllNotifications(t, engine, notificationsPath, tokenB); all.Updated != 0 {
		t.Fatalf("他人 read-all 影响了别的收件箱: %+v", all)
	}

	// 不存在的通知：404。
	rec, envelope = doAPI(t, engine, http.MethodPost, notificationsPath+"/999999/read", tokenA, nil)
	if rec.Code != http.StatusNotFound || envelope.Code != response.CodeNotFound {
		t.Fatalf("不存在的通知 = %d/%d，期望 404", rec.Code, envelope.Code)
	}

	// 管理端接口：会员 token → 401；会员端接口：管理端 token → 401。
	rec, envelope = doAPI(t, engine, http.MethodGet, adminNotificationsPath, tokenA, nil)
	if rec.Code != http.StatusUnauthorized || envelope.Code != response.CodeUnauthorized {
		t.Fatalf("会员 token 访问管理端通知 = %d/%d，期望 401", rec.Code, envelope.Code)
	}
	seedAdmin(t, gdb, "notifypromo", "password123", model.RoleAdmin, model.StatusActive)
	realAdminToken, _ := loginAdmin(t, engine, "notifypromo", "password123")
	rec, envelope = doAPI(t, engine, http.MethodGet, notificationsPath, realAdminToken, nil)
	if rec.Code != http.StatusUnauthorized || envelope.Code != response.CodeUnauthorized {
		t.Fatalf("管理端 token 访问会员通知 = %d/%d，期望 401", rec.Code, envelope.Code)
	}
}

// TestNotificationAdminInbox 覆盖管理端收件箱：admin / support 各读各的，
// finance 可访问但通常为空（契约 17.4 的扇出只覆盖 admin + support）。
func TestNotificationAdminInbox(t *testing.T) {
	gdb := testDatabase(t)
	engine, _, _ := newTicketNotifyEngine(t, gdb)

	admin := seedAdmin(t, gdb, "notifyadmin", "password123", model.RoleAdmin, model.StatusActive)
	support := seedAdmin(t, gdb, "notifysupport", "password123", model.RoleSupport, model.StatusActive)
	seedAdmin(t, gdb, "notifyfinance", "password123", model.RoleFinance, model.StatusActive)

	seedNotification(t, gdb, model.NotificationRecipientAdmin, admin.ID,
		model.NotificationEventTicketCreated, "新工单：主机无法连接", "会员 alice 提交了工单。", false)
	seedNotification(t, gdb, model.NotificationRecipientAdmin, admin.ID,
		model.NotificationEventTicketReplied, "工单收到会员回复：主机无法连接", "会员 alice 回复了工单。", false)
	seedNotification(t, gdb, model.NotificationRecipientAdmin, support.ID,
		model.NotificationEventTicketCreated, "新工单：主机无法连接", "会员 alice 提交了工单。", false)

	adminToken, _ := loginAdmin(t, engine, "notifyadmin", "password123")
	supportToken, _ := loginAdmin(t, engine, "notifysupport", "password123")
	financeToken, _ := loginAdmin(t, engine, "notifyfinance", "password123")

	list := listNotifications(t, engine, adminNotificationsPath, adminToken, "")
	if list.Total != 2 || list.Unread != 2 {
		t.Fatalf("admin 收件箱 = %+v，期望 2 条未读", list)
	}
	if got := listNotifications(t, engine, adminNotificationsPath, supportToken, "").Total; got != 1 {
		t.Fatalf("support 收件箱 = %d，期望 1", got)
	}
	if got := listNotifications(t, engine, adminNotificationsPath, financeToken, "").Total; got != 0 {
		t.Fatalf("finance 收件箱 = %d，期望 0（扇出不含 finance）", got)
	}

	// support 已读自己的通知，不影响 admin。
	if view := readNotification(t, engine, adminNotificationsPath, supportToken, 3); view.AlreadyRead {
		t.Fatal("support 首次已读不应是 already_read")
	}
	if got := unreadCount(t, engine, adminNotificationsPath, adminToken); got != 2 {
		t.Fatalf("admin 未读数 = %d，期望 2（不受 support 影响）", got)
	}
	// support 读 admin 的通知 → 404。
	rec, envelope := doAPI(t, engine, http.MethodPost,
		adminNotificationsPath+"/1/read", supportToken, nil)
	if rec.Code != http.StatusNotFound || envelope.Code != response.CodeNotFound {
		t.Fatalf("support 读 admin 的通知 = %d/%d，期望 404", rec.Code, envelope.Code)
	}

	all := readAllNotifications(t, engine, adminNotificationsPath, adminToken)
	if all.Updated != 2 || all.Unread != 0 {
		t.Fatalf("admin 全部已读 = %+v", all)
	}
	if got := unreadCount(t, engine, adminNotificationsPath, adminToken); got != 0 {
		t.Fatalf("admin 未读数 = %d，期望 0", got)
	}
}

// TestNotificationValidation 覆盖参数校验：分页越界、非法 unread、非法 ID。
func TestNotificationValidation(t *testing.T) {
	gdb := testDatabase(t)
	engine, _, _ := newTicketNotifyEngine(t, gdb)
	token, _ := memberTokenFor(t, engine, "notifyvalid")

	for _, tc := range []struct {
		name   string
		method string
		path   string
		code   int
	}{
		{"page 越界", http.MethodGet, notificationsPath + "?page=0", response.CodeInvalidParam},
		{"page_size 越界", http.MethodGet, notificationsPath + "?page_size=1000", response.CodeInvalidParam},
		{"unread 非法", http.MethodGet, notificationsPath + "?unread=maybe", response.CodeInvalidParam},
		{"ID 非数字", http.MethodPost, notificationsPath + "/abc/read", response.CodeInvalidParam},
		{"ID 为 0", http.MethodPost, notificationsPath + "/0/read", response.CodeInvalidParam},
	} {
		rec, envelope := doAPI(t, engine, tc.method, tc.path, token, nil)
		if envelope.Code != tc.code {
			t.Errorf("%s: code=%d（HTTP %d），期望 %d", tc.name, envelope.Code, rec.Code, tc.code)
		}
	}
}

// TestNotificationPaging 覆盖分页参数（page_size=2 时第二页）。
func TestNotificationPaging(t *testing.T) {
	gdb := testDatabase(t)
	engine, _, _ := newTicketNotifyEngine(t, gdb)
	token, member := memberTokenFor(t, engine, "notifypage")

	for i := 0; i < 3; i++ {
		seedNotification(t, gdb, model.NotificationRecipientMember, member.ID,
			model.NotificationEventOrderDelivered, "订单交付成功", "内容", false)
	}
	page1 := listNotifications(t, engine, notificationsPath, token, "?page=1&page_size=2")
	if page1.Total != 3 || len(page1.Items) != 2 {
		t.Fatalf("第一页 = %d/%d", page1.Total, len(page1.Items))
	}
	page2 := listNotifications(t, engine, notificationsPath, token, "?page=2&page_size=2")
	if len(page2.Items) != 1 {
		t.Fatalf("第二页 = %d 条，期望 1", len(page2.Items))
	}
	if page2.Items[0].ID >= page1.Items[1].ID {
		t.Fatalf("分页顺序异常: %d vs %d", page2.Items[0].ID, page1.Items[1].ID)
	}
}
