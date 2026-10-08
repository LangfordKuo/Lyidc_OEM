import { render } from '@testing-library/react'
import { createMemoryRouter } from 'react-router-dom'
import { vi } from 'vitest'

import { setMemberToken } from '../api/tokens'
import { routes } from '../app/routes'
import App from '../App'

// 会员区页面测试的公共脚手架：假响应包、按「方法 + 路径」匹配的 fetch 桩、带登录态的渲染。
// 说明：不引入 MSW 等重型依赖，沿用手写 fetch 桩（与 7a 的页面测试一致）。
export function envelopeResponse(payload: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(payload),
  } as unknown as Response
}

export const ok = (data: unknown) => envelopeResponse({ code: 0, message: 'ok', data })

export const fail = (code: number, message: string, status = 400) =>
  envelopeResponse({ code, message, data: null }, status)

export type ApiHandler = (url: string, init: RequestInit) => Response | undefined

/** 会员资料：进入会员区时 AuthProvider 会带 token 校验一次。 */
export const memberProfile = {
  id: 1,
  username: 'demo7b',
  email: 'demo7b@example.com',
  nickname: '演示会员',
  phone: null,
  status: 'active',
  balance: '88.00',
  created_at: '2026-10-08T06:18:31Z',
  updated_at: '2026-10-08T06:18:31Z',
  last_login_at: null,
}

function matches(url: string, init: RequestInit, method: string, prefix: string): boolean {
  const used = (init.method ?? 'GET').toUpperCase()
  return used === method && url.startsWith(prefix)
}

export function apiGet(prefix: string, data: unknown): ApiHandler {
  return (url, init) => (matches(url, init, 'GET', prefix) ? ok(data) : undefined)
}

export function apiPost(prefix: string, data: unknown): ApiHandler {
  return (url, init) => (matches(url, init, 'POST', prefix) ? ok(data) : undefined)
}

/** 按顺序匹配的 fetch 桩；未命中返回 404 响应包，返回的数组记录全部调用。 */
export function stubApi(handlers: ApiHandler[]): [string, RequestInit][] {
  const calls: [string, RequestInit][] = []
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation((url: string, init: RequestInit = {}) => {
      calls.push([url, init])
      for (const handler of handlers) {
        const response = handler(url, init)
        if (response) {
          return Promise.resolve(response)
        }
      }
      return Promise.resolve(fail(404, '资源不存在', 404))
    }),
  )
  return calls
}

export function fetchCalls(): [string, RequestInit][] {
  const mock = fetch as unknown as ReturnType<typeof vi.fn>
  return mock.mock.calls as [string, RequestInit][]
}

/** 以已登录状态渲染指定会员区路径（附带的会员资料与未读计数桩可被业务桩覆盖）。 */
export function renderConsole(path: string, handlers: ApiHandler[] = []) {
  setMemberToken('member-token')
  const calls = stubApi([
    ...handlers,
    apiGet('/api/v1/members/me', memberProfile),
    apiGet('/api/v1/notifications/unread-count', { unread: 0 }),
    apiGet('/api/v1/finance/balance', { member_id: 1, balance: '88.00' }),
  ])
  const router = createMemoryRouter(routes, { initialEntries: [path] })
  return { calls, ...render(<App router={router} />) }
}

/** 取出某个方法 + 路径的请求体（用于断言提交姿态）。 */
export function requestBody(method: string, pathPrefix: string): unknown {
  const call = fetchCalls().find(
    ([url, init]) =>
      url.startsWith(pathPrefix) && (init.method ?? 'GET').toUpperCase() === method,
  )
  if (!call) {
    throw new Error(`未找到请求：${method} ${pathPrefix}`)
  }
  return call[1].body === undefined ? undefined : JSON.parse(String(call[1].body))
}
