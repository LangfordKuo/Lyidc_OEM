import { screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { renderAdmin } from '../../test/adminHarness'
import { apiGet, fetchCalls } from '../../test/consoleHarness'

// 仪表盘：只用既有接口能取到的指标；无权限的卡片显示「无权限」且不发起请求。
const paged = (total: number) => ({ items: [], page: 1, page_size: 1, total })

function dashboardHandlers() {
  return [
    apiGet('/api/v1/admin/members', paged(7)),
    apiGet('/api/v1/admin/instances', paged(3)),
    apiGet('/api/v1/admin/tickets', paged(2)),
    apiGet('/api/v1/admin/recharges', paged(5)),
    apiGet('/api/v1/admin/ledger', paged(9)),
    apiGet('/api/v1/admin/upstream/health', {
      connected: true,
      base_url: 'https://idc.example.com',
      latency_ms: 12,
      api_key_masked: 'abcd****',
      checked_at: '2026-10-08T06:00:00Z',
    }),
  ]
}

/** 读取某张指标卡内部的文本（按卡标题定位，避免数字在页面多处出现时误匹配）。 */
function cardText(label: string): string {
  const card = screen.getByText(label).closest('[data-slot="card"]')
  if (!card) {
    throw new Error(`未找到指标卡：${label}`)
  }
  return card.textContent ?? ''
}

describe('管理后台 · 仪表盘', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('admin：渲染会员/实例/工单/财务指标与上游连通性', async () => {
    renderAdmin('/admin', 'admin', dashboardHandlers())

    expect(await screen.findByRole('heading', { name: '仪表盘' })).toBeInTheDocument()
    // 等指标加载完成
    expect(await screen.findByText('会员总数')).toBeInTheDocument()

    expect(cardText('会员总数')).toContain('7')
    expect(cardText('实例总数')).toContain('3')
    expect(cardText('待处理工单')).toContain('2')
    expect(cardText('已到账充值')).toContain('5')

    // 指标只读总数：请求带 page_size=1（轻量）
    const urls = fetchCalls().map(([url]) => url)
    expect(urls.some((url) => url.startsWith('/api/v1/admin/members?') && url.includes('page_size=1'))).toBe(
      true,
    )
    expect(await screen.findByText('已连通')).toBeInTheDocument()
  })

  it('finance：工单卡显示无权限，且不请求工单接口', async () => {
    renderAdmin('/admin', 'finance', dashboardHandlers())

    expect(await screen.findByText('待处理工单')).toBeInTheDocument()
    expect(cardText('待处理工单')).toContain('无权限')

    const urls = fetchCalls().map(([url]) => url)
    expect(urls.some((url) => url.includes('/api/v1/admin/tickets'))).toBe(false)
    // 财务仍可读对账数据；上游探活按契约第 9 节三类角色都可只读调用
    expect(urls.some((url) => url.includes('/api/v1/admin/recharges'))).toBe(true)
    expect(urls.some((url) => url.includes('/api/v1/admin/upstream/health'))).toBe(true)
  })

  it('support：可见工单指标，但财务卡显示无权限', async () => {
    renderAdmin('/admin', 'support', dashboardHandlers())

    expect(await screen.findByText('已到账充值')).toBeInTheDocument()
    expect(cardText('已到账充值')).toContain('无权限')

    const urls = fetchCalls().map(([url]) => url)
    expect(urls.some((url) => url.includes('/api/v1/admin/tickets'))).toBe(true)
    expect(urls.some((url) => url.includes('/api/v1/admin/ledger'))).toBe(false)
  })

  it('明确标注订单类指标暂缺（不造假）', async () => {
    renderAdmin('/admin', 'admin', dashboardHandlers())

    expect(await screen.findByText('暂缺的指标（接口未提供）')).toBeInTheDocument()
    expect(screen.getByText(/GET \/admin\/orders/)).toBeInTheDocument()
  })

  it('侧栏按角色矩阵渲染入口：finance 看不到工单与设置', async () => {
    renderAdmin('/admin', 'finance', dashboardHandlers())

    const nav = await screen.findByRole('navigation', { name: '管理后台导航' })
    expect(within(nav).queryByRole('link', { name: /工单/ })).not.toBeInTheDocument()
    expect(within(nav).queryByRole('link', { name: /设置/ })).not.toBeInTheDocument()
    expect(within(nav).getByRole('link', { name: /商品/ })).toBeInTheDocument()
  })
})
