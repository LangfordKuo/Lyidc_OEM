import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { EmailSMTPSettings, EpaySettings, NotificationSettings } from '../../api/types'
import { renderAdmin } from '../../test/adminHarness'
import { apiGet, apiPost, apiPut, fetchCalls } from '../../test/consoleHarness'

// 后台设置：仅 admin 可读写；密钥三态（保持 / 替换 / 清空）与掩码展示。
const epay: EpaySettings = {
  enabled: true,
  gateway: 'https://pay.example.com/',
  pid: '1001',
  key_configured: true,
  key_masked: 'abcd****',
  notify_url: 'https://oem.example.com/api/v1/payments/epay/notify',
  notify_url_recommended: 'http://127.0.0.1:5173/api/v1/payments/epay/notify',
  return_url: 'https://oem.example.com/pay/result',
  updated_by: 1,
  updated_at: '2026-10-08T08:00:00Z',
}

const upstream = {
  base_url: 'https://idc.example.com/',
  username: 'oem',
  api_key_configured: true,
  api_key_masked: 'efgh****',
  timeout_seconds: 5,
  updated_by: 1,
  updated_at: '2026-10-08T08:00:00Z',
}

const smtp: EmailSMTPSettings = {
  enabled: true,
  host: 'smtp.example.com',
  port: 0,
  port_effective: 587,
  username: 'noreply@example.com',
  password_configured: true,
  password_masked: 'ijkl****',
  from: 'noreply@example.com',
  from_name: '岭云互联',
  encryption: 'starttls',
  updated_by: 1,
  updated_at: '2026-10-08T08:00:00Z',
}

const notifications: NotificationSettings = {
  inapp_enabled: true,
  email_enabled: true,
  expiry_reminder_enabled: true,
  expiry_reminder_days: 7,
  updated_by: 1,
  updated_at: '2026-10-08T08:00:00Z',
}

function handlers() {
  return [
    apiGet('/api/v1/admin/settings/payment/epay', epay),
    apiGet('/api/v1/admin/settings/upstream', upstream),
    apiGet('/api/v1/admin/settings/email/smtp', smtp),
    apiGet('/api/v1/admin/settings/notifications', notifications),
    apiPut('/api/v1/admin/settings/payment/epay', { ...epay, gateway: 'https://pay2.example.com/' }),
    apiPut('/api/v1/admin/settings/email/smtp', smtp),
    apiPost('/api/v1/admin/settings/email/test', { sent: true, to: 'admin@example.com' }),
  ]
}

describe('管理后台 · 设置', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('finance / support：整页无权访问，且不请求任何设置接口', async () => {
    renderAdmin('/admin/settings', 'finance', handlers())

    expect(await screen.findByText('无权访问后台设置')).toBeInTheDocument()
    const urls = fetchCalls().map(([url]) => url)
    expect(urls.some((url) => url.includes('/api/v1/admin/settings'))).toBe(false)
  })

  it('admin：渲染四组设置与密钥掩码（不含明文）', async () => {
    renderAdmin('/admin/settings', 'admin', handlers())

    expect(await screen.findByText('易支付（payment.epay）')).toBeInTheDocument()
    expect(screen.getByText('上游对接（upstream）')).toBeInTheDocument()
    expect(screen.getByText('邮件 SMTP（email.smtp）')).toBeInTheDocument()
    expect(screen.getByText('通知开关（notifications）')).toBeInTheDocument()

    expect(screen.getAllByText(/已配置（abcd\*\*\*\*）/).length).toBeGreaterThan(0)
    expect(screen.getAllByText(/已配置（efgh\*\*\*\*）/).length).toBeGreaterThan(0)
    expect(screen.getByText(/已配置（ijkl\*\*\*\*）/)).toBeInTheDocument()
  })

  it('密钥留空保存：请求体不含 key（保持不变）', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/settings', 'admin', handlers())

    const gateway = await screen.findByLabelText('网关地址')
    await user.clear(gateway)
    await user.type(gateway, 'https://pay2.example.com/')

    await user.click(screen.getByRole('button', { name: '保存易支付设置' }))

    await waitFor(() => {
      expect(requestBodyOf('PUT', '/api/v1/admin/settings/payment/epay')).toEqual({
        gateway: 'https://pay2.example.com/',
      })
    })
  })

  it('密钥三态：点「清空商户密钥」后保存会显式发送空串', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/settings', 'admin', handlers())

    await screen.findByLabelText('网关地址')
    await user.click(screen.getByRole('button', { name: '清空商户密钥' }))

    expect(await screen.findByText('保存后清空')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '保存易支付设置' }))

    await waitFor(() => {
      expect(requestBodyOf('PUT', '/api/v1/admin/settings/payment/epay')).toEqual({ key: '' })
    })
  })

  it('测试邮件：调用测试接口并展示结果', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/settings', 'admin', handlers())

    await user.type(await screen.findByLabelText('收件地址（可空）'), 'admin@example.com')
    await user.click(screen.getByRole('button', { name: '发送测试邮件' }))

    await waitFor(() => {
      expect(requestBodyOf('POST', '/api/v1/admin/settings/email/test')).toEqual({
        to: 'admin@example.com',
      })
    })
    expect(await screen.findByText(/测试邮件已发送至 admin@example.com/)).toBeInTheDocument()
  })

  it('未做修改时提示「没有需要保存的修改」且不发请求', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/settings', 'admin', handlers())

    await screen.findByText('易支付（payment.epay）')
    await user.click(screen.getByRole('button', { name: '保存通知开关' }))

    expect(await screen.findByText('没有需要保存的修改')).toBeInTheDocument()
    expect(
      fetchCalls().some(
        ([url, init]) =>
          url.startsWith('/api/v1/admin/settings/notifications') &&
          (init.method ?? 'GET').toUpperCase() === 'PUT',
      ),
    ).toBe(false)
  })
})

function requestBodyOf(method: string, prefix: string): unknown {
  const call = fetchCalls().find(
    ([url, init]) => url.startsWith(prefix) && (init.method ?? 'GET').toUpperCase() === method,
  )
  if (!call) {
    throw new Error(`未找到请求：${method} ${prefix}`)
  }
  return call[1].body === undefined ? undefined : JSON.parse(String(call[1].body))
}
