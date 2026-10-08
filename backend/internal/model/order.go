package model

import "time"

// 订单状态取值（orders.status）。
const (
	// OrderStatusPending 待支付：可重复发起支付，也可取消。
	OrderStatusPending = "pending"
	// OrderStatusPaid 已支付：上游交付由阶段 5 触发，本批只落状态。
	OrderStatusPaid = "paid"
	// OrderStatusCancelled 已取消：仅 pending 可取消；取消后再收到回调只告警不处理（契约见第 12 节）。
	OrderStatusCancelled = "cancelled"
)

// 支付渠道取值（orders.pay_channel / recharges.channel）。
const (
	// PayChannelBalance 余额支付（本地事务扣款，不经渠道）。
	PayChannelBalance = "balance"
)

// OrderTradeNoPrefix 是订单本地单号前缀（渠道 out_trade_no 直接复用本地单号）。
const OrderTradeNoPrefix = "O"

// IsValidOrderStatus 判断订单状态是否在 orders.status 枚举内。
func IsValidOrderStatus(status string) bool {
	switch status {
	case OrderStatusPending, OrderStatusPaid, OrderStatusCancelled:
		return true
	default:
		return false
	}
}

// Order 是 orders 表的 GORM 模型（阶段 4：创建/支付；上游交付留阶段 5）。
//
// 字段语义（契约见 docs/api-contract.md 第 12 节）：
//   - TradeNo 是本地单号，同时作为渠道 out_trade_no（重复发起支付沿用同一单号）；
//   - Amount / DiscountAmount / FinalAmount 为下单时快照，全部定点小数字符串；
//   - ConfigJSON 是所选配置项快照 {"<配置项 upstream_id>": <所选值 upstream_id>}，
//     供阶段 5 拼装上游下单参数（configoption）；
//   - CouponID / CouponCode 为下单快照；CouponCode 空串表示未用码；
//   - PayChannel / ChannelTradeNo / PayTime 在支付成功时写入（余额支付时 PayChannel=balance）。
type Order struct {
	ID             uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	TradeNo        string     `gorm:"column:trade_no"`
	MemberID       uint64     `gorm:"column:member_id"`
	ProductID      uint64     `gorm:"column:product_id"`
	ProductName    string     `gorm:"column:product_name"`
	Cycle          string     `gorm:"column:cycle"`
	Qty            int        `gorm:"column:qty"`
	ConfigJSON     string     `gorm:"column:config_json"`
	Amount         Money      `gorm:"column:amount"`
	DiscountAmount Money      `gorm:"column:discount_amount"`
	FinalAmount    Money      `gorm:"column:final_amount"`
	CouponID       *uint64    `gorm:"column:coupon_id"`
	CouponCode     string     `gorm:"column:coupon_code"`
	Status         string     `gorm:"column:status"`
	PayChannel     string     `gorm:"column:pay_channel"`
	ChannelTradeNo string     `gorm:"column:channel_trade_no"`
	PayTime        *time.Time `gorm:"column:pay_time"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
}

// TableName 返回订单表名。
func (Order) TableName() string { return "orders" }
