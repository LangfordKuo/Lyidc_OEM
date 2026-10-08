import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AdminOrder, Order } from '../../api/types'
import { apiFail, renderAdmin } from '../../test/adminHarness'
import { apiGet, apiPost, fetchCalls } from '../../test/consoleHarness'

// 订单详情（阶段 8b 新增接口 GET /admin/orders/:id）：交付信息 + 时间线 + 会员概要；
// 重试交付仅 admin 且需二次确认（契约 14.4）。
const deliveredOrder: AdminOrder = {
  id: 12,
  trade_no: 'O20261008105520T0J03J',
  member_id: 9,
  product_id: 3,
  product_name: '香港二区 CN2 A型',
  cycle: 'monthly',
  qty: 1,
  config: { '11': '111' },
  amount: '100.00',
  discount_amount: '0.00',
  final_amount: '100.00',
  coupon_code: '',
  status: 'active',
  type: 'new',
  pay_channel: 'epay',
  channel_trade_no: 'CH20261008001',
  pay_time: '2026-10-08T10:55:25Z',
  host_id: 10922,
  instance_id: null,
  provision_error: '',
  delivered_at: '2026-10-08T10:55:40Z',
  created_at: '2026-10-08T10:55:20Z',
  updated_at: '2026-10-08T10:55:40Z',
  member: {
    id: 9,
    username: 'demo9',
    nickname: '演示会员',
    email: 'demo9@example.com',
    status: 'active',
  },
}

const failedOrder: AdminOrder = {
  ...deliveredOrder,
  status: 'failed',
  host_id: null,
  delivered_at: null,
  provision_error: '上游余额不足',
}

/** 重试交付成功后返回的订单（会员端订单视图口径，无 member 概要）。 */
const retriedOrder: Order = {
  ...deliveredOrder,
  status: 'active',
  host_id: 10922,
  delivered_at: '2026-10-08T11:00:00Z',
  provision_error: '',
}

function handlers(order: AdminOrder = deliveredOrder, retry: Order = retriedOrder) {
  return [
    apiGet('/api/v1/admin/orders/12', order),
    apiPost('/api/v1/admin/orders/12/retry-delivery', retry),
  ]
}

describe('管理后台 · 订单详情', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('渲染订单全字段、会员概要与交付信息', async () => {
    renderAdmin('/admin/orders/12', 'support', handlers())

    expect(await screen.findByRole('heading', { name: 'O20261008105520T0J03J' })).toBeInTheDocument()
    expect(screen.getByText('10922')).toBeInTheDocument()
    expect(screen.getByText('demo9（#9 · demo9@example.com）')).toBeInTheDocument()
    // 时间线：创建订单 → 支付到账 → 交付完成
    expect(screen.getByText('创建订单')).toBeInTheDocument()
    expect(screen.getByText('支付到账（在线支付）')).toBeInTheDocument()
    expect(screen.getByText('交付完成')).toBeInTheDocument()
    // 配置快照按上游本地 ID 原样展示
    expect(screen.getByText('配置项 #11')).toBeInTheDocument()
  })

  it('交付失败订单标红并展示脱敏原因', async () => {
    renderAdmin('/admin/orders/12', 'admin', handlers(failedOrder))

    // 「交付失败」同时出现在状态徽标、时间线与失败原因提示中
    expect((await screen.findAllByText('交付失败')).length).toBeGreaterThan(0)
    expect(screen.getAllByText('上游余额不足').length).toBeGreaterThan(0)
    expect(screen.getByText('未交付')).toBeInTheDocument()
  })

  it('admin：重试交付需二次确认，成功后按返回状态提示并刷新详情', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/orders/12', 'admin', handlers(failedOrder))

    await user.click(await screen.findByRole('button', { name: '重试交付' }))
    expect(await screen.findByText('确认重试交付？')).toBeInTheDocument()
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: '开始重试交付' }))

    await waitFor(() => {
      const call = fetchCalls().find(([url]) => url === '/api/v1/admin/orders/12/retry-delivery')
      expect(call?.[1].method).toBe('POST')
    })
    // 重试成功后重新拉取详情
    await waitFor(() => {
      expect(fetchCalls().filter(([url]) => url === '/api/v1/admin/orders/12').length).toBeGreaterThan(1)
    })
  })

  it('非 admin 角色：重试交付按钮禁用且不发起请求', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/orders/12', 'finance', handlers(failedOrder))

    const button = await screen.findByRole('button', { name: '重试交付' })
    expect(button).toBeDisabled()
    expect(screen.getByText(/仅超级管理员可执行/)).toBeInTheDocument()

    await user.click(button)
    expect(fetchCalls().some(([url]) => url.includes('/retry-delivery'))).toBe(false)
  })

  it('已交付订单：按钮禁用并提示当前状态不可重试', async () => {
    renderAdmin('/admin/orders/12', 'admin', handlers())

    const button = await screen.findByRole('button', { name: '重试交付' })
    expect(button).toBeDisabled()
    expect(screen.getByText(/仅「已支付」与「交付失败」的订单可重试/)).toBeInTheDocument()
  })

  it('重试被后端拒绝（40002）时展示 message', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/orders/12', 'admin', [
      apiGet('/api/v1/admin/orders/12', failedOrder),
      apiFail('POST', '/api/v1/admin/orders/12/retry-delivery', 40002, '订单已交付完成，无需重试'),
    ])

    await user.click(await screen.findByRole('button', { name: '重试交付' }))
    await user.click(within(await screen.findByRole('dialog')).getByRole('button', { name: '开始重试交付' }))

    expect(await screen.findByText('订单已交付完成，无需重试')).toBeInTheDocument()
  })

  it('订单不存在：给出空态与返回列表入口', async () => {
    renderAdmin('/admin/orders/999', 'admin', [
      apiFail('GET', '/api/v1/admin/orders/999', 404, '订单不存在', 404),
    ])

    expect(await screen.findByText('订单不存在')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '返回订单列表' })).toHaveAttribute('href', '/admin/orders')
  })

  it('订单 ID 非法：不发请求并给出提示', async () => {
    renderAdmin('/admin/orders/abc', 'admin', handlers())

    expect(await screen.findByText('订单 ID 不正确')).toBeInTheDocument()
    expect(fetchCalls().some(([url]) => url.includes('/admin/orders/abc'))).toBe(false)
  })
})
