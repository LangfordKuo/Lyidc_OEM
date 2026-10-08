package store

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

// TicketInput 是创建工单的落库字段（校验与裁剪已由 handler 完成）。
//
// Content 是**首条消息正文**：工单与首条消息在同一事务内写入（契约 16.1 的结构性保证）。
type TicketInput struct {
	TradeNo    string
	MemberID   uint64
	InstanceID *uint64
	Category   string
	Subject    string
	Content    string
}

// TicketReplyInput 是一次回复 / 内部备注的落库输入。
type TicketReplyInput struct {
	AuthorType string
	AuthorID   uint64
	Content    string
	Internal   bool
	// NewStatus 非空时把工单状态改为该值（会员回复 → open、管理员公开回复 → replied）；
	// 空串表示**不改状态**（管理员内部备注，契约 16.2 定稿第 2 条）。
	NewStatus string
}

// TicketFilter 是工单分页查询条件（page 从 1 开始）。
type TicketFilter struct {
	// MemberID 为 0 表示不按会员过滤（管理端）；会员端一律传本人 ID。
	MemberID uint64
	Status   string
	Category string
	// Keyword 仅管理端使用：模糊匹配 subject 或 trade_no（通配符已转义）。
	Keyword  string
	Page     int
	PageSize int
}

// CreateTicketWithMessage 在一个事务内写入工单与首条会员消息；
// 工单号冲突时返回 ErrTradeNoTaken（调用方用 createWithTradeNo 换号重试）。
func (s *Store) CreateTicketWithMessage(ctx context.Context, in TicketInput) (*model.Ticket, *model.TicketMessage, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, nil, err
	}

	now := time.Now().UTC()
	ticket := model.Ticket{
		TradeNo:     in.TradeNo,
		MemberID:    in.MemberID,
		InstanceID:  in.InstanceID,
		Category:    in.Category,
		Subject:     in.Subject,
		Status:      model.TicketStatusOpen,
		LastReplyAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	message := model.TicketMessage{
		AuthorType: model.TicketAuthorMember,
		AuthorID:   in.MemberID,
		Content:    in.Content,
		Internal:   false,
		CreatedAt:  now,
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&ticket).Error; err != nil {
			return mapDuplicateError(err)
		}
		message.TicketID = ticket.ID
		return tx.Create(&message).Error
	})
	if err != nil {
		return nil, nil, err
	}
	return &ticket, &message, nil
}

// ListTickets 按条件分页查询工单（最近活动在前：last_reply_at DESC, id DESC），返回当页数据与总数。
func (s *Store) ListTickets(ctx context.Context, filter TicketFilter) ([]model.Ticket, int64, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, 0, err
	}

	query := db.Model(&model.Ticket{})
	if filter.MemberID != 0 {
		query = query.Where("member_id = ?", filter.MemberID)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Category != "" {
		query = query.Where("category = ?", filter.Category)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		pattern := containsKeyword(keyword)
		query = query.Where("subject LIKE ? OR trade_no LIKE ?", pattern, pattern)
	}

	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	items := make([]model.Ticket, 0)
	if err := query.Session(&gorm.Session{}).
		Order("last_reply_at DESC, id DESC").
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// TicketByID 按主键查询工单（管理端；不限归属）。
func (s *Store) TicketByID(ctx context.Context, id uint64) (*model.Ticket, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	return ticketBy(db, "id = ?", id)
}

// TicketByIDForMember 查询**本人**工单；他人工单与不存在的工单统一返回 ErrNotFound（对外 404）。
func (s *Store) TicketByIDForMember(ctx context.Context, id, memberID uint64) (*model.Ticket, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	return ticketBy(db, "id = ? AND member_id = ?", id, memberID)
}

// CountOpenTickets 统计会员的**未关闭**工单数（open / replied），用于提单上限（契约 16.4 第 1 条）。
func (s *Store) CountOpenTickets(ctx context.Context, memberID uint64) (int64, error) {
	db, err := s.session(ctx)
	if err != nil {
		return 0, err
	}
	var total int64
	if err := db.Model(&model.Ticket{}).
		Where("member_id = ? AND status <> ?", memberID, model.TicketStatusClosed).
		Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

// ListTicketMessages 查询工单消息（按 id 升序，等价于按时间正序）。
// includeInternal 为 false 时过滤掉管理员内部备注（会员端一律 false，契约 16.3）。
func (s *Store) ListTicketMessages(ctx context.Context, ticketID uint64, includeInternal bool) ([]model.TicketMessage, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	query := db.Where("ticket_id = ?", ticketID)
	if !includeInternal {
		query = query.Where("internal = ?", false)
	}
	items := make([]model.TicketMessage, 0)
	if err := query.Order("id ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// AppendTicketReply 在事务内追加一条消息，并按 NewStatus 推进工单状态与 last_reply_at。
//
// 并发安全：状态更新带 `status <> 'closed'` 条件——已关闭的工单**任何回复都写不进去**
// （返回 ErrStateConflict），不会出现「消息写进了已关闭工单」的竞态。
// 工单不存在返回 ErrNotFound；已关闭返回 ErrStateConflict。
func (s *Store) AppendTicketReply(ctx context.Context, ticketID uint64, in TicketReplyInput) (*model.Ticket, *model.TicketMessage, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, nil, err
	}

	now := time.Now().UTC()
	var ticket model.Ticket
	var message model.TicketMessage

	err = db.Transaction(func(tx *gorm.DB) error {
		fields := map[string]any{"last_reply_at": now, "updated_at": now}
		if in.NewStatus != "" {
			fields["status"] = in.NewStatus
		}
		result := tx.Model(&model.Ticket{}).
			Where("id = ? AND status <> ?", ticketID, model.TicketStatusClosed).
			Updates(fields)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			// 已关闭、或工单不存在，或写入值与现值完全相同（同一秒内重复回复）：回读区分。
			current, err := ticketBy(tx, "id = ?", ticketID)
			if err != nil {
				return err
			}
			if current.Status == model.TicketStatusClosed {
				return ErrStateConflict
			}
		}

		message = model.TicketMessage{
			TicketID:   ticketID,
			AuthorType: in.AuthorType,
			AuthorID:   in.AuthorID,
			Content:    in.Content,
			Internal:   in.Internal,
			CreatedAt:  now,
		}
		if err := tx.Create(&message).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", ticketID).Take(&ticket).Error
	})
	if err != nil {
		return nil, nil, err
	}
	return &ticket, &message, nil
}

// CloseTicket 关闭工单（幂等）：open / replied → closed 并写入 closed_at；
// 已关闭时不报错、不覆盖 closed_at，返回 changed=false（契约 16.2 定稿第 3 条）。
func (s *Store) CloseTicket(ctx context.Context, ticketID uint64) (*model.Ticket, bool, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, false, err
	}

	now := time.Now().UTC()
	result := db.Model(&model.Ticket{}).
		Where("id = ? AND status <> ?", ticketID, model.TicketStatusClosed).
		Updates(map[string]any{
			"status":     model.TicketStatusClosed,
			"closed_at":  now,
			"updated_at": now,
		})
	if result.Error != nil {
		return nil, false, result.Error
	}

	ticket, err := ticketBy(db, "id = ?", ticketID)
	if err != nil {
		return nil, false, err
	}
	return ticket, result.RowsAffected > 0, nil
}

// ticketBy 是内部按条件查询单条工单。
func ticketBy(db *gorm.DB, query string, args ...any) (*model.Ticket, error) {
	var ticket model.Ticket
	if err := db.Where(query, args...).Take(&ticket).Error; err != nil {
		return nil, notFoundIfNeeded(err)
	}
	return &ticket, nil
}
