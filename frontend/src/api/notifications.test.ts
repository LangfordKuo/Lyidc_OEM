import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  fetchUnreadCount,
  listNotifications,
  readAllNotifications,
  readNotification,
} from './notifications'
import { clearMemberToken, setMemberToken } from './tokens'

// 站内通知接口调用姿态对齐契约 17.2：
//   GET  /notifications?page&page_size&unread
//   GET  /notifications/unread-count
//   POST /notifications/:id/read、POST /notifications/read-all（均无请求体）
function envelopeResponse(payload: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(payload),
  } as unknown as Response
}

function stubOk(data: unknown = {}) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue(envelopeResponse({ code: 0, message: 'ok', data })),
  )
}

function calls(): [string, RequestInit][] {
  const mock = fetch as unknown as ReturnType<typeof vi.fn>
  return mock.mock.calls as [string, RequestInit][]
}

describe('通知接口调用姿态', () => {
  beforeEach(() => {
    localStorage.clear()
    setMemberToken('member-token')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    clearMemberToken()
  })

  it('列表：未读筛选只在 unread=true 时拼参数', async () => {
    stubOk({ items: [], page: 1, page_size: 20, total: 0, unread: 0 })

    await listNotifications({ page: 1, page_size: 20, unread: true })
    await listNotifications({ page: 1, page_size: 20 })

    expect(calls()[0][0]).toBe('/api/v1/notifications?page=1&page_size=20&unread=true')
    expect(calls()[1][0]).toBe('/api/v1/notifications?page=1&page_size=20')
  })

  it('未读计数：GET /notifications/unread-count', async () => {
    stubOk({ unread: 3 })

    await fetchUnreadCount()

    expect(calls()[0][0]).toBe('/api/v1/notifications/unread-count')
  })

  it('单条已读：POST /notifications/:id/read（无请求体）', async () => {
    stubOk({ notification: {}, already_read: true })

    await readNotification(12)

    const [url, init] = calls()[0]
    expect(url).toBe('/api/v1/notifications/12/read')
    expect(init.method).toBe('POST')
    expect(init.body).toBeUndefined()
  })

  it('全部已读：POST /notifications/read-all', async () => {
    stubOk({ updated: 2, unread: 0 })

    await readAllNotifications()

    expect(calls()[0][0]).toBe('/api/v1/notifications/read-all')
  })
})
