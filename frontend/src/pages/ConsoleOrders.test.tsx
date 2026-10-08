import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { makeMember, makeOrder, makeProductDetail } from '@/test/fixtures'
import { installFetchMock, ok, renderApp, requestBody, seedMemberToken } from '@/test/harness'

// 会员区「我的订单」：列表渲染、行内展开详情（配置快照/金额明细）、继续支付与取消订单。

const PENDING = makeOrder({ id: 1, status: 'pending', final_amount: '220.00' })
const ACTIVE = makeOrder({
  id: 2,
  trade_no: 'O20261008ACTIVE01',
  status: 'active',
  pay_time: '2026-10-08T15:00:00Z',
  delivered_at: '2026-10-08T15:00:30Z',
})

function mockOrders(overrides: { onPay?: () => void; onCancel?: () => void } = {}) {
  return installFetchMock((url, init) => {
    if (url.pathname === '/api/v1/members/me') {
      return ok(makeMember({ balance: '250.00' }))
    }
    if (url.pathname === '/api/v1/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    if (url.pathname === '/api/v1/finance/balance') {
      return ok({ member_id: 1, balance: '250.00' })
    }
    if (url.pathname === '/api/v1/orders') {
      return ok({ items: [PENDING, ACTIVE], page: 1, page_size: 20, total: 2 })
    }
    if (url.pathname === '/api/v1/orders/1') {
      return ok(PENDING)
    }
    if (url.pathname === '/api/v1/orders/2') {
      return ok(ACTIVE)
    }
    if (url.pathname === '/api/v1/products/1') {
      return ok(makeProductDetail())
    }
    if (url.pathname === '/api/v1/orders/1/pay' && init.method === 'POST') {
      overrides.onPay?.()
      return ok({
        order: { ...PENDING, status: 'paid' },
        pay: { channel: 'balance', paid: true, balance_after: '30.00' },
      })
    }
    if (url.pathname === '/api/v1/orders/1/cancel' && init.method === 'POST') {
      overrides.onCancel?.()
      return ok({ ...PENDING, status: 'cancelled' })
    }
    return undefined
  })
}

async function renderOrders() {
  seedMemberToken()
  renderApp(['/console/orders'])
  await screen.findByRole('heading', { name: '我的订单' })
}

describe('会员区 · 我的订单', () => {
  it('列表展示订单号、类型与状态，并按状态筛选请求后端', async () => {
    const fetchMock = mockOrders()
    const user = userEvent.setup()
    await renderOrders()

    expect(await screen.findByText('O20261008ACTIVE01')).toBeInTheDocument()
    // 状态徽标在卡片内查询，避免与上方筛选按钮的同名文案冲突
    expect(within(screen.getByTestId('order-card-1')).getByText('待支付')).toBeInTheDocument()
    expect(within(screen.getByTestId('order-card-2')).getByText('已开通')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '交付失败' }))
    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(([input]) => String(input).includes('status=failed')),
      ).toBe(true)
    })
  })

  it('展开详情展示配置快照与金额明细', async () => {
    mockOrders()
    const user = userEvent.setup()
    await renderOrders()

    const detailButtons = await screen.findAllByRole('button', { name: '查看详情' })
    await user.click(detailButtons[0])

    const panel = await screen.findByTestId('order-detail-panel')
    // 配置快照：商品 / 周期 / 配置项（由商品详情翻译：11 → area|区域 的值 111 → HK^香港）
    expect(within(panel).getByText('配置快照')).toBeInTheDocument()
    expect(within(panel).getByText('区域')).toBeInTheDocument()
    expect(within(panel).getByText('HK · 香港')).toBeInTheDocument()
    // 金额明细（原价与应付金额同值，故用 getAllByText）
    expect(within(panel).getByText('金额明细')).toBeInTheDocument()
    expect(within(panel).getAllByText('¥220.00').length).toBeGreaterThanOrEqual(2)
    // 时间线
    expect(within(panel).getByText('订单时间线')).toBeInTheDocument()
  })

  it('继续支付打开支付弹窗，余额支付提交 channel=balance', async () => {
    const fetchMock = mockOrders()
    const user = userEvent.setup()
    await renderOrders()
    await screen.findByText('O20261008ACTIVE01')

    const payButtons = await screen.findAllByRole('button', { name: '继续支付' })
    await user.click(payButtons[0])

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('支付订单')).toBeInTheDocument()
    expect(within(dialog).getByText('O20261008143015K7Q2ZP')).toBeInTheDocument()

    await user.click(within(dialog).getByRole('radio', { name: /余额支付/ }))
    await user.click(within(dialog).getByRole('button', { name: '余额支付' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(([input]) =>
        String(input).includes('/api/v1/orders/1/pay'),
      )
      expect(call).toBeTruthy()
      expect(requestBody(call![1] ?? {})).toEqual({ channel: 'balance' })
    })
  })

  it('取消订单需二次确认，确认后提交取消请求并收起详情', async () => {
    const fetchMock = mockOrders()
    const user = userEvent.setup()
    await renderOrders()

    const detailButtons = await screen.findAllByRole('button', { name: '查看详情' })
    await user.click(detailButtons[0])
    const panel = await screen.findByTestId('order-detail-panel')

    await user.click(within(panel).getByRole('button', { name: '取消订单' }))
    const dialog = await screen.findByRole('alertdialog')
    expect(within(dialog).getByText('确认取消订单？')).toBeInTheDocument()

    await user.click(within(dialog).getByRole('button', { name: '取消订单' }))

    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(([input]) => String(input).includes('/api/v1/orders/1/cancel')),
      ).toBe(true)
    })
    await waitFor(() => {
      expect(screen.queryByTestId('order-detail-panel')).not.toBeInTheDocument()
    })
  })

  it('待支付订单展示继续支付，非待支付订单不展示', async () => {
    mockOrders()
    await renderOrders()
    await screen.findByText('O20261008ACTIVE01')

    // 仅 PENDING 一条有「继续支付」；ACTIVE 一条展示「查看实例」
    expect(screen.getAllByRole('button', { name: '继续支付' })).toHaveLength(1)
    expect(screen.getByRole('button', { name: '查看实例' })).toBeInTheDocument()
  })
})
