import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  cancelInstance,
  fetchInstance,
  listInstanceLogs,
  listInstances,
  powerInstance,
  reinstallInstance,
  renewInstance,
  resetInstancePassword,
} from './instances'
import { clearMemberToken, setMemberToken } from './tokens'

// 实例接口调用姿态严格对齐契约 15.2 / 15.8.2：
//   POST /instances/:id/power {op}
//   POST /instances/:id/reinstall {os_id, port?}
//   POST /instances/:id/reset-password {password?}（省略即服务端自动生成）
//   POST /instances/:id/renew {cycle}
//   POST /instances/:id/cancel {type, reason?}
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

describe('实例接口调用姿态', () => {
  beforeEach(() => {
    localStorage.clear()
    setMemberToken('member-token')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    clearMemberToken()
  })

  it('列表：分页与状态筛选拼到 query，并带会员鉴权头', async () => {
    stubOk({ items: [], page: 2, page_size: 20, total: 0 })

    await listInstances({ page: 2, page_size: 20, status: 'suspended' })

    const [url, init] = calls()[0]
    expect(url).toBe('/api/v1/instances?page=2&page_size=20&status=suspended')
    expect(new Headers(init.headers).get('Authorization')).toBe('Bearer member-token')
  })

  it('详情与操作记录：GET 路径正确（日志带分页参数）', async () => {
    stubOk({})
    await fetchInstance(3)
    await listInstanceLogs(3, { page: 1, page_size: 20 })

    expect(calls()[0][0]).toBe('/api/v1/instances/3')
    expect(calls()[1][0]).toBe('/api/v1/instances/3/logs?page=1&page_size=20')
  })

  it('电源操作：POST body 为 {op}', async () => {
    stubOk({ action: 'power_off' })

    await powerInstance(3, 'soft_off')

    const [url, init] = calls()[0]
    expect(url).toBe('/api/v1/instances/3/power')
    expect(init.method).toBe('POST')
    expect(JSON.parse(String(init.body))).toEqual({ op: 'soft_off' })
  })

  it('重装：POST body 为 {os_id}（不指定端口时不带 port）', async () => {
    stubOk({ action: 'reinstall' })

    await reinstallInstance(3, { os_id: 278 })

    const [url, init] = calls()[0]
    expect(url).toBe('/api/v1/instances/3/reinstall')
    expect(JSON.parse(String(init.body))).toEqual({ os_id: 278 })
  })

  it('重置密码：自动生成传空对象；自定义传 password', async () => {
    stubOk({ action: 'reset_password', password: 'Abc12345Xyz9' })
    await resetInstancePassword(3)
    await resetInstancePassword(3, 'Abc12345')

    expect(calls()[0][0]).toBe('/api/v1/instances/3/reset-password')
    expect(JSON.parse(String(calls()[0][1].body))).toEqual({})
    expect(JSON.parse(String(calls()[1][1].body))).toEqual({ password: 'Abc12345' })
  })

  it('续费：POST body 为 {cycle}（契约 15.2 不接受优惠码）', async () => {
    stubOk({ id: 1, type: 'renew' })

    await renewInstance(3, 'annual')

    const [url, init] = calls()[0]
    expect(url).toBe('/api/v1/instances/3/renew')
    expect(JSON.parse(String(init.body))).toEqual({ cycle: 'annual' })
  })

  it('取消申请：type 必填、reason 可选（空原因不提交该字段）', async () => {
    stubOk({ cancel_status: 'pending' })
    await cancelInstance(3, { type: 'immediate', reason: '业务迁移' })
    await cancelInstance(3, { type: 'end_of_billing' })

    expect(JSON.parse(String(calls()[0][1].body))).toEqual({
      type: 'immediate',
      reason: '业务迁移',
    })
    expect(JSON.parse(String(calls()[1][1].body))).toEqual({ type: 'end_of_billing' })
  })
})
