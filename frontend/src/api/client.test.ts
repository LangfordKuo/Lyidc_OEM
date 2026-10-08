import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiError, request } from './client'

function jsonResponse(payload: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => payload,
  } as unknown as Response
}

describe('统一响应包解析', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('code=0 时返回 data', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(jsonResponse({ code: 0, message: 'ok', data: { db: 'up' } })),
    )

    await expect(request('/health')).resolves.toEqual({ db: 'up' })
    expect(fetch).toHaveBeenCalledWith(
      '/api/v1/health',
      expect.objectContaining({ headers: { Accept: 'application/json' } }),
    )
  })

  it('code!=0 时抛出携带错误码的 ApiError', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(jsonResponse({ code: 404, message: '资源不存在', data: null }, 404)),
    )

    const error = await request('/unknown').catch((err: unknown) => err)

    expect(error).toBeInstanceOf(ApiError)
    expect(error).toMatchObject({ code: 404, message: '资源不存在' })
  })

  it('响应非 JSON 时抛出 ApiError', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => {
          throw new SyntaxError('Unexpected token <')
        },
      } as unknown as Response),
    )

    await expect(request('/health')).rejects.toBeInstanceOf(ApiError)
  })

  it('网络异常时抛出 ApiError 并提示无法连接', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')))

    const error = await request('/health').catch((err: unknown) => err)

    expect(error).toBeInstanceOf(ApiError)
    expect((error as ApiError).message).toContain('无法连接后端服务')
  })
})
