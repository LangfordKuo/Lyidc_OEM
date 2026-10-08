import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type { Order, OrderStatus } from '@/api/types'
import { makeMember, makeOrder } from '@/test/fixtures'
import { installFetchMock, ok, renderApp, seedMemberToken } from '@/test/harness'

/** 支付结果页要求登录（订单接口只对本人开放）。 */
function mockOrder(order: Order) {
  seedMemberToken()
  return installFetchMock((url) => {
    if (url.pathname === '/api/v1/members/me') {
      return ok(makeMember())
    }
    if (url.pathname === `/api/v1/orders/${order.id}`) {
      return ok(order)
    }
    return undefined
  })
}

const CASES: { status: OrderStatus; title: string }[] = [
  { status: 'pending', title: '支付结果确认中' },
  { status: 'active', title: '支付成功，实例已开通' },
  { status: 'provisioning', title: '支付成功，正在开通' },
  { status: 'failed', title: '支付成功，但开通失败' },
  { status: 'cancelled', title: '订单已取消' },
  { status: 'paid', title: '支付成功' },
]

describe('支付结果页', () => {
  it.each(CASES)('订单状态 $status 展示「$title」', async ({ status, title }) => {
    const order = makeOrder({
      status,
      pay_channel: status === 'pending' ? '' : 'epay',
      pay_time: status === 'pending' ? null : '2026-10-08T14:40:00Z',
    })
    mockOrder(order)
    renderApp([`/pay/result?order=${order.id}`])

    expect(await screen.findByRole('heading', { name: title })).toBeInTheDocument()
    expect(screen.getByText(order.trade_no)).toBeInTheDocument()
    expect(screen.getByText('香港二区 CN2 A型')).toBeInTheDocument()
  })

  it('待支付订单展示「继续支付」与「重新查询」按钮', async () => {
    mockOrder(makeOrder({ status: 'pending' }))
    renderApp(['/pay/result?order=1'])

    expect(await screen.findByRole('button', { name: '继续支付' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /重新查询/ })).toBeInTheDocument()
  })

  it('已支付订单不展示「继续支付」', async () => {
    mockOrder(makeOrder({ status: 'active' }))
    renderApp(['/pay/result?order=1'])

    await screen.findByRole('heading', { name: '支付成功，实例已开通' })
    expect(screen.queryByRole('button', { name: '继续支付' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /刷新状态/ })).toBeInTheDocument()
  })

  it('渠道回跳携带 out_trade_no 时按本地记录找回订单', async () => {
    const order = makeOrder({ status: 'active' })
    sessionStorage.setItem(
      'lyidc.checkout.lastOrder',
      JSON.stringify({
        id: 1,
        trade_no: order.trade_no,
        productId: 1,
        createdAt: '2026-10-08T14:30:15Z',
      }),
    )
    mockOrder(order)

    renderApp([`/pay/result?out_trade_no=${order.trade_no}`])

    expect(await screen.findByRole('heading', { name: '支付成功，实例已开通' })).toBeInTheDocument()
  })

  it('回跳未带任何参数时，用最近订单记录兜底找回订单', async () => {
    const order = makeOrder({ status: 'active' })
    sessionStorage.setItem(
      'lyidc.checkout.lastOrder',
      JSON.stringify({
        id: 1,
        trade_no: order.trade_no,
        productId: 1,
        createdAt: '2026-10-08T14:30:15Z',
      }),
    )
    mockOrder(order)

    renderApp(['/pay/result'])

    expect(await screen.findByRole('heading', { name: '支付成功，实例已开通' })).toBeInTheDocument()
    expect(screen.getByText(order.trade_no)).toBeInTheDocument()
    expect(screen.queryByText('未找到对应的订单信息')).not.toBeInTheDocument()
  })

  it('order 参数非法（非数字）时不回退到最近订单，避免猜测', async () => {
    const order = makeOrder({ status: 'active' })
    sessionStorage.setItem(
      'lyidc.checkout.lastOrder',
      JSON.stringify({
        id: 1,
        trade_no: order.trade_no,
        productId: 1,
        createdAt: '2026-10-08T14:30:15Z',
      }),
    )
    installFetchMock((url) => (url.pathname === '/api/v1/members/me' ? ok(makeMember()) : undefined))

    renderApp(['/pay/result?order=abc'])

    expect(await screen.findByText('未找到对应的订单信息')).toBeInTheDocument()
  })

  it('未登录访问带订单的结果页时引导登录（不误报支付状态）', async () => {
    installFetchMock(() => undefined)
    renderApp(['/pay/result?order=1'])

    expect(await screen.findByText('请先登录后查看订单状态')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '登录后查看' })).toBeInTheDocument()
  })

  it('无参数且无最近订单记忆时展示引导，不猜测支付状态', async () => {
    installFetchMock(() => undefined)
    renderApp(['/pay/result'])

    expect(await screen.findByText('未找到对应的订单信息')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '返回首页' })).toBeInTheDocument()
  })
})
