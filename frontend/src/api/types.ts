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
  /** 商品简介解析出的展示行（契约 10.3 的 description_lines；空简介恒为 []，商品卡配置列表直接渲染） */
  description_lines: string[]
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
  /** 数量型配置（契约 10.3 备注）：取值下界与上界；选项型为 0。 */
  qty_minimum?: number
  qty_maximum?: number
}

export interface ConfigOption {
  id: number
  name: string
  /** 上游 option_type（选择方式编码）：控件映射与实测依据见 lib/configControl.ts（R6）。 */
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

/** 实例操作记录（契约 15.4：成功与失败尝试都留痕）。 */
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
 * 工单详情：`GET /tickets/:id` 返回**嵌套**的 `{ticket, messages}`
 * （契约 16.3，阶段 8 起后端实现与契约统一；messages 不含内部备注）。
 */
export interface TicketDetail {
  ticket: Ticket
  messages: TicketMessage[]
}

/** 提交工单与回复的响应：嵌套的 `{ticket, message}`。 */
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

// ---------------------------------------------------------------------------
// 管理端（契约 6.3 / 10.4 / 12.6 / 14.4 / 15.3 / 16.3 / 17.2 / 17.3）
//
// 类型来源：backend/internal/router 的 view 结构体与 docs/api-contract.md 的响应示例
// （两者不一致时以 router 为准）。管理端 token 走独立的 storage key（lyidc.admin.token），
// 全部接口 auth: 'admin'。
// ---------------------------------------------------------------------------

/** 管理端角色（契约 6.4 / 12.7 的角色矩阵权威定义）。 */
export type AdminRole = 'admin' | 'finance' | 'support'

export interface AdminAccount {
  id: number
  username: string
  nickname: string
  role: AdminRole
  status: MemberStatus
  created_at: string
  updated_at: string
  last_login_at: string | null
}

export interface AdminLoginResult {
  token: string
  expires_at: string
  admin: AdminAccount
}

// —— 商品与计费（管理端，契约 10.4） ——

/** 定价模式：upstream=直接用上游价、markup=上游价加价、fixed=固定覆盖价。 */
export type PricingMode = 'upstream' | 'markup' | 'fixed'

export interface PricingRule {
  mode: PricingMode
  /** 加价率（百分比，最多两位小数）；mode=markup 时必填，fixed 时可选。 */
  markup_percent?: number
  /** 固定覆盖价（周期 → 金额字符串）；mode=fixed 时至少一项。 */
  fixed?: Partial<Record<BillingCycle, string>>
}

export interface CustomField {
  id: number
  name: string
  description: string
  type: string
  required: boolean
  regexpr: string
}

export interface AdminProduct {
  id: number
  upstream_pid: number
  upstream_group_id: number
  group_id: number
  group_name: string
  name: string
  type: string
  module: string
  status: 'on' | 'off'
  sort: number
  stock_qty: number
  stock_control: number
  ontrial_max: number
  pricing: PricingRule
  /** 六周期本地售价；null 表示该周期不可售 */
  prices: CyclePrices
  created_at: string
  updated_at: string
}

/** 上游价格缓存原文（契约 10.1：`upstream_prices_json` 的解析视图）。 */
export interface UpstreamPrices {
  code: string
  prices: Record<string, string>
  rows?: unknown[]
}

export interface AdminProductDetail extends AdminProduct {
  description: string
  /** 简介展示行（R5；与会员端同口径，契约 10.3） */
  description_lines: string[]
  config_groups: ConfigGroup[]
  custom_fields: CustomField[]
  upstream_prices: UpstreamPrices
}

export interface AdminProductGroup {
  id: number
  upstream_group_id: number
  name: string
  sort: number
  products: { total: number; on: number; off: number }
  created_at: string
  updated_at: string
}

/** 上游导入结果（契约 10.4；failed=0 时不含 failed_pids）。 */
export interface ProductImportResult {
  created: number
  updated: number
  unchanged: number
  groups: number
  failed: number
  failed_pids?: number[]
}

export interface UpdateProductInput {
  pricing_json?: PricingRule | string | null
  status?: 'on' | 'off'
  sort?: number
}

// —— 订单（管理端，契约 12.6）：字段与会员端订单一致，额外回带会员概要 ——

/** 订单归属会员的概要（管理端订单视图 `member` 字段）。 */
export interface AdminOrderMember {
  id: number
  username: string
  nickname: string
  email: string
  status: MemberStatus
}

/** 会员行缺失时为 null（异常数据，不阻断订单展示）。 */
export interface AdminOrder extends Order {
  member: AdminOrderMember | null
}

// —— 实例（管理端，契约 14.4 / 15.3） ——

/** 管理端实例列表项：会员端摘要 + member_id。 */
export interface AdminInstance extends InstanceSummary {
  member_id: number
}

/** 管理端实例详情（阶段 8 新增）：会员端详情口径 + member_id。 */
export interface AdminInstanceDetail extends InstanceDetail {
  member_id: number
}

/** 同步接口返回（契约 15.3）。 */
export interface InstanceSyncResult extends InstanceActionResult {
  power_state: string
  power_desc: string
  status_changed: boolean
  /** true = 上游主机已不存在/已删除，本次已把本地收敛为 terminated。 */
  terminated: boolean
  instance: InstanceSummary
  next_due_date: string | null
  upstream_status: string
}

// —— 工单（管理端，契约 16.3） ——

export interface AdminTicket extends Ticket {
  member_id: number
  member: { id: number; username: string; nickname: string }
}

/** 管理端工单详情：嵌套 `{ticket, messages}`，消息流含内部备注。 */
export interface AdminTicketDetail {
  ticket: AdminTicket
  messages: TicketMessage[]
}

export interface AdminReplyTicketInput {
  content: string
  /** true = 内部备注（会员端不可见，状态不变）。 */
  internal?: boolean
}

/** 管理端回复响应 `{ticket, message}`（internal=true 时状态不变）。 */
export interface AdminTicketMutationResult {
  ticket: AdminTicket
  message: TicketMessage
}

/** 管理端关闭响应（already_closed=true 表示幂等重复关闭）。 */
export interface AdminTicketCloseResult {
  ticket: AdminTicket
  already_closed: boolean
}

// —— 后台设置（契约 12.1 / 17.3；全部仅 admin 角色，含读取） ——

export interface EpaySettings {
  enabled: boolean
  gateway: string
  pid: string
  key_configured: boolean
  key_masked: string
  notify_url: string
  notify_url_recommended: string
  return_url: string
  updated_by: number | null
  updated_at: string | null
}

export interface UpdateEpaySettingsInput {
  enabled?: boolean
  gateway?: string
  pid?: string
  /** 三态：省略 = 保持不变、给值 = 替换、空串 = 清空。 */
  key?: string
  notify_url?: string
  return_url?: string
}

export interface UpstreamSettings {
  base_url: string
  username: string
  api_key_configured: boolean
  api_key_masked: string
  timeout_seconds: number
  updated_by: number | null
  updated_at: string | null
}

export interface UpdateUpstreamSettingsInput {
  base_url?: string
  username?: string
  /** 三态同上。 */
  api_key?: string
  timeout_seconds?: number
}

export type SMTPEncryption = 'none' | 'starttls' | 'ssl'

export interface EmailSMTPSettings {
  enabled: boolean
  host: string
  port: number
  /** 按加密方式推导的实际端口（none→25 / starttls→587 / ssl→465）。 */
  port_effective: number
  username: string
  password_configured: boolean
  password_masked: string
  from: string
  from_name: string
  encryption: SMTPEncryption
  updated_by: number | null
  updated_at: string | null
}

export interface UpdateEmailSMTPSettingsInput {
  enabled?: boolean
  host?: string
  port?: number
  username?: string
  /** 三态同上。 */
  password?: string
  from?: string
  from_name?: string
  encryption?: SMTPEncryption
}

export interface EmailTestResult {
  sent: boolean
  to: string
}

export interface NotificationSettings {
  inapp_enabled: boolean
  email_enabled: boolean
  expiry_reminder_enabled: boolean
  expiry_reminder_days: number
  updated_by: number | null
  updated_at: string | null
}

export interface UpdateNotificationSettingsInput {
  inapp_enabled?: boolean
  email_enabled?: boolean
  expiry_reminder_enabled?: boolean
  expiry_reminder_days?: number
}

// —— 上游探活（契约第 9 节） ——

export interface UpstreamHealth {
  connected: boolean
  base_url: string
  latency_ms: number
  api_key_masked: string
  checked_at: string
  /** 仅 connected=false 时出现，内容已脱敏。 */
  error?: string
}
