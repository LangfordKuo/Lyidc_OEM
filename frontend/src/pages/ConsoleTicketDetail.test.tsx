import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { makeMember, makeTicket, makeTicketMessage } from '@/test/fixtures'
import { installFetchMock, ok, renderApp, requestBody, seedMemberToken } from '@/test/harness'

// 会员区「工单详情」：对话渲染（会员/客服分侧）与回复提交参数、关闭后禁回复。

const MEMBER_MESSAGE = makeTicketMessage({ id: 1, author_type: 'member', author_name: 'demo7a' })
const ADMIN_MESSAGE = makeTicketMessage({
  id: 2,
  author_type: 'admin',
  author_id: 1,
  author_name: 'cs01',
  content: '已为您重启主机，请再试。',
})

function mockTicketDetail(options: { closed?: boolean } = {}) {
  const ticket = makeTicket(
    options.closed
      ? { status: 'closed', closed_at: '2026-10-08T12:19:14Z' }
      : { status: 'replied' },
  )
  // 回复后追加到消息流，模拟真实后端的持久化效果。
  const extraMessages: ReturnType<typeof makeTicketMessage>[] = []
  return installFetchMock((url, init) => {
    if (url.pathname === '/api/v1/members/me') {
      return ok(makeMember())
    }
    if (url.pathname === '/api/v1/notifications/unread-count') {
      return ok({ unread: 1 })
    }
    if (url.pathname === '/api/v1/tickets/1' ) {
      return ok({ ticket, messages: [MEMBER_MESSAGE, ADMIN_MESSAGE, ...extraMessages] })
    }
    if (url.pathname === '/api/v1/tickets/1/reply' && init.method === 'POST') {
      const body = requestBody<{ content: string }>(init)
      const message = makeTicketMessage({
        id: 3 + extraMessages.length,
        author_type: 'member',
        author_name: 'demo7a',
        content: body?.content ?? '',
      })
      extraMessages.push(message)
      return ok({ ticket: { ...ticket, status: 'open' }, message })
    }
    if (url.pathname === '/api/v1/tickets/1/close' && init.method === 'POST') {
      return ok({ ticket: { ...ticket, status: 'closed' }, already_closed: false })
    }
    return undefined
  })
}

async function renderTicketDetail() {
  seedMemberToken()
  renderApp(['/console/tickets/1'])
  await screen.findByRole('heading', { name: '主机无法连接，请协助排查' })
}

describe('会员区 · 工单详情', () => {
  it('渲染对话记录：会员与客服消息、作者与状态', async () => {
    mockTicketDetail()
    await renderTicketDetail()

    expect(await screen.findByText('从今天早上 9 点开始 SSH 就一直连不上（超时）。')).toBeInTheDocument()
    expect(screen.getByText('已为您重启主机，请再试。')).toBeInTheDocument()
    expect(screen.getByText(/^我 ·/)).toBeInTheDocument()
    expect(screen.getByText(/客服 cs01/)).toBeInTheDocument()
    expect(screen.getByText('待会员回复')).toBeInTheDocument()
  })

  it('发送回复按 content 提交请求并刷新对话', async () => {
    const fetchMock = mockTicketDetail()
    const user = userEvent.setup()
    await renderTicketDetail()

    await user.type(screen.getByLabelText('回复内容'), '已重试，仍然不通。')
    await user.click(screen.getByRole('button', { name: '发送回复' }))

    const call = await waitFor(() => {
      const found = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/api/v1/tickets/1/reply') &&
          (init as RequestInit)?.method === 'POST',
      )
      expect(found).toBeTruthy()
      return found as [RequestInfo | URL, RequestInit]
    })
    expect(requestBody(call[1])).toEqual({ content: '已重试，仍然不通。' })

    // 提交后清空输入并重新拉取（回复出现在对话中）
    await waitFor(() => expect(screen.getByLabelText('回复内容')).toHaveValue(''))
    expect(await screen.findByText('已重试，仍然不通。')).toBeInTheDocument()
  })

  it('已关闭工单禁止回复并提示新开工单', async () => {
    mockTicketDetail({ closed: true })
    await renderTicketDetail()

    expect(await screen.findByText(/工单已于/)).toBeInTheDocument()
    expect(screen.getByLabelText('回复内容')).toBeDisabled()
    expect(screen.getByRole('button', { name: '发送回复' })).toBeDisabled()
    expect(screen.queryByRole('button', { name: '关闭工单' })).not.toBeInTheDocument()
  })

  it('关闭工单需二次确认，确认后提交关闭请求', async () => {
    const fetchMock = mockTicketDetail()
    const user = userEvent.setup()
    await renderTicketDetail()

    await user.click(screen.getByRole('button', { name: '关闭工单' }))
    const dialog = await screen.findByRole('alertdialog')
    expect(dialog).toHaveTextContent('关闭后双方都不能再回复')

    await user.click(within(dialog).getByRole('button', { name: '关闭工单' }))

    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(([input]) =>
          String(input).includes('/api/v1/tickets/1/close'),
        ),
      ).toBe(true)
    })
  })
})
