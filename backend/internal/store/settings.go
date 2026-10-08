package store

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

// Setting 按 key 读取一条设置；不存在返回 ErrNotFound。
func (s *Store) Setting(ctx context.Context, key string) (*model.Setting, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	return settingBy(db, key)
}

// UpsertSetting 写入（或覆盖）一条设置，并记录最后修改的管理员 ID。
// created_at 只在首次插入时写入，后续更新保持不变。
func (s *Store) UpsertSetting(ctx context.Context, key, value string, updatedBy uint64) (*model.Setting, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	setting := model.Setting{
		Key:       key,
		Value:     value,
		UpdatedBy: &updatedBy,
		CreatedAt: now,
		UpdatedAt: now,
	}
	// MySQL：INSERT ... ON DUPLICATE KEY UPDATE value / updated_by / updated_at。
	if err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_by", "updated_at"}),
	}).Create(&setting).Error; err != nil {
		return nil, err
	}
	return settingBy(db, key)
}

// settingBy 是内部按 key 查询单条设置。
func settingBy(db *gorm.DB, key string) (*model.Setting, error) {
	var setting model.Setting
	if err := db.Where("`key` = ?", key).Take(&setting).Error; err != nil {
		return nil, notFoundIfNeeded(err)
	}
	return &setting, nil
}
