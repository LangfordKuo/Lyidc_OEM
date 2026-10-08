import { screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import {
  makeAdminAccount,
  makeAdminInstance,
  makeAdminOrder,
  makeAdminProduct,
  makeAdminTicket,
  makeMember,
  makeNotification,
  makeRecharge,
} from '@/test/fixtures'
import { installFetchMock, ok, renderApp, seedAdminProfile, seedAdminToken } from '@/test/harness'

// 管理后台仪表盘：指标口径（total 只读）、最近列表渲染、角色矩阵（无权限不打请求）。

/** 按 pathname + status 参数给出不同的 total，便于断言每个指标卡取到的是对应接口。 */
function mockDashboard(role: 'admin' | 'finance' | 'support' = 'admin') {
  const admin = makeAdminAccount({ role, username: role, nickname: role })
  return installFetchMock((url) => {
    const path = url.pathname
    if (path === '/api/v1/admin/profile') {
      return ok(admin)
    }
    if (path === '/api/v1/admin/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    if (path === '/api/v1/admin/notifications') {
      return ok({
        items: [makeNotification({ title: '工单有新的回复' })],
        page: 1,
        page_size: 20,
        total: 1,
        unread: 0,
      })
    }
    if (path === '/api/v1/admin/upstream/health') {
      return ok({
        connected: true,
        base_url: 'https://idc.example.com/',
        latency_ms: 18,
        api_key_masked: 'abcd****wxyz',
        checked_at: '2026-10-08T06:20:00Z',
      })
    }
    if (path === '/api/v1/admin/members') {
      return ok({ items: [makeMember()], page: 1, page_size: 1, total: 12 })
    }
    if (path === '/api/v1/admin/orders') {
      const status = url.searchParams.get('status')
      if (status === 'paid') {
        return ok({ items: [], page: 1, page_size: 1, total: 3 })
      }
      if (status === 'failed') {
        return ok({ items: [], page: 1, page_size: 1, total: 1 })
      }
      const items = url.searchParams.get('page_size') === '5' ? [makeAdminOrder()] : []
      return ok({ items, page: 1, page_size: 5, total: 7 })
    }
    if (path === '/api/v1/admin/instances') {
      const status = url.searchParams.get('status')
      if (status === 'active') {
        return ok({ items: [], page: 1, page_size: 1, total: 3 })
      }
      if (status === 'suspended') {
        return ok({ items: [], page: 1, page_size: 1, total: 1 })
      }
      return ok({ items: [makeAdminInstance()], page: 1, page_size: 1, total: 4 })
    }
    if (path === '/api/v1/admin/tickets') {
      const status = url.searchParams.get('status')
      if (status === 'open') {
        return ok({ items: [], page: 1, page_size: 1, total: 2 })
      }
      return ok({ items: [makeAdminTicket()], page: 1, page_size: 5, total: 2 })
    }
    if (path === '/api/v1/admin/recharges') {
      const status = url.searchParams.get('status')
      if (status === 'paid') {
        return ok({ items: [], page: 1, page_size: 1, total: 5 })
      }
      return ok({ items: [makeRecharge()], page: 1, page_size: 5, total: 6 })
    }
    if (path === '/api/v1/admin/ledger') {
      return ok({ items: [], page: 1, page_size: 1, total: 9 })
    }
    if (path === '/api/v1/admin/product-groups') {
      return ok({ items: [] })
    }
    if (path === '/api/v1/admin/products') {
      return ok({ items: [makeAdminProduct()], page: 1, page_size: 20, total: 1 })
    }
    return undefined
  })
}

describe('管理后台 · 仪表盘', () => {
  it('渲染关键指标（各接口 total）与最近订单/工单/充值单', async () => {
    mockDashboard()
    seedAdminToken()
    seedAdminProfile()

    renderApp(['/admin'])
    expect(await screen.findByRole('heading', { name: '仪表盘' })).toBeInTheDocument()

    // 指标卡：会员 12 / 订单 7（待交付 3、失败 1）/ 实例 4 / 待处理工单 2 / 到账充值 5
    expect(await screen.findByText('12')).toBeInTheDocument()
    expect(screen.getByText('7')).toBeInTheDocument()
    expect(screen.getByTestId('stat-paid-orders')).toHaveTextContent('3')
    expect(screen.getByTestId('stat-failed-orders')).toHaveTextContent('1')
    expect(screen.getByText('4')).toBeInTheDocument()
    expect(screen.getByText('2')).toBeInTheDocument()
    expect(screen.getByText('5')).toBeInTheDocument()
    expect(screen.getByText(/流水条数 9/)).toBeInTheDocument()

    // 最近订单与最近工单条目
    expect(await screen.findByText('O20261008143015K7Q2ZP')).toBeInTheDocument()
    expect(screen.getByText('主机无法连接，请协助排查')).toBeInTheDocument()
    expect(screen.getByText('R20261008143500M3P8QT')).toBeInTheDocument()

    // 上游探活（第 9 节）
    expect(await screen.findByText('已连通')).toBeInTheDocument()
    expect(screen.getByText(/耗时 18 ms/)).toBeInTheDocument()
  })

  it('财务角色：工单指标显示「无权限」且不请求工单接口', async () => {
    const fetchMock = mockDashboard('finance')
    seedAdminToken()
    seedAdminProfile({ role: 'finance', username: 'fin6a', nickname: '财务' })

    renderApp(['/admin'])
    await screen.findByRole('heading', { name: '仪表盘' })

    await waitFor(() => expect(screen.getAllByText('无权限').length).toBeGreaterThan(0))
    expect(
      fetchMock.mock.calls.some(([input]) => String(input).includes('/admin/tickets')),
    ).toBe(false)
    // 财务侧栏不含工单与设置入口（桌面侧栏与移动端导航各渲染一份，故按桌面侧栏断言）
    const sidebar = within(screen.getByRole('navigation', { name: '管理后台导航' }))
    expect(sidebar.queryByRole('link', { name: '工单' })).not.toBeInTheDocument()
    expect(sidebar.queryByRole('link', { name: '设置' })).not.toBeInTheDocument()
    expect(sidebar.getByRole('link', { name: '商品' })).toBeInTheDocument()
  })

  it('客服角色：含工单入口但不含设置入口', async () => {
    mockDashboard('support')
    seedAdminToken()
    seedAdminProfile({ role: 'support', username: 'cs6a', nickname: '客服' })

    renderApp(['/admin'])
    await screen.findByRole('heading', { name: '仪表盘' })

    const sidebar = within(screen.getByRole('navigation', { name: '管理后台导航' }))
    expect(sidebar.getByRole('link', { name: '工单' })).toBeInTheDocument()
    expect(sidebar.queryByRole('link', { name: '设置' })).not.toBeInTheDocument()
  })
})
