import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { apiGet, apiPost, renderConsole, requestBody } from '../../test/consoleHarness'

// 实例详情页：敏感字段默认遮蔽、危险操作走弹窗二次确认后才发起请求。
const instanceDetail = {
  id: 3,
  order_id: 12,
  host_id: 10923,
  product_id: 11,
  product_name: '美国一区 Kurun A型',
  name: 'oem-o20261008105520t0j03j',
  billing_cycle: 'monthly',
  next_due_date: '2026-11-08T10:55:25Z',
  status: 'active',
  upstream_status: 'Active',
  dedicated_ip: '203.0.113.10',
  cancel_status: 'none',
  cancel_type: '',
  cancel_request_id: 0,
  cancel_requested_at: null,
  created_at: '2026-10-08T10:55:20Z',
  assigned_ips: ['203.0.113.11'],
  port: 22,
  username: 'root',
  password: 'Abc1234567890xyz',
  updated_at: '2026-10-08T10:56:00Z',
}

const emptyLogs = { items: [], page: 1, page_size: 20, total: 0 }

function renderDetail() {
  return renderConsole('/console/servers/3', [
    apiGet('/api/v1/instances/3/logs', emptyLogs),
    apiPost('/api/v1/instances/3/power', {
      instance_id: 3,
      action: 'power_off',
      message: '关机指令已提交',
      status: 'active',
    }),
    apiGet('/api/v1/instances/3', instanceDetail),
  ])
}

describe('会员区 · 实例详情', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('主机密码默认遮蔽，点击「显示」后才出现明文', async () => {
    const user = userEvent.setup()
    renderDetail()

    expect(await screen.findByText('oem-o20261008105520t0j03j')).toBeInTheDocument()
    expect(screen.getByText('••••••••')).toBeInTheDocument()
    expect(screen.queryByText('Abc1234567890xyz')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '显示' }))

    expect(screen.getByText('Abc1234567890xyz')).toBeInTheDocument()
  })

  it('关机需在弹窗内二次确认，确认后才调用电源接口', async () => {
    const user = userEvent.setup()
    renderDetail()

    await screen.findByRole('heading', { name: '实例操作' })
    await user.click(screen.getByRole('button', { name: '关机' }))

    // 弹窗出现前不发起任何电源请求
    expect(screen.getByText('确认关机？')).toBeInTheDocument()
    expect(
      fetchCallsOf('/api/v1/instances/3/power'),
    ).toHaveLength(0)

    const dialog = screen.getByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '关机' }))

    await waitFor(() => {
      expect(requestBody('POST', '/api/v1/instances/3/power')).toEqual({ op: 'soft_off' })
    })
  })

  it('强制关机标注风险提示', async () => {
    const user = userEvent.setup()
    renderDetail()

    await screen.findByRole('heading', { name: '实例操作' })
    await user.click(screen.getByRole('button', { name: '强制关机' }))

    expect(screen.getByText('确认强制关机？')).toBeInTheDocument()
    expect(screen.getByText(/可能造成数据损坏或文件系统异常/)).toBeInTheDocument()
  })
})

function fetchCallsOf(prefix: string): [string, RequestInit][] {
  const mock = fetch as unknown as ReturnType<typeof vi.fn>
  return (mock.mock.calls as [string, RequestInit][]).filter(([url]) => url.startsWith(prefix))
}
