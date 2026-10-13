package store

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

// ListGroupCategoryMaps 返回全部「上游分组 → 本地子分类」映射（R12，契约 10.4），
// 按 upstream_group_id 升序（数量级 = 上游分组数，一次性返回，不分页）。
func (s *Store) ListGroupCategoryMaps(ctx context.Context) ([]model.ProductGroupCategoryMap, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]model.ProductGroupCategoryMap, 0)
	if err := db.Order("upstream_group_id ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// GroupCategoryMapByUpstreamID 按上游分组 ID 查询映射；不存在返回 ErrNotFound。
func (s *Store) GroupCategoryMapByUpstreamID(ctx context.Context, upstreamGroupID int) (*model.ProductGroupCategoryMap, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	var item model.ProductGroupCategoryMap
	if err := db.Where("upstream_group_id = ?", upstreamGroupID).First(&item).Error; err != nil {
		return nil, mapNotFound(err)
	}
	return &item, nil
}

// UpsertGroupCategoryMap 新增或更新某上游分组的映射（契约 10.4 的 PUT）。
//
// 唯一键 upstream_group_id 决定 upsert 语义：已存在则改 category_id，否则新建。
// **调用方必须先校验 categoryID 指向一个存在的子分类**（store 不重复查分类表，
// 保持「store 只做落库、router 做业务校验」的既有分工）。
func (s *Store) UpsertGroupCategoryMap(ctx context.Context, upstreamGroupID int, categoryID uint64) (*model.ProductGroupCategoryMap, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}

	var result *model.ProductGroupCategoryMap
	err = db.Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		var current model.ProductGroupCategoryMap
		found := true
		if err := tx.Where("upstream_group_id = ?", upstreamGroupID).First(&current).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			found = false
		}

		if !found {
			created := model.ProductGroupCategoryMap{
				UpstreamGroupID: upstreamGroupID,
				CategoryID:      categoryID,
				CreatedAt:       now,
				UpdatedAt:       now,
			}
			if err := tx.Create(&created).Error; err != nil {
				return err
			}
			result = &created
			return nil
		}

		if current.CategoryID != categoryID {
			if err := tx.Model(&model.ProductGroupCategoryMap{}).Where("id = ?", current.ID).
				Updates(map[string]any{"category_id": categoryID, "updated_at": now}).Error; err != nil {
				return err
			}
			current.CategoryID = categoryID
		}
		current.UpdatedAt = now
		result = &current
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// DeleteGroupCategoryMap 删除某上游分组的映射；不存在返回 ErrNotFound（契约 10.4 的 DELETE）。
func (s *Store) DeleteGroupCategoryMap(ctx context.Context, upstreamGroupID int) error {
	db, err := s.session(ctx)
	if err != nil {
		return err
	}

	result := db.Where("upstream_group_id = ?", upstreamGroupID).Delete(&model.ProductGroupCategoryMap{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
