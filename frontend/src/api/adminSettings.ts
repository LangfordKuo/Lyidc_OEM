import { http } from './client'
import type {
  EmailSMTPSettings,
  EmailTestResult,
  EpaySettings,
  NotificationSettings,
  UpdateEmailSMTPSettingsInput,
  UpdateEpaySettingsInput,
  UpdateNotificationSettingsInput,
  UpdateUpstreamSettingsInput,
  UpstreamHealth,
  UpstreamSettings,
} from './types'

// 后台设置接口（契约 12.1 / 17.3 / 第 9 节）：**全部仅 admin 角色，含读取**
// （finance / support 一律 403）。密钥/口令永不回显明文：GET 只给 `*_configured` 与掩码，
// PUT 的密钥字段是三态——省略 = 保持不变、给值 = 替换、空串 = 清空。

/** GET /api/v1/admin/settings/payment/epay —— 易支付设置。 */
export function fetchEpaySettings(): Promise<EpaySettings> {
  return http.get<EpaySettings>('/admin/settings/payment/epay', { auth: 'admin' })
}

/** PUT /api/v1/admin/settings/payment/epay —— 局部更新（至少一个字段），改完立即生效。 */
export function updateEpaySettings(input: UpdateEpaySettingsInput): Promise<EpaySettings> {
  return http.put<EpaySettings>('/admin/settings/payment/epay', input, { auth: 'admin' })
}

/** GET /api/v1/admin/settings/upstream —— 上游对接设置。 */
export function fetchUpstreamSettings(): Promise<UpstreamSettings> {
  return http.get<UpstreamSettings>('/admin/settings/upstream', { auth: 'admin' })
}

/** PUT /api/v1/admin/settings/upstream —— 局部更新（至少一个字段），下一次调用即生效。 */
export function updateUpstreamSettings(
  input: UpdateUpstreamSettingsInput,
): Promise<UpstreamSettings> {
  return http.put<UpstreamSettings>('/admin/settings/upstream', input, { auth: 'admin' })
}

/** GET /api/v1/admin/upstream/health —— 上游探活（只读；未配置时 connected=false）。 */
export function fetchUpstreamHealth(): Promise<UpstreamHealth> {
  return http.get<UpstreamHealth>('/admin/upstream/health', {
    auth: 'admin',
    timeoutMs: 30_000,
  })
}

/** GET /api/v1/admin/settings/email/smtp —— SMTP 设置（口令只回显配置标志与掩码）。 */
export function fetchEmailSMTPSettings(): Promise<EmailSMTPSettings> {
  return http.get<EmailSMTPSettings>('/admin/settings/email/smtp', { auth: 'admin' })
}

/** PUT /api/v1/admin/settings/email/smtp —— 局部更新（口令三态），改完立即生效。 */
export function updateEmailSMTPSettings(
  input: UpdateEmailSMTPSettingsInput,
): Promise<EmailSMTPSettings> {
  return http.put<EmailSMTPSettings>('/admin/settings/email/smtp', input, { auth: 'admin' })
}

/**
 * POST /api/v1/admin/settings/email/test —— 发送测试邮件（同步等待结果）。
 * to 缺省时后端回退站点 admin_email；SMTP 未配置 → 40002，发送失败 → 50004。
 */
export function sendTestEmail(to?: string): Promise<EmailTestResult> {
  return http.post<EmailTestResult>(
    '/admin/settings/email/test',
    to ? { to } : {},
    { auth: 'admin', timeoutMs: 60_000 },
  )
}

/** GET /api/v1/admin/settings/notifications —— 通知开关。 */
export function fetchNotificationSettings(): Promise<NotificationSettings> {
  return http.get<NotificationSettings>('/admin/settings/notifications', { auth: 'admin' })
}

/** PUT /api/v1/admin/settings/notifications —— 局部更新通知开关。 */
export function updateNotificationSettings(
  input: UpdateNotificationSettingsInput,
): Promise<NotificationSettings> {
  return http.put<NotificationSettings>('/admin/settings/notifications', input, { auth: 'admin' })
}
