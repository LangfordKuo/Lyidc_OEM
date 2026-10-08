import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { closeTicket, createTicket, fetchTicket, listTickets, replyTicket } from './tickets'
import { clearMemberToken, setMemberToken } from './tokens'

// 工单接口调用姿态对齐契约 16.3：
//   POST /tickets {subject, content, category, instance_id?}
//   GET  /tickets?page&page_size&status
//   POST /tickets/:id/reply {content}、POST /tickets/:id/close（无请求体）
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

describe('工单接口调用姿态', () => {
  beforeEach(() => {
    localStorage.clear()
    setMemberToken('member-token')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    clearMemberToken()
  })

  it('提交工单：POST /tickets，未关联实例时不带 instance_id', async () => {
    stubOk({ ticket: { id: 1 }, message: { id: 2 } })

    await createTicket({ subject: '主机无法连接', content: '从早上开始 SSH 超时', category: 'technical' })

    const [url, init] = calls()[0]
    expect(url).toBe('/api/v1/tickets')
    expect(init.method).toBe('POST')
    expect(JSON.parse(String(init.body))).toEqual({
      subject: '主机无法连接',
      content: '从早上开始 SSH 超时',
      category: 'technical',
    })
  })

  it('提交工单：关联实例时带 instance_id', async () => {
    stubOk({})

    await createTicket({
      subject: '主机无法连接',
      content: '内容',
      category: 'technical',
      instance_id: 3,
    })

    expect(JSON.parse(String(calls()[0][1].body)).instance_id).toBe(3)
  })

  it('列表：状态筛选进 query', async () => {
    stubOk({ items: [], page: 1, page_size: 20, total: 0 })

    await listTickets({ page: 1, page_size: 20, status: 'open' })

    expect(calls()[0][0]).toBe('/api/v1/tickets?page=1&page_size=20&status=open')
  })

  it('详情：GET /tickets/:id', async () => {
    stubOk({ id: 7, subject: '主机无法连接', messages: [] })

    await fetchTicket(7)

    expect(calls()[0][0]).toBe('/api/v1/tickets/7')
  })

  it('回复：POST /tickets/:id/reply {content}', async () => {
    stubOk({ ticket: {}, message: {} })

    await replyTicket(7, '已按建议重启，问题仍在')

    const [url, init] = calls()[0]
    expect(url).toBe('/api/v1/tickets/7/reply')
    expect(JSON.parse(String(init.body))).toEqual({ content: '已按建议重启，问题仍在' })
  })

  it('关闭：POST 且不带请求体', async () => {
    stubOk({ ticket: { status: 'closed' }, already_closed: false })

    await closeTicket(7)

    const [url, init] = calls()[0]
    expect(url).toBe('/api/v1/tickets/7/close')
    expect(init.body).toBeUndefined()
  })
})
