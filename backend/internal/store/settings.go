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
	return s.UpsertSettingBy(ctx, key, value, &updatedBy)
}

// UpsertSettingBy 同上，但操作者可为空（nil 表示没有具体管理员，如安装向导写站点信息）。
func (s *Store) UpsertSettingBy(ctx context.Context, key, value string, updatedBy *uint64) (*model.Setting, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	setting := model.Setting{
		Key:       key,
		Value:     value,
		UpdatedBy: updatedBy,
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

// InsertSettingIfAbsent 仅在键不存在时插入设置，返回「本次调用是否真的写入了」。
//
// 用于 installed 标记这类**一次性**写入（契约 13.5）：重复调用与并发调用中只有第一次返回 true，
// 其余返回 false（键已存在）。updated_by 留空（安装阶段还没有管理员账号可用）。
func (s *Store) InsertSettingIfAbsent(ctx context.Context, key, value string) (bool, error) {
	db, err := s.session(ctx)
	if err != nil {
		return false, err
	}

	now := time.Now().UTC()
	setting := model.Setting{Key: key, Value: value, CreatedAt: now, UpdatedAt: now}
	// MySQL：ON DUPLICATE KEY UPDATE key=key（GORM 对 DoNothing 的写法）——
	// 冲突时 MySQL 返回受影响行数 0，据此判断是否由本次调用写入。
	result := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&setting)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// DeleteSetting 删除一条设置（不存在时视为成功）。
func (s *Store) DeleteSetting(ctx context.Context, key string) error {
	db, err := s.session(ctx)
	if err != nil {
		return err
	}
	return db.Where("`key` = ?", key).Delete(&model.Setting{}).Error
}

// settingBy 是内部按 key 查询单条设置。
func settingBy(db *gorm.DB, key string) (*model.Setting, error) {
	var setting model.Setting
	if err := db.Where("`key` = ?", key).Take(&setting).Error; err != nil {
		return nil, notFoundIfNeeded(err)
	}
	return &setting, nil
}
