package store

import (
	"context"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

// InstanceFilter 是实例分页查询条件（page 从 1 开始）。
type InstanceFilter struct {
	// MemberID 为 0 表示不按会员过滤（管理端）；会员端一律传本人 ID。
	MemberID uint64
	Status   string
	Page     int
	PageSize int
}

// ListInstances 按条件分页查询实例（新建在前），返回当页数据与总数。
func (s *Store) ListInstances(ctx context.Context, filter InstanceFilter) ([]model.Instance, int64, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, 0, err
	}

	query := db.Model(&model.Instance{})
	if filter.MemberID != 0 {
		query = query.Where("member_id = ?", filter.MemberID)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}

	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	items := make([]model.Instance, 0)
	if err := query.Session(&gorm.Session{}).
		Order("id DESC").
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// InstanceByIDForMember 查询**本人**实例；他人实例与不存在的实例统一返回 ErrNotFound（对外 404）。
func (s *Store) InstanceByIDForMember(ctx context.Context, id, memberID uint64) (*model.Instance, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	return instanceBy(db, "id = ? AND member_id = ?", id, memberID)
}

// instanceBy 是内部按条件查询单条实例。
func instanceBy(db *gorm.DB, query string, args ...any) (*model.Instance, error) {
	var instance model.Instance
	if err := db.Where(query, args...).Take(&instance).Error; err != nil {
		return nil, notFoundIfNeeded(err)
	}
	return &instance, nil
}
