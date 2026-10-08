import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { makeAdminAccount } from '@/test/fixtures'
import {
  fail,
  installFetchMock,
  ok,
  renderApp,
  requestBody,
  seedAdminProfile,
  seedAdminToken,
} from '@/test/harness'

// 管理后台登录页与守卫：未登录跳转、表单校验、登录参数与失败提示、已登录自动回跳。

/** 管理员登录成功后进入的仪表盘会立刻拉取一批指标；这里给最小可用的空数据。 */
function mockDashboard() {
  return installFetchMock((url) => {
    if (url.pathname === '/api/v1/admin/profile') {
      return ok(makeAdminAccount())
    }
    if (url.pathname === '/api/v1/admin/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    if (url.pathname === '/api/v1/admin/upstream/health') {
      return ok({
        connected: true,
        base_url: 'https://idc.example.com/',
        latency_ms: 12,
        api_key_masked: 'abcd****wxyz',
        checked_at: '2026-10-08T06:20:00Z',
      })
    }
    return ok({ items: [], page: 1, page_size: 1, total: 0 })
  })
}

describe('管理后台 · 登录与守卫', () => {
  it('未登录访问 /admin/orders 跳管理端登录页并带 redirect', async () => {
    installFetchMock(() => undefined)
    const { router } = renderApp(['/admin/orders'])

    await waitFor(() => expect(router.state.location.pathname).toBe('/admin/login'))
    expect(router.state.location.search).toBe('?redirect=%2Fadmin%2Forders')
    expect(screen.getByText('登录管理后台')).toBeInTheDocument()
  })

  it('空表单提交展示校验错误且不发起登录请求', async () => {
    const fetchMock = installFetchMock(() => undefined)
    const user = userEvent.setup()
    renderApp(['/admin/login'])

    await screen.findByText('登录管理后台')
    await user.click(screen.getByRole('button', { name: '登录' }))

    expect(await screen.findByText('请输入用户名')).toBeInTheDocument()
    expect(screen.getByText('请输入密码')).toBeInTheDocument()
    expect(
      fetchMock.mock.calls.some(([input]) => String(input).includes('/admin/auth/login')),
    ).toBe(false)
  })

  it('登录成功写入 admin token（与会员 token 分离）并跳转 /admin', async () => {
    const fetchMock = installFetchMock((url, init) => {
      if (url.pathname === '/api/v1/admin/auth/login') {
        expect(requestBody<{ username: string; password: string }>(init)).toEqual({
          username: 'admin',
          password: 'admin123456',
        })
        return ok({
          token: 'admin-token-1',
          expires_at: '2026-10-15T06:22:10Z',
          admin: makeAdminAccount(),
        })
      }
      if (url.pathname === '/api/v1/admin/profile') {
        expect(new Headers(init.headers).get('Authorization')).toBe('Bearer admin-token-1')
        return ok(makeAdminAccount())
      }
      if (url.pathname === '/api/v1/admin/notifications/unread-count') {
        return ok({ unread: 0 })
      }
      if (url.pathname === '/api/v1/admin/upstream/health') {
        return ok({
          connected: true,
          base_url: 'https://idc.example.com/',
          latency_ms: 12,
          api_key_masked: 'abcd****wxyz',
          checked_at: '2026-10-08T06:20:00Z',
        })
      }
      return ok({ items: [], page: 1, page_size: 1, total: 0 })
    })

    const user = userEvent.setup()
    const { router } = renderApp(['/admin/login'])
    await screen.findByText('登录管理后台')

    await user.type(screen.getByLabelText('管理员用户名'), 'admin')
    await user.type(screen.getByLabelText('密码'), 'admin123456')
    await user.click(screen.getByRole('button', { name: '登录' }))

    await waitFor(() => expect(router.state.location.pathname).toBe('/admin'))
    expect(localStorage.getItem('lyidc.admin.token')).toBe('admin-token-1')
    // 管理端登录不写会员 token。
    expect(localStorage.getItem('lyidc.member.token')).toBeNull()
    expect(
      fetchMock.mock.calls.some(([input]) => String(input).includes('/admin/auth/login')),
    ).toBe(true)
  })

  it('登录失败展示后端 message（账号被禁用 403）且停留在登录页', async () => {
    installFetchMock((url) => {
      if (url.pathname === '/api/v1/admin/auth/login') {
        return fail(403, '管理员账号已被禁用', 403)
      }
      return undefined
    })

    const user = userEvent.setup()
    const { router } = renderApp(['/admin/login'])
    await screen.findByText('登录管理后台')

    await user.type(screen.getByLabelText('管理员用户名'), 'cs6a')
    await user.type(screen.getByLabelText('密码'), 'wrong-password')
    await user.click(screen.getByRole('button', { name: '登录' }))

    expect(await screen.findByText('管理员账号已被禁用')).toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/admin/login')
    expect(localStorage.getItem('lyidc.admin.token')).toBeNull()
  })

  it('已登录访问登录页自动回跳 /admin', async () => {
    seedAdminToken()
    seedAdminProfile()
    mockDashboard()

    const { router } = renderApp(['/admin/login'])
    await waitFor(() => expect(router.state.location.pathname).toBe('/admin'))
  })

  it('admin token 失效（401）：清管理端 token 并跳登录页带回跳地址', async () => {
    seedAdminToken('expired-admin-token')
    seedAdminProfile()
    installFetchMock((url) => {
      if (url.pathname === '/api/v1/admin/profile') {
        return fail(401, '登录状态已失效，请重新登录', 401)
      }
      return ok({ items: [], page: 1, page_size: 1, total: 0 })
    })

    const { router } = renderApp(['/admin/settings'])

    await waitFor(() => expect(router.state.location.pathname).toBe('/admin/login'))
    expect(router.state.location.search).toBe('?redirect=%2Fadmin%2Fsettings')
    expect(localStorage.getItem('lyidc.admin.token')).toBeNull()
    // 会员 token 不受管理端 401 影响
    expect(screen.getByText('登录管理后台')).toBeInTheDocument()
  })

  it('已登录访问带 redirect 的登录页回到目标页', async () => {
    seedAdminToken()
    seedAdminProfile()
    mockDashboard()

    const { router } = renderApp(['/admin/login?redirect=%2Fadmin%2Fmembers'])
    await waitFor(() => expect(router.state.location.pathname).toBe('/admin/members'))
    expect(await screen.findByRole('heading', { name: '会员' })).toBeInTheDocument()
  })
})
