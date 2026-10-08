import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { apiGet, apiPost, renderConsole, requestBody } from '../../test/consoleHarness'

// 通知页：未读样式、单条已读（点击即读）、全部已读与侧栏徽章联动。
const unreadNotification = {
  id: 12,
  event: 'ticket_replied',
  title: '工单已回复：主机无法连接',
  content: '客服已回复您的工单 T20261008225813K7Q2ZP。',
  read: false,
  read_at: null,
  created_at: '2026-10-08T22:58:13Z',
}

const readNotification = {
  id: 11,
  event: 'order_delivered',
  title: '订单已开通：香港二区 CN2 A型',
  content: '您的实例 oem-o2026 已开通完成。',
  read: true,
  read_at: '2026-10-08T22:00:00Z',
  created_at: '2026-10-08T21:58:13Z',
}

function renderNotifications() {
  return renderConsole('/console/notifications', [
    apiGet('/api/v1/notifications?', {
      items: [unreadNotification, readNotification],
      page: 1,
      page_size: 20,
      total: 2,
      unread: 1,
    }),
    apiPost('/api/v1/notifications/read-all', { updated: 1, unread: 0 }),
    apiPost('/api/v1/notifications/12/read', {
      notification: { ...unreadNotification, read: true, read_at: '2026-10-09T00:00:00Z' },
      already_read: false,
    }),
  ])
}

describe('会员区 · 通知', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('列表回带未读数并显示在侧栏通知入口上', async () => {
    renderNotifications()

    expect(await screen.findByText('工单已回复：主机无法连接')).toBeInTheDocument()
    expect(screen.getByText('订单已开通：香港二区 CN2 A型')).toBeInTheDocument()

    await waitFor(() => {
      const navLinks = screen.getAllByRole('link', { name: /通知/ })
      expect(navLinks.some((link) => link.textContent?.includes('1'))).toBe(true)
    })
  })

  it('单条标记已读会调用已读接口', async () => {
    const user = userEvent.setup()
    renderNotifications()

    const buttons = await screen.findAllByRole('button', { name: '标记已读' })
    expect(buttons).toHaveLength(1)
    await user.click(buttons[0])

    await waitFor(() => {
      expect(requestBody('POST', '/api/v1/notifications/12/read')).toBeUndefined()
    })
  })

  it('全部已读调用 read-all 接口', async () => {
    const user = userEvent.setup()
    renderNotifications()

    await screen.findByText('工单已回复：主机无法连接')
    await user.click(screen.getByRole('button', { name: '全部已读' }))

    await waitFor(() => {
      expect(requestBody('POST', '/api/v1/notifications/read-all')).toBeUndefined()
    })
  })

  it('未读筛选会把 unread=true 拼进请求', async () => {
    const user = userEvent.setup()
    renderNotifications()

    await screen.findByText('工单已回复：主机无法连接')
    const calls = fetchCallsLength()
    await user.click(screen.getByRole('button', { name: '未读' }))

    await waitFor(() => {
      expect(fetchCallsLength()).toBeGreaterThan(calls)
      const urls = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls as [string][]
      expect(urls.some(([url]) => url.includes('unread=true'))).toBe(true)
    })
  })
})

function fetchCallsLength(): number {
  return (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls.length
}
