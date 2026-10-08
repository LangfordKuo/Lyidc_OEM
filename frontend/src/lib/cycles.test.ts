import { describe, expect, it } from 'vitest'

import {
  BILLING_CYCLES,
  availableCycles,
  cheapestCycle,
  isBillingCycle,
  toBillingCycle,
  type CyclePrices,
} from './cycles'
import { formatCycleLabel, formatDuration, formatMoney, monthlyEquivalent } from './format'

const PRICES: CyclePrices = {
  monthly: '22.00',
  quarterly: '66.00',
  semiannual: null,
  annual: '220.00',
  biennial: null,
  triennial: null,
}

describe('计费周期', () => {
  it('六个本地周期名与契约 10.2 一致（顺序固定）', () => {
    expect(BILLING_CYCLES).toEqual([
      'monthly',
      'quarterly',
      'semiannual',
      'annual',
      'biennial',
      'triennial',
    ])
  })

  it('availableCycles 过滤掉不可售（null）周期并保持标准顺序', () => {
    expect(availableCycles(PRICES)).toEqual(['monthly', 'quarterly', 'annual'])
  })

  it('cheapestCycle 取最低价周期', () => {
    expect(cheapestCycle(PRICES)).toEqual({ cycle: 'monthly', amount: '22.00' })
  })

  it('全部不可售时 cheapestCycle 返回 null，availableCycles 返回空数组', () => {
    const none: CyclePrices = {
      monthly: null,
      quarterly: null,
      semiannual: null,
      annual: null,
      biennial: null,
      triennial: null,
    }
    expect(cheapestCycle(none)).toBeNull()
    expect(availableCycles(none)).toEqual([])
  })

  it('周期判定与安全转换', () => {
    expect(isBillingCycle('annual')).toBe(true)
    expect(isBillingCycle('annually')).toBe(false) // 上游字段名不是本地周期名
    expect(toBillingCycle('semiannual')).toBe('semiannual')
    expect(toBillingCycle('')).toBeNull()
    expect(toBillingCycle(undefined)).toBeNull()
  })
})

describe('金额与周期格式化', () => {
  it('周期中文名与时长', () => {
    expect(formatCycleLabel('monthly')).toBe('月付')
    expect(formatCycleLabel('semiannual')).toBe('半年付')
    expect(formatCycleLabel('triennial')).toBe('三年付')
    expect(formatDuration('triennial')).toBe('36 个月')
  })

  it('金额统一两位小数（后端金额为定点两位小数字符串）', () => {
    expect(formatMoney('22')).toBe('¥22.00')
    expect(formatMoney('22.5')).toBe('¥22.50')
    expect(formatMoney('0')).toBe('¥0.00')
    expect(formatMoney(null)).toBe('—')
    expect(formatMoney('abc')).toBe('abc')
  })

  it('月均价按周期月数折算（整数分，四舍五入到分）', () => {
    expect(monthlyEquivalent('220.00', 'annual')).toBe('18.33')
    expect(monthlyEquivalent('66.00', 'quarterly')).toBe('22.00')
    expect(monthlyEquivalent('22.00', 'monthly')).toBe('22.00')
    expect(monthlyEquivalent(null, 'monthly')).toBeNull()
  })
})
