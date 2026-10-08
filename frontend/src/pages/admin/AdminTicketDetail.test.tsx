import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { makeAdminAccount, makeAdminTicket, makeTicketMessage } from '@/test/fixtures'
import {
  installFetchMock,
  ok,
  renderApp,
  requestBody,
  seedAdminProfile,
  seedAdminToken,
} from '@/test/harness'

// 管理后台「工单详情」：内部备注开关（会员不可见标注 + internal 参数）、关闭确认、角色矩阵。

const TICKET = makeAdminTicket({
  id: 1,
  subject: '主机无法连接，请协助排查',
  status: 'open',
  instance_id: 101,
  instance: {
    id: 101,
    name: 'oem-o20261008105520t0j03j',
    product_name: '香港二区 CN2 A型',
    status: 'active',
  },
})

const MESSAGES = [
  makeTicketMessage({ id: 1, author_type: 'member', author_name: 'demo7a' }),
  makeTicketMessage({
    id: 2,
    author_type: 'admin',
    author_id: 1,
    author_name: 'cs6a',
    content: '已为您重启主机，请再试。',
  }),
  makeTicketMessage({
    id: 3,
    author_type: 'admin',
    author_id: 1,
    author_name: 'cs6a',
    content: '内部核实：该主机带宽被限速，正在与上游确认。',
    internal: true,
  }),
]

function mockTicket(role: 'admin' | 'support' | 'finance' = 'admin') {
  return installFetchMock((url, init) => {
    const path = url.pathname
    if (path === '/api/v1/admin/profile') {
      return ok(makeAdminAccount({ role, username: role }))
    }
    if (path === '/api/v1/admin/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    if (path === '/api/v1/admin/tickets/1') {
      return ok({ ticket: TICKET, messages: MESSAGES })
    }
    if (path === '/api/v1/admin/tickets/1/reply' && init.method === 'POST') {
      const body = requestBody<{ content: string; internal?: boolean }>(init)
      return ok({
        ticket: { ...TICKET, status: body?.internal ? 'open' : 'replied' },
        message: makeTicketMessage({
          id: 4,
          author_type: 'admin',
          author_id: 1,
          author_name: role,
          content: body?.content ?? '',
          internal: Boolean(body?.internal),
        }),
      })
    }
    if (path === '/api/v1/admin/tickets/1/close' && init.method === 'POST') {
      return ok({ ticket: { ...TICKET, status: 'closed' }, already_closed: false })
    }
    return undefined
  })
}

async function renderTicket(role: 'admin' | 'support' | 'finance' = 'admin') {
  const fetchMock = mockTicket(role)
  seedAdminToken()
  seedAdminProfile({ role, username: role })
  renderApp(['/admin/tickets/1'])
  // finance 无工单域权限，渲染的是整页无权占位（不发请求）
  if (role === 'finance') {
    await screen.findByText('无权访问该页面')
  } else {
    await screen.findByRole('heading', { name: TICKET.subject })
  }
  return fetchMock
}

describe('管理后台 · 工单详情与内部备注', () => {
  it('消息流含内部备注并显著标注「会员不可见」', async () => {
    await renderTicket()

    expect(screen.getByText('已为您重启主机，请再试。')).toBeInTheDocument()
    expect(screen.getByText('内部备注（会员不可见）')).toBeInTheDocument()
    expect(
      screen.getByText('内部核实：该主机带宽被限速，正在与上游确认。'),
    ).toBeInTheDocument()
    // 页头信息：工单号、分类、会员与关联实例
    expect(screen.getByText(TICKET.trade_no)).toBeInTheDocument()
    expect(screen.getAllByText(/会员 demo7a/).length).toBeGreaterThan(0)
    expect(screen.getByText('oem-o20261008105520t0j03j')).toBeInTheDocument()
  })

  it('默认是公开回复；打开「内部备注」开关后提交 internal=true', async () => {
    const fetchMock = await renderTicket()
    const user = userEvent.setup()

    // 默认：公开回复文案
    expect(screen.getByText(/当前为公开回复/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '发送回复' })).toBeInTheDocument()

    await user.type(screen.getByLabelText('回复内容'), '请提供 SSH 报错截图')
    await user.click(screen.getByRole('switch', { name: '内部备注' }))

    // 开关打开：按钮文案与提示切换，并出现「会员不可见」徽标
    expect(screen.getByText(/当前为内部备注/)).toBeInTheDocument()
    expect(screen.getByText('会员不可见')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '记录内部备注' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/admin/tickets/1/reply') && init?.method === 'POST',
      )
      expect(call).toBeTruthy()
      expect(requestBody(call?.[1] as RequestInit)).toEqual({
        content: '请提供 SSH 报错截图',
        internal: true,
      })
    })
  })

  it('公开回复提交 internal=false 并把工单转为待会员回复', async () => {
    const fetchMock = await renderTicket()
    const user = userEvent.setup()

    await user.type(screen.getByLabelText('回复内容'), '已重启，请再试')
    await user.click(screen.getByRole('button', { name: '发送回复' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/admin/tickets/1/reply') && init?.method === 'POST',
      )
      expect(requestBody(call?.[1] as RequestInit)).toEqual({
        content: '已重启，请再试',
        internal: false,
      })
    })
  })

  it('关闭工单：二次确认后调用关闭接口', async () => {
    const fetchMock = await renderTicket()
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: '关闭工单' }))
    expect(await screen.findByText('确认关闭工单？')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '关闭工单' }))

    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(([input, init]) =>
          String(input).includes('/admin/tickets/1/close') && init?.method === 'POST',
        ),
      ).toBe(true)
    })
  })

  it('财务角色：整页无权占位且不请求工单接口', async () => {
    const fetchMock = await renderTicket('finance')

    expect(await screen.findByText('无权访问该页面')).toBeInTheDocument()
    expect(screen.getByText(/无权处理工单/)).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => String(input).includes('/admin/tickets'))).toBe(
      false,
    )
  })
})
