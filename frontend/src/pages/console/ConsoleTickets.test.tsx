import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { apiGet, apiPost, renderConsole, requestBody } from '../../test/consoleHarness'

// 工单列表与提单弹窗：字段校验、关联实例可选、带参进入时的预填。
const instanceOption = {
  id: 101,
  order_id: 101,
  host_id: 40011,
  product_id: 7,
  product_name: '香港二区 CN2 A型',
  name: 'oem-demo7b-hk01',
  billing_cycle: 'monthly',
  next_due_date: '2026-11-01T00:00:00Z',
  status: 'active',
  upstream_status: 'Active',
  dedicated_ip: '203.0.113.21',
  cancel_status: 'none',
  cancel_type: '',
  cancel_request_id: 0,
  cancel_requested_at: null,
  created_at: '2026-10-08T06:00:30Z',
}

const createdTicket = {
  ticket: { id: 104, trade_no: 'T20261008160000DEMO04', status: 'open' },
  message: { id: 110, content: '内容' },
}

function renderTickets(path = '/console/tickets') {
  return renderConsole(path, [
    apiGet('/api/v1/instances?', { items: [instanceOption], page: 1, page_size: 100, total: 1 }),
    apiPost('/api/v1/tickets', createdTicket),
    apiGet('/api/v1/tickets?', { items: [], page: 1, page_size: 20, total: 0 }),
  ])
}

describe('会员区 · 工单列表与提单', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('空态给出提单引导', async () => {
    renderTickets()

    expect(await screen.findByText('还没有工单')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '提交工单' })).toBeInTheDocument()
  })

  it('提单：默认不关联实例，标题与内容提交到 POST /tickets', async () => {
    const user = userEvent.setup()
    renderTickets()

    await user.click(await screen.findByRole('button', { name: '提交工单' }))
    const dialog = screen.getByRole('dialog')

    await user.type(within(dialog).getByPlaceholderText(/一句话描述问题/), '主机无法连接')
    await user.type(within(dialog).getByPlaceholderText(/请描述现象/), '从早上开始 SSH 超时')
    await user.click(within(dialog).getByRole('button', { name: '提交工单' }))

    await waitFor(() => {
      expect(requestBody('POST', '/api/v1/tickets')).toEqual({
        subject: '主机无法连接',
        content: '从早上开始 SSH 超时',
        category: 'technical',
      })
    })
  })

  it('标题过短时给出校验提示且不提交', async () => {
    const user = userEvent.setup()
    renderTickets()

    await user.click(await screen.findByRole('button', { name: '提交工单' }))
    const dialog = screen.getByRole('dialog')
    await user.type(within(dialog).getByPlaceholderText(/一句话描述问题/), '太短')
    await user.type(within(dialog).getByPlaceholderText(/请描述现象/), '内容')
    await user.click(within(dialog).getByRole('button', { name: '提交工单' }))

    // 标题的校验提示会同时出现在字段下方与底部错误条，因此用 findAllByText
    expect((await screen.findAllByText(/标题需为 5-100 个字符/)).length).toBeGreaterThan(0)
    expect(
      (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls.some(
        ([url, init]) =>
          String(url).startsWith('/api/v1/tickets') &&
          (init as RequestInit)?.method === 'POST',
      ),
    ).toBe(false)
  })

  it('带 compose=1 参数进入时自动打开弹窗并预填（订单页「提交工单」入口）', async () => {
    renderTickets('/console/tickets?compose=1&subject=%E8%AE%A2%E5%8D%95%E4%BA%A4%E4%BB%98%E5%A4%B1%E8%B4%A5&content=%E8%AF%B7%E5%8D%8F%E5%8A%A9')

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByDisplayValue('订单交付失败')).toBeInTheDocument()
    expect(within(dialog).getByDisplayValue('请协助')).toBeInTheDocument()
  })
})
