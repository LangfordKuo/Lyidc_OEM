package store

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

// CouponInput 是创建优惠码的落库字段（全部已由 handler 校验与归一化）。
type CouponInput struct {
	Code       string
	Type       string
	Value      string
	CyclesJSON string
	StartsAt   *time.Time
	ExpiresAt  *time.Time
	MaxUses    int
	Status     string
	Comment    string
}

// CouponTimeUpdate 表示一个可清空的时间字段更新：Set=true 时才生效，
// Value 为 nil 表示清空（写入 NULL，即「立即生效 / 永不过期」）。
type CouponTimeUpdate struct {
	Set   bool
	Value *time.Time
}

// CouponUpdate 描述 PUT /admin/coupons/:id 的可选字段：nil 表示不修改。
type CouponUpdate struct {
	Code       *string
	Value      *string
	CyclesJSON *string
	StartsAt   CouponTimeUpdate
	ExpiresAt  CouponTimeUpdate
	MaxUses    *int
	Status     *string
	Comment    *string
}

// CouponFilter 是 GET /admin/coupons 的查询条件（page 从 1 开始）。
type CouponFilter struct {
	Page     int
	PageSize int
	Status   string
	// Keyword 按 code 模糊匹配（列排序规则大小写不敏感）。
	Keyword string
}

// CreateCoupon 创建优惠码；code 唯一键冲突时返回 ErrCouponCodeTaken（大小写不敏感）。
func (s *Store) CreateCoupon(ctx context.Context, in CouponInput) (*model.Coupon, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	coupon := model.Coupon{
		Code:       in.Code,
		Type:       in.Type,
		Value:      model.Money(in.Value),
		CyclesJSON: in.CyclesJSON,
		StartsAt:   in.StartsAt,
		ExpiresAt:  in.ExpiresAt,
		MaxUses:    in.MaxUses,
		UsedCount:  0,
		Status:     in.Status,
		Comment:    in.Comment,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := db.Create(&coupon).Error; err != nil {
		return nil, mapDuplicateError(err)
	}
	return &coupon, nil
}

// ListCoupons 按条件分页查询优惠码（新建在前），返回当页数据与总数。
func (s *Store) ListCoupons(ctx context.Context, filter CouponFilter) ([]model.Coupon, int64, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, 0, err
	}

	query := db.Model(&model.Coupon{})
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Keyword != "" {
		query = query.Where("code LIKE ?", containsKeyword(filter.Keyword))
	}

	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	items := make([]model.Coupon, 0)
	if err := query.Session(&gorm.Session{}).
		Order("id DESC").
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// CouponByID 按本地主键查询优惠码。
func (s *Store) CouponByID(ctx context.Context, id uint64) (*model.Coupon, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	return couponBy(db, "id = ?", id)
}

// CouponByCode 按 code 查询优惠码；比较走列的 utf8mb4_general_ci 排序规则，
// 因此大小写不敏感（"welcome10" 与 "WELCOME10" 命中同一条）。
func (s *Store) CouponByCode(ctx context.Context, code string) (*model.Coupon, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	return couponBy(db, "code = ?", code)
}

// UpdateCoupon 更新优惠码字段并返回最新记录。
func (s *Store) UpdateCoupon(ctx context.Context, id uint64, upd CouponUpdate) (*model.Coupon, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := couponBy(db, "id = ?", id); err != nil {
		return nil, err
	}

	columns := map[string]any{"updated_at": time.Now().UTC()}
	if upd.Code != nil {
		columns["code"] = *upd.Code
	}
	if upd.Value != nil {
		columns["value"] = *upd.Value
	}
	if upd.CyclesJSON != nil {
		columns["cycles_json"] = *upd.CyclesJSON
	}
	if upd.StartsAt.Set {
		columns["starts_at"] = timeValue(upd.StartsAt.Value)
	}
	if upd.ExpiresAt.Set {
		columns["expires_at"] = timeValue(upd.ExpiresAt.Value)
	}
	if upd.MaxUses != nil {
		columns["max_uses"] = *upd.MaxUses
	}
	if upd.Status != nil {
		columns["status"] = *upd.Status
	}
	if upd.Comment != nil {
		columns["comment"] = *upd.Comment
	}
	if err := db.Model(&model.Coupon{}).Where("id = ?", id).Updates(columns).Error; err != nil {
		return nil, mapDuplicateError(err)
	}
	return couponBy(db, "id = ?", id)
}

// couponBy 是内部按条件查询单条优惠码。
func couponBy(db *gorm.DB, query string, args ...any) (*model.Coupon, error) {
	var coupon model.Coupon
	if err := db.Where(query, args...).Take(&coupon).Error; err != nil {
		return nil, notFoundIfNeeded(err)
	}
	return &coupon, nil
}

// timeValue 把可空时间转成 GORM Updates 的列值：nil → NULL。
func timeValue(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}
