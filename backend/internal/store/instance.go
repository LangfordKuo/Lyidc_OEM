package store

import (
	"context"
	"time"

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

// InstanceByID 按本地主键查询实例（管理端；不限会员）。
func (s *Store) InstanceByID(ctx context.Context, id uint64) (*model.Instance, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	return instanceBy(db, "id = ?", id)
}

// InstanceByHostID 按上游主机 ID 查询实例（交付链路用；上游主机 ID 全局唯一）。
func (s *Store) InstanceByHostID(ctx context.Context, hostID int) (*model.Instance, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	return instanceBy(db, "host_id = ?", hostID)
}

// InstanceSyncInput 是实例上游同步字段的落库输入（阶段 5b）。
//
// 语义：**调用方提供完整快照**（未回读到的字段由调用方保留旧值后传入），
// 本方法对提供的字段全量覆盖；空串/零值同样覆盖（用于「上游确实没有该值」的情形）。
type InstanceSyncInput struct {
	NextDueDate    *time.Time
	UpstreamStatus string
	DedicatedIP    string
	AssignedIPs    string
	Port           int
	Username       string
	Password       string
}

// UpdateInstanceSync 覆盖实例的上游同步字段并更新 updated_at（单条 UPDATE）。
func (s *Store) UpdateInstanceSync(ctx context.Context, id uint64, in InstanceSyncInput) error {
	db, err := s.session(ctx)
	if err != nil {
		return err
	}
	result := db.Model(&model.Instance{}).Where("id = ?", id).Updates(map[string]any{
		"next_due_date":   in.NextDueDate,
		"upstream_status": in.UpstreamStatus,
		"dedicated_ip":    in.DedicatedIP,
		"assigned_ips":    in.AssignedIPs,
		"port":            in.Port,
		"username":        in.Username,
		"password":        in.Password,
		"updated_at":      time.Now().UTC(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		// RowsAffected 为 0 也可能是「值完全相同」，因此回读一次以区分不存在。
		_, err := instanceBy(db, "id = ?", id)
		return err
	}
	return nil
}

// UpdateInstanceStatus 条件更新实例状态（from → to）；实例不存在返回 ErrNotFound，
// 当前状态不等于 from 返回 ErrStateConflict（不回写）。
func (s *Store) UpdateInstanceStatus(ctx context.Context, id uint64, from, to string) error {
	db, err := s.session(ctx)
	if err != nil {
		return err
	}
	result := db.Model(&model.Instance{}).
		Where("id = ? AND status = ?", id, from).
		Updates(map[string]any{"status": to, "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		current, err := instanceBy(db, "id = ?", id)
		if err != nil {
			return err
		}
		if current.Status != from {
			return ErrStateConflict
		}
	}
	return nil
}

// UpdateInstancePassword 仅更新实例密码字段（重置密码成功后落库；实例不存在返回 ErrNotFound）。
func (s *Store) UpdateInstancePassword(ctx context.Context, id uint64, password string) error {
	db, err := s.session(ctx)
	if err != nil {
		return err
	}
	result := db.Model(&model.Instance{}).Where("id = ?", id).Updates(map[string]any{
		"password":   password,
		"updated_at": time.Now().UTC(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		_, err := instanceBy(db, "id = ?", id)
		return err
	}
	return nil
}

// ListDueInstances 返回到期未续的 active 实例（next_due_date 非空且早于 dueBefore），
// 按 next_due_date 升序最多 limit 条（到期扫描用；调用方循环取批直到取空）。
func (s *Store) ListDueInstances(ctx context.Context, dueBefore time.Time, limit int) ([]model.Instance, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]model.Instance, 0)
	if err := db.Where("status = ? AND next_due_date IS NOT NULL AND next_due_date < ?",
		model.InstanceStatusActive, dueBefore.UTC()).
		Order("next_due_date ASC, id ASC").
		Limit(limit).
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// instanceBy 是内部按条件查询单条实例。
func instanceBy(db *gorm.DB, query string, args ...any) (*model.Instance, error) {
	var instance model.Instance
	if err := db.Where(query, args...).Take(&instance).Error; err != nil {
		return nil, notFoundIfNeeded(err)
	}
	return &instance, nil
}
