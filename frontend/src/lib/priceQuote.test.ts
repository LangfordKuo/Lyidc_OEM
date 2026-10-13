import { describe, expect, it } from 'vitest'

import type { ConfigGroup, ConfigOption, ConfigOptionValue, ConfigValuePrices } from '@/api/types'
import {
  NO_PRICING_DATA_NOTE,
  billableQuantity,
  fromCents,
  nonZeroDeltaCount,
  quantityTiers,
  quotePrice,
  tierFor,
  toCents,
} from './priceQuote'

/**
 * R7 价格联动纯函数测试（契约 10.2.1）：
 * 成交价 = 基础价 + Σ 配置差价；数量型差价 = 单位差价 × 可计费数量。
 * 前端只做预览累加，不重套定价规则（差价已由服务端按定价模式换算后下发）。
 */

function prices(overrides: Partial<ConfigValuePrices> = {}): ConfigValuePrices {
  return {
    monthly: null,
    quarterly: null,
    semiannual: null,
    annual: null,
    biennial: null,
    triennial: null,
    ...overrides,
  }
}

function value(
  id: number,
  name: string,
  overrides: Partial<ConfigOptionValue> = {},
): ConfigOptionValue {
  return { id, name, upstream_id: 0, ...overrides }
}

function option(
  overrides: Partial<ConfigOption> & Pick<ConfigOption, 'id' | 'name' | 'values'>,
): ConfigOption {
  return { type: 6, upstream_id: 0, ...overrides }
}

function catalog(...items: ConfigOption[]): ConfigGroup[] {
  return [{ id: 1, name: '配置', options: items }]
}

describe('priceQuote 金额换算', () => {
  it('toCents 解析两位小数字符串为整数分', () => {
    expect(toCents('0')).toBe(0)
    expect(toCents('20.00')).toBe(2000)
    expect(toCents('0.5')).toBe(50)
    expect(toCents('12.34')).toBe(1234)
  })

  it('toCents 对非法/空值返回 null（按无差价数据处理）', () => {
    expect(toCents(null)).toBeNull()
    expect(toCents(undefined)).toBeNull()
    expect(toCents('')).toBeNull()
    expect(toCents('abc')).toBeNull()
    expect(toCents('-1.00')).toBeNull()
    expect(toCents('1.234')).toBeNull()
  })

  it('fromCents 始终输出两位小数', () => {
    expect(fromCents(0)).toBe('0.00')
    expect(fromCents(50)).toBe('0.50')
    expect(fromCents(2000)).toBe('20.00')
    expect(fromCents(1234)).toBe('12.34')
  })
})

describe('quotePrice 选型差价', () => {
  it('选项型换值：成交价 = 基础价 + 该值差价', () => {
    const groups = catalog(
      option({
        id: 10,
        name: 'cpu|CPU',
        values: [
          value(101, '1|1核', { pricings: prices({ monthly: '0.00' }) }),
          value(102, '2|2核', { pricings: prices({ monthly: '20.00' }) }),
        ],
      }),
    )

    const picked = quotePrice(groups, { '10': '102' }, 'monthly', '20.00')
    expect(picked.base).toBe('20.00')
    expect(picked.total).toBe('40.00')
    expect(picked.deltas).toHaveLength(1)
    expect(picked.deltas[0]).toMatchObject({
      option_id: 10,
      option_name: 'CPU',
      kind: 'select',
      value_id: 102,
      amount: '20.00',
    })
  })

  it('各周期差价独立（月付 0 / 年付 200）', () => {
    const groups = catalog(
      option({
        id: 10,
        name: 'cpu|CPU',
        values: [
          value(102, '2|2核', { pricings: prices({ monthly: '0.00', annual: '200.00' }) }),
        ],
      }),
    )

    expect(quotePrice(groups, { '10': '102' }, 'monthly', '20.00').total).toBe('20.00')
    expect(quotePrice(groups, { '10': '102' }, 'annual', '200.00').total).toBe('400.00')
  })
})

describe('quotePrice 数量型差价', () => {
  it('单档位：差价 = 单位差价 × 数量（数据盘 0.50 × 50 = 25.00）', () => {
    const groups = catalog(
      option({
        id: 11,
        name: 'data_disk_size|数据盘',
        type: 14,
        values: [
          value(111, '数据盘', {
            qty_minimum: 0,
            qty_maximum: 100,
            pricings: prices({ monthly: '0.50' }),
          }),
        ],
      }),
    )

    const quote = quotePrice(groups, { '11': '50' }, 'monthly', '20.00')
    expect(quote.total).toBe('45.00')
    expect(quote.deltas[0]).toMatchObject({
      kind: 'quantity',
      qty: 50,
      billable: 50,
      unit: '0.50',
      amount: '25.00',
    })
  })

  it('多档位：可计费数量 = 数量 − 低一档上限（30 落在 [21-100]，可计费 10）', () => {
    const opt = option({
      id: 12,
      name: 'bw|带宽',
      type: 11,
      values: [
        value(121, '带宽', { qty_minimum: 20, qty_maximum: 20, pricings: prices({ monthly: '0.00' }) }),
        value(122, '带宽', { qty_minimum: 21, qty_maximum: 100, pricings: prices({ monthly: '1.00' }) }),
      ],
    })
    const groups = catalog(opt)

    const tiers = quantityTiers(opt)
    expect(billableQuantity(opt, tiers[1], 30)).toBe(10)
    expect(billableQuantity(opt, tiers[1], 21)).toBe(1)
    expect(billableQuantity(opt, tiers[0], 20)).toBe(20)

    const quote = quotePrice(groups, { '12': '30' }, 'monthly', '0.00')
    expect(quote.deltas[0]).toMatchObject({ billable: 10, unit: '1.00', amount: '10.00' })
    expect(quote.total).toBe('10.00')
  })
})

/**
 * R11（v21）：数量型多档位的价格联动修复 + 前后端同口径对照。
 *
 * 这里的夹具是**接口真实形状**——`values[].qty_minimum/qty_maximum` 已被后端归一成并集，
 * 每档真实区间只在 `tiers[]` 里（见契约 10.3 第 6 条）。算例与
 * backend/internal/router/product_view_test.go 的同名用例逐条一致，改一处必须同步另一处。
 */
describe('quotePrice 档位区间（R11 tiers）', () => {
  /** 商品 2「香港二区 CN2 B型」数据盘：并集 values + 真实区间 tiers（后端视图实际下发形状）。 */
  function bTypeDataDisk(): ConfigOption {
    return {
      id: 27,
      name: 'data_disk_size|数据盘',
      type: 19,
      upstream_id: 0,
      values: [
        // 并集 30~256：两档都被拉平，拿它挑档位会永远命中第一档（旧 bug 的根因）。
        value(88, '数据盘', {
          qty_minimum: 30,
          qty_maximum: 256,
          pricings: prices({ monthly: '0.00', quarterly: '0.00', annual: '0.00' }),
        }),
        value(10664, '数据盘', {
          qty_minimum: 30,
          qty_maximum: 256,
          pricings: prices({ monthly: '0.50', quarterly: '1.50', annual: '6.00' }),
        }),
      ],
      tiers: [
        {
          value_id: 88,
          qty_min: 30,
          qty_max: 30,
          pricings: prices({ monthly: '0.00', quarterly: '0.00', annual: '0.00' }),
        },
        {
          value_id: 10664,
          qty_min: 31,
          qty_max: 256,
          pricings: prices({ monthly: '0.50', quarterly: '1.50', annual: '6.00' }),
        },
      ],
    }
  }

  /** 商品 1「香港二区 CN2 A型」数据盘：单档位 [0-256 @0.50]。 */
  function aTypeDataDisk(): ConfigOption {
    return {
      id: 10,
      name: 'data_disk_size|数据盘',
      type: 14,
      upstream_id: 0,
      values: [
        value(37, '数据盘', {
          qty_minimum: 0,
          qty_maximum: 256,
          pricings: prices({ monthly: '0.50' }),
        }),
      ],
      tiers: [
        {
          value_id: 37,
          qty_min: 0,
          qty_max: 256,
          pricings: prices({ monthly: '0.50' }),
        },
      ],
    }
  }

  it('多档位 B 型：按 tiers 真实区间挑档（30 → 0.00 / 31 → 0.50 / 50 → 10.00）', () => {
    const opt = bTypeDataDisk()
    const groups = catalog(opt)

    // 验收算例（与后端 Quote 一致）：qty=30 落免费档、31 起每 GB 0.50。
    const at30 = quotePrice(groups, { '27': '30' }, 'monthly', '25.00')
    expect(at30.deltas[0]).toMatchObject({
      value_id: 88,
      billable: 30,
      unit: '0.00',
      amount: '0.00',
    })
    expect(at30.total).toBe('25.00') // 差价 0.00，只剩基础价

    const at31 = quotePrice(groups, { '27': '31' }, 'monthly', '25.00')
    expect(at31.deltas[0]).toMatchObject({
      value_id: 10664,
      billable: 1,
      unit: '0.50',
      amount: '0.50',
    })
    expect(at31.total).toBe('25.50')

    const at50 = quotePrice(groups, { '27': '50' }, 'monthly', '25.00')
    expect(at50.deltas[0]).toMatchObject({
      value_id: 10664,
      billable: 20,
      unit: '0.50',
      amount: '10.00', // (50 − 30) × 0.50
    })
    expect(at50.total).toBe('35.00')
  })

  it('多档位 B 型：用并集字段挑档会命中免费档（钉住旧 bug，防止回退）', () => {
    const opt = bTypeDataDisk()
    // 并集后两档区间都是 30~256，按区间 find 必然命中第一档 → 加量不加价。
    const naive = opt.values.find(
      (item) => 50 >= (item.qty_minimum ?? 0) && 50 <= (item.qty_maximum ?? 0),
    )
    expect(naive?.id).toBe(88) // 免费档——这正是修掉的错误结果
    // 正确路径（tiers）挑到计费档。
    expect(tierFor(opt, 50)?.value_id).toBe(10664)
  })

  it('单档位 A 型回归：billable = 数量（0 → 0.00 / 50 → 25.00）', () => {
    const opt = aTypeDataDisk()
    const groups = catalog(opt)

    const at0 = quotePrice(groups, { '10': '0' }, 'monthly', '22.00')
    expect(at0.deltas[0]).toMatchObject({ billable: 0, amount: '0.00' })
    expect(at0.total).toBe('22.00')

    const at50 = quotePrice(groups, { '10': '50' }, 'monthly', '22.00')
    expect(at50.deltas[0]).toMatchObject({ billable: 50, unit: '0.50', amount: '25.00' })
    expect(at50.total).toBe('47.00')
  })

  it('tiers 缺省时从 values 派生（旧夹具/未升级数据不回归）', () => {
    const legacy = option({
      id: 12,
      name: 'bw|带宽',
      type: 11,
      values: [
        value(121, '带宽', { qty_minimum: 20, qty_maximum: 20, pricings: prices({ monthly: '0.00' }) }),
        value(122, '带宽', { qty_minimum: 21, qty_maximum: 100, pricings: prices({ monthly: '1.00' }) }),
      ],
    })
    expect(quantityTiers(legacy)).toEqual([
      { value_id: 121, qty_min: 20, qty_max: 20, pricings: legacy.values[0].pricings },
      { value_id: 122, qty_min: 21, qty_max: 100, pricings: legacy.values[1].pricings },
    ])
    expect(quotePrice(catalog(legacy), { '12': '30' }, 'monthly', '0.00').total).toBe('10.00')
  })
})

describe('quotePrice 降级与边界', () => {
  it('该周期无差价数据（pricings 为 null）→ 按 0.00 计并标注', () => {
    const groups = catalog(
      option({
        id: 13,
        name: 'area|区域',
        type: 12,
        values: [value(131, '1|HK^香港', { pricings: prices() })],
      }),
    )

    const quote = quotePrice(groups, { '13': '131' }, 'monthly', '20.00')
    expect(quote.total).toBe('20.00') // 基础价不变
    expect(quote.deltas[0]).toMatchObject({ amount: '0.00', note: NO_PRICING_DATA_NOTE })
  })

  it('未知配置项 / 未知值被跳过，不影响成交价', () => {
    const groups = catalog(
      option({
        id: 10,
        name: 'cpu|CPU',
        values: [value(101, '1|1核', { pricings: prices({ monthly: '5.00' }) })],
      }),
    )

    const quote = quotePrice(groups, { '10': '999', '99': '1' }, 'monthly', '20.00')
    expect(quote.total).toBe('20.00')
    expect(quote.deltas).toHaveLength(0)
  })

  it('基础价不可售（null）时按 0 计，仅剩配置差价', () => {
    const groups = catalog(
      option({
        id: 10,
        name: 'cpu|CPU',
        values: [value(102, '2|2核', { pricings: prices({ monthly: '20.00' }) })],
      }),
    )

    const quote = quotePrice(groups, { '10': '102' }, 'monthly', null)
    expect(quote.base).toBe('0.00')
    expect(quote.total).toBe('20.00')
  })

  it('nonZeroDeltaCount 统计有差价的项数', () => {
    const groups = catalog(
      option({
        id: 10,
        name: 'cpu|CPU',
        values: [value(102, '2|2核', { pricings: prices({ monthly: '20.00' }) })],
      }),
      option({
        id: 13,
        name: 'area|区域',
        type: 12,
        values: [value(131, '1|HK^香港', { pricings: prices() })],
      }),
    )

    const quote = quotePrice(groups, { '10': '102', '13': '131' }, 'monthly', '20.00')
    expect(quote.deltas).toHaveLength(2)
    expect(nonZeroDeltaCount(quote)).toBe(1)
  })
})
