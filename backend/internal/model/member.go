// Package model 定义 Lyidc_OEM 的持久化实体（GORM 模型）与列取值常量。
package model

import "time"

// Member 是 members 表的 GORM 模型（阶段 1：会员账号）。
//
// 时间语义：created_at / updated_at / last_login_at 均为 UTC（见 docs/api-contract.md 第 1.2 节）。
type Member struct {
	ID           uint64  `gorm:"column:id;primaryKey;autoIncrement"`
	Username     string  `gorm:"column:username"`
	Email        string  `gorm:"column:email"`
	PasswordHash string  `gorm:"column:password_hash"`
	Nickname     string  `gorm:"column:nickname"`
	Phone        *string `gorm:"column:phone"`
	Status       string  `gorm:"column:status"`
	Balance      Money   `gorm:"column:balance"`
	// InviterID 是邀请人会员 ID（0 = 无邀请人）。首绑永久生效：注册时绑定后，
	// 后续任何路径（他人优惠码等）都不改绑（契约 18.4）。
	InviterID uint64 `gorm:"column:inviter_id"`
	// InviteCode 是我的邀请码（惰性生成：会员首次开通返现时才生成）；nil = 尚未生成。
	InviteCode *string `gorm:"column:invite_code"`
	// ReferralEnabled 是邀请返现开关（会员自己在后台开/关）。
	ReferralEnabled bool `gorm:"column:referral_enabled"`
	// RebateBalance 是邀请返现余额（与 Balance 完全独立，可为负；契约 18.3）。
	RebateBalance Money `gorm:"column:rebate_balance"`
	// RebatePercentOverride 是会员级返佣比例（0.00-100.00，管理员特批）；nil = 不覆盖。
	RebatePercentOverride *string    `gorm:"column:rebate_percent_override"`
	CreatedAt             time.Time  `gorm:"column:created_at"`
	UpdatedAt             time.Time  `gorm:"column:updated_at"`
	LastLoginAt           *time.Time `gorm:"column:last_login_at"`
}

// TableName 返回会员表名。
func (Member) TableName() string { return "members" }
