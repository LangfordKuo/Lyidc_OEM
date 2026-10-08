import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { cancelOrder, createOrder, listOrders, payOrder } from './orders'
import { clearMemberToken, setMemberToken } from './tokens'

function envelopeResponse(payload: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(payload),
  } as unknown as Response
}

function calls(): [string, RequestInit][] {
  const mock = fetch as unknown as ReturnType<typeof vi.fn>
  return mock.mock.calls as [string, RequestInit][]
}

// 下单/支付请求体严格对齐契约 12.4：
//   POST /orders  { product_id, cycle, config, coupon_code }
//   POST /orders/:id/pay { channel, pay_type? }
describe('订单接口调用姿态', () => {
  beforeEach(() => {
    localStorage.clear()
    setMemberToken('member-token')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    clearMemberToken()
  })

  it('创建订单：路径、请求体与会员鉴权头', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(envelopeResponse({ code: 0, message: 'ok', data: { id: 1 } })),
    )

    await createOrder({
      product_id: 7,
      cycle: 'annual',
      config: { '11': '111' },
      coupon_code: 'CASH20',
    })

    const [url, init] = calls()[0]
    expect(url).toBe('/api/v1/orders')
    expect(init.method).toBe('POST')
    expect(new Headers(init.headers).get('Authorization')).toBe('Bearer member-token')
    expect(JSON.parse(String(init.body))).toEqual({
      product_id: 7,
      cycle: 'annual',
      config: { '11': '111' },
      coupon_code: 'CASH20',
    })
  })

  it('在线支付：channel=epay 带 pay_type', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(envelopeResponse({ code: 0, message: 'ok', data: { pay: {} } })),
    )

    await payOrder(12, { channel: 'epay', pay_type: 'wxpay' })

    const [url, init] = calls()[0]
    expect(url).toBe('/api/v1/orders/12/pay')
    expect(JSON.parse(String(init.body))).toEqual({ channel: 'epay', pay_type: 'wxpay' })
  })

  it('余额支付：只带 channel', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(envelopeResponse({ code: 0, message: 'ok', data: { pay: {} } })),
    )

    await payOrder(12, { channel: 'balance' })

    const [, init] = calls()[0]
    expect(JSON.parse(String(init.body))).toEqual({ channel: 'balance' })
  })

  it('取消订单：POST 且不带请求体', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(envelopeResponse({ code: 0, message: 'ok', data: { status: 'cancelled' } })),
    )

    await cancelOrder(12)

    const [url, init] = calls()[0]
    expect(url).toBe('/api/v1/orders/12/cancel')
    expect(init.method).toBe('POST')
    expect(init.body).toBeUndefined()
  })

  it('订单列表：分页与状态筛选拼到 query', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        envelopeResponse({ code: 0, message: 'ok', data: { items: [], page: 2, page_size: 10, total: 0 } }),
      ),
    )

    await listOrders({ page: 2, page_size: 10, status: 'pending' })

    const [url] = calls()[0]
    expect(url).toBe('/api/v1/orders?page=2&page_size=10&status=pending')
  })
})
