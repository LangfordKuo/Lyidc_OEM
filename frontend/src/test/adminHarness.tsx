import { render } from '@testing-library/react'
import { createMemoryRouter } from 'react-router-dom'

import { setAdminToken } from '../api/tokens'
import type { AdminAccount, AdminRole } from '../api/types'
import { routes } from '../app/routes'
import App from '../App'
import { apiGet, fail, stubApi, type ApiHandler } from './consoleHarness'

// 管理后台页面测试的公共脚手架：复用会员区的手写 fetch 桩（不引入 MSW），
// 区别只在登录态——管理端用 lyidc.admin.token，且 AuthProvider（会员侧）不参与。

/** 管理员资料：默认 admin 角色；传 role 可构造 finance / support 视角。 */
export function adminAccount(role: AdminRole = 'admin', overrides: Partial<AdminAccount> = {}): AdminAccount {
  return {
    id: 1,
    username: role === 'admin' ? 'admin' : `${role}01`,
    nickname: role === 'admin' ? '超级管理员' : role,
    role,
    status: 'active',
    created_at: '2026-10-08T06:18:31Z',
    updated_at: '2026-10-08T06:18:31Z',
    last_login_at: null,
    ...overrides,
  }
}

/**
 * 以已登录的管理员身份渲染后台路径。
 * 默认附带 /admin/profile 与未读计数两个桩，业务桩可覆盖它们（按顺序匹配，先命中的生效）。
 */
export function renderAdmin(
  path: string,
  role: AdminRole = 'admin',
  handlers: ApiHandler[] = [],
  profile: AdminAccount = adminAccount(role),
) {
  setAdminToken('admin-token')
  const calls = stubApi([
    ...handlers,
    apiGet('/api/v1/admin/profile', profile),
    apiGet('/api/v1/admin/notifications/unread-count', { unread: 0 }),
  ])
  const router = createMemoryRouter(routes, { initialEntries: [path] })
  return { calls, ...render(<App router={router} />) }
}

/** 未登录状态渲染后台路径（用于验证守卫跳登录页 / 登录页本身）。 */
export function renderAdminAnonymous(path: string, handlers: ApiHandler[] = []) {
  const calls = stubApi([...handlers, apiGet('/api/v1/admin/profile', adminAccount())])
  const router = createMemoryRouter(routes, { initialEntries: [path] })
  return { calls, ...render(<App router={router} />) }
}

/**
 * apiFail 构造「返回失败响应包」的桩：consoleHarness 的 fail(...) 返回的是 Response 本身，
 * 不能直接当 ApiHandler 用（需要包一层按方法 + 路径匹配的函数）。
 */
export function apiFail(
  method: string,
  prefix: string,
  code: number,
  message: string,
  status = 400,
): ApiHandler {
  return (url, init) => {
    const used = (init.method ?? 'GET').toUpperCase()
    return used === method.toUpperCase() && url.startsWith(prefix)
      ? fail(code, message, status)
      : undefined
  }
}
