import type {
  AdminAccount,
  AdminCategory,
  AdminInstance,
  AdminInstanceDetail,
  AdminOrder,
  AdminProduct,
  AdminProductGroup,
  AdminSubcategory,
  AdminTicket,
  CouponValidation,
  InstanceDetail,
  InstanceLog,
  InstanceSummary,
  LedgerEntry,
  Member,
  Notification,
  Order,
  ProductCatalog,
  ProductDetail,
  ProductGroupCategoryMap,
  ProductSummary,
  Recharge,
  ReferralInvitee,
  ReferralOverview,
  ReferralPayout,
  ReferralRebate,
  MemberCoupon,
  Ticket,
  TicketMessage,
} from '@/api/types'
import type { CyclePrices } from '@/lib/cycles'

// 测试夹具：字段与 docs/api-contract.md 的响应示例保持一致。

export function makePrices(overrides: Partial<CyclePrices> = {}): CyclePrices {
  return {
    monthly: '22.00',
    quarterly: '66.00',
    semiannual: '132.00',
    annual: '220.00',
    biennial: null,
    triennial: null,
    ...overrides,
  }
}

export function makeProductSummary(overrides: Partial<ProductSummary> = {}): ProductSummary {
  return {
    id: 1,
    name: '香港二区 CN2 A型',
    type: 'dcimcloud',
    sort: 0,
    prices: makePrices(),
    stock_qty: 70,
    stock_control: 1,
    ontrial_max: 0,
    // 配置简介行（R5）：与 description 的首两行同源（服务端解析），另加一行带宽。
    description_lines: ['CPU:2核心', '内存:1G', '带宽:20M'],
    ...overrides,
  }
}

export function makeProductDetail(overrides: Partial<ProductDetail> = {}): ProductDetail {
  return {
    ...makeProductSummary(),
    description: '<li>CPU:2核心</li>\n<li>内存:1G</li>',
    group: { id: 1, name: '香港二区', parent: { id: 101, name: '香港' } },
    config_groups: [
      {
        id: 1,
        name: '区域',
        options: [
          {
            id: 11,
            name: 'area|区域',
            type: 12,
            upstream_id: 0,
            values: [
              // R7：区域/系统类上游无差价数据（pricings 为 null）。
              {
                id: 111,
                name: '1|HK^香港',
                upstream_id: 0,
                pricings: {
                  monthly: null,
                  quarterly: null,
                  semiannual: null,
                  annual: null,
                  biennial: null,
                  triennial: null,
                },
              },
              {
                id: 112,
                name: '2|US^美国',
                upstream_id: 0,
                pricings: {
                  monthly: null,
                  quarterly: null,
                  semiannual: null,
                  annual: null,
                  biennial: null,
                  triennial: null,
                },
              },
            ],
          },
        ],
      },
    ],
    custom_fields: [],
    updated_at: '2026-10-08T09:12:03Z',
    ...overrides,
  }
}

export function makeCatalog(): ProductCatalog {
  return {
    categories: [
      {
        id: 101,
        name: '香港',
        sort: 0,
        subcategories: [{ id: 1, name: '香港二区', sort: 0, product_count: 2 }],
      },
      {
        id: 102,
        name: '国内',
        sort: 1,
        subcategories: [{ id: 2, name: '襄阳云服务器', sort: 0, product_count: 1 }],
      },
    ],
    groups: [
      {
        id: 1,
        name: '香港二区',
        sort: 0,
        parent: { id: 101, name: '香港' },
        products: [
          makeProductSummary({ id: 1, name: '香港二区 CN2 A型' }),
          makeProductSummary({ id: 2, name: '香港二区 CN2 B型', prices: makePrices({ monthly: '45.00' }) }),
        ],
      },
      {
        id: 2,
        name: '襄阳云服务器',
        sort: 1,
        parent: { id: 102, name: '国内' },
        products: [
          makeProductSummary({
            id: 112,
            name: '襄阳云服务器-A型',
            prices: makePrices({ monthly: '30.00', biennial: '800.00', triennial: '1200.00' }),
          }),
        ],
      },
    ],
    total: 3,
  }
}

export function makeMember(overrides: Partial<Member> = {}): Member {
  return {
    id: 1,
    username: 'demo7a',
    email: 'demo7a@example.com',
    nickname: 'demo7a',
    phone: null,
    status: 'active',
    balance: '250.00',
    created_at: '2026-10-08T06:18:31Z',
    updated_at: '2026-10-08T06:18:31Z',
    last_login_at: '2026-10-08T06:20:02Z',
    rebate_percent_override: null,
    ...overrides,
  }
}

export function makeOrder(overrides: Partial<Order> = {}): Order {
  return {
    id: 1,
    trade_no: 'O20261008143015K7Q2ZP',
    member_id: 1,
    product_id: 1,
    product_name: '香港二区 CN2 A型',
    cycle: 'annual',
    qty: 1,
    config: { '11': '111' },
    amount: '220.00',
    discount_amount: '0.00',
    final_amount: '220.00',
    coupon_code: '',
    price_breakdown: {
      base: '220.00',
      deltas: [],
      total: '220.00',
    },
    status: 'pending',
    type: 'new',
    pay_channel: '',
    channel_trade_no: '',
    pay_time: null,
    host_id: null,
    instance_id: null,
    provision_error: '',
    delivered_at: null,
    created_at: '2026-10-08T14:30:15Z',
    updated_at: '2026-10-08T14:30:15Z',
    ...overrides,
  }
}

export function makeCouponValid(overrides: Partial<Extract<CouponValidation, { valid: true }>> = {}) {
  return {
    valid: true as const,
    code: 'WELCOME10',
    type: 'percent' as const,
    value: '10.00',
    price: '220.00',
    discount_amount: '22.00',
    final_amount: '198.00',
    ...overrides,
  }
}

// ---------------------------------------------------------------------------
// 会员区：实例 / 工单 / 通知 / 充值（字段与契约 14.4 / 16.3 / 17.2 / 12.4 示例一致）
// ---------------------------------------------------------------------------

export function makeInstance(overrides: Partial<InstanceSummary> = {}): InstanceSummary {
  // 到期时间默认取「10 天后」，避免测试随当前日期漂移出临期窗口。
  const due = new Date(Date.now() + 10 * 86_400_000).toISOString()
  return {
    id: 101,
    order_id: 11,
    host_id: 10922,
    product_id: 1,
    product_name: '香港二区 CN2 A型',
    name: 'oem-o20261008105520t0j03j',
    billing_cycle: 'monthly',
    next_due_date: due,
    status: 'active',
    upstream_status: 'Active',
    dedicated_ip: '203.0.113.9',
    cancel_status: 'none',
    cancel_type: '',
    cancel_request_id: 0,
    cancel_requested_at: null,
    created_at: '2026-10-08T10:55:40Z',
    ...overrides,
  }
}

export function makeInstanceDetail(overrides: Partial<InstanceDetail> = {}): InstanceDetail {
  return {
    ...makeInstance(),
    assigned_ips: [],
    port: 22,
    username: 'root',
    password: 'Abcd1234Efgh5678',
    updated_at: '2026-10-08T11:00:00Z',
    ...overrides,
  }
}

export function makeInstanceLog(overrides: Partial<InstanceLog> = {}): InstanceLog {
  return {
    id: 1,
    instance_id: 101,
    actor_type: 'system',
    actor_id: 0,
    action: 'create',
    status: 'success',
    message: '开通完成，主机 ID 10922',
    created_at: '2026-10-08T10:55:40Z',
    ...overrides,
  }
}

export function makeTicket(overrides: Partial<Ticket> = {}): Ticket {
  return {
    id: 1,
    trade_no: 'T20261008201530K7Q2ZP',
    subject: '主机无法连接，请协助排查',
    category: 'technical',
    status: 'open',
    instance_id: null,
    instance: null,
    last_reply_at: '2026-10-08T12:15:30Z',
    closed_at: null,
    created_at: '2026-10-08T12:15:30Z',
    updated_at: '2026-10-08T12:15:30Z',
    ...overrides,
  }
}

export function makeTicketMessage(overrides: Partial<TicketMessage> = {}): TicketMessage {
  return {
    id: 1,
    author_type: 'member',
    author_id: 1,
    author_name: 'demo7a',
    content: '从今天早上 9 点开始 SSH 就一直连不上（超时）。',
    internal: false,
    created_at: '2026-10-08T12:15:30Z',
    ...overrides,
  }
}

export function makeNotification(overrides: Partial<Notification> = {}): Notification {
  return {
    id: 1,
    event: 'order_delivered',
    title: '订单已开通：香港二区 CN2 A型',
    content: '您的订单 O20261008143015K7Q2ZP 已开通完成，实例 oem-xxx。',
    read: false,
    read_at: null,
    created_at: '2026-10-08T22:58:13Z',
    ...overrides,
  }
}

export function makeRecharge(overrides: Partial<Recharge> = {}): Recharge {
  return {
    id: 1,
    trade_no: 'R20261008143500M3P8QT',
    member_id: 1,
    amount: '100.00',
    channel: 'epay',
    status: 'pending',
    channel_trade_no: null,
    created_at: '2026-10-08T14:35:00Z',
    paid_at: null,
    expires_at: null,
    ...overrides,
  }
}

export function makeLedgerEntry(overrides: Partial<LedgerEntry> = {}): LedgerEntry {
  return {
    id: 1,
    member_id: 1,
    type: 'recharge',
    amount: '100.00',
    balance_before: '180.00',
    balance_after: '280.00',
    ref_type: 'recharge',
    ref_id: 1,
    note: '充值 R20261008143500M3P8QT',
    created_at: '2026-10-08T14:40:00Z',
    ...overrides,
  }
}

// ---------------------------------------------------------------------------
// 管理后台：管理员账号 / 订单 / 商品 / 实例 / 工单（字段与契约 6.3 / 12.6 / 10.4 / 14.4+15.3 / 16.3 一致）
// ---------------------------------------------------------------------------

export function makeAdminAccount(overrides: Partial<AdminAccount> = {}): AdminAccount {
  return {
    id: 1,
    username: 'admin',
    nickname: '超级管理员',
    role: 'admin',
    status: 'active',
    created_at: '2026-10-08T06:00:00Z',
    updated_at: '2026-10-08T06:00:00Z',
    last_login_at: '2026-10-08T06:20:00Z',
    ...overrides,
  }
}

/** 管理端订单视图：订单全字段 + 会员概要。 */
export function makeAdminOrder(overrides: Partial<AdminOrder> = {}): AdminOrder {
  return {
    ...makeOrder(),
    member: {
      id: 1,
      username: 'demo7a',
      nickname: 'demo7a',
      email: 'demo7a@example.com',
      status: 'active',
    },
    ...overrides,
  }
}

export function makeAdminProduct(overrides: Partial<AdminProduct> = {}): AdminProduct {
  return {
    id: 1,
    upstream_pid: 501,
    upstream_group_id: 9,
    group_id: 1,
    group_name: '香港二区',
    name: '香港二区 CN2 A型',
    type: 'dcimcloud',
    module: 'dcimcloud',
    status: 'on',
    sort: 0,
    stock_qty: 70,
    stock_control: 1,
    ontrial_max: 0,
    pricing: { mode: 'upstream' },
    prices: makePrices(),
    category_id: 201,
    category_name: '香港二区',
    category_parent: { id: 101, name: '香港' },
    created_at: '2026-10-08T09:00:00Z',
    updated_at: '2026-10-08T09:12:03Z',
    ...overrides,
  }
}

/** 管理端子分类（含商品计数，契约 10.4 的 GET /admin/categories）。 */
export function makeAdminSubcategory(
  overrides: Partial<AdminSubcategory> = {},
): AdminSubcategory {
  return {
    id: 201,
    key: 'z:香港二区',
    name: '香港二区',
    sort: 0,
    products: { total: 6, on: 3, off: 3 },
    created_at: '2026-10-13T00:00:00Z',
    updated_at: '2026-10-13T00:00:00Z',
    ...overrides,
  }
}

/** 管理端主分类（带子分类，契约 10.4 的 GET /admin/categories）。 */
export function makeAdminCategory(overrides: Partial<AdminCategory> = {}): AdminCategory {
  return {
    id: 101,
    key: 'm:香港',
    name: '香港',
    sort: 0,
    created_at: '2026-10-13T00:00:00Z',
    updated_at: '2026-10-13T00:00:00Z',
    subcategories: [makeAdminSubcategory()],
    ...overrides,
  }
}

export function makeAdminProductGroup(
  overrides: Partial<AdminProductGroup> = {},
): AdminProductGroup {
  return {
    id: 1,
    upstream_group_id: 9,
    name: '香港二区',
    sort: 0,
    products: { total: 2, on: 1, off: 1 },
    created_at: '2026-10-08T09:00:00Z',
    updated_at: '2026-10-08T09:00:00Z',
    ...overrides,
  }
}

/** 上游分组 → 本地子分类映射（R12，契约 10.4 的 GET /admin/product-group-maps）。 */
export function makeProductGroupCategoryMap(
  overrides: Partial<ProductGroupCategoryMap> = {},
): ProductGroupCategoryMap {
  return {
    upstream_group_id: 9,
    upstream_group_name: '香港二区',
    category_id: 201,
    category_name: '香港二区',
    category_parent_name: '香港',
    ...overrides,
  }
}

/** 管理端实例列表项：会员端摘要 + member_id。 */
export function makeAdminInstance(overrides: Partial<AdminInstance> = {}): AdminInstance {
  return {
    ...makeInstance(),
    member_id: 1,
    ...overrides,
  }
}

/** 管理端实例详情：含主机账号密码等敏感字段。 */
export function makeAdminInstanceDetail(
  overrides: Partial<AdminInstanceDetail> = {},
): AdminInstanceDetail {
  return {
    ...makeInstanceDetail(),
    member_id: 1,
    ...overrides,
  }
}

/** 管理端工单：工单全字段 + 会员概要。 */
export function makeAdminTicket(overrides: Partial<AdminTicket> = {}): AdminTicket {
  return {
    ...makeTicket(),
    member_id: 1,
    member: { id: 1, username: 'demo7a', nickname: 'demo7a' },
    ...overrides,
  }
}

// ---------------------------------------------------------------------------
// 邀请返佣（契约 18.5）
// ---------------------------------------------------------------------------

/** 我的返佣概览。 */
export function makeReferralOverview(overrides: Partial<ReferralOverview> = {}): ReferralOverview {
  return {
    enabled: true,
    invite_code: 'ABCD2345',
    invite_link: 'https://shop.example.com/register?invite=ABCD2345',
    rebate_balance: '20.00',
    effective_percent: '20.00',
    invitee_count: 1,
    total_rebate: '20.00',
    ...overrides,
  }
}

/** 返佣流水行。 */
export function makeReferralRebate(overrides: Partial<ReferralRebate> = {}): ReferralRebate {
  return {
    id: 1,
    order_id: 1,
    order_trade_no: 'O20261008143015K7Q2ZP',
    order_type: 'new',
    buyer_id: 2,
    buyer_username: 'friend1',
    base_amount: '100.00',
    discount_amount: '0.00',
    ratio_percent: '20.00',
    ratio_source: 'global',
    rebate_amount: '20.00',
    status: 'granted',
    note: '',
    created_at: '2026-10-09T02:00:00Z',
    ...overrides,
  }
}

/** 我邀请的会员行。 */
export function makeReferralInvitee(overrides: Partial<ReferralInvitee> = {}): ReferralInvitee {
  return {
    member_id: 2,
    username: 'friend1',
    joined_at: '2026-10-08T12:00:00Z',
    order_count: 1,
    total_spent: '100.00',
    total_rebate: '20.00',
    ...overrides,
  }
}

/** 邀请人自建券。 */
export function makeMemberCoupon(overrides: Partial<MemberCoupon> = {}): MemberCoupon {
  return {
    id: 11,
    code: 'FRIEND20',
    type: 'percent',
    value: '20.00',
    cycles: [],
    starts_at: null,
    expires_at: null,
    max_uses: 0,
    used_count: 2,
    status: 'on',
    comment: '老带新',
    owner_member_id: 1,
    source: 'member',
    created_at: '2026-10-09T01:00:00Z',
    updated_at: '2026-10-09T01:00:00Z',
    ...overrides,
  }
}

/** 提现 / 转余额申请（账号字段后端已脱敏）。 */
export function makeReferralPayout(overrides: Partial<ReferralPayout> = {}): ReferralPayout {
  return {
    id: 21,
    kind: 'withdraw',
    member_id: 1,
    amount: '15.00',
    channel: 'wechat',
    account_name: '张**',
    account_no: '12****34',
    status: 'pending',
    remark: '',
    review_note: '',
    reviewed_by: 0,
    reviewed_at: null,
    paid_at: null,
    created_at: '2026-10-09T03:00:00Z',
    updated_at: '2026-10-09T03:00:00Z',
    ...overrides,
  }
}
