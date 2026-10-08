package model

import "time"

// Admin 是 admins 表的 GORM 模型（阶段 1：后台管理员账号）。
//
// 时间语义：created_at / updated_at / last_login_at 均为 UTC（见 docs/api-contract.md 第 1.2 节）。
type Admin struct {
	ID           uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	Username     string     `gorm:"column:username"`
	PasswordHash string     `gorm:"column:password_hash"`
	Nickname     string     `gorm:"column:nickname"`
	Role         string     `gorm:"column:role"`
	Status       string     `gorm:"column:status"`
	CreatedAt    time.Time  `gorm:"column:created_at"`
	UpdatedAt    time.Time  `gorm:"column:updated_at"`
	LastLoginAt  *time.Time `gorm:"column:last_login_at"`
}

// TableName 返回管理员表名。
func (Admin) TableName() string { return "admins" }
