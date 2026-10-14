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
  /** 会员级返佣特批比例（契约 18.5 / 6.3）：字符串设值、null = 不覆盖（走全局默认）。 */
  rebate_percent_override?: string | null
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
  /** 邀请码（契约 18.5）：可选；非法/不存在/自邀由后端 40002 判定，前端原样展示 message。 */
  invite_code?: string
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
  /** R7：所属主分类（两级分类） */
  parent: CategoryRef
  products: ProductSummary[]
}

/** 主分类精简引用（R7） */
export interface CategoryRef {
  id: number
  name: string
}

/** 主分类下的子分类索引（R7，两级筛选用） */
export interface Subcategory {
  id: number
  name: string
  sort: number
  product_count: number
}

/** 主分类（R7） */
export interface ProductCategory {
  id: number
  name: string
  sort: number
  subcategories: Subcategory[]
}

export interface ProductCatalog {
  /** R7：主分类树，供「主分类 → 子分类」两级筛选 */
  categories: ProductCategory[]
  /** R7 起语义为**子分类**（骨架 id/name/sort/products 不变，新增 parent） */
  groups: ProductGroup[]
  total: number
}

/** 六周期配置差价（R7，契约 10.3 第 8 条）；null = 该周期无差价数据（按 0 计） */
export type ConfigValuePrices = Record<BillingCycle, string | null>

export interface ConfigOptionValue {
  id: number
  name: string
  upstream_id: number
  /** 数量型配置（契约 10.3 备注）：取值下界与上界；选项型为 0。
   *  R11 起数量型这两个字段是**全部档位的并集**（只给数量控件定 min/max 与默认值）；
   *  挑档位请用 ConfigOption.tiers。 */
  qty_minimum?: number
  qty_maximum?: number
  /** R7：该值六周期配置差价（已按定价模式换算）；数量型为**单位差价** */
  pricings?: ConfigValuePrices
}

/**
 * 数量型配置的一个价格档位（R11，契约 10.3 第 6 条）。
 *
 * `qty_min`/`qty_max` 是**该档真实区间**（不是并集）；数量落在哪一档就用哪一档的
 * `pricings`（单位差价），可计费数量 = 数量 − 低一档的 `qty_max`。
 * 仅数量型下发；选项型不出现 `tiers` 键。
 */
export interface ConfigTier {
  /** 对应 values[].id（同一子项 = 同一档位） */
  value_id: number
  qty_min: number
  qty_max: number
  pricings?: ConfigValuePrices
}

export interface ConfigOption {
  id: number
  name: string
  /** 上游 option_type（选择方式编码）：控件映射与实测依据见 lib/configControl.ts（R6）。 */
  type: number
  upstream_id: number
  values: ConfigOptionValue[]
  /** R11：数量型的真实档位区间（按 qty_min 升序）。挑档位的唯一依据。 */
  tiers?: ConfigTier[]
}

export interface ConfigGroup {
  id: number
  name: string
  options: ConfigOption[]
}

export interface ProductDetail extends ProductSummary {
  description: string
  group: CategoryRef & { parent: CategoryRef }
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

/**
 * R7 成交价明细（契约 10.2.1 / 12.4）：成交价 = base + Σ deltas[].amount。
 * 服务端与前端同口径，前端只做预览，下单仍以服务端计算为准。
 */
export interface PriceDeltaLine {
  option_id: number
  option_name: string
  /** select=选项型（value_id 有值）/ quantity=数量型（qty/billable 有值） */
  kind: 'select' | 'quantity'
  value_id?: number
  qty?: number
  /** 单位差价（数量型为单位价；已按定价模式换算） */
  unit: string
  billable?: number
  amount: string
  /** 「无差价数据」时该项按 0.00 计 */
  note?: string
}

export interface PriceBreakdown {
  /** 基础价（不含配置差价） */
  base: string
  deltas: PriceDeltaLine[]
  /** base + Σ deltas[].amount */
  total: string
}

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
  /** R7 成交价差价明细快照（契约 10.2.1 / 12.4）；存量订单为 null */
  price_breakdown: PriceBreakdown | null
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

/**
 * 余额流水类型（契约 12.3 / 18.1）：R10-A 起枚举扩展 `referral_rebate`
 * （返佣发放/冲回/提现冻结/退回，金额有符号；会员端 `GET /finance/ledger` 会过滤掉它，
 * 只在管理端对账出现）。
 */
export type LedgerType = 'recharge' | 'order_pay' | 'refund' | 'adjust' | 'referral_rebate'

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
  /** R9：所属**子分类** ID；0 = 未归类（契约 10.4） */
  category_id: number
  /** R9：所属子分类名；未归类为空串 */
  category_name: string
  /** R9：所属主分类引用（形状同会员端 group.parent）；未归类为 {id:0,name:''} */
  category_parent: CategoryRef
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
  /** R12：本次选中的上游分组 ID（升序去重）；**全量导入时不出现** */
  selected_groups?: number[]
  /** R12：因映射目标失效而回落自动派生的上游分组 ID；无失效映射时不出现 */
  map_fallback_group_ids?: number[]
}

/** POST /admin/products/import 的可选请求体（R12，契约 10.4）。 */
export interface ImportProductsInput {
  /** 只导入这些上游分组；缺省 / 空数组 = 全量导入 */
  upstream_group_ids?: number[]
}

/**
 * POST /admin/products/sync 的请求体（R20，契约 10.7）：
 * **本地商品 ID** 列表（不是上游 pid），去重后 1-100 个。
 */
export interface SyncProductsInput {
  product_ids: number[]
}

/** 单条同步结果（契约 10.7）：只更新已存在商品；失败条目带可读原因。 */
export interface ProductSyncItem {
  product_id: number
  /** 上游 pid；本地 ID 不存在时为 0 */
  upstream_pid: number
  /** 库内当前商品名（失败条目用于指明是哪个商品；本地 ID 不存在时为空串） */
  name: string
  status: 'updated' | 'unchanged' | 'failed'
  /** 仅失败条目出现 */
  reason?: string
}

/** 商品同步结果（契约 10.7）：逐条结果 + 汇总计数；total = succeeded + failed。 */
export interface ProductSyncResult {
  total: number
  succeeded: number
  failed: number
  /** 上游字段发生变化的商品数 */
  updated: number
  /** 与库内完全一致的数 */
  unchanged: number
  items: ProductSyncItem[]
}

/**
 * 上游分组 → 本地子分类映射（R12，契约 10.4 的 GET /admin/product-group-maps）。
 * 目标分类已被删除时 category_name / category_parent_name 为空串（前端提示「目标分类已失效」）。
 */
export interface ProductGroupCategoryMap {
  upstream_group_id: number
  upstream_group_name: string
  category_id: number
  category_name: string
  category_parent_name: string
}

export interface UpdateProductInput {
  pricing_json?: PricingRule | string | null
  status?: 'on' | 'off'
  sort?: number
  /** R9：改挂到**子分类**；0 = 取消归类（契约 10.4）。设置后 category_source=manual，导入不再覆盖。 */
  category_id?: number
}

// —— 分类管理（管理端，契约 10.4 的 GET/PUT /admin/categories） ——

/** 管理端子分类（含商品计数）。 */
export interface AdminSubcategory {
  id: number
  /** 派生稳定键，仅供排查与导入对照，不可修改 */
  key: string
  name: string
  sort: number
  products: { total: number; on: number; off: number }
  created_at: string
  updated_at: string
}

/** 管理端主分类（带子分类列表）。 */
export interface AdminCategory {
  id: number
  key: string
  name: string
  sort: number
  created_at: string
  updated_at: string
  subcategories: AdminSubcategory[]
}

/** PUT /admin/categories/:id 请求体：字段均可选但至少提供一个（契约 10.4）。 */
export interface UpdateCategoryInput {
  name?: string
  /** 仅子分类可传：改挂到另一个主分类 */
  parent_id?: number
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
  /** 上游地址：构建期注入、**只读**（契约 8.6 / 12.1，R17）。 */
  base_url: string
  /** 恒为 true：界面据此把地址渲染成不可编辑项。 */
  base_url_fixed: boolean
  username: string
  api_key_configured: boolean
  api_key_masked: string
  timeout_seconds: number
  updated_by: number | null
  updated_at: string | null
}

/** 上游地址不可修改：请求体里出现 base_url 会被后端 40002 拒绝，故本结构没有该字段。 */
export interface UpdateUpstreamSettingsInput {
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

// ---------------------------------------------------------------------------
// 邀请返佣（契约 18.5，R10-C 前端消费 R10-A / R10-B 的后端接口）
// 金额一律定点小数字符串；`rebate_balance` 可为负（负数禁提现与转余额）。
// ---------------------------------------------------------------------------

/** GET/PUT /api/v1/referral —— 我的返佣概览（PUT 成功也返回同一对象）。 */
export interface ReferralOverview {
  enabled: boolean
  /** 从未开通时为 ""（惰性生成）；已生成则开/关/开不换码。 */
  invite_code: string
  /** 站内邀请链接 `{站点}/register?invite={邀请码}`；未配置站点或无码时为 ""。 */
  invite_link: string
  /** 邀请返现余额，可为负。 */
  rebate_balance: string
  effective_percent: string
  invitee_count: number
  total_rebate: string
}

export interface UpdateReferralInput {
  enabled: boolean
}

/** 返佣比例来源（契约 18.1）：商品级 / 会员级特批 / 全局默认。 */
export type ReferralRatioSource = 'product' | 'member' | 'global'

export type ReferralRebateStatus = 'granted' | 'rolled_back'

/** GET /api/v1/referral/rebates —— 返佣流水行。 */
export interface ReferralRebate {
  id: number
  order_id: number
  order_trade_no: string
  order_type: string
  buyer_id: number
  buyer_username: string
  base_amount: string
  discount_amount: string
  ratio_percent: string
  ratio_source: string
  rebate_amount: string
  status: string
  note: string
  created_at: string
}

/** GET /api/v1/referral/invitees —— 我邀请的会员行。 */
export interface ReferralInvitee {
  member_id: number
  username: string
  joined_at: string
  order_count: number
  total_spent: string
  total_rebate: string
}

/** 邀请人自建券（契约 11.3 券对象 + owner_member_id / source）。 */
export interface MemberCoupon {
  id: number
  code: string
  /** 本批自建券只允许 percent。 */
  type: string
  /** 折扣百分比（percent 时是百分比），一经创建不可改。 */
  value: string
  cycles: string[]
  starts_at: string | null
  expires_at: string | null
  max_uses: number
  used_count: number
  /** on / off。 */
  status: string
  comment: string
  owner_member_id: number
  /** platform / member。 */
  source: string
  created_at: string
  updated_at: string
}

export interface CreateMemberCouponInput {
  code: string
  type: 'percent'
  value: string
  cycles?: string[]
  starts_at?: string | null
  expires_at?: string | null
  max_uses?: number
  comment?: string
}

/**
 * PUT /api/v1/referral/coupons/:id —— 只允许 status 与 expires_at（三态：null = 清空 = 永不过期）。
 * 请求体里出现 value / code / type / owner_member_id 一律 40001，故类型上也不给这些字段。
 */
export interface UpdateMemberCouponInput {
  status?: string
  expires_at?: string | null
}

/** 出账申请类型：提现 / 转余额（契约 18.1.1）。 */
export type PayoutKind = 'withdraw' | 'convert'

export type PayoutStatus = 'pending' | 'approved' | 'rejected' | 'paid'

export type PayoutChannel = 'wechat' | 'alipay'

/** 出账申请对象（会员端列表与管理端列表共用；账号字段后端已脱敏）。 */
export interface ReferralPayout {
  id: number
  kind: string
  member_id: number
  amount: string
  /** 提现有 wechat/alipay；转余额恒为空串。 */
  channel: string
  /** 已脱敏（姓氏保留 + **）。 */
  account_name: string
  /** 已脱敏（前 2 后 2，中间 ****）。 */
  account_no: string
  status: string
  remark: string
  review_note: string
  reviewed_by: number
  reviewed_at: string | null
  paid_at: string | null
  created_at: string
  updated_at: string
  /** 管理端列表额外带会员概要；会员端列表不带（omitempty）。 */
  member?: ReferralPayoutMember | null
}

export interface ReferralPayoutMember {
  id: number
  username: string
  nickname: string
}

export interface CreateWithdrawalInput {
  amount: string
  channel: string
  account_name: string
  account_no: string
  remark?: string
}

export interface CreateConversionInput {
  amount: string
  remark?: string
}

/** POST .../review —— approve / reject + 可选审核备注。 */
export interface PayoutReviewInput {
  action: 'approve' | 'reject'
  review_note?: string
}

/** POST .../mark-paid —— 线下已打款；空体或带追加备注。 */
export interface MarkPaidInput {
  review_note?: string
}

/** 审核/打款结果：重复提交幂等时 already=true。 */
export interface PayoutReviewResult {
  payout: ReferralPayout
  already: boolean
}

/** GET/PUT /api/v1/admin/settings/referral —— 返佣设置（仅 admin）。 */
export interface ReferralSettings {
  enabled: boolean
  default_percent: string
  max_coupon_percent: string
  updated_by: number | null
  updated_at: string | null
}

export interface UpdateReferralSettingsInput {
  enabled?: boolean
  default_percent?: string
  max_coupon_percent?: string
}

/** PUT /api/v1/admin/members/:id —— 本批只承载会员级特批比例（三态）。 */
export interface UpdateMemberInput {
  /** 键缺席 = 不改；null = 清空；字符串 = 设值（0.00-100.00）。 */
  rebate_percent_override?: string | null
}

// —— 系统版本与更新检查（契约 19.3 / 19.5，R13-A；全部仅 admin 角色） ——

/** 检查结果状态（契约 19.3）：ok 为成功；not_checked 为从未尝试；其余为失败态，message 为失败原因。 */
export type UpdateStatusCode =
  | 'ok'
  | 'not_checked'
  | 'no_release'
  | 'rate_limited'
  | 'unauthorized'
  | 'timeout'
  | 'unreachable'
  | 'bad_response'

/** 版本比较结论（契约 19.2）：ok 已比较；dev_build 开发构建；unknown 无法比较。 */
export type UpdateCompare = 'ok' | 'dev_build' | 'unknown'

/** Release 产物（契约 19.3）：platform_match 当且仅当名称匹配服务器运行平台（.sha256 不标记）。 */
export interface UpdateAsset {
  name: string
  size: number
  download_url: string
  platform_match: boolean
}

/**
 * GET /api/v1/admin/updates 与 GET /api/v1/admin/updates/check 的共同数据形状（契约 19.3）。
 * 失败尝试只改写 status / message，latest_version 等字段保留上一次成功的结果（19.4）。
 */
export interface UpdateStatus {
  current_version: string
  /** 构建注入的提交与构建时间；未注入为 unknown。 */
  commit: string
  built_at: string
  latest_version: string
  has_update: boolean
  compare: UpdateCompare
  status: UpdateStatusCode
  message: string
  source: string
  repo: string
  release_name: string
  /** RFC3339 UTC；无正式版时为空串。 */
  published_at: string
  /** Release 说明的 Markdown 源文本，前端必须按纯文本展示。 */
  notes: string
  notes_truncated: boolean
  html_url: string
  /** 服务器运行平台（GOOS-GOARCH，如 linux-amd64）。 */
  platform: string
  assets: UpdateAsset[]
  /** 最近一次成功检查的时间；从未成功为空串。 */
  checked_at: string
}

/** GET/PUT /api/v1/admin/settings/updates —— 更新源设置（契约 19.5，仅 admin）。 */
export interface UpdatesSettings {
  repo: string
  github_token_configured: boolean
  github_token_masked: string
  /** 检查更新访问的 API 根地址；空串表示用缺省 api.github.com（契约 19.5，仅 admin）。 */
  api_base: string
  /** 资产下载基地址；空串表示原样使用 Release 的 download_url（契约 20.8）。 */
  download_base: string
  updated_by: number | null
  updated_at: string | null
}

export interface UpdateUpdatesSettingsInput {
  repo?: string
  /** 三态同上：省略 = 不变、给值 = 替换、空串 = 清空。 */
  github_token?: string
  /** 非三态：省略 = 不变、空串 = 清除（回到缺省）；非空须为 http(s) 绝对地址。 */
  api_base?: string
  download_base?: string
}

// —— 一键自动升级（契约 20，R13-B；全部仅 admin 角色） ——

/** 升级任务状态（契约 20.4）：running 进行中 / done 成功 / failed 失败 / rolled_back 已回滚。 */
export type UpdateJobStatus = 'running' | 'done' | 'failed' | 'rolled_back'

/** 升级阶段（契约 20.4）；失败或回滚后保留最后一次到达的阶段。 */
export type UpdateJobPhase =
  | 'preparing'
  | 'downloading'
  | 'verifying'
  | 'extracting'
  | 'staging'
  | 'handing_off'
  | 'replacing'
  | 'migrating'
  | 'restarting'
  | 'health_check'
  | 'done'
  | 'failed'
  | 'rolled_back'

/**
 * 升级任务（契约 20.4）。状态在服务端持久化（安装目录下的 upgrade-state.json），
 * 因此进程重启、升级完成与回滚之后刷新页面仍能读到同一份状态。
 */
export interface UpdateJob {
  job_id: string
  status: UpdateJobStatus
  phase: UpdateJobPhase
  /** 阶段中文名，由后端下发（前端不自建映射，避免两边漂移）。 */
  phase_label: string
  /** 0-100 的阶段进度。 */
  progress: number
  from_version: string
  target_version: string
  /** RFC3339 UTC；未结束为空串。 */
  started_at: string
  finished_at: string
  updated_at: string
  install_dir: string
  /** 成功为空串；失败为已脱敏原因；回滚时为回滚原因。 */
  message: string
  /** 是否已回滚到旧版本。 */
  rolled_back: boolean
  /** 迁移是否已执行成功（回滚后据此提示「库已前进」的中间态）。 */
  db_migrated: boolean
  /** upgrade.log 末尾最多 100 行。 */
  log_tail: string[]
  log_truncated: boolean
}

/** POST /api/v1/admin/updates/apply 请求体（契约 20.3）：二次确认 + 目标版本回传校验。 */
export interface ApplyUpdateInput {
  confirm: true
  target_version: string
}

// —— 数据库备份与恢复（契约 21，R14；全部仅 admin 角色） ——

/**
 * 一条备份的**元信息**（契约 21.3）。接口只输出元信息——**没有任何接口能读到备份文件内容**
 * （契约 21.4：不可下载，防止拖库）。
 */
export interface DbBackup {
  /** 备份 ID（= 文件名去掉 .sql）；恢复与删除接口只接受它，不接受路径。 */
  id: string
  file_name: string
  /** RFC3339 UTC。 */
  created_at: string
  database: string
  /** 备份时的 schema_migrations 版本。 */
  schema_version: number
  table_count: number
  row_count: number
  /** 备份文件字节数（不含元信息 sidecar）。 */
  file_size: number
  /** 发起备份的管理员用户名。 */
  operator: string
  /** 备份时的应用版本（buildinfo.Version）。 */
  app_version: string
  /** 文件是否仍在磁盘上（被手工删除时为 false，恢复与删除都不可用）。 */
  file_exists: boolean
  /** 元信息 sidecar 丢失（只可能是被手工删除），字段按可得信息回退。 */
  meta_missing: boolean
}

/** GET /api/v1/admin/db-backups 的 data（契约 21.3）。 */
export interface DbBackupList {
  /** 当前连接的库名。 */
  database: string
  backup_dir: string
  /** 目录来自缺省值（设置项 db_backup 留空）而非显式配置。 */
  backup_dir_default: boolean
  /** 目录不在 Web 根内（false 时不可备份，原因见 backup_dir_message）。 */
  backup_dir_outside_web_root: boolean
  backup_dir_message: string
  /** 全部备份文件占用的磁盘空间（不含元信息 sidecar）。 */
  total_size_bytes: number
  /** 按时间倒序。 */
  backups: DbBackup[]
  /** 恒为 false：本批不自动清理，删除由管理员手动执行。 */
  auto_cleanup: boolean
}

/** POST /api/v1/admin/db-backups/:id/restore 请求体（契约 21.5）：二次确认 + 当前管理员密码。 */
export interface RestoreDbBackupInput {
  confirm: true
  password: string
}

/** 恢复结果（契约 21.5）：只报结果，不含任何备份内容。 */
export interface DbBackupRestoreResult {
  backup_id: string
  file_name: string
  database: string
  /** 备份里的 schema 版本。 */
  from_schema_version: number
  /** 恢复后的实际版本（低版本备份会被自动追迁移）。 */
  to_schema_version: number
  /** 恢复后是否执行了迁移把库追到当前代码所需版本。 */
  migrated: boolean
  table_count: number
  /** 备份里没有、恢复时被删掉的表（覆盖而非合并的证据）。 */
  dropped_tables: string[]
  restored_at: string
  /** 备份版本高于当前代码等需要管理员知悉的提示；空串表示无。 */
  warning: string
}

/** DELETE /api/v1/admin/db-backups/:id 的 data。 */
export interface DeleteDbBackupResult {
  deleted: boolean
  id: string
  file_name: string
}

/**
 * GET/PUT /api/v1/admin/settings/db_backup 的 data（契约 21.10，R15）：
 * 备份目录设置——配置原值 + 解析后的绝对路径 + 目录可用性（供界面展示校验结果）。
 */
export interface DbBackupSettings {
  /** 设置项原值：空串表示用缺省目录。 */
  dir: string
  /** 当前是否用缺省目录（dir 留空）。 */
  default: boolean
  /** 缺省目录的绝对路径（与当前是否在用无关，供界面展示「留空 = 用这个」）。 */
  default_dir: string
  /** 当前配置解析后的绝对路径（dir 留空时等于 default_dir）。 */
  resolved_dir: string
  /** 目录不在 Web 根内；false 时备份接口拒绝写入，原因见 message。 */
  outside_web_root: boolean
  /** 目录当前是否存在（GET 不创建目录）。 */
  exists: boolean
  /** 写入探针是否通过（目录不存在时恒为 false）。 */
  writable: boolean
  /** 需要管理员注意的原因；一切正常时为空串。 */
  message: string
  updated_by: number | null
  updated_at: string | null
}

/** PUT /api/v1/admin/settings/db_backup 请求体（契约 21.10）：dir 必须提供，空串 = 恢复缺省目录。 */
export interface UpdateDbBackupSettingsInput {
  dir: string
}

/**
 * 「站点对外地址」设置（R16，契约 22.4；GET/PUT /api/v1/admin/settings/site，**仅 admin**）。
 * 该地址是交付时登记给上游的回推地址（downstream_url）；空串 = 未配置（按触发请求的 Host 推断）。
 */
export interface SiteSettings {
  /** 站点对外地址原值；空串 = 未配置。 */
  url: string
  /** 是否已配置（等价于 url !== ''）。 */
  configured: boolean
  /** 按当前请求推断的地址（scheme://host），供界面提示「留空时用这个」。 */
  url_recommended: string
  updated_by: number | null
  updated_at: string | null
}

/** PUT /api/v1/admin/settings/site 请求体（契约 22.4）：url 必须提供，空串 = 清空。 */
export interface UpdateSiteSettingsInput {
  url: string
}

// ---------------------------------------------------------------------------
// 操作日志（契约第 23 节，R18）
// ---------------------------------------------------------------------------

/** 操作日志通道（operation_logs.channel）：管理员 / 会员 / 定时任务三类留痕。 */
export type OplogChannel = 'admin' | 'member' | 'scheduler'

/** 操作日志结果（operation_logs.status）。 */
export type OplogStatus = 'success' | 'failed'

/**
 * GET /api/v1/admin/operation-logs 的列表项（契约 23.4.1）。
 * `actor_name` 是**用户名快照**（账号改名或删除后仍可读）；`summary` 已由后端脱敏。
 */
export interface OperationLog {
  id: number
  channel: OplogChannel
  /** 按通道解释为 admins.id / members.id；0 = 系统、定时任务与未登录尝试。 */
  actor_id: number
  /** 用户名快照；定时任务与未登录尝试为空串。 */
  actor_name: string
  action: string
  method: string
  /** 路由模板（如 /api/v1/admin/products/:id）；定时任务为空串。 */
  path: string
  http_status: number
  status: OplogStatus
  ip: string
  duration_ms: number
  summary: string
  created_at: string
}

/** DELETE /api/v1/admin/operation-logs?channel=… 的 data（契约 23.4.2）。 */
export interface ClearOperationLogsResult {
  /** 本次清空的通道（all = 清空全部）。 */
  channel: OplogChannel | 'all'
  /** 删除条数（清空全部时是 TRUNCATE 前的总条数）。 */
  deleted: number
  /** 清空留痕写入的通道（未被清空的通道，契约 23.4.2）。 */
  record_channel: OplogChannel
}

// ---------------------------------------------------------------------------
// 缓存（契约第 24 节，R19 批 1）
// ---------------------------------------------------------------------------

/** 缓存驱动（settings 键 `cache` 的 driver；默认 file，Redis 默认关闭）。 */
export type CacheDriver = 'file' | 'redis'

/** GET/PUT /api/v1/admin/settings/cache 的 redis 部分（契约 24.3）：口令只回配置标志与掩码。 */
export interface CacheRedisSettings {
  host: string
  port: number
  password_configured: boolean
  password_masked: string
  db: number
  key_prefix: string
  timeout_seconds: number
}

/** GET/PUT /api/v1/admin/settings/cache 的 data（契约 24.3）。 */
export interface CacheSettings {
  driver: CacheDriver
  /** 启用状态：等价于 driver === 'redis'（Redis 默认关闭）。 */
  redis_enabled: boolean
  /** 实际生效的驱动（降级时为 file）。 */
  effective_driver: CacheDriver
  degraded: boolean
  /** 降级原因（可读，如「连接被拒绝（127.0.0.1:6399）」）；未降级时为空串。 */
  degraded_reason: string
  /** 文件缓存目录（无论当前是否生效都给，便于排障）。 */
  file_dir: string
  redis: CacheRedisSettings
  updated_by: number | null
  updated_at: string | null
}

/** PUT /api/v1/admin/settings/cache 请求体（契约 24.3）：至少一个字段。 */
export interface UpdateCacheSettingsInput {
  driver?: CacheDriver
  redis?: {
    host?: string
    port?: number
    /** 三态：省略 = 保持不变、给值 = 替换、空串 = 清空。 */
    password?: string
    db?: number
    key_prefix?: string
    timeout_seconds?: number
  }
}

/** GET /api/v1/admin/cache/status 的 data（契约 24.4）：**不含 Redis 口令**。 */
export interface CacheStatus {
  driver: CacheDriver
  effective_driver: CacheDriver
  degraded: boolean
  degraded_reason: string
  /** 当前生效驱动的键数量（文件按未过期文件数、Redis 按本前缀 SCAN 去重计数）。 */
  keys: number
  /** 进程内累计命中 / 未命中与命中率（百分比，两位小数）；重启归零、可重置。 */
  hits: number
  misses: number
  hit_rate: number
  stats_since: string
  file: { dir: string }
  redis: {
    host: string
    port: number
    db: number
    key_prefix: string
    timeout_seconds: number
    /** 服务端版本（最近一次成功探活取得；服务端不报时为空串）。 */
    version: string
    /** 最近一次成功 PING 的往返耗时（毫秒；Windows 计时精度下可能为 0）。 */
    latency_ms: number
  }
}

/** POST /api/v1/admin/cache/reset-stats 的 data（契约 24.4）：归零后的计数。 */
export interface CacheStatsReset {
  hits: number
  misses: number
  hit_rate: number
  stats_since: string
}

/** POST /api/v1/admin/cache/flush 的 data（契约 24.4）。 */
export interface CacheFlushResult {
  /** 清空时实际生效的驱动。 */
  driver: CacheDriver
  flushed: boolean
  /** 清空后的键数量（应为 0）。 */
  keys: number
}
