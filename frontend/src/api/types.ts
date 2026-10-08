import type { BillingCycle, CyclePrices } from '../lib/cycles'

// 本文件的类型与 docs/api-contract.md 的响应包 examples 一一对应：
// 会员账号（6.1）、商品与计费（10.3）、优惠码校验（11.4）、订单与支付（12.3 / 12.4）。

// ---------------------------------------------------------------------------
// 通用
// ---------------------------------------------------------------------------
export interface Paged<T> {
  items: T[]
  page: number
  page_size: number
  total: number
}

// ---------------------------------------------------------------------------
// 会员账号（契约 6.1 / 6.2）
// ---------------------------------------------------------------------------
export type MemberStatus = 'active' | 'disabled'

export interface Member {
  id: number
  username: string
  email: string
  nickname: string
  phone: string | null
  status: MemberStatus
  /** 定点小数字符串，如 "0.00" */
  balance: string
  created_at: string
  updated_at: string
  last_login_at: string | null
}

/** 登录返回的会员摘要（契约 6.2：仅 id / username / status）。 */
export interface MemberSummary {
  id: number
  username: string
  status: MemberStatus
  nickname?: string
  email?: string
  balance?: string
}

export interface LoginResult {
  token: string
  expires_at: string
  member: MemberSummary
}

export interface RegisterInput {
  username: string
  email: string
  password: string
}

// ---------------------------------------------------------------------------
// 商品与计费（契约 10.3）
// ---------------------------------------------------------------------------
export interface ProductSummary {
  id: number
  name: string
  type: string
  sort: number
  /** 六周期本地售价；键始终存在，null 表示该周期不可售 */
  prices: CyclePrices
  stock_qty: number
  /** 1 = 库存有效；0 = 上游不限库存 */
  stock_control: number
  /** 可试用数量，0 表示不提供试用 */
  ontrial_max: number
}

export interface ProductGroup {
  id: number
  name: string
  sort: number
  products: ProductSummary[]
}

export interface ProductCatalog {
  groups: ProductGroup[]
  total: number
}

export interface ConfigOptionValue {
  id: number
  name: string
  upstream_id: number
}

export interface ConfigOption {
  id: number
  name: string
  type: number
  upstream_id: number
  values: ConfigOptionValue[]
}

export interface ConfigGroup {
  id: number
  name: string
  options: ConfigOption[]
}

export interface ProductDetail extends ProductSummary {
  description: string
  group: { id: number; name: string }
  config_groups: ConfigGroup[]
  custom_fields: unknown[]
  updated_at: string
}

// ---------------------------------------------------------------------------
// 优惠码（契约 11.4）
// ---------------------------------------------------------------------------
export type CouponInvalidReason =
  | 'disabled'
  | 'not_started'
  | 'expired'
  | 'used_up'
  | 'cycle_not_applicable'

export interface CouponValidationValid {
  valid: true
  code: string
  type: 'percent' | 'fixed'
  value: string
  price: string
  discount_amount: string
  final_amount: string
}

export interface CouponValidationInvalid {
  valid: false
  reason: CouponInvalidReason
}

export type CouponValidation = CouponValidationValid | CouponValidationInvalid

// ---------------------------------------------------------------------------
// 订单与支付（契约 12.3 / 12.4）
// ---------------------------------------------------------------------------
export type OrderStatus =
  | 'pending'
  | 'paid'
  | 'provisioning'
  | 'active'
  | 'failed'
  | 'cancelled'

export type OrderType = 'new' | 'renew'

export interface Order {
  id: number
  trade_no: string
  member_id: number
  product_id: number
  product_name: string
  cycle: BillingCycle
  qty: number
  /** 所选配置项快照 {配置项 id: 所选值 id} */
  config: Record<string, string>
  amount: string
  discount_amount: string
  final_amount: string
  coupon_code: string
  status: OrderStatus
  type: OrderType
  pay_channel: string
  channel_trade_no: string
  pay_time: string | null
  host_id: number | null
  instance_id: number | null
  provision_error: string
  delivered_at: string | null
  created_at: string
  updated_at: string
}

export interface CreateOrderInput {
  product_id: number
  cycle: BillingCycle
  /** 键为该商品可配置项的 id，值为所选可选值的 id */
  config?: Record<string, string>
  coupon_code?: string
}

export type PayChannel = 'epay' | 'balance'
export type EpayType = 'alipay' | 'wxpay'

export interface PayInfo {
  channel: PayChannel
  pay_type?: EpayType
  channel_trade_no?: string
  /** 在线支付跳转地址（channel=epay 时存在） */
  payurl?: string
  /** 余额支付是否已入账 */
  paid?: boolean
  /** 余额支付后的余额 */
  balance_after?: string
  extra?: Record<string, string>
}

export interface OrderPayResult {
  order: Order
  pay: PayInfo
}

export interface PayOrderInput {
  channel: PayChannel
  pay_type?: EpayType
}

export interface BalanceInfo {
  member_id: number
  balance: string
}

// ---------------------------------------------------------------------------
// 实例与实例操作（契约 14.4 / 15.1 / 15.2 / 15.8）
// ---------------------------------------------------------------------------
export type InstanceStatus = 'active' | 'suspended' | 'cancelled' | 'terminated'

/** 取消申请标记：与 status 正交（契约 15.8.1）。 */
export type CancelStatus = 'none' | 'pending' | 'done'
export type CancelType = 'immediate' | 'end_of_billing'

/** 实例摘要：列表与详情共有字段（不含敏感字段，契约 14.4）。 */
export interface InstanceSummary {
  id: number
  order_id: number
  host_id: number
  product_id: number
  product_name: string
  name: string
  billing_cycle: BillingCycle
  next_due_date: string | null
  status: InstanceStatus
  upstream_status: string
  dedicated_ip: string
  cancel_status: CancelStatus
  cancel_type: CancelType | ''
  cancel_request_id: number
  cancel_requested_at: string | null
  created_at: string
}

/** 实例详情：摘要 + 敏感字段（仅会员本人可见，契约 14.2）。 */
export interface InstanceDetail extends InstanceSummary {
  assigned_ips: string[]
  port: number
  username: string
  password: string
  updated_at: string
}

/** 电源操作（契约 15.1 的操作矩阵；hard_* 为强制操作，需二次确认）。 */
export type PowerOp = 'soft_on' | 'soft_off' | 'reboot' | 'hard_off' | 'hard_reboot'

/** 实例操作响应（电源 / 重装 / 改密 / 取消共用；改密多回带 password）。 */
export interface InstanceActionResult {
  instance_id: number
  action: string
  message: string
  status: string
  password?: string
}

export interface InstanceLog {
  id: number
  instance_id: number
  actor_type: 'member' | 'admin' | 'system'
  actor_id: number
  action: string
  status: 'success' | 'fail'
  message: string
  created_at: string
}

export interface ReinstallOption {
  id: number
  name: string
  group: string
}

export interface ReinstallOptions {
  instance_id: number
  os: ReinstallOption[]
  groups: { id: string; name: string }[]
}

export interface CancelInstanceInput {
  type: CancelType
  reason?: string
}

export interface CancelResult extends InstanceActionResult {
  cancel_request_id: number
  cancel_type: CancelType | ''
  cancel_status: CancelStatus
  cancel_requested_at: string | null
  /** true = 已有在途申请，本次未重复提交上游（幂等返回）。 */
  duplicate: boolean
}

// ---------------------------------------------------------------------------
// 工单（契约 16.3）
// ---------------------------------------------------------------------------
export type TicketStatus = 'open' | 'replied' | 'closed'
export type TicketCategory = 'technical' | 'billing' | 'other'
export type TicketAuthorType = 'member' | 'admin'

export interface TicketInstanceRef {
  id: number
  name: string
  product_name: string
  status: InstanceStatus
}

export interface Ticket {
  id: number
  trade_no: string
  subject: string
  category: TicketCategory
  status: TicketStatus
  instance_id: number | null
  instance: TicketInstanceRef | null
  last_reply_at: string
  closed_at: string | null
  created_at: string
  updated_at: string
}

/** 消息（会员端响应中 internal 恒为 false，内部备注不返回）。 */
export interface TicketMessage {
  id: number
  author_type: TicketAuthorType
  author_id: number
  author_name: string
  content: string
  internal: boolean
  created_at: string
}

/**
 * 工单详情：会员端 `GET /tickets/:id` 把工单字段**平铺**在 data 里，再加 `messages`
 * （实现口径，契约 16.3 写作「ticket + messages」；以实际响应为准）。
 */
export type TicketDetail = Ticket & { messages: TicketMessage[] }

/** 提交工单与回复的响应：这里是嵌套的 `{ticket, message}`。 */
export interface TicketMutationResult {
  ticket: Ticket
  message: TicketMessage
}

export interface CreateTicketInput {
  subject: string
  content: string
  category: TicketCategory
  instance_id?: number
}

export type TicketReplyResult = TicketMutationResult

export interface TicketCloseResult {
  ticket: Ticket
  already_closed: boolean
}

// ---------------------------------------------------------------------------
// 站内通知（契约 17.2）
// ---------------------------------------------------------------------------
export type NotificationEvent =
  | 'order_delivered'
  | 'order_failed'
  | 'renew_succeeded'
  | 'instance_suspended'
  | 'instance_terminated'
  | 'ticket_created'
  | 'ticket_replied'
  | 'ticket_closed'
  | 'expiry_reminder'

export interface Notification {
  id: number
  event: NotificationEvent
  title: string
  content: string
  /** false = 未读（read_at 为 null）。 */
  read: boolean
  read_at: string | null
  created_at: string
}

/** 通知列表回带未读数（契约 17.2：`unread` 与 items 同层）。 */
export interface NotificationPage {
  items: Notification[]
  page: number
  page_size: number
  total: number
  unread: number
}

export interface NotificationUnread {
  unread: number
}

export interface NotificationReadResult {
  notification: Notification
  already_read: boolean
}

export interface NotificationReadAllResult {
  updated: number
  unread: number
}

// ---------------------------------------------------------------------------
// 充值单与余额流水（契约 12.3 / 12.4）
// ---------------------------------------------------------------------------
export type RechargeStatus = 'pending' | 'paid' | 'closed'

export interface Recharge {
  id: number
  trade_no: string
  member_id: number
  amount: string
  channel: string
  status: RechargeStatus
  channel_trade_no: string | null
  created_at: string
  paid_at: string | null
  expires_at: string | null
}

export interface CreateRechargeInput {
  amount: string
  channel: 'epay'
  pay_type?: EpayType
}

export interface RechargeResult {
  recharge: Recharge
  pay: PayInfo
}

export type LedgerType = 'recharge' | 'order_pay' | 'refund' | 'adjust'

export interface LedgerEntry {
  id: number
  member_id: number
  type: LedgerType
  /** 有符号金额：入账为正、出账为负。 */
  amount: string
  balance_before: string
  balance_after: string
  ref_type: string
  ref_id: number
  note: string
  created_at: string
}
