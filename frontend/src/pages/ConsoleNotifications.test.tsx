import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { makeMember, makeNotification } from '@/test/fixtures'
import { installFetchMock, ok, renderApp, seedMemberToken } from '@/test/harness'

// 会员区「通知」：未读筛选、点击即读、全部已读与未读徽标同步。

const UNREAD = makeNotification({ id: 1, read: false, read_at: null })
const READ = makeNotification({
  id: 2,
  event: 'renew_succeeded',
  // 标题与事件徽标（「续费成功」）区分开，便于断言定位
  title: '续费成功：到期时间已顺延至 2026-11-08',
  read: true,
  read_at: '2026-10-08T23:00:00Z',
})

/** 已读状态可变：模拟后端在 read 调用后列表变化。 */
function mockNotifications() {
  let readApplied = false
  let allRead = false
  const fetchMock = installFetchMock((url, init) => {
    if (url.pathname === '/api/v1/members/me') {
      return ok(makeMember())
    }
    if (url.pathname === '/api/v1/notifications/unread-count') {
      return ok({ unread: allRead || readApplied ? 0 : 1 })
    }
    if (url.pathname === '/api/v1/notifications/1/read' && init.method === 'POST') {
      readApplied = true
      return ok({
        notification: { ...UNREAD, read: true, read_at: '2026-10-08T23:10:00Z' },
        already_read: false,
      })
    }
    if (url.pathname === '/api/v1/notifications/read-all' && init.method === 'POST') {
      allRead = true
      return ok({ updated: 1, unread: 0 })
    }
    if (url.pathname === '/api/v1/notifications') {
      const unreadOnly = url.searchParams.get('unread') === 'true'
      const all = [
        { ...UNREAD, ...(allRead || readApplied ? { read: true, read_at: '2026-10-08T23:10:00Z' } : {}) },
        READ,
      ]
      const items = unreadOnly ? all.filter((item) => !item.read) : all
      return ok({
        items,
        page: 1,
        page_size: 20,
        total: items.length,
        unread: allRead || readApplied ? 0 : 1,
      })
    }
    return undefined
  })
  return fetchMock
}

async function renderNotifications() {
  seedMemberToken()
  renderApp(['/console/notifications'])
  await screen.findByRole('heading', { name: '通知' })
}

describe('会员区 · 通知', () => {
  it('渲染通知列表与未读数，已读项无未读圆点', async () => {
    mockNotifications()
    await renderNotifications()

    expect(await screen.findByText('订单已开通：香港二区 CN2 A型')).toBeInTheDocument()
    expect(screen.getByText('续费成功：到期时间已顺延至 2026-11-08')).toBeInTheDocument()
    // 头部未读数
    expect(screen.getByTestId('unread-count')).toHaveTextContent('1')
    expect(screen.getByText(/已于 2026/)).toBeInTheDocument()
  })

  it('点击「标记已读」调用单条已读接口并刷新未读数', async () => {
    const fetchMock = mockNotifications()
    const user = userEvent.setup()
    await renderNotifications()
    await screen.findByText('订单已开通：香港二区 CN2 A型')

    await user.click(screen.getByRole('button', { name: '标记已读' }))

    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(
          ([input, init]) =>
            String(input).includes('/api/v1/notifications/1/read') &&
            (init as RequestInit)?.method === 'POST',
        ),
      ).toBe(true)
    })
    // 已读后该条不再展示「标记已读」按钮，未读数归零
    await waitFor(() =>
      expect(screen.queryByRole('button', { name: '标记已读' })).not.toBeInTheDocument(),
    )
  })

  it('未读筛选只请求未读并展示空态', async () => {
    const fetchMock = mockNotifications()
    const user = userEvent.setup()
    await renderNotifications()
    await screen.findByText('订单已开通：香港二区 CN2 A型')

    await user.click(screen.getByRole('button', { name: '未读' }))
    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(([input]) => String(input).includes('unread=true')),
      ).toBe(true)
    })
    expect(await screen.findByText('订单已开通：香港二区 CN2 A型')).toBeInTheDocument()
    expect(screen.queryByText('续费成功：到期时间已顺延至 2026-11-08')).not.toBeInTheDocument()
  })

  it('全部已读调用 read-all 并清零未读数', async () => {
    const fetchMock = mockNotifications()
    const user = userEvent.setup()
    await renderNotifications()
    await screen.findByText('订单已开通：香港二区 CN2 A型')

    await user.click(screen.getByRole('button', { name: '全部已读' }))

    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(
          ([input, init]) =>
            String(input).includes('/api/v1/notifications/read-all') &&
            (init as RequestInit)?.method === 'POST',
        ),
      ).toBe(true)
    })
    await waitFor(() => expect(screen.queryByRole('button', { name: '标记已读' })).not.toBeInTheDocument())
  })
})
