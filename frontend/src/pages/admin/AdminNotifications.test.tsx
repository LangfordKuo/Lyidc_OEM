import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AdminRole } from '../../api/types'
import { renderAdmin } from '../../test/adminHarness'
import { apiGet, apiPost, fetchCalls } from '../../test/consoleHarness'

// 管理后台「通知」页：管理员本人收件箱（与会员端同构）——未读样式、单条已读、全部已读、
// 未读筛选，以及列表回带的未读数驱动后台侧栏徽章。

const unreadNotification = {
  id: 12,
  event: 'ticket_created',
  title: '新工单：主机无法连接',
  content: '会员 alice 提交了工单 T20261008225813K7Q2ZP（主机无法连接）。',
  read: false,
  read_at: null,
  created_at: '2026-10-08T22:58:13Z',
}

const readNotification = {
  id: 11,
  event: 'ticket_replied',
  title: '工单已回复：主机无法连接',
  content: '客服已回复工单 T20261008220000ABCD。',
  read: true,
  read_at: '2026-10-08T22:00:00Z',
  created_at: '2026-10-08T21:58:13Z',
}

const notificationPage = {
  items: [unreadNotification, readNotification],
  page: 1,
  page_size: 20,
  total: 2,
  unread: 1,
}

function renderNotifications(role: AdminRole = 'admin') {
  return renderAdmin('/admin/notifications', role, [
    apiGet('/api/v1/admin/notifications?', notificationPage),
    apiGet('/api/v1/admin/notifications/unread-count', { unread: 1 }),
    apiPost('/api/v1/admin/notifications/read-all', { updated: 1, unread: 0 }),
    apiPost('/api/v1/admin/notifications/12/read', {
      notification: { ...unreadNotification, read: true, read_at: '2026-10-09T00:00:00Z' },
      already_read: false,
    }),
  ])
}

describe('管理后台 · 通知', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('列表渲染未读样式，未读数同步到侧栏徽章', async () => {
    renderNotifications()

    expect(await screen.findByText('新工单：主机无法连接')).toBeInTheDocument()
    expect(screen.getByText('工单已回复：主机无法连接')).toBeInTheDocument()
    // 未读条目：圆点 + 「标记已读」按钮只出现一次（已读条目不显示）
    expect(screen.getByLabelText('未读')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: '标记已读' })).toHaveLength(1)
    // 未读计数（列表回带）驱动侧栏通知入口的徽章，且「全部已读」因有未读而可用
    expect(screen.getByRole('button', { name: '全部已读' })).toBeEnabled()
    await waitFor(() => {
      const navLinks = screen.getAllByRole('link', { name: /通知/ })
      expect(navLinks.some((link) => link.textContent?.includes('1'))).toBe(true)
    })
  })

  it('单条标记已读调用管理端已读接口', async () => {
    const user = userEvent.setup()
    renderNotifications()

    const buttons = await screen.findAllByRole('button', { name: '标记已读' })
    expect(buttons).toHaveLength(1)
    await user.click(buttons[0])

    await waitFor(() => {
      expect(
        fetchCalls().some(
          ([url, init]) =>
            url.startsWith('/api/v1/admin/notifications/12/read') &&
            (init.method ?? 'GET').toUpperCase() === 'POST',
        ),
      ).toBe(true)
    })
  })

  it('全部已读调用 read-all 接口', async () => {
    const user = userEvent.setup()
    renderNotifications()

    await screen.findByText('新工单：主机无法连接')
    await user.click(screen.getByRole('button', { name: '全部已读' }))

    await waitFor(() => {
      expect(
        fetchCalls().some(
          ([url, init]) =>
            url.startsWith('/api/v1/admin/notifications/read-all') &&
            (init.method ?? 'GET').toUpperCase() === 'POST',
        ),
      ).toBe(true)
    })
  })

  it('未读筛选把 unread=true 拼进请求', async () => {
    const user = userEvent.setup()
    renderNotifications()

    await screen.findByText('新工单：主机无法连接')
    await user.click(screen.getByRole('button', { name: '未读' }))

    await waitFor(() => {
      expect(
        fetchCalls().some(
          ([url]) => url.includes('/api/v1/admin/notifications?') && url.includes('unread=true'),
        ),
      ).toBe(true)
    })
  })

  it('finance 角色同样可访问（无权限闸门），空收件箱给出说明', async () => {
    renderAdmin('/admin/notifications', 'finance', [
      apiGet('/api/v1/admin/notifications?', {
        items: [],
        page: 1,
        page_size: 20,
        total: 0,
        unread: 0,
      }),
    ])

    expect(await screen.findByText('暂无通知')).toBeInTheDocument()
    expect(screen.getByText(/财务角色的收件箱通常为空/)).toBeInTheDocument()
  })
})
