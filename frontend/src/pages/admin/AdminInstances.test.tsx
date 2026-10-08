import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AdminInstance } from '../../api/types'
import { renderAdmin } from '../../test/adminHarness'
import { apiGet, fetchCalls } from '../../test/consoleHarness'

// 实例列表：三角色均可读；状态与会员筛选落到查询参数。
const instance: AdminInstance = {
  id: 5,
  order_id: 12,
  host_id: 10922,
  product_id: 3,
  product_name: '香港二区 CN2 A型',
  name: 'oem-o20261008105520t0j03j',
  billing_cycle: 'monthly',
  next_due_date: '2026-11-08T10:55:25Z',
  status: 'active',
  upstream_status: 'Active',
  dedicated_ip: '203.0.113.9',
  cancel_status: 'none',
  cancel_type: '',
  cancel_request_id: 0,
  cancel_requested_at: null,
  created_at: '2026-10-08T10:55:40Z',
  member_id: 9,
}

function handlers() {
  return [
    apiGet('/api/v1/admin/instances', { items: [instance], page: 1, page_size: 20, total: 1 }),
  ]
}

describe('管理后台 · 实例列表', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('渲染实例行（含会员归属与状态）', async () => {
    renderAdmin('/admin/instances', 'admin', handlers())

    expect(await screen.findByText('oem-o20261008105520t0j03j')).toBeInTheDocument()
    // 表格内断言（「运行中」同时出现在状态筛选按钮上，需限定在表格范围内）
    const grid = screen.getByRole('grid')
    expect(within(grid).getByText('#9')).toBeInTheDocument()
    expect(within(grid).getByText('运行中')).toBeInTheDocument()
    expect(within(grid).getByText('203.0.113.9')).toBeInTheDocument()
  })

  it('support 角色也能查看列表（实例域只读开放）', async () => {
    renderAdmin('/admin/instances', 'support', handlers())

    expect(await screen.findByText('oem-o20261008105520t0j03j')).toBeInTheDocument()
  })

  it('状态筛选把 status 拼进请求', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/instances', 'admin', handlers())

    await screen.findByText('oem-o20261008105520t0j03j')
    await user.click(screen.getByRole('button', { name: '已暂停' }))

    await waitFor(() => {
      const urls = fetchCalls().map(([url]) => url)
      expect(urls.some((url) => url.includes('status=suspended'))).toBe(true)
    })
  })

  it('会员 ID 筛选：非法值给提示，合法值进请求', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/instances', 'admin', handlers())

    const input = await screen.findByLabelText('会员 ID')
    await user.type(input, 'abc')
    await user.click(screen.getByRole('button', { name: '查询' }))
    expect(await screen.findByText('会员 ID 必须为正整数')).toBeInTheDocument()

    await user.clear(input)
    await user.type(input, '9')
    await user.click(screen.getByRole('button', { name: '查询' }))

    await waitFor(() => {
      const urls = fetchCalls().map(([url]) => url)
      expect(urls.some((url) => url.includes('member_id=9'))).toBe(true)
    })
  })
})
