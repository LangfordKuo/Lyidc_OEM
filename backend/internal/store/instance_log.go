package store

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

// maxInstanceLogMessageRunes 对应 instance_operation_logs.message 的 VARCHAR(512)（按字符截断，留足余量）。
const maxInstanceLogMessageRunes = 500

// InstanceLogInput 是写入一条实例操作审计的输入（message 由调用方保证已脱敏）。
type InstanceLogInput struct {
	InstanceID uint64
	ActorType  string
	ActorID    uint64
	Action     string
	Status     string
	Message    string
}

// InstanceLogFilter 是实例操作记录的分页查询条件（page 从 1 开始）。
type InstanceLogFilter struct {
	InstanceID uint64
	Page       int
	PageSize   int
}

// AppendInstanceLog 写入一条实例操作审计（失败尝试与系统自动操作同样写入）。
//
// 审计写入**不参与业务事务**：写失败只返回错误由调用方记日志，不影响操作结果本身
// （契约 15.3：审计尽力而为，操作结果以实例状态与上游回读为准）。
func (s *Store) AppendInstanceLog(ctx context.Context, in InstanceLogInput) error {
	db, err := s.session(ctx)
	if err != nil {
		return err
	}
	entry := model.InstanceOperationLog{
		InstanceID: in.InstanceID,
		ActorType:  in.ActorType,
		ActorID:    in.ActorID,
		Action:     in.Action,
		Status:     in.Status,
		Message:    truncateRunes(in.Message, maxInstanceLogMessageRunes),
		CreatedAt:  time.Now().UTC(),
	}
	return db.Create(&entry).Error
}

// ListInstanceLogs 按实例分页查询操作记录（新记录在前），返回当页与总数。
func (s *Store) ListInstanceLogs(ctx context.Context, filter InstanceLogFilter) ([]model.InstanceOperationLog, int64, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, 0, err
	}

	query := db.Model(&model.InstanceOperationLog{}).Where("instance_id = ?", filter.InstanceID)

	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	items := make([]model.InstanceOperationLog, 0)
	if err := query.Session(&gorm.Session{}).
		Order("id DESC").
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
