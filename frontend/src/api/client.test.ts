import { describe, expect, it, vi } from 'vitest'

import { ApiError, errorMessage, http, onUnauthorized } from '@/api/client'
import { fail, installFetchMock, ok, requestBody } from '@/test/harness'

describe('api client', () => {
  it('code=0 时解包 data 并返回', async () => {
    const mock = installFetchMock((url) => {
      expect(url.pathname).toBe('/api/v1/products')
      return ok({ total: 3 })
    })

    await expect(http.get('/products')).resolves.toEqual({ total: 3 })
    expect(mock).toHaveBeenCalledTimes(1)
    expect(mock.mock.calls[0][1]?.method).toBe('GET')
  })

  it('查询参数拼进 URL，null/undefined 跳过', async () => {
    installFetchMock((url) => {
      expect(url.searchParams.get('page')).toBe('2')
      expect(url.searchParams.get('status')).toBe('annual')
      expect(url.searchParams.has('empty')).toBe(false)
      return ok([])
    })

    await http.get('/orders', { query: { page: 2, status: 'annual', empty: null, missing: undefined } })
  })

  it('POST 请求自动 JSON 序列化并带 Content-Type', async () => {
    installFetchMock((_url, init) => {
      expect(init.method).toBe('POST')
      expect(new Headers(init.headers).get('Content-Type')).toBe('application/json')
      expect(requestBody(init)).toEqual({ product_id: 1, cycle: 'annual' })
      return ok({ id: 1 })
    })

    await http.post('/orders', { product_id: 1, cycle: 'annual' })
  })

  it('code!=0 抛 ApiError，携带 code / message / data / HTTP 状态', async () => {
    installFetchMock(() => fail(40002, '优惠码已过期', 400))

    const error = await http.post('/orders', {}).catch((err: unknown) => err)

    expect(error).toBeInstanceOf(ApiError)
    const apiError = error as ApiError
    expect(apiError.code).toBe(40002)
    expect(apiError.message).toBe('优惠码已过期')
    expect(apiError.status).toBe(400)
    expect(apiError.isValidation).toBe(true)
    expect(apiError.isUnauthorized).toBe(false)
  })

  it('auth 请求注入 Bearer 头', async () => {
    localStorage.setItem('lyidc.member.token', 'tok-123')
    installFetchMock((_url, init) => {
      expect(new Headers(init.headers).get('Authorization')).toBe('Bearer tok-123')
      return ok({ id: 1 })
    })

    await http.get('/members/me', { auth: true })
  })

  it('未登录时 auth 请求直接抛 401，不打网络', async () => {
    const mock = installFetchMock(() => ok(null))

    const error = await http.get('/members/me', { auth: true }).catch((err: unknown) => err)

    expect((error as ApiError).code).toBe(401)
    expect(mock).not.toHaveBeenCalled()
  })

  it('auth 请求 401 时清 token 并触发未认证通知', async () => {
    localStorage.setItem('lyidc.member.token', 'tok-expired')
    localStorage.setItem('lyidc.member.profile', '{}')
    const listener = vi.fn()
    const off = onUnauthorized(listener)
    installFetchMock(() => fail(401, '请先登录', 401))

    await http.get('/finance/balance', { auth: true }).catch(() => undefined)

    expect(localStorage.getItem('lyidc.member.token')).toBeNull()
    expect(listener).toHaveBeenCalledTimes(1)
    off()
  })

  it('网络不可达时转成可展示的中文错误', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new TypeError('Failed to fetch'))),
    )

    const error = await http.get('/health').catch((err: unknown) => err)

    expect(error).toBeInstanceOf(ApiError)
    expect((error as ApiError).code).toBe(-1)
    expect((error as ApiError).message).toContain('无法连接后端服务')
  })

  it('errorMessage 提取 ApiError 的 message，未知异常用兜底文案', () => {
    expect(errorMessage(new ApiError(400, '参数错误'), '兜底')).toBe('参数错误')
    expect(errorMessage(new Error(''), '兜底')).toBe('兜底')
    expect(errorMessage(null, '兜底')).toBe('兜底')
  })
})
