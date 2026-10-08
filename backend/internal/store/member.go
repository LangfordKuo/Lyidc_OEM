package store

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

// MemberProfileUpdate 描述 PUT /members/me 的可选字段：nil 表示不修改。
// Phone 指向空字符串表示清空手机号（数据库写 NULL）。
type MemberProfileUpdate struct {
	Nickname *string
	Phone    *string
	Email    *string
}

// MemberFilter 是 GET /admin/members 的查询条件（page 从 1 开始）。
type MemberFilter struct {
	Page     int
	PageSize int
	Username string
	Email    string
	Status   string
}

// CreateMember 写入新会员；username/email 重复时返回 ErrUsernameTaken / ErrEmailTaken。
func (s *Store) CreateMember(ctx context.Context, member *model.Member) error {
	db, err := s.session(ctx)
	if err != nil {
		return err
	}
	if err := db.Create(member).Error; err != nil {
		return mapDuplicateError(err)
	}
	return nil
}

// MemberByID 按主键查询会员。
func (s *Store) MemberByID(ctx context.Context, id uint64) (*model.Member, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	return memberByID(db, id)
}

// MemberByUsername 按用户名查询会员（utf8mb4_general_ci 大小写不敏感）。
func (s *Store) MemberByUsername(ctx context.Context, username string) (*model.Member, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	return memberBy(db, "username = ?", username)
}

// MemberEmailTaken 判断邮箱是否已被其它会员占用（excludeID 用于“改自己资料”场景）。
func (s *Store) MemberEmailTaken(ctx context.Context, email string, excludeID uint64) (bool, error) {
	db, err := s.session(ctx)
	if err != nil {
		return false, err
	}
	var count int64
	if err := db.Model(&model.Member{}).
		Where("email = ? AND id <> ?", email, excludeID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// UpdateMemberProfile 更新昵称/手机号/邮箱并返回最新记录。
func (s *Store) UpdateMemberProfile(ctx context.Context, id uint64, upd MemberProfileUpdate) (*model.Member, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := memberByID(db, id); err != nil {
		return nil, err
	}

	columns := map[string]any{"updated_at": time.Now().UTC()}
	if upd.Nickname != nil {
		columns["nickname"] = *upd.Nickname
	}
	if upd.Phone != nil {
		if *upd.Phone == "" {
			columns["phone"] = nil
		} else {
			columns["phone"] = *upd.Phone
		}
	}
	if upd.Email != nil {
		columns["email"] = *upd.Email
	}

	if err := db.Model(&model.Member{}).Where("id = ?", id).Updates(columns).Error; err != nil {
		return nil, mapDuplicateError(err)
	}
	return memberByID(db, id)
}

// UpdateMemberPassword 覆盖会员密码哈希。
func (s *Store) UpdateMemberPassword(ctx context.Context, id uint64, passwordHash string) error {
	db, err := s.session(ctx)
	if err != nil {
		return err
	}
	return db.Model(&model.Member{}).Where("id = ?", id).
		Updates(map[string]any{
			"password_hash": passwordHash,
			"updated_at":    time.Now().UTC(),
		}).Error
}

// UpdateMemberStatus 修改会员状态（active/disabled）并返回最新记录。
func (s *Store) UpdateMemberStatus(ctx context.Context, id uint64, status string) (*model.Member, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := memberByID(db, id); err != nil {
		return nil, err
	}
	if err := db.Model(&model.Member{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":     status,
			"updated_at": time.Now().UTC(),
		}).Error; err != nil {
		return nil, err
	}
	return memberByID(db, id)
}

// TouchMemberLogin 记录会员最后登录时间（UTC）。
// 使用 UpdateColumn 以避免 GORM 顺带刷新 updated_at（登录不算资料变更）。
func (s *Store) TouchMemberLogin(ctx context.Context, id uint64, at time.Time) error {
	db, err := s.session(ctx)
	if err != nil {
		return err
	}
	return db.Model(&model.Member{}).Where("id = ?", id).
		UpdateColumn("last_login_at", at.UTC()).Error
}

// ListMembers 按条件分页查询会员，返回当页数据与总数（按 id 倒序）。
func (s *Store) ListMembers(ctx context.Context, filter MemberFilter) ([]model.Member, int64, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, 0, err
	}

	query := db.Model(&model.Member{})
	if filter.Username != "" {
		query = query.Where("username LIKE ?", containsKeyword(filter.Username))
	}
	if filter.Email != "" {
		query = query.Where("email LIKE ?", containsKeyword(filter.Email))
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}

	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	items := make([]model.Member, 0)
	if err := query.Session(&gorm.Session{}).
		Order("id DESC").
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// MembersByIDs 按主键批量查询会员（管理端工单列表/详情组装 member 概要用）；
// ids 为空时返回空切片（不发起查询）。
func (s *Store) MembersByIDs(ctx context.Context, ids []uint64) ([]model.Member, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]model.Member, 0)
	if len(ids) == 0 {
		return items, nil
	}
	if err := db.Where("id IN ?", ids).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// memberByID 是内部按主键查询，复用已有 GORM 句柄。
func memberByID(db *gorm.DB, id uint64) (*model.Member, error) {
	return memberBy(db, "id = ?", id)
}

// memberBy 是内部按条件查询单条会员。
func memberBy(db *gorm.DB, query string, args ...any) (*model.Member, error) {
	var member model.Member
	if err := db.Where(query, args...).Take(&member).Error; err != nil {
		return nil, notFoundIfNeeded(err)
	}
	return &member, nil
}
