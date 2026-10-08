package model

import "time"

// 订单状态取值（orders.status，阶段 5a 扩为 6 态）。
const (
	// OrderStatusPending 待支付：可重复发起支付，也可取消。
	OrderStatusPending = "pending"
	// OrderStatusPaid 已支付待交付：支付成功后的初始态，交付触发后转 provisioning。
	OrderStatusPaid = "paid"
	// OrderStatusProvisioning 交付中：已认领交付（正在调上游开通），不可再次触发/取消。
	OrderStatusProvisioning = "provisioning"
	// OrderStatusActive 已交付：上游开通成功、实例已落库。
	OrderStatusActive = "active"
	// OrderStatusFailed 交付失败：记录 provision_error，可由管理员重试（failed → provisioning → …）。
	OrderStatusFailed = "failed"
	// OrderStatusCancelled 已取消：仅 pending 可取消；取消后再收到回调只告警不处理（契约见第 12 节）。
	OrderStatusCancelled = "cancelled"
)

// 订单类型取值（orders.type，阶段 5b）。
const (
	// OrderTypeNew 新购：交付 = 上游开通（CreateHost）→ 落 instances。
	OrderTypeNew = "new"
	// OrderTypeRenew 续费：交付 = 上游续费（RenewHost）→ 更新 instances 到期时间。
	OrderTypeRenew = "renew"
)

// IsValidOrderType 判断订单类型是否在 orders.type 枚举内。
func IsValidOrderType(orderType string) bool {
	switch orderType {
	case OrderTypeNew, OrderTypeRenew:
		return true
	default:
		return false
	}
}

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
	case OrderStatusPending, OrderStatusPaid, OrderStatusProvisioning,
		OrderStatusActive, OrderStatusFailed, OrderStatusCancelled:
		return true
	default:
		return false
	}
}

// IsOrderSettled 判断订单是否已入账（支付成功及其后的全部交付状态）。
//
// 支付入账的幂等锚点（契约 14.1）：这些状态下重复回调 / 重复余额支付一律按
// OutcomeAlreadyPaid 处理——不重复入账、不重复计数、不重复触发交付。
func IsOrderSettled(status string) bool {
	switch status {
	case OrderStatusPaid, OrderStatusProvisioning, OrderStatusActive, OrderStatusFailed:
		return true
	default:
		return false
	}
}

// Order 是 orders 表的 GORM 模型（阶段 4：创建/支付；阶段 5a：交付与实例绑定）。
//
// 字段语义（契约见 docs/api-contract.md 第 12 / 14 节）：
//   - TradeNo 是本地单号，同时作为渠道 out_trade_no（重复发起支付沿用同一单号）；
//   - Amount / DiscountAmount / FinalAmount 为下单时快照，全部定点小数字符串；
//   - ConfigJSON 是所选配置项快照 {"<配置项 id>": "<所选值 id>"}（阶段 5a 直达口径，见契约 14.5），
//     交付时用于原样拼装上游 configoption[<配置项 id>]=<所选值 id>；
//   - CouponID / CouponCode 为下单快照；CouponCode 空串表示未用码；
//   - PayChannel / ChannelTradeNo / PayTime 在支付成功时写入（余额支付时 PayChannel=balance）；
//   - HostID / ProvisionError / DeliveredAt 在交付链路写入（阶段 5a）：host_id 为上游主机 ID，
//     provision_error 为最近一次失败原因（成功清空），delivered_at 为交付完成时间；
//   - Type / InstanceID 由阶段 5b 引入（迁移 0008）：type 区分新购（new）/ 续费（renew），
//     续费单记录 instance_id（新购单为 NULL，实例由 instances.order_id 反向关联）。
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
	Type           string     `gorm:"column:type"`
	PayChannel     string     `gorm:"column:pay_channel"`
	ChannelTradeNo string     `gorm:"column:channel_trade_no"`
	PayTime        *time.Time `gorm:"column:pay_time"`
	HostID         *int       `gorm:"column:host_id"`
	InstanceID     *uint64    `gorm:"column:instance_id"`
	ProvisionError string     `gorm:"column:provision_error"`
	DeliveredAt    *time.Time `gorm:"column:delivered_at"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
}

// TableName 返回订单表名。
func (Order) TableName() string { return "orders" }
