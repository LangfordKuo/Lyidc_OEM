import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { gotoPayurl } from '@/lib/pay'
import { makeLedgerEntry, makeMember, makeRecharge } from '@/test/fixtures'
import { installFetchMock, ok, renderApp, requestBody, seedMemberToken } from '@/test/harness'

// 支付跳转替身：jsdom 里不触发真实导航，只记录跳转地址。
vi.mock('@/lib/pay', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/pay')>()
  return { ...actual, gotoPayurl: vi.fn() }
})

// 会员区「余额充值」：金额校验（前端预校验不请求）与创建充值单参数。

function mockRecharge() {
  return installFetchMock((url, init) => {
    if (url.pathname === '/api/v1/members/me') {
      return ok(makeMember({ balance: '180.00' }))
    }
    if (url.pathname === '/api/v1/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    if (url.pathname === '/api/v1/finance/balance') {
      return ok({ member_id: 1, balance: '180.00' })
    }
    if (url.pathname === '/api/v1/recharges' && init.method === 'POST') {
      return ok({
        recharge: makeRecharge(),
        pay: {
          channel: 'epay',
          pay_type: 'alipay',
          channel_trade_no: '2026100822002',
          payurl: 'https://pay.example.com/pay/yyy',
        },
      })
    }
    if (url.pathname === '/api/v1/recharges') {
      return ok({ items: [makeRecharge({ status: 'paid', paid_at: '2026-10-08T14:36:00Z' })], page: 1, page_size: 20, total: 1 })
    }
    if (url.pathname === '/api/v1/finance/ledger') {
      return ok({ items: [makeLedgerEntry()], page: 1, page_size: 20, total: 1 })
    }
    return undefined
  })
}

async function renderRecharge() {
  seedMemberToken()
  renderApp(['/console/recharge'])
  await screen.findByRole('heading', { name: '余额充值' })
  return screen.getByLabelText('充值金额（元）')
}

describe('会员区 · 余额充值', () => {
  it('低于下限的金额给出提示且禁用提交（不发请求）', async () => {
    const fetchMock = mockRecharge()
    const user = userEvent.setup()
    const input = await renderRecharge()

    await user.type(input, '0.5')

    expect(await screen.findByText('充值金额不能低于 ¥1.00')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '去支付' })).toBeDisabled()
    expect(
      fetchMock.mock.calls.some(
        ([input_, init]) =>
          String(input_).includes('/api/v1/recharges') && (init as RequestInit)?.method === 'POST',
      ),
    ).toBe(false)
  })

  it('格式非法（超两位小数）给出提示', async () => {
    mockRecharge()
    const user = userEvent.setup()
    const input = await renderRecharge()

    await user.type(input, '10.555')
    expect(await screen.findByText('金额格式不正确（最多两位小数）')).toBeInTheDocument()
  })

  it('合法金额提交创建充值单并跳转渠道收银台', async () => {
    const fetchMock = mockRecharge()
    vi.mocked(gotoPayurl).mockClear()
    const user = userEvent.setup()
    const input = await renderRecharge()

    // 快捷额度按钮回填
    await user.click(screen.getByRole('button', { name: '¥100' }))
    expect(input).toHaveValue('100')

    await user.click(screen.getByRole('button', { name: '去支付' }))

    const call = await waitFor(() => {
      const found = fetchMock.mock.calls.find(
        ([input_, init]) =>
          String(input_).includes('/api/v1/recharges') && (init as RequestInit)?.method === 'POST',
      )
      expect(found).toBeTruthy()
      return found as [RequestInfo | URL, RequestInit]
    })
    expect(requestBody(call[1])).toEqual({
      amount: '100',
      channel: 'epay',
      pay_type: 'alipay',
    })
    await waitFor(() =>
      expect(gotoPayurl).toHaveBeenCalledWith('https://pay.example.com/pay/yyy'),
    )
  })

  it('展示充值记录与余额流水（收入为正、支出为负）', async () => {
    mockRecharge()
    const user = userEvent.setup()
    await renderRecharge()

    // 充值记录（默认页签）
    expect(await screen.findByText('R20261008143500M3P8QT')).toBeInTheDocument()
    expect(screen.getByText('已到账')).toBeInTheDocument()

    // 切到余额流水
    await user.click(screen.getByRole('tab', { name: '余额流水' }))
    expect(await screen.findByText('充值入账')).toBeInTheDocument()
    expect(screen.getByText('+¥100.00')).toBeInTheDocument()
    expect(screen.getByText(/余额 ¥180\.00 → ¥280\.00/)).toBeInTheDocument()
  })
})
