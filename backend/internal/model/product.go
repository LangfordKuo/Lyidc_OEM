package model

import "time"

// 商品上架状态取值（products.status）。
const (
	// ProductStatusOn 已上架：会员端可见、可被下单（阶段 4）。
	ProductStatusOn = "on"
	// ProductStatusOff 已下架：仅管理端可见（导入的新商品默认下架）。
	ProductStatusOff = "off"
)

// ProductGroup 是 product_groups 表的 GORM 模型（上游产品组的本地映射）。
//
// 字段归属：upstream_group_id 为上游键；name / sort 是本地字段（导入只创建不覆盖）。
type ProductGroup struct {
	ID              uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	UpstreamGroupID int       `gorm:"column:upstream_group_id"`
	Name            string    `gorm:"column:name"`
	Sort            int       `gorm:"column:sort"`
	CreatedAt       time.Time `gorm:"column:created_at"`
	UpdatedAt       time.Time `gorm:"column:updated_at"`
}

// TableName 返回商品分组表名。
func (ProductGroup) TableName() string { return "product_groups" }

// Product 是 products 表的 GORM 模型（上游商品 + 本地定价与上下架）。
//
// 字段归属（导入接口的写入边界，见 docs/api-contract.md 商品与计费章节）：
//   - 上游字段（导入覆盖）：Name / Description / Type / Module / ConfigJSON /
//     UpstreamPricesJSON / StockQty / OntrialMax；
//   - 本地字段（导入不覆盖）：PricingJSON / Status / Sort。
type Product struct {
	ID                 uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	UpstreamPID        int       `gorm:"column:upstream_pid"`
	UpstreamGroupID    int       `gorm:"column:upstream_group_id"`
	Name               string    `gorm:"column:name"`
	Description        string    `gorm:"column:description"`
	Type               string    `gorm:"column:type"`
	Module             string    `gorm:"column:module"`
	ConfigJSON         string    `gorm:"column:config_json"`
	UpstreamPricesJSON string    `gorm:"column:upstream_prices_json"`
	PricingJSON        string    `gorm:"column:pricing_json"`
	StockQty           int       `gorm:"column:stock_qty"`
	OntrialMax         int       `gorm:"column:ontrial_max"`
	Status             string    `gorm:"column:status"`
	Sort               int       `gorm:"column:sort"`
	CreatedAt          time.Time `gorm:"column:created_at"`
	UpdatedAt          time.Time `gorm:"column:updated_at"`
}

// TableName 返回商品表名。
func (Product) TableName() string { return "products" }

// IsProductStatusValid 判断商品状态是否在 products.status 枚举内。
func IsProductStatusValid(status string) bool {
	switch status {
	case ProductStatusOn, ProductStatusOff:
		return true
	default:
		return false
	}
}
