import type {
  CouponValidation,
  Member,
  Order,
  ProductCatalog,
  ProductDetail,
  ProductSummary,
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
