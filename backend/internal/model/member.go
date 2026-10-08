// Package model 定义 Lyidc_OEM 的持久化实体（GORM 模型）与列取值常量。
package model

import "time"

// Member 是 members 表的 GORM 模型（阶段 1：会员账号）。
//
// 时间语义：created_at / updated_at / last_login_at 均为 UTC（见 docs/api-contract.md 第 1.2 节）。
type Member struct {
	ID           uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	Username     string     `gorm:"column:username"`
	Email        string     `gorm:"column:email"`
	PasswordHash string     `gorm:"column:password_hash"`
	Nickname     string     `gorm:"column:nickname"`
	Phone        *string    `gorm:"column:phone"`
	Status       string     `gorm:"column:status"`
	Balance      Money      `gorm:"column:balance"`
	CreatedAt    time.Time  `gorm:"column:created_at"`
	UpdatedAt    time.Time  `gorm:"column:updated_at"`
	LastLoginAt  *time.Time `gorm:"column:last_login_at"`
}

// TableName 返回会员表名。
func (Member) TableName() string { return "members" }
