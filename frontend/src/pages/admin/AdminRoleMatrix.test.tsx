import { cleanup as cleanupRender, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type { AdminRole } from '@/api/types'
import { makeAdminAccount, makeAdminTicket } from '@/test/fixtures'
import { installFetchMock, ok, renderApp, seedAdminProfile, seedAdminToken } from '@/test/harness'

// 管理后台**角色权限渲染矩阵**：
//   1. 侧栏入口按角色隐藏（工单仅 admin/support，设置仅 admin）；
//   2. 整页无权限时渲染占位且**不发起无权限请求**（服务端仍会独立 403，前端只是提前规避）；
//   3. admin 与 finance 的可见性差异（对账、写操作）。

function mockBackend(role: AdminRole) {
  return installFetchMock((url) => {
    const path = url.pathname
    if (path === '/api/v1/admin/profile') {
      return ok(makeAdminAccount({ role, username: role }))
    }
    if (path === '/api/v1/admin/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    if (path === '/api/v1/admin/notifications') {
      return ok({ items: [], page: 1, page_size: 20, total: 0, unread: 0 })
    }
    if (path === '/api/v1/admin/tickets') {
      return ok({ items: [makeAdminTicket()], page: 1, page_size: 20, total: 1 })
    }
    if (path === '/api/v1/admin/orders') {
      return ok({ items: [], page: 1, page_size: 20, total: 0 })
    }
    if (path === '/api/v1/admin/members') {
      return ok({ items: [], page: 1, page_size: 20, total: 0 })
    }
    return undefined
  })
}

async function renderAs(role: AdminRole, path: string) {
  const fetchMock = mockBackend(role)
  seedAdminToken()
  seedAdminProfile({ role, username: role })
  renderApp([path])
  await screen.findByRole('navigation', { name: '管理后台导航' })
  return fetchMock
}

/** 侧栏可见的入口标签（桌面侧栏；移动端导航另有一份）。 */
function sidebarLabels(): string[] {
  return within(screen.getByRole('navigation', { name: '管理后台导航' }))
    .getAllByRole('link')
    .map((link) => link.textContent ?? '')
}

describe('管理后台 · 角色权限矩阵', () => {
  it('admin 侧栏含全部 8 个入口', async () => {
    await renderAs('admin', '/admin')
    await screen.findByRole('heading', { name: '仪表盘' })

    expect(sidebarLabels().map((label) => label.trim())).toEqual([
      '仪表盘',
      '商品',
      '订单',
      '会员',
      '实例',
      '工单',
      '设置',
      '通知',
    ])
  })

  it('finance 侧栏无工单与设置入口，其余保留', async () => {
    await renderAs('finance', '/admin')
    await screen.findByRole('heading', { name: '仪表盘' })

    const labels = sidebarLabels().map((label) => label.trim())
    expect(labels).not.toContain('工单')
    expect(labels).not.toContain('设置')
    expect(labels).toEqual(['仪表盘', '商品', '订单', '会员', '实例', '通知'])
  })

  it('support 侧栏无设置入口、保留工单入口', async () => {
    await renderAs('support', '/admin')
    await screen.findByRole('heading', { name: '仪表盘' })

    const labels = sidebarLabels().map((label) => label.trim())
    expect(labels).toContain('工单')
    expect(labels).not.toContain('设置')
    expect(labels).toEqual(['仪表盘', '商品', '订单', '会员', '实例', '工单', '通知'])
  })

  it('finance 直接访问工单页：无权占位且不请求工单接口', async () => {
    const fetchMock = await renderAs('finance', '/admin/tickets')

    expect(await screen.findByText('无权访问该页面')).toBeInTheDocument()
    expect(screen.getByText(/无权处理工单/)).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => String(input).includes('/admin/tickets'))).toBe(
      false,
    )
  })

  it('support 访问工单页：正常加载并请求工单接口', async () => {
    const fetchMock = await renderAs('support', '/admin/tickets')

    expect(await screen.findByRole('heading', { name: '工单' })).toBeInTheDocument()
    await waitFor(() => {
      expect(fetchMock.mock.calls.some(([input]) => String(input).includes('/admin/tickets'))).toBe(
        true,
      )
    })
  })

  it('support 直接访问设置页：无权占位且不请求设置接口', async () => {
    const fetchMock = await renderAs('support', '/admin/settings')

    expect(await screen.findByText('无权访问后台设置')).toBeInTheDocument()
    expect(
      fetchMock.mock.calls.some(([input]) => String(input).includes('/admin/settings')),
    ).toBe(false)
  })

  it('订单页为查看类：三个角色都可读', async () => {
    for (const role of ['admin', 'finance', 'support'] as const) {
      const fetchMock = await renderAs(role, '/admin/orders')
      await waitFor(() => {
        expect(
          fetchMock.mock.calls.some(([input]) =>
            String(input).includes('/api/v1/admin/orders'),
          ),
        ).toBe(true)
      })
      expect(screen.queryByText('无权访问该页面')).not.toBeInTheDocument()
      expect(screen.getAllByRole('heading', { name: '订单' }).length).toBeGreaterThan(0)
      // 每次循环结束卸载上一次渲染，避免多份应用同时挂在 document 上。
      cleanupRender()
    }
  })
})
