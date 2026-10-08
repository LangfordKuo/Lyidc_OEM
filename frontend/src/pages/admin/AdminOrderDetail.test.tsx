import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { makeAdminAccount, makeAdminOrder } from '@/test/fixtures'
import { installFetchMock, ok, renderApp, seedAdminProfile, seedAdminToken } from '@/test/harness'

// 管理后台「订单详情」：交付信息/时间线渲染、重试交付的二次确认与角色矩阵（仅 admin）。

const FAILED_ORDER = makeAdminOrder({
  id: 5,
  trade_no: 'O20261008143015K7Q2ZP',
  status: 'failed',
  provision_error: '上游开通失败：余额不足',
  pay_time: '2026-10-08T14:31:00Z',
  pay_channel: 'balance',
})

const ACTIVE_ORDER = makeAdminOrder({
  id: 6,
  trade_no: 'O20261008150000BBBBBB',
  status: 'active',
  delivered_at: '2026-10-08T14:33:00Z',
  host_id: 10922,
})

function mockOrderDetail(
  order = FAILED_ORDER,
  role: 'admin' | 'finance' | 'support' = 'admin',
) {
  return installFetchMock((url, init) => {
    const path = url.pathname
    if (path === '/api/v1/admin/profile') {
      return ok(makeAdminAccount({ role, username: role }))
    }
    if (path === '/api/v1/admin/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    if (path === '/api/v1/admin/orders/5' || path === '/api/v1/admin/orders/6') {
      return ok(order)
    }
    if (path.endsWith('/retry-delivery') && init.method === 'POST') {
      return ok({ ...order, status: 'active', delivered_at: '2026-10-08T15:00:00Z' })
    }
    return undefined
  })
}

async function renderDetail(orderID = 5, role: 'admin' | 'finance' | 'support' = 'admin') {
  const order = orderID === 6 ? ACTIVE_ORDER : FAILED_ORDER
  const fetchMock = mockOrderDetail(order, role)
  seedAdminToken()
  seedAdminProfile({ role, username: role })
  renderApp([`/admin/orders/${orderID}`])
  // 单号在标题与订单信息里各出现一次
  await screen.findAllByText(order.trade_no)
  return fetchMock
}

describe('管理后台 · 订单详情与重试交付', () => {
  it('交付失败订单：展示失败原因、交付信息与时间线', async () => {
    await renderDetail()

    // 状态徽标 + 交付信息 + 失败提示里都会出现「交付失败」
    expect((await screen.findAllByText('交付失败')).length).toBeGreaterThan(1)
    expect(screen.getAllByText('上游开通失败：余额不足').length).toBeGreaterThan(0)
    expect(screen.getByText('未交付')).toBeInTheDocument()
    // 时间线：创建 → 支付到账（余额支付）
    expect(screen.getByText('创建订单')).toBeInTheDocument()
    expect(screen.getByText('支付到账（余额支付）')).toBeInTheDocument()
    // 会员概要
    expect(screen.getByText('会员概要')).toBeInTheDocument()
    expect(screen.getByText('demo7a@example.com')).toBeInTheDocument()
  })

  it('重试交付：二次确认文案含「可能真实扣费」，确认后调用接口并刷新', async () => {
    const fetchMock = await renderDetail()
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: '重试交付' }))

    expect(await screen.findByText('确认重试交付？')).toBeInTheDocument()
    expect(screen.getByText(/可能真实扣费/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '开始重试交付' }))

    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(([input, init]) =>
          String(input).includes('/admin/orders/5/retry-delivery') && init?.method === 'POST',
        ),
      ).toBe(true)
    })
  })

  it('财务角色：重试交付按钮禁用并说明仅超级管理员可执行', async () => {
    const fetchMock = await renderDetail(5, 'finance')

    const retry = screen.getByRole('button', { name: '重试交付' })
    expect(retry).toBeDisabled()
    expect(screen.getByText(/仅超级管理员可执行/)).toBeInTheDocument()
    expect(
      fetchMock.mock.calls.some(([input]) => String(input).includes('retry-delivery')),
    ).toBe(false)
  })

  it('已开通订单：按钮禁用并提示仅「已支付/交付失败」可重试', async () => {
    await renderDetail(6)

    expect(screen.getByRole('button', { name: '重试交付' })).toBeDisabled()
    expect(screen.getByText(/仅「已支付」与「交付失败」的订单可重试/)).toBeInTheDocument()
  })
})
