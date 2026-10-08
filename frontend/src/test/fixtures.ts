import type {
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
  ProductSummary,
  Recharge,
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
    ...overrides,
  }
}

export function makeProductDetail(overrides: Partial<ProductDetail> = {}): ProductDetail {
  return {
    ...makeProductSummary(),
    description: '&lt;li&gt;CPU:2核心&lt;/li&gt;\n&lt;li&gt;内存:1G&lt;/li&gt;',
    group: { id: 1, name: '香港二区' },
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
              { id: 111, name: '1|HK^香港', upstream_id: 0 },
              { id: 112, name: '2|US^美国', upstream_id: 0 },
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
    groups: [
      {
        id: 1,
        name: '香港二区',
        sort: 0,
        products: [
          makeProductSummary({ id: 1, name: '香港二区 CN2 A型' }),
          makeProductSummary({ id: 2, name: '香港二区 CN2 B型', prices: makePrices({ monthly: '45.00' }) }),
        ],
      },
      {
        id: 2,
        name: '襄阳云服务器',
        sort: 1,
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
