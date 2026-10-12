package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"gorm.io/gorm"
)

// 分类字段约束（契约 10.4 的 PUT /admin/categories/:id）。
const (
	categoryNameMaxLen = 128
	categorySortMin    = -999999
	categorySortMax    = 999999
)

// ErrCategoryNotSub 分类不是子分类（改挂主分类等操作只对子分类合法）。
var ErrCategoryNotSub = errors.New("该分类不是子分类")

// ErrCategoryParentInvalid 改挂目标不合法（不存在 / 是子分类 / 指向自身）。
var ErrCategoryParentInvalid = errors.New("parent_id 必须指向一个存在的主分类")

// CategoryUpdate 描述 PUT /admin/categories/:id 的可选字段：nil 表示不修改。
type CategoryUpdate struct {
	Name *string
	// ParentID 改挂到另一个主分类；指针指向 0 表示取消父级（不允许，返回错误）。
	// 仅对子分类合法；主分类传 ParentID 一律报错。
	ParentID *uint64
	Sort     *int
}

// ListProductCategories 返回全部分类（主分类在前，各自主分类下子分类按 sort 升序）。
func (s *Store) ListProductCategories(ctx context.Context) ([]model.ProductCategory, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	var items []model.ProductCategory
	if err := db.Order("parent_id IS NULL DESC, COALESCE(parent_id, id), sort, id").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// mapNotFound 把 GORM 的 ErrRecordNotFound 归一成 store.ErrNotFound（router 只认哨兵错误）。
func mapNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}

// ProductCategoryByID 按本地主键查询分类。
func (s *Store) ProductCategoryByID(ctx context.Context, id uint64) (*model.ProductCategory, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	var item model.ProductCategory
	if err := db.Where("id = ?", id).First(&item).Error; err != nil {
		return nil, mapNotFound(err)
	}
	return &item, nil
}

// UpdateProductCategory 改名 / 改排序 / 子分类改挂主分类（契约 10.4）。
//
// 改挂校验：目标必须存在且是主分类，且不能是自己；主分类不允许传 parent_id。
// name / sort 是本地字段，导入不覆盖，因此可以放心改。
func (s *Store) UpdateProductCategory(ctx context.Context, id uint64, upd CategoryUpdate) (*model.ProductCategory, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}

	var updated *model.ProductCategory
	err = db.Transaction(func(tx *gorm.DB) error {
		var current model.ProductCategory
		if err := tx.Where("id = ?", id).First(&current).Error; err != nil {
			return mapNotFound(err)
		}

		columns := map[string]any{}
		if upd.Name != nil {
			name := strings.TrimSpace(*upd.Name)
			columns["name"] = name
		}
		if upd.Sort != nil {
			columns["sort"] = *upd.Sort
		}
		if upd.ParentID != nil {
			if current.IsMain() {
				return ErrCategoryNotSub
			}
			targetID := *upd.ParentID
			if targetID == 0 || targetID == current.ID {
				return ErrCategoryParentInvalid
			}
			var target model.ProductCategory
			if err := tx.Where("id = ?", targetID).First(&target).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrCategoryParentInvalid
				}
				return err
			}
			if !target.IsMain() {
				return ErrCategoryParentInvalid
			}
			columns["parent_id"] = targetID
		}
		if len(columns) == 0 {
			return ErrNoFieldsToUpdate
		}
		columns["updated_at"] = time.Now().UTC()
		if err := tx.Model(&model.ProductCategory{}).Where("id = ?", id).Updates(columns).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ?", id).First(&updated).Error; err != nil {
			return mapNotFound(err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// ProductCategoryCounts 返回每个**子分类**下商品的三态计数（主分类由调用方汇总）。
func (s *Store) ProductCategoryCounts(ctx context.Context) (map[uint64]GroupCounts, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	type row struct {
		CategoryID uint64
		Total      int64
		On         int64 `gorm:"column:on_count"`
		Off        int64 `gorm:"column:off_count"`
	}
	var rows []row
	err = db.Model(&model.Product{}).
		Select("category_id, COUNT(*) AS total, SUM(status = 'on') AS on_count, SUM(status = 'off') AS off_count").
		Where("category_id > 0").
		Group("category_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	counts := make(map[uint64]GroupCounts, len(rows))
	for _, item := range rows {
		counts[item.CategoryID] = GroupCounts{Total: item.Total, On: item.On, Off: item.Off}
	}
	return counts, nil
}

// SubCategoryByID 查询**子分类**（parent_id 非空）；不存在或本身是主分类都返回 ErrNotFound。
// 供 PUT /admin/products/:id 校验 category_id（契约 10.4：必须挂到子分类）。
func (s *Store) SubCategoryByID(ctx context.Context, id uint64) (*model.ProductCategory, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	var item model.ProductCategory
	if err := db.Where("id = ? AND parent_id IS NOT NULL", id).First(&item).Error; err != nil {
		return nil, mapNotFound(err)
	}
	return &item, nil
}

// EnsureProductCategories 给尚未归类（category_id=0）的商品补派生两级分类。
//
// 这是导入派生的**读时兜底**（等价规则、幂等）：升级到 R7 后、尚未重跑导入期间，
// 会员端目录不能因为 category_id 全是 0 而空掉。已归类与 manual 的商品一律不动。
// groupName 传上游组名（product_groups.name）；派生规则见 internal/category。
func (s *Store) EnsureProductCategories(
	ctx context.Context,
	groups []model.ProductGroup,
	products []model.Product,
	derive func(productName, groupName string, upstreamGroupID, productSort int) model.ProductCategoryInput,
) error {
	need := false
	for _, product := range products {
		if product.CategoryID == 0 && product.CategorySource != model.CategorySourceManual {
			need = true
			break
		}
	}
	if !need {
		return nil
	}

	db, err := s.session(ctx)
	if err != nil {
		return err
	}
	groupNameByID := make(map[int]string, len(groups))
	for _, group := range groups {
		groupNameByID[group.UpstreamGroupID] = group.Name
	}

	return db.Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		inputs := make([]ProductInput, 0)
		productIDs := make([]uint64, 0)
		for _, product := range products {
			if product.CategoryID != 0 || product.CategorySource == model.CategorySourceManual {
				continue
			}
			derived := derive(product.Name, groupNameByID[product.UpstreamGroupID], product.UpstreamGroupID, product.Sort)
			inputs = append(inputs, ProductInput{Category: &derived})
			productIDs = append(productIDs, product.ID)
		}
		ids, err := importCategories(tx, inputs, now)
		if err != nil {
			return err
		}
		for i, input := range inputs {
			subID := ids[input.Category.SubKey]
			if subID == 0 {
				continue
			}
			if err := tx.Model(&model.Product{}).
				Where("id = ?", productIDs[i]).
				Updates(map[string]any{
					"category_id":     subID,
					"category_source": model.CategorySourceDerived,
				}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
