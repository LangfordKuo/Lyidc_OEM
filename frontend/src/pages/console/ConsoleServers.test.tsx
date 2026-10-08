import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { apiGet, fetchCalls, renderConsole } from '../../test/consoleHarness'

// 会员区「我的服务器」列表：实例渲染、临期高亮、状态筛选。
const inDays = (days: number) => new Date(Date.now() + days * 86_400_000).toISOString()

const instanceSummary = (overrides: Record<string, unknown>) => ({
  id: 3,
  order_id: 12,
  host_id: 10923,
  product_id: 11,
  product_name: '美国一区 Kurun A型',
  name: 'oem-o20261008105520t0j03j',
  billing_cycle: 'monthly',
  next_due_date: inDays(30),
  status: 'active',
  upstream_status: 'Active',
  dedicated_ip: '203.0.113.10',
  cancel_status: 'none',
  cancel_type: '',
  cancel_request_id: 0,
  cancel_requested_at: null,
  created_at: '2026-10-08T10:55:20Z',
  ...overrides,
})

describe('会员区 · 我的服务器列表', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('渲染实例信息、状态与到期天数（临期显示天数提示）', async () => {
    renderConsole('/console/servers', [
      apiGet('/api/v1/instances?', {
        items: [
          instanceSummary({ next_due_date: inDays(3) }),
          instanceSummary({
            id: 4,
            name: 'oem-suspended',
            status: 'suspended',
            cancel_status: 'pending',
            cancel_type: 'immediate',
          }),
        ],
        page: 1,
        page_size: 20,
        total: 2,
      }),
    ])

    expect(await screen.findByText('oem-o20261008105520t0j03j')).toBeInTheDocument()
    // 「运行中」「已暂停」既是筛选按钮也是状态 chip，这里断言至少各出现一次
    expect(screen.getAllByText('运行中').length).toBeGreaterThan(0)
    expect(screen.getAllByText('已暂停').length).toBeGreaterThan(0)
    // 取消申请在途标记（契约 15.8.1）
    expect(screen.getByText('终止申请在途')).toBeInTheDocument()
    // 到期天数（3 天后到期，7 天阈值内为临期）
    expect(screen.getByText(/3 天后到期/)).toBeInTheDocument()
    expect(screen.getAllByText(/203\.0\.113\.10/).length).toBeGreaterThan(0)
  })

  it('切换状态筛选会把 status 拼进请求', async () => {
    const user = userEvent.setup()
    renderConsole('/console/servers', [
      apiGet('/api/v1/instances?', { items: [], page: 1, page_size: 20, total: 0 }),
    ])

    await screen.findByText('还没有服务器', { exact: false })
    await user.click(screen.getByRole('button', { name: '已暂停' }))

    await waitFor(() => {
      const urls = fetchCalls().map(([url]) => url)
      expect(urls.some((url) => url.includes('/api/v1/instances?') && url.includes('status=suspended'))).toBe(
        true,
      )
    })
  })

  it('无实例时给出选购引导', async () => {
    renderConsole('/console/servers', [
      apiGet('/api/v1/instances?', { items: [], page: 1, page_size: 20, total: 0 }),
    ])

    expect(await screen.findByText('还没有服务器')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '选购服务器' })).toBeInTheDocument()
  })
})
