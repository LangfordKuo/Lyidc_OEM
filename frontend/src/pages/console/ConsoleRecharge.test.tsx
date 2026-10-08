import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { apiGet, apiPost, renderConsole, requestBody } from '../../test/consoleHarness'

// 余额充值页：金额区间校验（契约 12.4：1.00 ~ 50000.00）与充值单创建姿态。
// 渠道跳转被替换成桩函数，测试里不触发真实导航。
vi.mock('../../lib/pay', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../lib/pay')>()
  return { ...actual, gotoPayurl: vi.fn() }
})

const emptyPage = { items: [], page: 1, page_size: 20, total: 0 }

function renderRecharge() {
  return renderConsole('/console/recharge', [
    apiGet('/api/v1/recharges?', emptyPage),
    apiGet('/api/v1/finance/ledger?', emptyPage),
    apiPost('/api/v1/recharges', {
      recharge: { id: 1, trade_no: 'R20261008143500M3P8QT', amount: '100.00', status: 'pending' },
      pay: { channel: 'epay', pay_type: 'alipay', payurl: 'https://pay.example.com/pay/yyy' },
    }),
  ])
}

describe('会员区 · 余额充值', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('展示实时余额与充值记录/余额流水两个页签', async () => {
    renderRecharge()

    expect(await screen.findByText('¥88.00')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '充值记录' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '余额流水' })).toBeInTheDocument()
  })

  it('金额越界时给出提示并禁止提交', async () => {
    const user = userEvent.setup()
    renderRecharge()

    const input = await screen.findByPlaceholderText('如 100')
    await user.type(input, '0.5')

    expect(await screen.findByText('充值金额不能低于 ¥1.00')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '去支付' })).toBeDisabled()
  })

  it('创建充值单：提交金额与渠道参数（在线支付走易支付）', async () => {
    const user = userEvent.setup()
    renderRecharge()

    const input = await screen.findByPlaceholderText('如 100')
    await user.type(input, '100')
    await user.click(screen.getByRole('button', { name: '去支付' }))

    expect(requestBody('POST', '/api/v1/recharges')).toEqual({
      amount: '100',
      channel: 'epay',
      pay_type: 'alipay',
    })
  })
})
