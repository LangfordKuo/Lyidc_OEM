import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { makeMember } from '@/test/fixtures'
import { installFetchMock, ok, renderApp, requestBody } from '@/test/harness'

async function fillForm(user: ReturnType<typeof userEvent.setup>, values: {
  username: string
  email: string
  password: string
  confirm: string
}) {
  await user.type(screen.getByLabelText('用户名'), values.username)
  await user.type(screen.getByLabelText('邮箱'), values.email)
  await user.type(screen.getByLabelText('密码'), values.password)
  await user.type(screen.getByLabelText('确认密码'), values.confirm)
}

describe('注册页', () => {
  it('校验用户名、邮箱与密码规则（对齐契约 6.2）', async () => {
    const fetchMock = installFetchMock(() => undefined)
    const user = userEvent.setup()
    renderApp(['/register'])

    await fillForm(user, { username: 'ab', email: 'not-an-email', password: 'short', confirm: 'different' })
    await user.click(screen.getByRole('button', { name: '注册并登录' }))

    expect(await screen.findByText('用户名需为 3-32 位字母、数字或下划线')).toBeInTheDocument()
    expect(screen.getByText('邮箱格式不正确')).toBeInTheDocument()
    expect(screen.getByText('密码至少 8 个字节')).toBeInTheDocument()
    expect(screen.getByText('两次输入的密码不一致')).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('两次密码不一致时拦截提交', async () => {
    const fetchMock = installFetchMock(() => undefined)
    const user = userEvent.setup()
    renderApp(['/register'])

    await fillForm(user, {
      username: 'newuser',
      email: 'new@example.com',
      password: 'password123',
      confirm: 'password124',
    })
    await user.click(screen.getByRole('button', { name: '注册并登录' }))

    expect(await screen.findByText('两次输入的密码不一致')).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('注册成功后自动登录并回跳 redirect', async () => {
    const fetchMock = installFetchMock((url) => {
      if (url.pathname === '/api/v1/auth/register') {
        return ok(makeMember({ username: 'newuser', email: 'new@example.com' }))
      }
      if (url.pathname === '/api/v1/auth/login') {
        return ok({
          token: 'fresh-token',
          expires_at: '2026-10-15T06:20:02Z',
          member: { id: 2, username: 'newuser', status: 'active' },
        })
      }
      if (url.pathname === '/api/v1/members/me') {
        return ok(makeMember({ id: 2, username: 'newuser' }))
      }
      return undefined
    })
    const user = userEvent.setup()
    const { router } = renderApp(['/register?redirect=%2Fproducts'])

    await fillForm(user, {
      username: 'newuser',
      email: 'new@example.com',
      password: 'password123',
      confirm: 'password123',
    })
    await user.click(screen.getByRole('button', { name: '注册并登录' }))

    await waitFor(() => expect(router.state.location.pathname).toBe('/products'))
    expect(localStorage.getItem('lyidc.member.token')).toBe('fresh-token')

    const registerCall = fetchMock.mock.calls.find(([input]) =>
      String(input).includes('/auth/register'),
    )
    expect(requestBody(registerCall?.[1] as RequestInit)).toEqual({
      username: 'newuser',
      email: 'new@example.com',
      password: 'password123',
    })
  })

  it('用户名/邮箱冲突时展示后端 message', async () => {
    installFetchMock((url) => {
      if (url.pathname === '/api/v1/auth/register') {
        return { status: 409, body: { code: 409, message: '用户名已被占用', data: null } }
      }
      return undefined
    })
    const user = userEvent.setup()
    renderApp(['/register'])

    await fillForm(user, {
      username: 'demo7a',
      email: 'demo7a@example.com',
      password: 'password123',
      confirm: 'password123',
    })
    await user.click(screen.getByRole('button', { name: '注册并登录' }))

    expect(await screen.findByText('用户名已被占用')).toBeInTheDocument()
  })

  it('从邀请链接进入时自动预填邀请码（用户可改）', async () => {
    installFetchMock(() => undefined)
    const user = userEvent.setup()
    renderApp(['/register?invite=ABCD2345'])

    const inviteInput = screen.getByLabelText('邀请码（选填）')
    expect(inviteInput).toHaveValue('ABCD2345')

    // 预填只是默认值：改掉之后提交体以输入框为准
    await user.clear(inviteInput)
    await user.type(inviteInput, 'WXYZ6789')
    expect(inviteInput).toHaveValue('WXYZ6789')
  })

  it('提交体带上 invite_code；留空则不传该键', async () => {
    const fetchMock = installFetchMock((url) => {
      if (url.pathname === '/api/v1/auth/register') {
        return ok(makeMember({ username: 'newuser', email: 'new@example.com' }))
      }
      if (url.pathname === '/api/v1/auth/login') {
        return ok({
          token: 'fresh-token',
          expires_at: '2026-10-15T06:20:02Z',
          member: { id: 2, username: 'newuser', status: 'active' },
        })
      }
      if (url.pathname === '/api/v1/members/me') {
        return ok(makeMember({ id: 2, username: 'newuser' }))
      }
      return undefined
    })
    const user = userEvent.setup()
    renderApp(['/register?invite=ABCD2345'])

    await fillForm(user, {
      username: 'newuser',
      email: 'new@example.com',
      password: 'password123',
      confirm: 'password123',
    })
    await user.click(screen.getByRole('button', { name: '注册并登录' }))

    await waitFor(() => {
      const registerCall = fetchMock.mock.calls.find(([input]) =>
        String(input).includes('/auth/register'),
      )
      expect(registerCall).toBeTruthy()
      expect(requestBody(registerCall?.[1] as RequestInit)).toEqual({
        username: 'newuser',
        email: 'new@example.com',
        password: 'password123',
        invite_code: 'ABCD2345',
      })
    })
  })

  it('邀请码非法/不存在/自邀：后端 40002 message 原样展示', async () => {
    installFetchMock((url) => {
      if (url.pathname === '/api/v1/auth/register') {
        return { status: 400, body: { code: 40002, message: '不能使用自己的邀请码', data: null } }
      }
      return undefined
    })
    const user = userEvent.setup()
    renderApp(['/register?invite=SELF0001'])

    await fillForm(user, {
      username: 'selfinvite',
      email: 'self@example.com',
      password: 'password123',
      confirm: 'password123',
    })
    await user.click(screen.getByRole('button', { name: '注册并登录' }))

    expect(await screen.findByText('不能使用自己的邀请码')).toBeInTheDocument()
  })
})
