import { render } from '@testing-library/react'
import { RouterProvider, createMemoryRouter } from 'react-router-dom'
import { vi } from 'vitest'

import AppProviders from '@/app/providers'
import { routes } from '@/app/routes'

/** 用真实路由表 + Provider 渲染应用（等价于浏览器里跑，只是路由为 memory 实现）。 */
export function renderApp(initialEntries: string[] = ['/']) {
  const router = createMemoryRouter(routes, { initialEntries })
  const utils = render(
    <AppProviders>
      <RouterProvider router={router} />
    </AppProviders>,
  )
  return { ...utils, router }
}

export interface MockResponse {
  status?: number
  body: unknown
}

export type FetchHandler = (url: URL, init: RequestInit) => MockResponse | Promise<MockResponse> | undefined

/**
 * 安装 fetch 替身：按 URL/method 分派返回统一响应包。
 * 未匹配的请求返回 404 响应包（便于发现漏 mock 的调用）。
 */
export function installFetchMock(handler: FetchHandler) {
  const mock = vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const raw = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    const url = new URL(raw, 'http://localhost')
    const result = await handler(url, init)
    if (!result) {
      return jsonResponse({ code: 404, message: '接口不存在', data: null }, 404)
    }
    return jsonResponse(result.body, result.status ?? 200)
  })
  vi.stubGlobal('fetch', mock)
  return mock
}

export function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
  })
}

/** 成功响应包（code=0）。 */
export function ok<T>(data: T): MockResponse {
  return { body: { code: 0, message: 'ok', data } }
}

/** 失败响应包（业务错误码 + HTTP 状态）。 */
export function fail(code: number, message: string, status = 400): MockResponse {
  return { status, body: { code, message, data: null } }
}

/** 读取请求体 JSON（无请求体时返回 null）。 */
export function requestBody<T = unknown>(init: RequestInit): T | null {
  if (typeof init.body !== 'string' || !init.body) {
    return null
  }
  return JSON.parse(init.body) as T
}

/** 写入会员 token（模拟已登录）。 */
export function seedMemberToken(token = 'test-member-token'): void {
  localStorage.setItem('lyidc.member.token', token)
}

/**
 * 写入管理端 token（模拟已登录后台）。
 * 与会员 token 分离存储（键名不同），两处登录互不覆盖。
 */
export function seedAdminToken(token = 'test-admin-token'): void {
  localStorage.setItem('lyidc.admin.token', token)
}

/**
 * 写入管理员资料缓存并返回该资料：后台首屏在 /admin/profile 返回前先用缓存占位。
 * 传入 role 即模拟以对应角色登录（admin / finance / support）。
 */
export function seedAdminProfile(
  overrides: Partial<import('@/api/types').AdminAccount> = {},
): import('@/api/types').AdminAccount {
  const profile = {
    id: 1,
    username: 'admin',
    nickname: '超级管理员',
    role: 'admin' as const,
    status: 'active' as const,
    created_at: '2026-10-08T06:00:00Z',
    updated_at: '2026-10-08T06:00:00Z',
    last_login_at: '2026-10-08T06:20:00Z',
    ...overrides,
  }
  localStorage.setItem('lyidc.admin.profile', JSON.stringify(profile))
  return profile
}
