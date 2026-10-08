import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { getAdminToken } from '../../api/tokens'
import { adminAccount, apiFail, renderAdminAnonymous } from '../../test/adminHarness'
import { apiGet, apiPost, fetchCalls } from '../../test/consoleHarness'

// 后台登录页：字段必填、错误提示直出后端 message、成功后写入 admin token 并进入后台。
describe('管理后台 · 登录', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('渲染管理员登录表单（含后台标识）', () => {
    renderAdminAnonymous('/admin/login')

    expect(screen.getByRole('heading', { name: '登录管理后台' })).toBeInTheDocument()
    expect(screen.getByLabelText('管理员用户名')).toBeInTheDocument()
    expect(screen.getByLabelText('密码')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '登录' })).toBeInTheDocument()
  })

  it('空字段提交给出必填提示，且不发起请求', async () => {
    const user = userEvent.setup()
    renderAdminAnonymous('/admin/login')

    await user.click(screen.getByRole('button', { name: '登录' }))

    expect(await screen.findByText('请输入用户名')).toBeInTheDocument()
    expect(screen.getByText('请输入密码')).toBeInTheDocument()
    expect(
      fetchCalls().some(([url]) => url.startsWith('/api/v1/admin/auth/login')),
    ).toBe(false)
  })

  it('凭据错误时展示后端 message', async () => {
    const user = userEvent.setup()
    renderAdminAnonymous('/admin/login', [
      apiFail('POST', '/api/v1/admin/auth/login', 401, '用户名或密码错误', 401),
    ])

    await user.type(screen.getByLabelText('管理员用户名'), 'admin')
    await user.type(screen.getByLabelText('密码'), 'wrong-password')
    await user.click(screen.getByRole('button', { name: '登录' }))

    expect(await screen.findByText('用户名或密码错误')).toBeInTheDocument()
    expect(getAdminToken()).toBeNull()
  })

  it('登录成功：写入 admin token 并进入后台仪表盘', async () => {
    const user = userEvent.setup()
    const account = adminAccount('admin')
    const paged = { items: [], page: 1, page_size: 1, total: 0 }
    renderAdminAnonymous('/admin/login', [
      apiPost('/api/v1/admin/auth/login', {
        token: 'admin-token-issued',
        expires_at: '2026-10-15T06:22:10Z',
        admin: account,
      }),
      // 登录后进入的仪表盘会用到的指标接口（骨架期只断言页面切换）。
      apiGet('/api/v1/admin/members', paged),
      apiGet('/api/v1/admin/instances', paged),
      apiGet('/api/v1/admin/tickets', paged),
      apiGet('/api/v1/admin/recharges', paged),
      apiGet('/api/v1/admin/ledger', paged),
      apiGet('/api/v1/admin/upstream/health', {
        connected: true,
        base_url: 'https://idc.example.com',
        latency_ms: 12,
        api_key_masked: 'abcd****',
        checked_at: '2026-10-08T06:00:00Z',
      }),
    ])

    await user.type(screen.getByLabelText('管理员用户名'), 'admin')
    await user.type(screen.getByLabelText('密码'), 'admin123456')
    await user.click(screen.getByRole('button', { name: '登录' }))

    await waitFor(() => {
      expect(getAdminToken()).toBe('admin-token-issued')
    })
    expect(await screen.findByRole('heading', { name: '仪表盘' })).toBeInTheDocument()
  })
})
