import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError, errorMessage, http, onUnauthorized, request } from './client'
import { clearMemberToken, getMemberToken, setAdminToken, setMemberToken } from './tokens'

function envelopeResponse(payload: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(payload),
  } as unknown as Response
}

function lastFetchCall(): [string, RequestInit] {
  const mock = fetch as unknown as ReturnType<typeof vi.fn>
  return mock.mock.calls[0] as [string, RequestInit]
}

describe('统一响应包解析', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('code=0 时返回 data', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(envelopeResponse({ code: 0, message: 'ok', data: { db: 'up' } })),
    )

    await expect(request('/health')).resolves.toEqual({ db: 'up' })

    const [url, init] = lastFetchCall()
    expect(url).toBe('/api/v1/health')
    expect(new Headers(init.headers).get('Accept')).toBe('application/json')
  })

  it('code!=0 时抛出携带错误码与 HTTP 状态的 ApiError', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        envelopeResponse({ code: 404, message: '资源不存在', data: null }, 404),
      ),
    )

    const error = await request('/unknown').catch((err: unknown) => err)

    expect(error).toBeInstanceOf(ApiError)
    expect(error).toMatchObject({ code: 404, message: '资源不存在', status: 404 })
    expect((error as ApiError).isNotFound).toBe(true)
  })

  it('校验类错误码（40002）可被调用方识别', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        envelopeResponse({ code: 40002, message: '余额不足，请先充值或改用在线支付' }, 400),
      ),
    )

    const error = (await request('/orders/1/pay', { method: 'POST' }).catch((e: unknown) => e)) as ApiError
    expect(error.isValidation).toBe(true)
    expect(error.message).toBe('余额不足，请先充值或改用在线支付')
  })

  it('响应不是合法 JSON 时给出可读错误', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        text: async () => '<html>502 Bad Gateway</html>',
      } as unknown as Response),
    )

    const error = (await request('/health').catch((e: unknown) => e)) as ApiError
    expect(error.message).toContain('不是合法 JSON')
  })

  it('网络异常统一转成 ApiError，不抛原始异常', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')))

    const error = (await request('/health').catch((e: unknown) => e)) as ApiError
    expect(error).toBeInstanceOf(ApiError)
    expect(error.message).toContain('无法连接后端服务')
  })
})

describe('请求参数与鉴权头', () => {
  beforeEach(() => {
    clearMemberToken()
    localStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('query 拼接并跳过 undefined/null', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(envelopeResponse({ code: 0, message: 'ok', data: null })))

    await http.get('/orders', { query: { page: 1, page_size: 20, status: undefined, keyword: null } })

    const [url] = lastFetchCall()
    expect(url).toBe('/api/v1/orders?page=1&page_size=20')
  })

  it('body 自动 JSON 序列化并补 Content-Type', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(envelopeResponse({ code: 0, message: 'ok', data: { id: 1 } })))

    await http.post('/orders', { product_id: 1, cycle: 'annual' })

    const [, init] = lastFetchCall()
    expect(init.method).toBe('POST')
    expect(init.body).toBe(JSON.stringify({ product_id: 1, cycle: 'annual' }))
    expect(new Headers(init.headers).get('Content-Type')).toBe('application/json')
  })

  it('auth=member 注入会员 token，且不被管理端 token 影响', async () => {
    setMemberToken('member-token-abc')
    setAdminToken('admin-token-xyz')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(envelopeResponse({ code: 0, message: 'ok', data: {} })))

    await http.get('/members/me', { auth: 'member' })

    const [, init] = lastFetchCall()
    expect(new Headers(init.headers).get('Authorization')).toBe('Bearer member-token-abc')
  })

  it('auth=admin 使用管理端 token（两类 token 分离存储）', async () => {
    setMemberToken('member-token-abc')
    setAdminToken('admin-token-xyz')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(envelopeResponse({ code: 0, message: 'ok', data: {} })))

    await http.get('/admin/profile', { auth: 'admin' })

    const [, init] = lastFetchCall()
    expect(new Headers(init.headers).get('Authorization')).toBe('Bearer admin-token-xyz')
  })

  it('缺少 token 时不发请求，直接抛 401', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    const error = (await http.get('/members/me', { auth: 'member' }).catch((e: unknown) => e)) as ApiError

    expect(error.code).toBe(401)
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('业务 401 会清理会员 token 并通知订阅者', async () => {
    setMemberToken('expired-token')
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(envelopeResponse({ code: 401, message: '未认证或凭证无效' }, 401)),
    )

    const listener = vi.fn()
    const unsubscribe = onUnauthorized(listener)

    await http.get('/members/me', { auth: 'member' }).catch(() => undefined)

    expect(getMemberToken()).toBeNull()
    expect(listener).toHaveBeenCalledWith('member')
    unsubscribe()
  })
})

describe('errorMessage', () => {
  it('优先使用 ApiError 的 message', () => {
    expect(errorMessage(new ApiError(40002, '优惠码已过期'))).toBe('优惠码已过期')
    expect(errorMessage(new Error('boom'))).toBe('boom')
    expect(errorMessage(null, '兜底提示')).toBe('兜底提示')
  })
})
