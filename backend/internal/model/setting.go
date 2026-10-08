package model

import "time"

// Setting 是 settings 表的 GORM 模型（通用后台设置，键值对）。
//
// 设计要点（契约见 docs/api-contract.md 第 12 节）：
//   - 所有「用户可设置」的内容都由本表承载，经管理端设置接口读写，不进 config.yaml；
//   - Value 为 JSON 文本，结构与键一一对应（本批：payment.epay / upstream）；
//   - UpdatedBy 记录最后修改的管理员 ID（审计用，NULL 表示没有接口写入记录）；
//   - 密钥类字段只存本表，任何日志/接口响应/错误信息都不得包含明文（接口返回掩码）。
type Setting struct {
	Key       string    `gorm:"column:key;primaryKey"`
	Value     string    `gorm:"column:value"`
	UpdatedBy *uint64   `gorm:"column:updated_by"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 返回设置表名。
func (Setting) TableName() string { return "settings" }
