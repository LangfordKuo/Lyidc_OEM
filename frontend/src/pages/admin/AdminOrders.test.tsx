import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AdminOrder } from '../../api/types'
import { apiFail, renderAdmin } from '../../test/adminHarness'
import { apiGet, fetchCalls } from '../../test/consoleHarness'

// 订单列表（阶段 8b 新增接口 GET /admin/orders）：三角色均可查看，筛选落到查询参数。
const order: AdminOrder = {
  id: 12,
  trade_no: 'O20261008105520T0J03J',
  member_id: 9,
  product_id: 3,
  product_name: '香港二区 CN2 A型',
  cycle: 'monthly',
  qty: 1,
  config: {},
  amount: '100.00',
  discount_amount: '0.00',
  final_amount: '100.00',
  coupon_code: '',
  status: 'failed',
  type: 'new',
  pay_channel: 'epay',
  channel_trade_no: 'CH20261008001',
  pay_time: '2026-10-08T10:55:25Z',
  host_id: null,
  instance_id: null,
  provision_error: '上游余额不足',
  delivered_at: null,
  created_at: '2026-10-08T10:55:20Z',
  updated_at: '2026-10-08T10:55:30Z',
  member: {
    id: 9,
    username: 'demo9',
    nickname: '演示会员',
    email: 'demo9@example.com',
    status: 'active',
  },
}

const paged = (items: AdminOrder[]) => ({ items, page: 1, page_size: 20, total: items.length })

function handlers(items: AdminOrder[] = [order]) {
  return [apiGet('/api/v1/admin/orders', paged(items))]
}

describe('管理后台 · 订单列表', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('渲染订单行（订单号、会员、商品、金额、状态）', async () => {
    renderAdmin('/admin/orders', 'admin', handlers())

    expect(await screen.findByText('O20261008105520T0J03J')).toBeInTheDocument()
    const grid = screen.getByRole('grid')
    expect(within(grid).getByText('demo9')).toBeInTheDocument()
    expect(within(grid).getByText('香港二区 CN2 A型')).toBeInTheDocument()
    expect(within(grid).getByText('¥100.00')).toBeInTheDocument()
    expect(within(grid).getByText('交付失败')).toBeInTheDocument()
    // 订单号链接到详情页
    expect(
      within(grid).getByRole('link', { name: 'O20261008105520T0J03J' }),
    ).toHaveAttribute('href', '/admin/orders/12')
  })

  it('support / finance 角色也能查看列表（订单查看类接口对三角色开放）', async () => {
    renderAdmin('/admin/orders', 'support', handlers())

    expect(await screen.findByText('O20261008105520T0J03J')).toBeInTheDocument()
  })

  it('状态与类型筛选把参数拼进请求', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/orders', 'admin', handlers())

    await screen.findByText('O20261008105520T0J03J')
    await user.selectOptions(screen.getByLabelText('状态'), 'failed')
    await user.selectOptions(screen.getByLabelText('类型'), 'renew')

    await waitFor(() => {
      const urls = fetchCalls().map(([url]) => url)
      expect(urls.some((url) => url.includes('status=failed') && url.includes('type=renew'))).toBe(true)
    })
  })

  it('会员 ID 与订单号筛选：非法会员 ID 给提示，合法值一起进请求', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/orders', 'admin', handlers())

    const memberInput = await screen.findByLabelText('会员 ID')
    await user.type(memberInput, 'abc')
    await user.click(screen.getByRole('button', { name: '查询' }))
    expect(await screen.findByText('会员 ID 必须为正整数')).toBeInTheDocument()

    await user.clear(memberInput)
    await user.type(memberInput, '9')
    await user.type(screen.getByLabelText('订单号'), 'O20261008')
    await user.click(screen.getByRole('button', { name: '查询' }))

    await waitFor(() => {
      const urls = fetchCalls().map(([url]) => url)
      expect(
        urls.some((url) => url.includes('member_id=9') && url.includes('trade_no=O20261008')),
      ).toBe(true)
    })
  })

  it('空列表给出筛选态空提示，并在重置后回到全量', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/orders', 'admin', handlers([]))

    expect(await screen.findByText('还没有订单')).toBeInTheDocument()
    await user.type(screen.getByLabelText('订单号'), 'O2026')
    await user.click(screen.getByRole('button', { name: '查询' }))
    expect(await screen.findByText('当前筛选下没有订单')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '重置筛选' }))
    await waitFor(() => {
      const urls = fetchCalls().map(([url]) => url)
      expect(urls[urls.length - 1]).not.toContain('trade_no=')
    })
  })

  it('接口失败时展示后端 message 并可重试', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/orders', 'admin', [
      apiFail('GET', '/api/v1/admin/orders', 50001, '数据库操作失败', 500),
    ])

    expect(await screen.findByText('数据库操作失败')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '重新加载' }))
    expect(await screen.findByText('数据库操作失败')).toBeInTheDocument()
  })
})
