import { describe, expect, it } from 'vitest'

import { availableCycles, cheapestCycle, isBillingCycle, toBillingCycle, type CyclePrices } from '@/lib/cycles'
import { decodeDescription, formatAmount, formatMoney, monthlyEquivalent } from '@/lib/format'
import { pageWindow, totalPages } from '@/lib/pagination'

const PRICES: CyclePrices = {
  monthly: '22.00',
  quarterly: '66.00',
  semiannual: null,
  annual: '200.00',
  biennial: null,
  triennial: null,
}

describe('计费周期工具（契约 10.2 / 10.3）', () => {
  it('availableCycles 按标准顺序返回可售周期，跳过 null', () => {
    expect(availableCycles(PRICES)).toEqual(['monthly', 'quarterly', 'annual'])
  })

  it('cheapestCycle 返回最低价周期，全不可售返回 null', () => {
    expect(cheapestCycle(PRICES)).toEqual({ cycle: 'monthly', amount: '22.00' })
    expect(
      cheapestCycle({ ...PRICES, monthly: null, quarterly: null, annual: null }),
    ).toBeNull()
  })

  it('cheapestCycle 同价时取周期更短者', () => {
    const prices: CyclePrices = { ...PRICES, monthly: '50.00', quarterly: '50.00' }
    expect(cheapestCycle(prices)?.cycle).toBe('monthly')
  })

  it('周期名守卫', () => {
    expect(isBillingCycle('annual')).toBe(true)
    expect(isBillingCycle('annually')).toBe(false)
    expect(toBillingCycle('annually')).toBeNull()
    expect(toBillingCycle('triennial')).toBe('triennial')
  })

  it('monthlyEquivalent 按整数分折算月均并四舍五入', () => {
    expect(monthlyEquivalent('220.00', 'annual')).toBe('18.33')
    expect(monthlyEquivalent('66.00', 'quarterly')).toBe('22.00')
    expect(monthlyEquivalent(null, 'annual')).toBeNull()
  })
})

describe('金额与文案格式化', () => {
  it('formatAmount 定点两位小数，非法值原样返回', () => {
    expect(formatAmount('22')).toBe('22.00')
    expect(formatAmount('22.056')).toBe('22.06')
    expect(formatAmount(null)).toBe('—')
    expect(formatAmount('abc')).toBe('abc')
  })

  it('formatMoney 带货币符号', () => {
    expect(formatMoney('198.00')).toBe('¥198.00')
    expect(formatMoney(null)).toBe('—')
  })

  it('decodeDescription 反转义 HTML 实体并去掉标签', () => {
    expect(decodeDescription('&lt;li&gt;CPU:2核心&lt;/li&gt;')).toBe('CPU:2核心')
    expect(decodeDescription('&lt;p&gt;A&lt;/p&gt;&lt;br&gt;B')).toBe('A\nB')
    expect(decodeDescription(null)).toBe('')
  })
})

describe('分页工具', () => {
  it('totalPages 至少 1 页', () => {
    expect(totalPages(0, 12)).toBe(1)
    expect(totalPages(12, 12)).toBe(1)
    expect(totalPages(13, 12)).toBe(2)
  })

  it('pageWindow 首尾页码 + 当前页窗口 + 省略号', () => {
    expect(pageWindow(1, 1)).toEqual([1])
    expect(pageWindow(5, 10)).toEqual([1, 'gap', 4, 5, 6, 'gap', 10])
    expect(pageWindow(2, 4)).toEqual([1, 2, 3, 4])
  })
})
