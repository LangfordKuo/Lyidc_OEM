import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { apiGet, apiPost, renderConsole, requestBody } from '../../test/consoleHarness'

// 工单详情：消息流渲染、回复提交、关闭工单（二次确认）与「已关闭不可回复」。
function ticketWith(status: string) {
  return {
    id: 7,
    trade_no: 'T20261008201530K7Q2ZP',
    subject: '主机无法连接，请协助排查',
    category: 'technical',
    status,
    instance_id: 3,
    instance: {
      id: 3,
      name: 'oem-o20261008105520t0j03j',
      product_name: '美国一区 Kurun A型',
      status: 'active',
    },
    last_reply_at: '2026-10-08T12:15:30Z',
    closed_at: status === 'closed' ? '2026-10-08T12:19:14Z' : null,
    created_at: '2026-10-08T12:15:30Z',
    updated_at: '2026-10-08T12:15:30Z',
  }
}

const messages = [
  {
    id: 1,
    author_type: 'member',
    author_id: 1,
    author_name: 'demo7b',
    content: '从今天早上 9 点开始 SSH 就一直连不上（超时）。',
    internal: false,
    created_at: '2026-10-08T12:15:30Z',
  },
  {
    id: 2,
    author_type: 'admin',
    author_id: 2,
    author_name: 'cs01',
    content: '已为您重启主机，请再试。',
    internal: false,
    created_at: '2026-10-08T12:20:00Z',
  },
]

function renderDetail(status: string) {
  return renderConsole('/console/tickets/7', [
    apiPost('/api/v1/tickets/7/reply', {
      ticket: ticketWith('open'),
      message: {
        id: 3,
        author_type: 'member',
        author_id: 1,
        author_name: 'demo7b',
        content: '仍然超时',
        internal: false,
        created_at: '2026-10-08T12:22:00Z',
      },
    }),
    apiPost('/api/v1/tickets/7/close', { ticket: ticketWith('closed'), already_closed: false }),
    // 详情响应的工单字段是平铺的（data.status / data.messages），与实现口径一致
    apiGet('/api/v1/tickets/7', { ...ticketWith(status), messages }),
  ])
}

describe('会员区 · 工单详情', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('渲染消息流（会员与客服消息都在）', async () => {
    renderDetail('open')

    expect(await screen.findByText(/SSH 就一直连不上/)).toBeInTheDocument()
    expect(screen.getByText(/已为您重启主机/)).toBeInTheDocument()
    expect(screen.getByText('待客服处理')).toBeInTheDocument()
  })

  it('回复工单：提交内容到 reply 接口', async () => {
    const user = userEvent.setup()
    renderDetail('open')

    const textarea = await screen.findByPlaceholderText(/输入回复内容/)
    await user.type(textarea, '仍然超时')
    await user.click(screen.getByRole('button', { name: '发送回复' }))

    await waitFor(() => {
      expect(requestBody('POST', '/api/v1/tickets/7/reply')).toEqual({ content: '仍然超时' })
    })
  })

  it('关闭工单需要二次确认', async () => {
    const user = userEvent.setup()
    renderDetail('open')

    await user.click(await screen.findByRole('button', { name: '关闭工单' }))
    expect(screen.getByText('确认关闭工单？')).toBeInTheDocument()

    const dialog = screen.getByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '关闭工单' }))

    await waitFor(() => {
      expect(requestBody('POST', '/api/v1/tickets/7/close')).toBeUndefined()
    })
  })

  it('已关闭工单不可回复', async () => {
    renderDetail('closed')

    expect(await screen.findByText('工单已关闭，无法继续回复。')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('工单已关闭')).toBeDisabled()
    expect(screen.queryByRole('button', { name: '关闭工单' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '发送回复' })).toBeDisabled()
  })
})
