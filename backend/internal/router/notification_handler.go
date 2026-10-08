package router

import (
	"errors"
	"log/slog"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// 通知相关提示文案（契约 17.2）。
const (
	msgNotificationMissing = "通知不存在"
)

// notificationHandler 处理站内通知接口（阶段 6b）：会员端与管理端**同构**，
// 差别只在接收方身份（会员 token → recipient_type=member，管理员 token → recipient_type=admin）。
//
// 权限（契约 17.2）：通知是**个人收件箱**，唯一权限边界是 (recipient_type, recipient_id) ——
// 会员只能读本人的通知，管理员只能读发给自己的通知；他人的通知与不存在的通知统一 404
// （不暴露他人通知的存在性）。管理端三类角色（admin / support / finance）都可读**本人**通知：
// 通知不属于工单域，finance 只是通常收不到事件（契约 17.4 的扇出只覆盖 admin + support）。
type notificationHandler struct {
	store  *store.Store
	logger *slog.Logger
}

// notificationView 是通知对外视图。
type notificationView struct {
	ID        uint64  `json:"id"`
	Event     string  `json:"event"`
	Title     string  `json:"title"`
	Content   string  `json:"content"`
	Read      bool    `json:"read"`
	ReadAt    *string `json:"read_at"`
	CreatedAt string  `json:"created_at"`
}

// notificationListView 是通知分页列表（顺带回带未读数，省一次往返）。
type notificationListView struct {
	Items    []notificationView `json:"items"`
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
	Total    int64              `json:"total"`
	Unread   int64              `json:"unread"`
}

// notificationUnreadView 是未读计数响应。
type notificationUnreadView struct {
	Unread int64 `json:"unread"`
}

// notificationReadView 是单条已读响应（already_read 表示本次是幂等重复已读）。
type notificationReadView struct {
	Notification notificationView `json:"notification"`
	AlreadyRead  bool             `json:"already_read"`
}

// notificationReadAllView 是全部已读响应（updated 为本次置为已读的条数；unread 恒为 0）。
type notificationReadAllView struct {
	Updated int64 `json:"updated"`
	Unread  int64 `json:"unread"`
}

// ---------------------------------------------------------------------------
// 会员端
// ---------------------------------------------------------------------------

// listMyNotifications 处理 GET /api/v1/notifications（分页 + unread 筛选）。
func (h *notificationHandler) listMyNotifications(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	h.list(c, model.NotificationRecipientMember, member.ID)
}

// myUnreadCount 处理 GET /api/v1/notifications/unread-count。
func (h *notificationHandler) myUnreadCount(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	h.unreadCount(c, model.NotificationRecipientMember, member.ID)
}

// readMyNotification 处理 POST /api/v1/notifications/:id/read（幂等）。
func (h *notificationHandler) readMyNotification(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	h.read(c, model.NotificationRecipientMember, member.ID)
}

// readAllMyNotifications 处理 POST /api/v1/notifications/read-all（幂等）。
func (h *notificationHandler) readAllMyNotifications(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	h.readAll(c, model.NotificationRecipientMember, member.ID)
}

// ---------------------------------------------------------------------------
// 管理端（与会员端同构，接收方换成当前管理员）
// ---------------------------------------------------------------------------

// listAdminNotifications 处理 GET /api/v1/admin/notifications。
func (h *notificationHandler) listAdminNotifications(c *gin.Context) {
	admin, ok := adminFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	h.list(c, model.NotificationRecipientAdmin, admin.ID)
}

// adminUnreadCount 处理 GET /api/v1/admin/notifications/unread-count。
func (h *notificationHandler) adminUnreadCount(c *gin.Context) {
	admin, ok := adminFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	h.unreadCount(c, model.NotificationRecipientAdmin, admin.ID)
}

// readAdminNotification 处理 POST /api/v1/admin/notifications/:id/read（幂等）。
func (h *notificationHandler) readAdminNotification(c *gin.Context) {
	admin, ok := adminFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	h.read(c, model.NotificationRecipientAdmin, admin.ID)
}

// readAllAdminNotifications 处理 POST /api/v1/admin/notifications/read-all（幂等）。
func (h *notificationHandler) readAllAdminNotifications(c *gin.Context) {
	admin, ok := adminFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	h.readAll(c, model.NotificationRecipientAdmin, admin.ID)
}

// ---------------------------------------------------------------------------
// 共用实现
// ---------------------------------------------------------------------------

// list 是列表接口的共用实现。
func (h *notificationHandler) list(c *gin.Context, recipientType string, recipientID uint64) {
	page, pageSize, ok := pagingParams(c)
	if !ok {
		return
	}
	unreadOnly, ok := unreadFilter(c)
	if !ok {
		return
	}

	ctx := c.Request.Context()
	items, total, err := h.store.ListNotifications(ctx, store.NotificationFilter{
		RecipientType: recipientType,
		RecipientID:   recipientID,
		UnreadOnly:    unreadOnly,
		Page:          page,
		PageSize:      pageSize,
	})
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	unread, err := h.store.CountUnreadNotifications(ctx, recipientType, recipientID)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	views := make([]notificationView, 0, len(items))
	for i := range items {
		views = append(views, newNotificationView(&items[i]))
	}
	response.Success(c, notificationListView{
		Items: views, Page: page, PageSize: pageSize, Total: total, Unread: unread,
	})
}

// unreadCount 是未读计数接口的共用实现。
func (h *notificationHandler) unreadCount(c *gin.Context, recipientType string, recipientID uint64) {
	count, err := h.store.CountUnreadNotifications(c.Request.Context(), recipientType, recipientID)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	response.Success(c, notificationUnreadView{Unread: count})
}

// read 是单条已读接口的共用实现（幂等：重复已读返回 already_read=true 且不报错）。
func (h *notificationHandler) read(c *gin.Context, recipientType string, recipientID uint64) {
	id, ok := notificationIDParam(c)
	if !ok {
		return
	}

	notification, changed, err := h.store.MarkNotificationRead(c.Request.Context(), id, recipientType, recipientID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgNotificationMissing)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}
	response.Success(c, notificationReadView{
		Notification: newNotificationView(notification),
		AlreadyRead:  !changed,
	})
}

// readAll 是全部已读接口的共用实现（幂等：无未读时 updated=0）。
func (h *notificationHandler) readAll(c *gin.Context, recipientType string, recipientID uint64) {
	updated, err := h.store.MarkAllNotificationsRead(c.Request.Context(), recipientType, recipientID)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	response.Success(c, notificationReadAllView{Updated: updated, Unread: 0})
}

// newNotificationView 组装通知视图（时间统一输出 RFC3339 UTC）。
func newNotificationView(notification *model.Notification) notificationView {
	return notificationView{
		ID:        notification.ID,
		Event:     notification.Event,
		Title:     notification.Title,
		Content:   notification.Content,
		Read:      notification.ReadAt != nil,
		ReadAt:    formatTimePtr(notification.ReadAt),
		CreatedAt: formatTime(notification.CreatedAt),
	}
}

// unreadFilter 解析 unread 查询参数（缺省 false；非法值写出 40001 并返回 ok=false）。
func unreadFilter(c *gin.Context) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(c.Query("unread"))) {
	case "", "false", "0":
		return false, true
	case "true", "1":
		return true, true
	default:
		response.Fail(c, response.CodeInvalidParam, "unread 只能是 true / false")
		return false, false
	}
}

// notificationIDParam 解析路径参数 :id。
func notificationIDParam(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, response.CodeInvalidParam, "通知 ID 必须为正整数")
		return 0, false
	}
	return id, true
}
