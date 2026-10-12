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

// 商品分类归属来源（products.category_source，R7 契约 10.1）。
const (
	// CategorySourceDerived 导入按商品名/上游组名派生，可被后续导入刷新。
	CategorySourceDerived = "derived"
	// CategorySourceManual 管理员手工改挂，后续导入不再覆盖。
	CategorySourceManual = "manual"
)

// ProductCategory 是 product_categories 表的 GORM 模型（R7 两级分类，迁移 0014）。
//
// 两级：parent_id 为 NULL 表示主分类，非空表示子分类并指向主分类。
// 字段归属：key 是派生稳定键（导入按它 find-or-create，不可改）；name / sort 是本地字段
// （首次派生写入，之后只能在管理端改）。
type ProductCategory struct {
	ID        uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	ParentID  *uint64   `gorm:"column:parent_id"`
	Key       string    `gorm:"column:key"`
	Name      string    `gorm:"column:name"`
	Sort      int       `gorm:"column:sort"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 返回商品分类表名。
func (ProductCategory) TableName() string { return "product_categories" }

// IsMain 判断是否为主分类（parent_id 为空）。
func (c ProductCategory) IsMain() bool { return c.ParentID == nil }

// ParentIDValue 返回父分类 id；主分类（无父级）返回 0。
func (c ProductCategory) ParentIDValue() uint64 {
	if c.ParentID == nil {
		return 0
	}
	return *c.ParentID
}

// ProductCategoryInput 是导入派生出的一个分类归属（主分类 + 子分类名）。
type ProductCategoryInput struct {
	// MainKey / MainName 是主分类的稳定键与展示名（首次创建时写入 name）。
	MainKey  string
	MainName string
	// SubKey / SubName 是子分类的稳定键与展示名。
	SubKey  string
	SubName string
	// MainSort / SubSort 是首次创建时写入的排序。
	MainSort int
	SubSort  int
}

// Product 是 products 表的 GORM 模型（上游商品 + 本地定价与上下架）。
//
// 字段归属（导入接口的写入边界，见 docs/api-contract.md 商品与计费章节）：
//   - 上游字段（导入覆盖）：Name / Description / Type / Module / ConfigJSON /
//     UpstreamPricesJSON / StockQty / OntrialMax；
//   - 本地字段（导入不覆盖）：PricingJSON / Status / Sort。
type Product struct {
	ID              uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	UpstreamPID     int    `gorm:"column:upstream_pid"`
	UpstreamGroupID int    `gorm:"column:upstream_group_id"`
	// CategoryID 是 R7 所属子分类（product_categories.id，parent_id 非空的行）；0 = 尚未归类。
	CategoryID uint64 `gorm:"column:category_id"`
	// CategorySource 标记归属来源（derived / manual）；manual 时导入不覆盖（契约 10.1）。
	CategorySource     string    `gorm:"column:category_source"`
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
