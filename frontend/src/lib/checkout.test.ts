import { beforeEach, describe, expect, it } from 'vitest'

import {
  clearCheckoutDraft,
  findRememberedOrderByTradeNo,
  readCheckoutDraft,
  readRememberedOrder,
  rememberOrder,
  saveCheckoutDraft,
} from './checkout'
import { safeRedirect, buildLoginUrl, buildRegisterUrl } from './redirect'

describe('下单草稿（sessionStorage）', () => {
  beforeEach(() => {
    sessionStorage.clear()
  })

  it('按商品 ID 存取周期与配置项', () => {
    saveCheckoutDraft({
      productId: 7,
      cycle: 'annual',
      config: { '11': '111' },
      productName: '香港二区 CN2 A型',
    })

    expect(readCheckoutDraft(7)).toEqual({
      productId: 7,
      cycle: 'annual',
      config: { '11': '111' },
      productName: '香港二区 CN2 A型',
    })
    // 不同商品互不影响
    expect(readCheckoutDraft(8)).toBeNull()
  })

  it('非法周期或商品不匹配时视为无草稿', () => {
    sessionStorage.setItem(
      'lyidc.checkout.draft.7',
      JSON.stringify({ productId: 7, cycle: 'annually', config: {} }),
    )
    expect(readCheckoutDraft(7)).toBeNull()
  })

  it('清除草稿', () => {
    saveCheckoutDraft({ productId: 7, cycle: 'monthly', config: {}, productName: 'x' })
    clearCheckoutDraft(7)
    expect(readCheckoutDraft(7)).toBeNull()
  })
})

describe('最近订单记录（支付回跳用）', () => {
  beforeEach(() => {
    sessionStorage.clear()
  })

  it('记录并按单号找回（大小写不敏感）', () => {
    rememberOrder({ id: 12, trade_no: 'O20261008143015K7Q2ZP', productId: 7 })

    expect(readRememberedOrder()?.id).toBe(12)
    expect(findRememberedOrderByTradeNo('o20261008143015k7q2zp')?.id).toBe(12)
    expect(findRememberedOrderByTradeNo('O00000000000000XXXXXX')).toBeNull()
    expect(findRememberedOrderByTradeNo(null)).toBeNull()
  })
})

describe('登录回跳参数', () => {
  it('只接受站内相对路径', () => {
    expect(safeRedirect('/console/orders')).toBe('/console/orders')
    expect(safeRedirect('/checkout/7?cycle=annual')).toBe('/checkout/7?cycle=annual')
    expect(safeRedirect('//evil.example.com')).toBe('/console')
    expect(safeRedirect('https://evil.example.com')).toBe('/console')
    expect(safeRedirect('javascript:alert(1)')).toBe('/console')
    expect(safeRedirect(null)).toBe('/console')
    expect(safeRedirect('', '/products')).toBe('/products')
  })

  it('登录/注册链接带编码后的 redirect', () => {
    expect(buildLoginUrl('/checkout/7')).toBe('/login?redirect=%2Fcheckout%2F7')
    expect(buildRegisterUrl('/checkout/7?cycle=annual')).toBe(
      '/register?redirect=%2Fcheckout%2F7%3Fcycle%3Dannual',
    )
  })
})
