import type { BillingCycle, CyclePrices } from '../lib/cycles'

// 本文件的类型与 docs/api-contract.md 的响应包 examples 一一对应：
// 会员账号（6.1）、商品与计费（10.3）、优惠码校验（11.4）、订单与支付（12.3 / 12.4）。
// 契约第 2 节：所有接口统一返回 `{code, message, data}`，由 src/api/client.ts 解包。

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
