import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { makeAdminAccount } from '@/test/fixtures'
import {
  installFetchMock,
  ok,
  renderApp,
  requestBody,
  seedAdminProfile,
  seedAdminToken,
} from '@/test/harness'

// 管理后台「设置」页：密钥脱敏展示与三态提交、上游探活、角色矩阵（仅 admin）。

const EPAY = {
  enabled: true,
  gateway: 'https://pay.example.com/',
  pid: '1001',
  key_configured: true,
  key_masked: '1sXR****ZG5',
  notify_url: '',
  notify_url_recommended: 'https://lyidc.example.com/api/v1/payments/epay/notify',
  return_url: '',
  updated_by: 1,
  updated_at: '2026-10-08T06:20:00Z',
}

const UPSTREAM = {
  base_url: 'https://idc.example.com/',
  username: 'oem',
  api_key_configured: true,
  api_key_masked: 'abcd****wxyz',
  timeout_seconds: 5,
  updated_by: 1,
  updated_at: '2026-10-08T06:22:00Z',
}

const SMTP = {
  enabled: false,
  host: 'smtp.example.com',
  port: 0,
  port_effective: 587,
  username: 'noreply@example.com',
  password_configured: true,
  password_masked: 'pass****word',
  from: 'noreply@example.com',
  from_name: '岭云互联',
  encryption: 'starttls' as const,
  updated_by: 1,
  updated_at: '2026-10-08T06:24:00Z',
}

const NOTIFY = {
  inapp_enabled: true,
  email_enabled: false,
  expiry_reminder_enabled: true,
  expiry_reminder_days: 7,
  updated_by: 1,
  updated_at: '2026-10-08T06:26:00Z',
}

function mockSettings(role: 'admin' | 'finance' | 'support' = 'admin') {
  return installFetchMock((url, init) => {
    const path = url.pathname
    if (path === '/api/v1/admin/profile') {
      return ok(makeAdminAccount({ role, username: role }))
    }
    if (path === '/api/v1/admin/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    if (path === '/api/v1/admin/settings/payment/epay') {
      return init.method === 'PUT' ? ok(EPAY) : ok(EPAY)
    }
    if (path === '/api/v1/admin/settings/upstream') {
      return init.method === 'PUT' ? ok(UPSTREAM) : ok(UPSTREAM)
    }
    if (path === '/api/v1/admin/upstream/health') {
      return ok({
        connected: true,
        base_url: UPSTREAM.base_url,
        latency_ms: 21,
        api_key_masked: UPSTREAM.api_key_masked,
        checked_at: '2026-10-08T06:30:00Z',
      })
    }
    if (path === '/api/v1/admin/settings/email/smtp') {
      return init.method === 'PUT' ? ok(SMTP) : ok(SMTP)
    }
    if (path === '/api/v1/admin/settings/email/test') {
      return ok({ sent: true, to: 'ops@example.com' })
    }
    if (path === '/api/v1/admin/settings/notifications') {
      return init.method === 'PUT' ? ok(NOTIFY) : ok(NOTIFY)
    }
    return undefined
  })
}

async function renderSettings(role: 'admin' | 'finance' | 'support' = 'admin') {
  const fetchMock = mockSettings(role)
  seedAdminToken()
  seedAdminProfile({ role, username: role })
  renderApp(['/admin/settings'])
  if (role === 'admin') {
    await screen.findByRole('heading', { name: '设置' })
    await screen.findByText('易支付（payment.epay）')
  } else {
    await screen.findByText('无权访问后台设置')
  }
  return fetchMock
}

describe('管理后台 · 全站设置', () => {
  it('密钥只回显掩码：输入框不回显明文，状态文案给出掩码', async () => {
    await renderSettings()

    // 易支付密钥：已配置 + 掩码展示
    expect(screen.getByTestId('epay_key-status')).toHaveTextContent('已配置（1sXR****ZG5）')
    const keyInput = screen.getByLabelText('商户密钥')
    expect(keyInput).toHaveValue('')
    expect(keyInput).toHaveAttribute('type', 'password')
    // 页面任何地方都不出现明文（掩码只出现前缀 + ****）
    expect(document.body.textContent).not.toContain('1sXRZG5')

    // 上游密钥与 SMTP 口令同理
    expect(screen.getByTestId('upstream_api_key-status')).toHaveTextContent(
      '已配置（abcd****wxyz）',
    )
    expect(screen.getByTestId('smtp_password-status')).toHaveTextContent('已配置（pass****word）')
  })

  it('密钥三态：留空 = 不改、输入 = 替换、清空 = 提交空串', async () => {
    const fetchMock = await renderSettings()
    const user = userEvent.setup()

    // 只切换启用开关 → 请求体只含 enabled
    await user.click(screen.getByRole('switch', { name: '启用易支付' }))
    await user.click(screen.getByRole('button', { name: '保存易支付设置' }))
    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/admin/settings/payment/epay') && init?.method === 'PUT',
      )
      expect(call).toBeTruthy()
      expect(requestBody(call?.[1] as RequestInit)).toEqual({ enabled: false })
    })

    // 输入新密钥 → 请求体含 key 的新值
    await user.type(screen.getByLabelText('商户密钥'), 'brand-new-secret')
    await user.click(screen.getByRole('button', { name: '保存易支付设置' }))
    await waitFor(() => {
      const call = fetchMock.mock.calls
        .filter(
          ([input, init]) =>
            String(input).includes('/admin/settings/payment/epay') && init?.method === 'PUT',
        )
        .at(-1)
      expect(requestBody(call?.[1] as RequestInit)).toEqual({ key: 'brand-new-secret' })
    })

    // 点「清空」→ 请求体 key 为空串（显式清空语义）
    await user.click(screen.getByRole('button', { name: '清空商户密钥' }))
    await user.click(screen.getByRole('button', { name: '保存易支付设置' }))
    await waitFor(() => {
      const call = fetchMock.mock.calls
        .filter(
          ([input, init]) =>
            String(input).includes('/admin/settings/payment/epay') && init?.method === 'PUT',
        )
        .at(-1)
      expect(requestBody(call?.[1] as RequestInit)).toEqual({ key: '' })
    })
  })

  it('上游设置：测试连通性展示探活结果；超时越界被拦截', async () => {
    await renderSettings()
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: '测试连通性' }))
    expect(await screen.findByTestId('upstream-probe-result')).toHaveTextContent('上游连通正常')
    expect(screen.getByText(/耗时 21 ms/)).toBeInTheDocument()

    // 超时越界（>120）→ 本地拦截，不发请求
    await user.type(screen.getByLabelText(/单次请求超时/), '999')
    await user.click(screen.getByRole('button', { name: '保存上游设置' }))
    expect(await screen.findByText(/超时需为 1-120 之间的整数/)).toBeInTheDocument()
  })

  it('SMTP：发送测试邮件前校验收件地址格式', async () => {
    const fetchMock = await renderSettings()
    const user = userEvent.setup()

    await user.type(screen.getByLabelText('收件地址（可空）'), 'not-an-email')
    await user.click(screen.getByRole('button', { name: '发送测试邮件' }))

    expect(await screen.findByText(/收件地址不正确/)).toBeInTheDocument()
    expect(
      fetchMock.mock.calls.some(([input]) => String(input).includes('/settings/email/test')),
    ).toBe(false)
  })

  it('通知开关：修改后保存只提交变化字段', async () => {
    const fetchMock = await renderSettings()
    const user = userEvent.setup()

    await user.click(screen.getByRole('switch', { name: '站内通知' }))
    await user.type(screen.getByLabelText('提前提醒天数（1-30）'), '3')
    await user.click(screen.getByRole('button', { name: '保存通知开关' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/admin/settings/notifications') && init?.method === 'PUT',
      )
      expect(call).toBeTruthy()
      expect(requestBody(call?.[1] as RequestInit)).toEqual({
        inapp_enabled: false,
        expiry_reminder_days: 3,
      })
    })
  })

  it('财务与客服角色：整页无权占位且不请求设置接口', async () => {
    for (const role of ['finance', 'support'] as const) {
      const fetchMock = mockSettings(role)
      seedAdminToken()
      seedAdminProfile({ role, username: role })
      const { unmount } = renderApp(['/admin/settings'])
      await screen.findByText('无权访问后台设置')

      expect(
        fetchMock.mock.calls.some(([input]) => String(input).includes('/admin/settings')),
      ).toBe(false)
      unmount()
    }
  })
})
