import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { makeInstance, makeMember, makeTicket, makeTicketMessage } from '@/test/fixtures'
import { installFetchMock, ok, renderApp, requestBody, seedMemberToken } from '@/test/harness'

// 会员区「工单列表」：列表渲染、提交工单弹窗（校验 + 提交参数）。

function mockTickets() {
  return installFetchMock((url, init) => {
    if (url.pathname === '/api/v1/members/me') {
      return ok(makeMember())
    }
    if (url.pathname === '/api/v1/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    if (url.pathname === '/api/v1/tickets' && init.method === 'POST') {
      return ok({
        ticket: makeTicket({ id: 9, status: 'open' }),
        message: makeTicketMessage({ id: 9 }),
      })
    }
    if (url.pathname === '/api/v1/tickets') {
      return ok({
        items: [makeTicket({ status: 'replied', instance: null })],
        page: 1,
        page_size: 20,
        total: 1,
      })
    }
    if (url.pathname === '/api/v1/instances') {
      return ok({ items: [makeInstance()], page: 1, page_size: 100, total: 1 })
    }
    return undefined
  })
}

async function renderTickets() {
  seedMemberToken()
  renderApp(['/console/tickets'])
  await screen.findByRole('heading', { name: '工单' })
}

describe('会员区 · 工单列表', () => {
  it('渲染工单列表项：标题、工单号、分类与状态', async () => {
    mockTickets()
    await renderTickets()

    expect(await screen.findByText('主机无法连接，请协助排查')).toBeInTheDocument()
    expect(screen.getByText('T20261008201530K7Q2ZP')).toBeInTheDocument()
    // 分类与状态徽标在卡片内查询，避免与筛选按钮的同名文案冲突
    const card = screen.getByTestId('ticket-card-1')
    expect(within(card).getByText('技术')).toBeInTheDocument()
    expect(within(card).getByText('待会员回复')).toBeInTheDocument()
  })

  it('提交工单：标题不合法时给出提示且不提交', async () => {
    const fetchMock = mockTickets()
    const user = userEvent.setup()
    await renderTickets()

    await user.click(screen.getByRole('button', { name: '提交工单' }))
    const dialog = await screen.findByRole('dialog')

    await user.type(within(dialog).getByLabelText('标题'), '太短')
    await user.type(within(dialog).getByLabelText('问题描述'), '描述内容')
    await user.click(within(dialog).getByRole('button', { name: '提交工单' }))

    // 字段下方的即时提示与提交兜底 Alert 都会出现同一文案，故用 getAllByText
    expect((await within(dialog).findAllByText(/标题需为 5-100 个字符/)).length).toBeGreaterThan(0)
    expect(
      fetchMock.mock.calls.some(
        ([input, init]) =>
          String(input).includes('/api/v1/tickets') && (init as RequestInit)?.method === 'POST',
      ),
    ).toBe(false)
  })

  it('提交工单：填写合法内容后按契约字段提交并跳转详情', async () => {
    const fetchMock = mockTickets()
    const user = userEvent.setup()
    await renderTickets()

    await user.click(screen.getByRole('button', { name: '提交工单' }))
    const dialog = await screen.findByRole('dialog')

    await user.type(within(dialog).getByLabelText('标题'), '主机无法连接，请协助排查')
    await user.click(within(dialog).getByRole('radio', { name: '财务' }))
    await user.type(within(dialog).getByLabelText('问题描述'), '从今天早上 9 点开始 SSH 一直超时。')

    // 关联实例来自实例列表接口
    const instanceRadio = await within(dialog).findByRole('radio', {
      name: /oem-o20261008105520t0j03j/,
    })
    await user.click(instanceRadio)

    await user.click(within(dialog).getByRole('button', { name: '提交工单' }))

    const call = await waitFor(() => {
      const found = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/api/v1/tickets') && (init as RequestInit)?.method === 'POST',
      )
      expect(found).toBeTruthy()
      return found as [RequestInfo | URL, RequestInit]
    })
    expect(requestBody(call[1])).toEqual({
      subject: '主机无法连接，请协助排查',
      content: '从今天早上 9 点开始 SSH 一直超时。',
      category: 'billing',
      instance_id: 101,
    })
  })
})
