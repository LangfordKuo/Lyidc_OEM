import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { renderAdmin } from '../../test/adminHarness'
import { apiGet, apiPost, fail, requestBody, type ApiHandler } from '../../test/consoleHarness'

// 管理端工单详情（契约 16.3）：内部备注开关（请求体 internal 真假）、内部备注的显著标记、
// 关闭工单的二次确认、finance 无权、已关闭工单不可回复。
// 管理端详情响应是**嵌套**结构 `{ticket, messages}`（会员端同一口径），消息流**含**内部备注。

/** 管理端工单对象（含 member 概要）。 */
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

/** 三条消息：会员消息、客服公开回复、客服内部备注（internal=true）。 */
const messages = [
  {
    id: 1,
    author_type: 'member',
    author_id: 9,
    author_name: 'vfy6a',
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
  {
    id: 3,
    author_type: 'admin',
    author_id: 2,
    author_name: 'cs01',
    content: '上游已确认是路由黑洞，本单先挂起。',
    internal: true,
    created_at: '2026-10-08T12:21:00Z',
  },
]

/** 详情页桩：GET 详情（嵌套结构）+ POST 回复 / 关闭。 */
function renderDetail(status = 'open') {
  return renderAdmin('/admin/tickets/7', 'admin', [
    apiPost('/api/v1/admin/tickets/7/reply', {
      ticket: adminTicket({ status }),
      message: messages[1],
    }),
    apiPost('/api/v1/admin/tickets/7/close', {
      ticket: adminTicket({ status: 'closed' }),
      already_closed: false,
    }),
    apiGet('/api/v1/admin/tickets/7', {
      ticket: adminTicket({
        status,
        closed_at: status === 'closed' ? '2026-10-08T12:19:14Z' : null,
      }),
      messages,
    }),
  ])
}

describe('管理端 · 工单详情', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('打开「内部备注」开关后提交：请求体 internal=true', async () => {
    const user = userEvent.setup()
    renderDetail()

    const textarea = await screen.findByPlaceholderText(/输入回复内容/)
    await user.type(textarea, '上游已确认是路由黑洞')

    const toggle = screen.getByRole('switch', { name: '内部备注' })
    expect(toggle).not.toBeChecked()
    await user.click(toggle)
    expect(toggle).toBeChecked()

    await user.click(screen.getByRole('button', { name: '记录内部备注' }))

    await waitFor(() => {
      expect(requestBody('POST', '/api/v1/admin/tickets/7/reply')).toEqual({
        content: '上游已确认是路由黑洞',
        internal: true,
      })
    })
  })

  it('开关关闭时为公开回复：请求体 internal=false', async () => {
    const user = userEvent.setup()
    renderDetail()

    const textarea = await screen.findByPlaceholderText(/输入回复内容/)
    await user.type(textarea, '已为您重启主机，请再试。')
    await user.click(screen.getByRole('button', { name: '发送回复' }))

    await waitFor(() => {
      expect(requestBody('POST', '/api/v1/admin/tickets/7/reply')).toEqual({
        content: '已为您重启主机，请再试。',
        internal: false,
      })
    })
  })

  it('消息流渲染内部备注并显著标记；公开消息不带该标记', async () => {
    renderDetail()

    expect(await screen.findByText('从今天早上 9 点开始 SSH 就一直连不上（超时）。')).toBeInTheDocument()
    expect(screen.getByText('已为您重启主机，请再试。')).toBeInTheDocument()
    // 内部备注内容对管理端可见（会员端接口不会下发）
    expect(screen.getByText('上游已确认是路由黑洞，本单先挂起。')).toBeInTheDocument()

    const tag = screen.getByText('内部备注（会员不可见）')
    // 标记与其内容同处一个气泡（警示色气泡内）
    expect(tag.parentElement).toHaveTextContent('上游已确认是路由黑洞，本单先挂起。')
    // 只有内部备注那条消息带标记
    expect(screen.getAllByText('内部备注（会员不可见）')).toHaveLength(1)
    expect(screen.getByText('已为您重启主机，请再试。').parentElement).not.toHaveTextContent(
      '内部备注（会员不可见）',
    )
  })

  it('关闭工单需要二次确认，确认后发出 close 请求（无请求体）', async () => {
    const user = userEvent.setup()
    renderDetail()

    await user.click(await screen.findByRole('button', { name: '关闭工单' }))
    expect(screen.getByText('确认关闭工单？')).toBeInTheDocument()

    const dialog = screen.getByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '关闭工单' }))

    await waitFor(() => {
      expect(requestBody('POST', '/api/v1/admin/tickets/7/close')).toBeUndefined()
    })
  })

  it('finance 角色渲染无权访问，且不发起任何工单接口请求', async () => {
    const { calls } = renderAdmin('/admin/tickets/7', 'finance', [
      apiGet('/api/v1/admin/tickets/7', { ticket: adminTicket(), messages }),
    ])

    expect(await screen.findByText('无权访问该页面')).toBeInTheDocument()
    expect(calls.some(([url]) => url.startsWith('/api/v1/admin/tickets'))).toBe(false)
  })

  it('已关闭工单：回复区禁用且不渲染「关闭工单」按钮', async () => {
    renderDetail('closed')

    expect(await screen.findByText('工单已关闭，无法继续回复或记录备注。')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('工单已关闭')).toBeDisabled()
    expect(screen.getByRole('button', { name: '发送回复' })).toBeDisabled()
    expect(screen.getByRole('switch', { name: '内部备注' })).toBeDisabled()
    expect(screen.queryByRole('button', { name: '关闭工单' })).not.toBeInTheDocument()
  })

  it('工单不存在（404）时给出空态与返回列表入口', async () => {
    // 404 响应包由后端给出「工单不存在」文案，页面据此渲染空态（其余错误走 ErrorState）。
    const apiTicketNotFound: ApiHandler = (url, init) =>
      (init.method ?? 'GET').toUpperCase() === 'GET' && url.startsWith('/api/v1/admin/tickets/7')
        ? fail(404, '工单不存在', 404)
        : undefined

    renderAdmin('/admin/tickets/7', 'admin', [apiTicketNotFound])

    expect(await screen.findByText('工单不存在')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '返回工单列表' })).toBeInTheDocument()
  })
})
