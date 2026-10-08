import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { renderAdmin } from '../../test/adminHarness'
import { apiGet } from '../../test/consoleHarness'

// 管理端工单列表（契约 16.3 管理端）：角色矩阵（finance 无权且不发请求）、列表渲染与筛选拼参。
// 管理端列表项在会员端视图之外多 member_id 与 member 概要。

/** 管理端工单列表项（含 member 概要）。 */
function adminTicket(overrides: Record<string, unknown> = {}) {
  return {
    id: 7,
    trade_no: 'T20261008201530K7Q2ZP',
    subject: '主机无法连接，请协助排查',
    category: 'technical',
    status: 'open',
    instance_id: 3,
    instance: {
      id: 3,
      name: 'oem-o20261008105520t0j03j',
      product_name: '美国一区 Kurun A型',
      status: 'active',
    },
    last_reply_at: '2026-10-08T12:15:30Z',
    closed_at: null,
    created_at: '2026-10-08T12:15:30Z',
    updated_at: '2026-10-08T12:15:30Z',
    member_id: 9,
    member: { id: 9, username: 'vfy6a', nickname: '验证会员' },
    ...overrides,
  }
}

/** 分页包（管理端工单列表口径 {items, page, page_size, total}）。 */
function paged(items: unknown[]) {
  return { items, page: 1, page_size: 20, total: items.length }
}

describe('管理端 · 工单列表', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('finance 角色渲染无权访问，且不发起任何工单接口请求', async () => {
    const { calls } = renderAdmin('/admin/tickets', 'finance', [
      apiGet('/api/v1/admin/tickets', paged([adminTicket()])),
    ])

    expect(await screen.findByText('无权访问该页面')).toBeInTheDocument()
    expect(calls.some(([url]) => url.startsWith('/api/v1/admin/tickets'))).toBe(false)
  })

  it('support 角色可看到工单列表（单号 / 会员 / 分类 / 状态 / 最近活动）', async () => {
    renderAdmin('/admin/tickets', 'support', [
      apiGet('/api/v1/admin/tickets', paged([adminTicket()])),
    ])

    expect(await screen.findByText('主机无法连接，请协助排查')).toBeInTheDocument()
    expect(screen.getByText('T20261008201530K7Q2ZP')).toBeInTheDocument()
    expect(screen.getByText('vfy6a')).toBeInTheDocument()
    expect(screen.getByText('验证会员')).toBeInTheDocument()

    // 分类 / 状态 / 操作都在行内（「技术」「待客服处理」等文案同时出现在筛选控件上，所以按行定位）。
    const row = screen.getByText('主机无法连接，请协助排查').closest('tr')
    expect(row).not.toBeNull()
    expect(within(row as HTMLElement).getByText('技术')).toBeInTheDocument()
    expect(within(row as HTMLElement).getByText('待客服处理')).toBeInTheDocument()
    expect(within(row as HTMLElement).getByText('查看详情')).toBeInTheDocument()
  })

  it('状态筛选把 status=open 拼进请求 URL', async () => {
    const user = userEvent.setup()
    const { calls } = renderAdmin('/admin/tickets', 'admin', [
      apiGet('/api/v1/admin/tickets', paged([])),
    ])

    await user.click(await screen.findByRole('button', { name: '待客服处理' }))

    await waitFor(() => {
      const matched = calls.filter(([url]) => url.startsWith('/api/v1/admin/tickets'))
      expect(matched.length).toBeGreaterThan(1)
      const url = new URL(matched[matched.length - 1][0], 'http://localhost')
      expect(url.searchParams.get('status')).toBe('open')
      expect(url.searchParams.get('page')).toBe('1')
    })
  })

  it('关键词搜索把 keyword 拼进请求 URL', async () => {
    const user = userEvent.setup()
    const { calls } = renderAdmin('/admin/tickets', 'admin', [
      apiGet('/api/v1/admin/tickets', paged([])),
    ])

    await user.type(await screen.findByPlaceholderText('标题或工单号'), 'T2026')
    await user.click(screen.getByRole('button', { name: '查询' }))

    await waitFor(() => {
      const matched = calls.filter(([url]) => url.startsWith('/api/v1/admin/tickets'))
      expect(matched.length).toBeGreaterThan(1)
      expect(new URL(matched[matched.length - 1][0], 'http://localhost').searchParams.get('keyword')).toBe(
        'T2026',
      )
    })
  })

  it('会员 ID 只接受正整数：非法输入给出提示且不带 member_id 查询', async () => {
    const user = userEvent.setup()
    const { calls } = renderAdmin('/admin/tickets', 'admin', [
      apiGet('/api/v1/admin/tickets', paged([])),
    ])

    await user.type(await screen.findByPlaceholderText('全部会员'), 'abc')
    await user.click(screen.getByRole('button', { name: '查询' }))

    expect(await screen.findByText('会员 ID 必须为正整数')).toBeInTheDocument()
    expect(calls.some(([url]) => url.includes('member_id='))).toBe(false)
  })
})
