package store

import (
	"context"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

// AdminByID 按主键查询管理员。
func (s *Store) AdminByID(ctx context.Context, id uint64) (*model.Admin, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	var admin model.Admin
	if err := db.Where("id = ?", id).Take(&admin).Error; err != nil {
		return nil, notFoundIfNeeded(err)
	}
	return &admin, nil
}

// AdminByUsername 按用户名查询管理员（utf8mb4_general_ci 大小写不敏感）。
func (s *Store) AdminByUsername(ctx context.Context, username string) (*model.Admin, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	var admin model.Admin
	if err := db.Where("username = ?", username).Take(&admin).Error; err != nil {
		return nil, notFoundIfNeeded(err)
	}
	return &admin, nil
}

// TouchAdminLogin 记录管理员最后登录时间（UTC）。
// 使用 UpdateColumn 以避免 GORM 顺带刷新 updated_at（登录不算资料变更）。
func (s *Store) TouchAdminLogin(ctx context.Context, id uint64, at time.Time) error {
	db, err := s.session(ctx)
	if err != nil {
		return err
	}
	return db.Model(&model.Admin{}).Where("id = ?", id).
		UpdateColumn("last_login_at", at.UTC()).Error
}

// AdminsByIDs 按主键批量查询管理员（工单消息的 author_name 解析用）；
// ids 为空时返回空切片（不发起查询）。
func (s *Store) AdminsByIDs(ctx context.Context, ids []uint64) ([]model.Admin, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]model.Admin, 0)
	if len(ids) == 0 {
		return items, nil
	}
	if err := db.Where("id IN ?", ids).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}
