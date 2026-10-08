package store

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

// NotificationInput 是站内通知的落库输入（契约 17.1）。
//
// Title / Content 由服务层按事件模板组装（纯文本，不含密码、IP 等敏感信息），
// 本层不做内容校验，只保证「一行 = 一个接收方的一条通知」。
type NotificationInput struct {
	// RecipientType 取 model.NotificationRecipientMember / NotificationRecipientAdmin。
	RecipientType string
	// RecipientID 按 RecipientType 解释为 members.id 或 admins.id。
	RecipientID uint64
	// Event 取 model.NotificationEvent* 之一。
	Event   string
	Title   string
	Content string
}

// NotificationFilter 是通知分页查询条件（page 从 1 开始）。
type NotificationFilter struct {
	// RecipientType / RecipientID 必填：**按接收方隔离**是通知接口的唯一权限边界。
	RecipientType string
	RecipientID   uint64
	// UnreadOnly 为 true 时只返回未读（read_at IS NULL）。
	UnreadOnly bool
	Page       int
	PageSize   int
}

// CreateNotification 写入一条站内通知（收件箱投递）。
func (s *Store) CreateNotification(ctx context.Context, in NotificationInput) (*model.Notification, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	notification := model.Notification{
		RecipientType: in.RecipientType,
		RecipientID:   in.RecipientID,
		Event:         in.Event,
		Title:         in.Title,
		Content:       in.Content,
		CreatedAt:     time.Now().UTC(),
	}
	if err := db.Create(&notification).Error; err != nil {
		return nil, err
	}
	return &notification, nil
}

// CreateNotifications 批量写入站内通知（管理端按「admin + support 逐个账号」扇出）。
// 空入参直接返回（不发起查询）；单条插入，任一条失败即返回错误（调用方只记日志）。
func (s *Store) CreateNotifications(ctx context.Context, inputs []NotificationInput) (int, error) {
	if len(inputs) == 0 {
		return 0, nil
	}
	db, err := s.session(ctx)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	rows := make([]model.Notification, 0, len(inputs))
	for _, in := range inputs {
		rows = append(rows, model.Notification{
			RecipientType: in.RecipientType,
			RecipientID:   in.RecipientID,
			Event:         in.Event,
			Title:         in.Title,
			Content:       in.Content,
			CreatedAt:     now,
		})
	}
	if err := db.Create(&rows).Error; err != nil {
		return 0, err
	}
	return len(rows), nil
}

// ListNotifications 按接收方分页查询通知（新建在前），返回当页数据与总数。
func (s *Store) ListNotifications(ctx context.Context, filter NotificationFilter) ([]model.Notification, int64, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, 0, err
	}

	query := db.Model(&model.Notification{}).
		Where("recipient_type = ? AND recipient_id = ?", filter.RecipientType, filter.RecipientID)
	if filter.UnreadOnly {
		query = query.Where("read_at IS NULL")
	}

	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	items := make([]model.Notification, 0)
	if err := query.Session(&gorm.Session{}).
		Order("id DESC").
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// CountUnreadNotifications 统计接收方的未读数（读接口与列表接口共用）。
func (s *Store) CountUnreadNotifications(ctx context.Context, recipientType string, recipientID uint64) (int64, error) {
	db, err := s.session(ctx)
	if err != nil {
		return 0, err
	}
	var count int64
	if err := db.Model(&model.Notification{}).
		Where("recipient_type = ? AND recipient_id = ? AND read_at IS NULL", recipientType, recipientID).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// MarkNotificationRead 把**本人**的一条通知置为已读（幂等）。
//
// 返回 (通知, changed, error)：changed 表示本次是否真的从未读变为已读（重复已读为 false 且不报错，
// 也不覆盖首次 read_at）；通知不存在或不属于该接收方一律 ErrNotFound（对外 404，不暴露他人通知的存在性）。
func (s *Store) MarkNotificationRead(ctx context.Context, id uint64, recipientType string, recipientID uint64) (*model.Notification, bool, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, false, err
	}

	result := db.Model(&model.Notification{}).
		Where("id = ? AND recipient_type = ? AND recipient_id = ? AND read_at IS NULL",
			id, recipientType, recipientID).
		Updates(map[string]any{"read_at": time.Now().UTC()})
	if result.Error != nil {
		return nil, false, result.Error
	}

	var notification model.Notification
	if err := db.Where("id = ? AND recipient_type = ? AND recipient_id = ?",
		id, recipientType, recipientID).Take(&notification).Error; err != nil {
		return nil, false, notFoundIfNeeded(err)
	}
	return &notification, result.RowsAffected > 0, nil
}

// MarkAllNotificationsRead 把接收方的全部未读置为已读，返回本次置为已读的条数（幂等）。
func (s *Store) MarkAllNotificationsRead(ctx context.Context, recipientType string, recipientID uint64) (int64, error) {
	db, err := s.session(ctx)
	if err != nil {
		return 0, err
	}
	result := db.Model(&model.Notification{}).
		Where("recipient_type = ? AND recipient_id = ? AND read_at IS NULL", recipientType, recipientID).
		Updates(map[string]any{"read_at": time.Now().UTC()})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// ListAdminRecipients 返回**在职**的管理端通知接收人（role ∈ {admin, support} 且 status=active），
// 按 id 升序。管理端通知扇出到这些账号（finance 不接收工单类通知，契约 17.4 定稿）。
func (s *Store) ListAdminRecipients(ctx context.Context) ([]model.Admin, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]model.Admin, 0)
	if err := db.Where("role IN ? AND status = ?",
		[]string{model.RoleAdmin, model.RoleSupport}, model.StatusActive).
		Order("id ASC").
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}
