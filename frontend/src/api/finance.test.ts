import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { createRecharge, listLedger, listRecharges } from './finance'
import { clearMemberToken, setMemberToken } from './tokens'

// 充值单与流水接口调用姿态对齐契约 12.4：
//   POST /recharges {amount, channel, pay_type?}
//   GET  /recharges?page&page_size&status、GET /finance/ledger?page&page_size&type
function envelopeResponse(payload: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(payload),
  } as unknown as Response
}

function stubOk(data: unknown = {}) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue(envelopeResponse({ code: 0, message: 'ok', data })),
  )
}

function calls(): [string, RequestInit][] {
  const mock = fetch as unknown as ReturnType<typeof vi.fn>
  return mock.mock.calls as [string, RequestInit][]
}

describe('充值与流水接口调用姿态', () => {
  beforeEach(() => {
    localStorage.clear()
    setMemberToken('member-token')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    clearMemberToken()
  })

  it('创建充值单：金额按输入原样提交，渠道固定 epay', async () => {
    stubOk({ recharge: {}, pay: {} })

    await createRecharge({ amount: '100.50', channel: 'epay', pay_type: 'wxpay' })

    const [url, init] = calls()[0]
    expect(url).toBe('/api/v1/recharges')
    expect(JSON.parse(String(init.body))).toEqual({
      amount: '100.50',
      channel: 'epay',
      pay_type: 'wxpay',
    })
  })

  it('充值记录：分页与状态筛选进 query', async () => {
    stubOk({ items: [], page: 1, page_size: 20, total: 0 })

    await listRecharges({ page: 1, page_size: 20, status: 'paid' })

    expect(calls()[0][0]).toBe('/api/v1/recharges?page=1&page_size=20&status=paid')
  })

  it('余额流水：分页与类型筛选进 query', async () => {
    stubOk({ items: [], page: 2, page_size: 20, total: 0 })

    await listLedger({ page: 2, page_size: 20, type: 'order_pay' })

    expect(calls()[0][0]).toBe('/api/v1/finance/ledger?page=2&page_size=20&type=order_pay')
  })
})
