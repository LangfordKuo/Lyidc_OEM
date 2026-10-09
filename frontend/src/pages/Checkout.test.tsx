import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { gotoPayurl } from '@/lib/pay'
import { makeCouponValid, makeMember, makeOrder, makeProductDetail } from '@/test/fixtures'
import { fail, installFetchMock, ok, renderApp, requestBody, seedMemberToken } from '@/test/harness'

// 支付跳转替身：jsdom 里不触发真实导航，只记录跳转地址。
vi.mock('@/lib/pay', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/pay')>()
  return { ...actual, gotoPayurl: vi.fn() }
})

const BALANCE = '250.00'

/** 结算页依赖：登录态 + 商品详情 + 余额 +（默认）已选年付的下单草稿。 */
function mockCheckout(options: { draft?: boolean; balance?: string } = {}) {
  const { draft = true, balance = BALANCE } = options
  if (draft) {
    sessionStorage.setItem(
      'lyidc.checkout.draft.1',
      JSON.stringify({
        productId: 1,
        cycle: 'annual',
        config: { '11': '111' },
        productName: '香港二区 CN2 A型',
      }),
    )
  }

  return installFetchMock((url, init) => {
    if (url.pathname === '/api/v1/members/me') {
      return ok(makeMember({ balance }))
    }
    if (url.pathname === '/api/v1/products/1') {
      return ok(makeProductDetail())
    }
    if (url.pathname === '/api/v1/finance/balance') {
      return ok({ member_id: 1, balance })
    }
    if (url.pathname === '/api/v1/orders' && init.method === 'POST') {
      return ok(makeOrder({ id: 7, trade_no: 'O20261008TEST0001' }))
    }
    if (url.pathname === '/api/v1/orders/7/pay' && init.method === 'POST') {
      return ok({
        order: makeOrder({ id: 7, trade_no: 'O20261008TEST0001' }),
        pay: {
          channel: 'epay',
          pay_type: 'alipay',
          channel_trade_no: '2026100822001',
          payurl: 'https://pay.example.com/pay/xxx',
        },
      })
    }
    if (url.pathname.startsWith('/api/v1/coupons/') && url.pathname.endsWith('/validate')) {
      return ok(makeCouponValid())
    }
    return undefined
  })
}

async function renderCheckout() {
  seedMemberToken()
  renderApp(['/checkout/1'])
  await screen.findByRole('heading', { name: '确认订单' })
  return screen.getByTestId('checkout-summary')
}

describe('结算页', () => {
  it('展示商品、周期、配置快照与金额明细，并显示账户余额', async () => {
    mockCheckout()
    const summary = await renderCheckout()

    expect(await screen.findByText('香港二区 · 香港二区 CN2 A型')).toBeInTheDocument()
    // 草稿里的年付被选中，金额与明细取年付价
    expect(screen.getByRole('radio', { name: /^年付/ })).toHaveAttribute('aria-checked', 'true')
    expect(within(summary).getByText('年付原价')).toBeInTheDocument()
    expect(within(summary).getByTestId('payable-amount')).toHaveTextContent('¥220.00')
    expect(within(summary).getByText('¥250.00')).toBeInTheDocument()
  })

  it('优惠码校验成功后展示减免并更新应付金额', async () => {
    mockCheckout()
    const user = userEvent.setup()
    const summary = await renderCheckout()

    await user.type(screen.getByLabelText(/优惠码/), 'WELCOME10')
    await user.click(screen.getByRole('button', { name: '校验' }))

    expect(await within(summary).findByText(/可用：减免 ¥22.00/)).toBeInTheDocument()
    expect(within(summary).getByTestId('payable-amount')).toHaveTextContent('¥198.00')
    expect(within(summary).getByText('-¥22.00')).toBeInTheDocument()
  })

  it('优惠码不适用当前周期时展示明确原因', async () => {
    sessionStorage.setItem(
      'lyidc.checkout.draft.1',
      JSON.stringify({
        productId: 1,
        cycle: 'annual',
        config: { '11': '111' },
        productName: '香港二区 CN2 A型',
      }),
    )
    installFetchMock((url, init) => {
      if (url.pathname === '/api/v1/members/me') {
        return ok(makeMember())
      }
      if (url.pathname === '/api/v1/products/1') {
        return ok(makeProductDetail())
      }
      if (url.pathname === '/api/v1/finance/balance') {
        return ok({ member_id: 1, balance: BALANCE })
      }
      if (url.pathname.startsWith('/api/v1/coupons/') && init.method === 'GET') {
        return ok({ valid: false, reason: 'cycle_not_applicable' })
      }
      return undefined
    })
    const user = userEvent.setup()
    const summary = await renderCheckout()

    await user.type(screen.getByLabelText(/优惠码/), 'ANNUALONLY')
    await user.click(screen.getByRole('button', { name: '校验' }))

    expect(await screen.findByText('优惠码不适用于该周期')).toBeInTheDocument()
    // 金额不变
    expect(within(summary).getByTestId('payable-amount')).toHaveTextContent('¥220.00')
  })

  it('提交订单携带商品/周期/配置/优惠码，成功后拉起支付弹窗', async () => {
    const fetchMock = mockCheckout()
    const user = userEvent.setup()
    await renderCheckout()

    await user.type(screen.getByLabelText(/优惠码/), 'WELCOME10')
    await user.click(screen.getByRole('button', { name: '校验' }))
    await screen.findByText(/可用：减免 ¥22.00/)

    await user.click(screen.getByRole('button', { name: '提交订单' }))

    const orderCall = await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/api/v1/orders') && (init as RequestInit)?.method === 'POST',
      )
      expect(call).toBeTruthy()
      return call as [RequestInfo | URL, RequestInit]
    })

    expect(requestBody(orderCall[1])).toEqual({
      product_id: 1,
      cycle: 'annual',
      config: { '11': '111' },
      coupon_code: 'WELCOME10',
    })

    // 支付弹窗自动打开
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('支付订单')).toBeInTheDocument()
    expect(within(dialog).getByText('O20261008TEST0001')).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: '前往支付' })).toBeInTheDocument()

    // 写入点①：下单成功即记录「最近订单」（渠道回跳不带参数时结果页据此兜底）
    expect(JSON.parse(sessionStorage.getItem('lyidc.checkout.lastOrder') ?? 'null')).toMatchObject({
      id: 7,
      trade_no: 'O20261008TEST0001',
      productId: 1,
    })
  })

  it('发起在线支付时再次记录最近订单并跳转渠道收银台', async () => {
    vi.mocked(gotoPayurl).mockClear()
    mockCheckout()
    const user = userEvent.setup()
    await renderCheckout()

    await user.click(screen.getByRole('button', { name: '提交订单' }))
    const dialog = await screen.findByRole('dialog')

    // 清掉下单时写的记忆，验证「发起支付」这条路径自己也会写
    sessionStorage.removeItem('lyidc.checkout.lastOrder')

    await user.click(within(dialog).getByRole('button', { name: '前往支付' }))

    await waitFor(() => expect(gotoPayurl).toHaveBeenCalledWith('https://pay.example.com/pay/xxx'))
    expect(JSON.parse(sessionStorage.getItem('lyidc.checkout.lastOrder') ?? 'null')).toMatchObject({
      id: 7,
      trade_no: 'O20261008TEST0001',
      productId: 1,
    })
  })

  it('下单失败展示后端 message，且不打开支付弹窗', async () => {
    sessionStorage.setItem(
      'lyidc.checkout.draft.1',
      JSON.stringify({
        productId: 1,
        cycle: 'annual',
        config: { '11': '111' },
        productName: '香港二区 CN2 A型',
      }),
    )
    installFetchMock((url, init) => {
      if (url.pathname === '/api/v1/members/me') {
        return ok(makeMember())
      }
      if (url.pathname === '/api/v1/products/1') {
        return ok(makeProductDetail())
      }
      if (url.pathname === '/api/v1/finance/balance') {
        return ok({ member_id: 1, balance: BALANCE })
      }
      if (url.pathname === '/api/v1/orders' && init.method === 'POST') {
        return fail(40002, '该商品在 biennial 周期不可售（无本地售价）', 400)
      }
      return undefined
    })
    const user = userEvent.setup()
    await renderCheckout()

    await user.click(screen.getByRole('button', { name: '提交订单' }))

    expect(await screen.findByText('该商品在 biennial 周期不可售（无本地售价）')).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('余额不足时支付弹窗禁用余额支付并提示', async () => {
    mockCheckout({ balance: '10.00' })
    const user = userEvent.setup()
    await renderCheckout()

    await user.click(screen.getByRole('button', { name: '提交订单' }))
    const dialog = await screen.findByRole('dialog')

    await user.click(within(dialog).getByRole('radio', { name: /余额支付/ }))

    const insufficient = await within(dialog).findAllByText(/余额不足/)
    expect(insufficient.length).toBeGreaterThan(0)
    expect(within(dialog).getByRole('button', { name: '余额支付' })).toBeDisabled()
  })

  it('未登录访问结算页跳登录并带 redirect', async () => {
    mockCheckout()
    const { router } = renderApp(['/checkout/1'])

    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
    expect(router.state.location.search).toBe('?redirect=%2Fcheckout%2F1')
  })

  it('数量型配置按 Qty 口径提交数量（而非值 id），快照显示数量+单位', async () => {
    const fetchMock = installFetchMock((url, init) => {
      if (url.pathname === '/api/v1/members/me') {
        return ok(makeMember({ balance: BALANCE }))
      }
      if (url.pathname === '/api/v1/products/1') {
        // R6：数量型配置（option_type=11，qty 范围 20~100，默认取 qty_minimum）。
        return ok(
          makeProductDetail({
            config_groups: [
              {
                id: 1,
                name: '带宽',
                options: [
                  {
                    id: 13,
                    name: 'bw|带宽',
                    type: 11,
                    upstream_id: 0,
                    values: [{ id: 131, name: '带宽', upstream_id: 0, qty_minimum: 20, qty_maximum: 100 }],
                  },
                ],
              },
            ],
          }),
        )
      }
      if (url.pathname === '/api/v1/finance/balance') {
        return ok({ member_id: 1, balance: BALANCE })
      }
      if (url.pathname === '/api/v1/orders' && init.method === 'POST') {
        return ok(makeOrder({ id: 7, trade_no: 'O20261008TEST0001', config: { '13': '21' } }))
      }
      return undefined
    })
    seedMemberToken()
    renderApp(['/checkout/1'])
    await screen.findByRole('heading', { name: '确认订单' })
    const user = userEvent.setup()

    // 默认值 = qty_minimum；步进一次 → 21。
    expect(screen.getByRole('spinbutton', { name: '带宽' })).toHaveValue(20)
    await user.click(screen.getByRole('button', { name: '增加带宽' }))

    await user.click(screen.getByRole('button', { name: '提交订单' }))

    const orderCall = await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/api/v1/orders') && (init as RequestInit)?.method === 'POST',
      )
      expect(call).toBeTruthy()
      return call as [RequestInfo | URL, RequestInit]
    })
    expect(requestBody(orderCall[1])).toEqual({
      product_id: 1,
      cycle: 'monthly',
      config: { '13': '21' },
      coupon_code: '',
    })

    // 订单快照摘要按「数量 + 单位」展示（非 `值 #id`）。
    expect(await screen.findByText('带宽 21Mbps')).toBeInTheDocument()
  })
})
