package store

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

// DefaultAdminPasswordHash 是迁移 0003 写入的开发默认管理员（admin / admin123456）的 bcrypt 哈希。
//
// 该哈希是「未修改的默认管理员」的唯一判据（bcrypt 加盐随机，复刻不出来）：
// 安装向导第 4 步把它改写成安装者账号，保证安装完成后库内不再存在该哈希的行（契约 13.3）。
const DefaultAdminPasswordHash = "$2a$10$qAK3HvL3qCHfyUPNbaXHzeao1T.IspaU9fy/9H15xgtM5av.ZZIdW"

// InstallerAdminResult 是 EnsureInstallerAdmin 的执行结果。
type InstallerAdminResult struct {
	// AdminID 是最终的管理员 ID（复用默认行的原 id 或新建行的 id）。
	AdminID uint64
	// ReplacedDefault 表示复用了迁移 0003 的默认管理员行（改写为安装者账号）。
	ReplacedDefault bool
	// Created 表示库内原本没有默认管理员行，本次为新建。
	Created bool
}

// EnsureInstallerAdmin 在一个事务内建立安装者管理员账号，并清掉「未修改的默认管理员」行。
//
// 步骤（契约 13.3）：
//  1. 锁定并取出密码哈希等于默认哈希的行（正常最多一行）；
//  2. 目标用户名不得被**其它**管理员占用（即将被改写的默认行不算占用），否则返回 ErrUsernameTaken；
//  3. 有默认行 → 复用该行（保留主键 id）改写为安装者账号；无默认行 → 新建；
//  4. 极端情况下（默认行被复制出多行）删除其余默认行，确保库内不残留默认凭据。
func (s *Store) EnsureInstallerAdmin(ctx context.Context, username, passwordHash, nickname string) (InstallerAdminResult, error) {
	db, err := s.session(ctx)
	if err != nil {
		return InstallerAdminResult{}, err
	}

	var result InstallerAdminResult
	err = db.Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()

		var defaults []model.Admin
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("password_hash = ?", DefaultAdminPasswordHash).
			Order("id").Find(&defaults).Error; err != nil {
			return err
		}

		var taken int64
		if err := tx.Model(&model.Admin{}).
			Where("username = ?", username).
			Where("password_hash <> ?", DefaultAdminPasswordHash).
			Count(&taken).Error; err != nil {
			return err
		}
		if taken > 0 {
			return ErrUsernameTaken
		}

		fields := map[string]any{
			"username":      username,
			"password_hash": passwordHash,
			"nickname":      nickname,
			"role":          model.RoleAdmin,
			"status":        model.StatusActive,
			"last_login_at": nil,
			"updated_at":    now,
		}

		if len(defaults) > 0 {
			if err := tx.Model(&model.Admin{}).Where("id = ?", defaults[0].ID).
				Updates(fields).Error; err != nil {
				return mapDuplicateError(err)
			}
			extra := make([]uint64, 0, len(defaults)-1)
			for _, item := range defaults[1:] {
				extra = append(extra, item.ID)
			}
			if len(extra) > 0 {
				if err := tx.Where("id IN ?", extra).Delete(&model.Admin{}).Error; err != nil {
					return err
				}
			}
			result = InstallerAdminResult{AdminID: defaults[0].ID, ReplacedDefault: true}
			return nil
		}

		admin := model.Admin{
			Username:     username,
			PasswordHash: passwordHash,
			Nickname:     nickname,
			Role:         model.RoleAdmin,
			Status:       model.StatusActive,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := tx.Create(&admin).Error; err != nil {
			return mapDuplicateError(err)
		}
		result = InstallerAdminResult{AdminID: admin.ID, Created: true}
		return nil
	})
	if err != nil {
		return InstallerAdminResult{}, err
	}
	return result, nil
}

// UpdateAdminAccount 改写指定管理员的账号信息（安装向导重复执行第 4 步时的幂等更新）。
func (s *Store) UpdateAdminAccount(ctx context.Context, id uint64, username, passwordHash, nickname string) error {
	db, err := s.session(ctx)
	if err != nil {
		return err
	}
	updates := map[string]any{
		"username":      username,
		"password_hash": passwordHash,
		"nickname":      nickname,
	}
	if err := db.Model(&model.Admin{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return mapDuplicateError(err)
	}
	return nil
}

// AdminStats 返回管理员总数与其中「未修改的默认管理员」行数（安装状态判定与向导进度用）。
func (s *Store) AdminStats(ctx context.Context) (total, unmodifiedDefault int64, err error) {
	db, err := s.session(ctx)
	if err != nil {
		return 0, 0, err
	}
	if err := db.Model(&model.Admin{}).Count(&total).Error; err != nil {
		return 0, 0, err
	}
	if err := db.Model(&model.Admin{}).
		Where("password_hash = ?", DefaultAdminPasswordHash).
		Count(&unmodifiedDefault).Error; err != nil {
		return 0, 0, err
	}
	return total, unmodifiedDefault, nil
}

// InstallerAdmin 返回「安装者管理员」（密码哈希不等于默认哈希、id 最小的一行）；不存在返回 nil。
// 安装状态视图据此展示「库里实际已就绪的管理员」（契约 13.3.1），避免界面依赖表单瞬时值。
func (s *Store) InstallerAdmin(ctx context.Context) (*model.Admin, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	var admin model.Admin
	err = db.Where("password_hash <> ?", DefaultAdminPasswordHash).Order("id").Take(&admin).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &admin, nil
}

// InstallerAdminID 返回「安装者管理员」的 ID；不存在时返回 0。
// 用于安装向导写 settings 时的审计归属（契约 13.3）。
func (s *Store) InstallerAdminID(ctx context.Context) (uint64, error) {
	admin, err := s.InstallerAdmin(ctx)
	if err != nil || admin == nil {
		return 0, err
	}
	return admin.ID, nil
}
