import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { makeAdminAccount, makeNotification } from '@/test/fixtures'
import { installFetchMock, ok, renderApp, seedAdminProfile, seedAdminToken } from '@/test/harness'

// 管理后台「通知」页（契约 17.2）：本人收件箱、未读筛选、单条已读与全部已读。

const UNREAD = makeNotification({ id: 1, title: '工单待处理：主机无法连接' })
const READ = makeNotification({
  id: 2,
  title: '工单已关闭',
  event: 'ticket_closed',
  read: true,
  read_at: '2026-10-08T23:00:00Z',
})

function mockNotifications() {
  // 有状态替身：全部已读之后列表接口回带的 unread 也应变为 0（与真实后端一致）。
  const state = { readAll: false }
  return installFetchMock((url, init) => {
    const path = url.pathname
    if (path === '/api/v1/admin/profile') {
      return ok(makeAdminAccount())
    }
    if (path === '/api/v1/admin/notifications/unread-count') {
      return ok({ unread: state.readAll ? 0 : 1 })
    }
    if (path === '/api/v1/admin/notifications') {
      const unreadOnly = url.searchParams.get('unread') === 'true'
      const items = unreadOnly ? [UNREAD] : [UNREAD, READ]
      return ok({
        items,
        page: 1,
        page_size: 20,
        total: items.length,
        unread: state.readAll ? 0 : 1,
      })
    }
    if (path === '/api/v1/admin/notifications/1/read' && init.method === 'POST') {
      return ok({
        notification: { ...UNREAD, read: true, read_at: '2026-10-08T23:10:00Z' },
        already_read: false,
      })
    }
    if (path === '/api/v1/admin/notifications/read-all' && init.method === 'POST') {
      state.readAll = true
      return ok({ updated: 1, unread: 0 })
    }
    return undefined
  })
}

async function renderNotifications() {
  const fetchMock = mockNotifications()
  seedAdminToken()
  seedAdminProfile()
  renderApp(['/admin/notifications'])
  await screen.findByRole('heading', { name: '通知' })
  await screen.findByText('工单待处理：主机无法连接')
  return fetchMock
}

describe('管理后台 · 通知收件箱', () => {
  it('渲染收件箱与未读数（列表响应回带）', async () => {
    await renderNotifications()

    expect(screen.getByTestId('admin-unread-count')).toHaveTextContent('1')
    expect(screen.getByText('工单已关闭')).toBeInTheDocument()
    // 事件徽标（契约 17.1 的 9 个事件枚举）；「工单」在侧栏也有同名入口，故按数量断言
    expect(screen.getAllByText('工单').length).toBeGreaterThan(1)
    expect(screen.getByText('工单关闭')).toBeInTheDocument()
  })

  it('未读筛选按 unread=true 请求', async () => {
    const fetchMock = await renderNotifications()
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: '未读' }))
    await waitFor(() => {
      expect(fetchMock.mock.calls.some(([input]) => String(input).includes('unread=true'))).toBe(
        true,
      )
    })
  })

  it('标记单条已读调用接口并刷新', async () => {
    const fetchMock = await renderNotifications()
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: '标记已读' }))
    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(([input, init]) =>
          String(input).includes('/admin/notifications/1/read') && init?.method === 'POST',
        ),
      ).toBe(true)
    })
  })

  it('全部已读调用接口并清零未读徽章', async () => {
    const fetchMock = await renderNotifications()
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: '全部已读' }))
    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(([input, init]) =>
          String(input).includes('/admin/notifications/read-all') && init?.method === 'POST',
        ),
      ).toBe(true)
    })
    await waitFor(() => expect(screen.getByTestId('admin-unread-count')).toHaveTextContent('0'))
  })
})
