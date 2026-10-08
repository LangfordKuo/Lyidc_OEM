import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { makeMember } from '@/test/fixtures'
import { fail, installFetchMock, ok, renderApp, requestBody } from '@/test/harness'

describe('登录页', () => {
  it('空表单提交展示必填校验，不打网络', async () => {
    const fetchMock = installFetchMock(() => undefined)
    const user = userEvent.setup()
    renderApp(['/login'])

    await user.click(within(screen.getByRole('form', { name: '登录表单' })).getByRole('button', { name: '登录' }))

    expect(await screen.findByText('请输入用户名')).toBeInTheDocument()
    expect(screen.getByText('请输入密码')).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('凭证错误时展示后端 message', async () => {
    installFetchMock((url) => {
      if (url.pathname === '/api/v1/auth/login') {
        return fail(401, '用户名或密码错误', 401)
      }
      return undefined
    })
    const user = userEvent.setup()
    renderApp(['/login'])

    await user.type(screen.getByLabelText('用户名'), 'demo7a')
    await user.type(screen.getByLabelText('密码'), 'wrong-password')
    await user.click(within(screen.getByRole('form', { name: '登录表单' })).getByRole('button', { name: '登录' }))

    expect(await screen.findByText('用户名或密码错误')).toBeInTheDocument()
  })

  it('登录成功写入 token 并回跳 redirect 参数', async () => {
    const fetchMock = installFetchMock((url) => {
      if (url.pathname === '/api/v1/auth/login') {
        return ok({
          token: 'member-token-1',
          expires_at: '2026-10-15T06:20:02Z',
          member: { id: 1, username: 'demo7a', status: 'active' },
        })
      }
      if (url.pathname === '/api/v1/members/me') {
        return ok(makeMember())
      }
      return undefined
    })
    const user = userEvent.setup()
    const { router } = renderApp(['/login?redirect=%2Fcheckout%2F1'])

    await user.type(screen.getByLabelText('用户名'), ' demo7a ')
    await user.type(screen.getByLabelText('密码'), 'demo7a123456')
    await user.click(within(screen.getByRole('form', { name: '登录表单' })).getByRole('button', { name: '登录' }))

    await waitFor(() => expect(router.state.location.pathname).toBe('/checkout/1'))
    expect(localStorage.getItem('lyidc.member.token')).toBe('member-token-1')

    // 用户名首尾空格被裁剪后提交
    const loginCall = fetchMock.mock.calls.find(([input]) =>
      String(input).includes('/auth/login'),
    )
    expect(requestBody(loginCall?.[1] as RequestInit)).toEqual({
      username: 'demo7a',
      password: 'demo7a123456',
    })
  })

  it('已登录访问登录页直接回跳', async () => {
    localStorage.setItem('lyidc.member.token', 'tok-existing')
    installFetchMock((url) => (url.pathname === '/api/v1/members/me' ? ok(makeMember()) : undefined))

    const { router } = renderApp(['/login?redirect=%2Fproducts'])

    await waitFor(() => expect(router.state.location.pathname).toBe('/products'))
  })

  it('redirect 为外站地址时回落到首页（防开放重定向）', async () => {
    localStorage.setItem('lyidc.member.token', 'tok-existing')
    installFetchMock((url) => (url.pathname === '/api/v1/members/me' ? ok(makeMember()) : undefined))

    const { router } = renderApp(['/login?redirect=https%3A%2F%2Fevil.com%2Fsteal'])

    await waitFor(() => expect(router.state.location.pathname).toBe('/'))
  })
})
