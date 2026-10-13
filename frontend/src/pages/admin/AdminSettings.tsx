import { useState } from 'react'
import {
  AlertCircleIcon,
  CheckCircle2Icon,
  DownloadIcon,
  ExternalLinkIcon,
  LoaderCircleIcon,
} from 'lucide-react'
import { toast } from 'sonner'

import { errorMessage } from '@/api/client'
import {
  fetchEmailSMTPSettings,
  fetchEpaySettings,
  fetchNotificationSettings,
  fetchReferralSettings,
  fetchUpdatesSettings,
  fetchUpstreamHealth,
  fetchUpstreamSettings,
  sendTestEmail,
  updateEmailSMTPSettings,
  updateEpaySettings,
  updateNotificationSettings,
  updateReferralSettings,
  updateUpdatesSettings,
  updateUpstreamSettings,
} from '@/api/adminSettings'
import { checkAdminUpdates } from '@/api/adminUpdates'
import type {
  EmailSMTPSettings,
  EpaySettings,
  NotificationSettings,
  ReferralSettings,
  SMTPEncryption,
  UpdateAsset,
  UpdateStatus,
  UpdateStatusCode,
  UpdateUpdatesSettingsInput,
  UpdatesSettings,
  UpstreamHealth,
  UpstreamSettings,
} from '@/api/types'
import { useAdminAuth } from '@/auth/adminAuthContext'
import FilterSelect from '@/components/admin/FilterSelect'
import NoPermission from '@/components/admin/NoPermission'
import SecretInput from '@/components/admin/SecretInput'
import UpdateDot from '@/components/admin/UpdateDot'
import CopyButton from '@/components/common/CopyButton'
import { ErrorState, LoadingBlock } from '@/components/common/PageState'
import StatusBadge from '@/components/common/StatusBadge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import {
  refreshAdminUpdateStatus,
  setAdminUpdateStatus,
  useAdminUpdateStatus,
} from '@/hooks/useAdminUpdateStatus'
import { useAsync } from '@/hooks/useAsync'
import { hasPermission } from '@/lib/adminRoles'
import { formatDateTimeOr, formatFileSize } from '@/lib/format'
import type { StatusTone } from '@/lib/orderStatus'
import { SECRET_KEEP, secretPayload, type SecretState } from '@/lib/secret'
import { STATUS_TONE_TEXT_CLASSES } from '@/lib/statusTone'
import { cn } from '@/lib/utils'
import { validateEmail, validateRebatePercent, validateUpdatesRepo } from '@/lib/validate'

/**
 * AdminSettings 是管理后台「设置」页（契约 12.1 / 17.3 / 19）：系统更新、易支付、上游对接、SMTP、通知开关与返佣。
 *
 * 权限：**仅 admin 角色，含读取**（finance / support 一律 403，页面直接给无权提示且不发请求）。
 * 密钥 / 口令三态：留空 = 保持不变、输入 = 替换、点「清空」= 清空（保存后生效）；
 * 接口只回显配置标志与掩码，前端任何地方都不保存明文。
 */
export default function AdminSettings() {
  const { role } = useAdminAuth()

  if (!hasPermission(role, 'settings.access')) {
    return <NoPermission permission="settings.access" title="无权访问后台设置" />
  }

  return (
    <div className="space-y-5">
      <header>
        <h1 className="text-2xl font-semibold tracking-tight text-foreground">设置</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          系统更新、支付渠道、上游对接、邮件、通知与返佣。保存后立即生效（无需重启服务）；密钥与口令永不回显明文。
        </p>
      </header>

      <UpdatesSection />
      <EpaySection />
      <UpstreamSection />
      <SMTPSection />
      <NotificationSection />
      <ReferralSection />
    </div>
  )
}

// ---------------------------------------------------------------------------
// 通用小组件：开关、区块错误、最近更新时间
// ---------------------------------------------------------------------------

function ToggleField({
  label,
  description,
  value,
  onChange,
  id,
}: {
  label: string
  description?: string
  value: boolean
  onChange: (value: boolean) => void
  id: string
}) {
  return (
    <div className="flex items-start gap-3">
      <Switch id={id} checked={value} onCheckedChange={onChange} aria-label={label} />
      <div className="space-y-0.5">
        <Label htmlFor={id} className="text-sm font-normal">
          {label}
        </Label>
        {description ? <p className="text-xs text-muted-foreground">{description}</p> : null}
      </div>
    </div>
  )
}

function SectionError({ message }: { message: string }) {
  if (!message) {
    return null
  }
  return (
    <Alert variant="destructive">
      <AlertCircleIcon aria-hidden />
      <AlertDescription>{message}</AlertDescription>
    </Alert>
  )
}

/** 各设置区块统一的「最近更新」脚注。 */
function UpdatedFootnote({ at, by }: { at: string | null; by: number | null }) {
  return (
    <span className="text-xs text-muted-foreground">
      最近更新：{formatDateTimeOr(at, '未设置')}
      {by ? ` · 操作人 #${by}` : ''}
    </span>
  )
}

const isHTTPURL = (value: string) => /^https?:\/\/\S+$/i.test(value.trim())

// ---------------------------------------------------------------------------
// 易支付（payment.epay）
// ---------------------------------------------------------------------------

function EpaySection() {
  const state = useAsync(fetchEpaySettings, [])
  const [form, setForm] = useState<EpaySettings | null>(null)
  const [key, setKey] = useState<SecretState>(SECRET_KEEP)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  const current = form ?? state.data

  if (state.loading && !current) {
    return <LoadingBlock label="正在读取易支付设置…" />
  }
  if (state.error && !current) {
    return <ErrorState message={state.error} onRetry={state.reload} />
  }
  if (!current) {
    return null
  }

  const update = (patch: Partial<EpaySettings>) => setForm({ ...current, ...patch })

  const handleSave = async () => {
    const base = state.data
    if (!base) {
      return
    }
    for (const [label, value] of [
      ['网关地址', current.gateway],
      ['异步通知地址', current.notify_url],
      ['同步跳转地址', current.return_url],
    ] as const) {
      if (value.trim() && !isHTTPURL(value)) {
        setError(`${label}必须是合法的 http(s) 地址（留空表示未配置）`)
        return
      }
    }

    // 只提交有变化的字段（局部更新；密钥按三态转换）。
    const payload: Record<string, unknown> = {}
    if (current.enabled !== base.enabled) {
      payload.enabled = current.enabled
    }
    if (current.gateway !== base.gateway) {
      payload.gateway = current.gateway.trim()
    }
    if (current.pid !== base.pid) {
      payload.pid = current.pid.trim()
    }
    if (current.notify_url !== base.notify_url) {
      payload.notify_url = current.notify_url.trim()
    }
    if (current.return_url !== base.return_url) {
      payload.return_url = current.return_url.trim()
    }
    const keyValue = secretPayload(key)
    if (keyValue !== undefined) {
      payload.key = keyValue
    }

    if (Object.keys(payload).length === 0) {
      toast.info('没有需要保存的修改')
      return
    }

    setPending(true)
    setError('')
    try {
      await updateEpaySettings(payload)
      // 清空本地表单改动并重新读取（服务端回带的掩码/更新时间才是权威展示）。
      setForm(null)
      setKey(SECRET_KEEP)
      state.reload()
      toast.success('易支付设置已保存并生效')
    } catch (err) {
      setError(errorMessage(err, '保存失败，请稍后重试'))
    } finally {
      setPending(false)
    }
  }

  return (
    <Card>
      <CardHeader className="border-b">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <CardTitle className="text-base">易支付（payment.epay）</CardTitle>
            <CardDescription>
              彩虹标准协议；启用时网关 / PID / 密钥 / 异步通知地址缺一不可。
            </CardDescription>
          </div>
          <StatusBadge
            tone={current.enabled ? 'ok' : 'pending'}
            label={current.enabled ? '已启用' : '未启用'}
          />
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <SectionError message={error} />

        <ToggleField
          id="epay_enabled"
          label="启用易支付"
          description="关闭后下单接口不再提供在线支付渠道（余额支付不受影响）"
          value={current.enabled}
          onChange={(value) => update({ enabled: value })}
        />

        <div className="grid gap-4 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="epay_gateway">网关地址</FieldLabel>
            <Input
              id="epay_gateway"
              name="epay_gateway"
              placeholder="https://pay.example.com/"
              value={current.gateway}
              onChange={(event) => update({ gateway: event.target.value })}
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="epay_pid">商户 PID</FieldLabel>
            <Input
              id="epay_pid"
              name="epay_pid"
              placeholder="1001"
              value={current.pid}
              onChange={(event) => update({ pid: event.target.value })}
            />
          </Field>
        </div>

        <SecretInput
          label="商户密钥"
          name="epay_key"
          configured={current.key_configured}
          masked={current.key_masked}
          state={key}
          onChange={setKey}
          hint="密钥不参与签名以外的任何展示；接口只回显前 4 位与掩码。"
        />

        <div className="grid gap-4 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="epay_notify_url">异步通知地址（notify_url）</FieldLabel>
            <Input
              id="epay_notify_url"
              name="epay_notify_url"
              placeholder={current.notify_url_recommended}
              value={current.notify_url}
              onChange={(event) => update({ notify_url: event.target.value })}
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="epay_return_url">同步跳转地址（return_url）</FieldLabel>
            <Input
              id="epay_return_url"
              name="epay_return_url"
              placeholder="https://example.com/pay/result"
              value={current.return_url}
              onChange={(event) => update({ return_url: event.target.value })}
            />
          </Field>
        </div>

        {current.notify_url_recommended ? (
          <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <span>推荐回调地址（按当前访问域名推导）：</span>
            <span className="font-mono text-foreground">{current.notify_url_recommended}</span>
            <CopyButton value={current.notify_url_recommended} label="复制回调地址" />
          </div>
        ) : null}

        <div className="flex flex-wrap items-center gap-3">
          <Button disabled={pending} onClick={handleSave}>
            {pending ? (
              <>
                <LoaderCircleIcon className="animate-spin" data-icon="inline-start" aria-hidden />
                保存中…
              </>
            ) : (
              '保存易支付设置'
            )}
          </Button>
          <UpdatedFootnote at={current.updated_at} by={current.updated_by} />
        </div>
      </CardContent>
    </Card>
  )
}

// ---------------------------------------------------------------------------
// 上游对接（upstream）+ 探活
// ---------------------------------------------------------------------------

function UpstreamSection() {
  const state = useAsync(fetchUpstreamSettings, [])
  const [form, setForm] = useState<UpstreamSettings | null>(null)
  const [apiKey, setAPIKey] = useState<SecretState>(SECRET_KEEP)
  const [timeoutText, setTimeoutText] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const [probe, setProbe] = useState<UpstreamHealth | null>(null)
  const [probing, setProbing] = useState(false)

  const current = form ?? state.data

  if (state.loading && !current) {
    return <LoadingBlock label="正在读取上游设置…" />
  }
  if (state.error && !current) {
    return <ErrorState message={state.error} onRetry={state.reload} />
  }
  if (!current) {
    return null
  }

  const update = (patch: Partial<UpstreamSettings>) => setForm({ ...current, ...patch })

  /** 测试连通性：契约第 9 节的只读探活（失败也是 200 + connected=false）。 */
  const handleProbe = async () => {
    setProbing(true)
    try {
      setProbe(await fetchUpstreamHealth())
    } catch (err) {
      toast.error(errorMessage(err, '探活失败'))
    } finally {
      setProbing(false)
    }
  }

  const handleSave = async () => {
    const base = state.data
    if (!base) {
      return
    }
    if (current.base_url.trim() && !isHTTPURL(current.base_url)) {
      setError('上游地址必须是合法的 http(s) 地址（留空表示未配置）')
      return
    }
    const timeout = timeoutText.trim() === '' ? undefined : Number(timeoutText.trim())
    if (timeout !== undefined && (!Number.isInteger(timeout) || timeout < 1 || timeout > 120)) {
      setError('超时需为 1-120 之间的整数（秒）')
      return
    }

    const payload: Record<string, unknown> = {}
    if (current.base_url !== base.base_url) {
      payload.base_url = current.base_url.trim()
    }
    if (current.username !== base.username) {
      payload.username = current.username.trim()
    }
    if (timeout !== undefined) {
      payload.timeout_seconds = timeout
    }
    const keyValue = secretPayload(apiKey)
    if (keyValue !== undefined) {
      payload.api_key = keyValue
    }

    if (Object.keys(payload).length === 0) {
      toast.info('没有需要保存的修改')
      return
    }

    setPending(true)
    setError('')
    try {
      await updateUpstreamSettings(payload)
      setForm(null)
      setAPIKey(SECRET_KEEP)
      setTimeoutText('')
      state.reload()
      toast.success('上游设置已保存，下一次调用即生效')
    } catch (err) {
      setError(errorMessage(err, '保存失败，请稍后重试'))
    } finally {
      setPending(false)
    }
  }

  return (
    <Card>
      <CardHeader className="border-b">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <CardTitle className="text-base">上游对接（upstream）</CardTitle>
            <CardDescription>
              商品导入、实例开通与所有上游操作都使用这里的参数；不落 config.yaml。
            </CardDescription>
          </div>
          <Button variant="outline" size="sm" disabled={probing} onClick={handleProbe}>
            {probing ? '探活中…' : '测试连通性'}
          </Button>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <SectionError message={error} />

        {probe ? (
          <Alert
            className={
              probe.connected
                ? 'border-success/40 bg-success/5'
                : 'border-warning/40 bg-warning/5'
            }
          >
            {probe.connected ? (
              <CheckCircle2Icon className="text-success" aria-hidden />
            ) : (
              <AlertCircleIcon className="text-warning" aria-hidden />
            )}
            <AlertTitle data-testid="upstream-probe-result">
              {probe.connected ? '上游连通正常' : '上游未连通'}
            </AlertTitle>
            <AlertDescription>
              {probe.connected
                ? `${probe.base_url} · 耗时 ${probe.latency_ms} ms`
                : (probe.error || '请检查地址、用户名与密钥')}
            </AlertDescription>
          </Alert>
        ) : null}

        <div className="grid gap-4 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="upstream_base_url">上游地址（base_url）</FieldLabel>
            <Input
              id="upstream_base_url"
              name="upstream_base_url"
              placeholder="https://idc.example.com/"
              value={current.base_url}
              onChange={(event) => update({ base_url: event.target.value })}
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="upstream_username">上游用户名</FieldLabel>
            <Input
              id="upstream_username"
              name="upstream_username"
              placeholder="对接账号"
              value={current.username}
              onChange={(event) => update({ username: event.target.value })}
            />
          </Field>
        </div>

        <SecretInput
          label="上游 API 密钥"
          name="upstream_api_key"
          configured={current.api_key_configured}
          masked={current.api_key_masked}
          state={apiKey}
          onChange={setAPIKey}
          hint="登录上游换取 JWT 使用；接口只回显掩码，日志中同样脱敏。"
        />

        <Field className="w-full sm:w-56">
          <FieldLabel htmlFor="upstream_timeout">单次请求超时（秒，1-120）</FieldLabel>
          <Input
            id="upstream_timeout"
            name="upstream_timeout"
            inputMode="numeric"
            placeholder={`当前 ${current.timeout_seconds}（留空保持不变）`}
            value={timeoutText}
            onChange={(event) => setTimeoutText(event.target.value)}
          />
        </Field>

        <div className="flex flex-wrap items-center gap-3">
          <Button disabled={pending} onClick={handleSave}>
            {pending ? (
              <>
                <LoaderCircleIcon className="animate-spin" data-icon="inline-start" aria-hidden />
                保存中…
              </>
            ) : (
              '保存上游设置'
            )}
          </Button>
          <UpdatedFootnote at={current.updated_at} by={current.updated_by} />
        </div>
      </CardContent>
    </Card>
  )
}

// ---------------------------------------------------------------------------
// 邮件 SMTP（email.smtp）+ 测试邮件
// ---------------------------------------------------------------------------

const ENCRYPTION_OPTIONS: { value: SMTPEncryption; label: string }[] = [
  { value: 'none', label: '不加密（缺省端口 25）' },
  { value: 'starttls', label: 'STARTTLS（缺省端口 587）' },
  { value: 'ssl', label: 'SSL/TLS（缺省端口 465）' },
]

function SMTPSection() {
  const state = useAsync(fetchEmailSMTPSettings, [])
  const [form, setForm] = useState<EmailSMTPSettings | null>(null)
  const [password, setPassword] = useState<SecretState>(SECRET_KEEP)
  const [portText, setPortText] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const [testTo, setTestTo] = useState('')
  const [testResult, setTestResult] = useState('')
  const [testError, setTestError] = useState('')
  const [testing, setTesting] = useState(false)

  const current = form ?? state.data

  if (state.loading && !current) {
    return <LoadingBlock label="正在读取邮件设置…" />
  }
  if (state.error && !current) {
    return <ErrorState message={state.error} onRetry={state.reload} />
  }
  if (!current) {
    return null
  }

  const update = (patch: Partial<EmailSMTPSettings>) => setForm({ ...current, ...patch })

  const handleTest = async () => {
    const trimmed = testTo.trim()
    if (trimmed) {
      const emailError = validateEmail(trimmed)
      if (emailError) {
        setTestError(`收件地址不正确：${emailError}`)
        setTestResult('')
        return
      }
    }
    setTesting(true)
    setTestError('')
    setTestResult('')
    try {
      const result = await sendTestEmail(trimmed || undefined)
      setTestResult(`测试邮件已发送至 ${result.to}，请查收（也记入 email_logs）。`)
    } catch (err) {
      setTestError(errorMessage(err, '测试邮件发送失败'))
    } finally {
      setTesting(false)
    }
  }

  const handleSave = async () => {
    const base = state.data
    if (!base) {
      return
    }
    if (current.from.trim()) {
      const emailError = validateEmail(current.from.trim())
      if (emailError) {
        setError(`发件人地址不正确：${emailError}`)
        return
      }
    }
    const port = portText.trim() === '' ? undefined : Number(portText.trim())
    if (port !== undefined && (!Number.isInteger(port) || port < 0 || port > 65535)) {
      setError('端口需为 0-65535 的整数（留空表示按加密方式取缺省）')
      return
    }

    const payload: Record<string, unknown> = {}
    if (current.enabled !== base.enabled) {
      payload.enabled = current.enabled
    }
    if (current.host !== base.host) {
      payload.host = current.host.trim()
    }
    if (port !== undefined && port !== base.port) {
      payload.port = port
    }
    if (current.username !== base.username) {
      payload.username = current.username.trim()
    }
    if (current.from !== base.from) {
      payload.from = current.from.trim()
    }
    if (current.from_name !== base.from_name) {
      payload.from_name = current.from_name.trim()
    }
    if (current.encryption !== base.encryption) {
      payload.encryption = current.encryption
    }
    const passwordValue = secretPayload(password)
    if (passwordValue !== undefined) {
      payload.password = passwordValue
    }

    if (Object.keys(payload).length === 0) {
      toast.info('没有需要保存的修改')
      return
    }

    setPending(true)
    setError('')
    try {
      await updateEmailSMTPSettings(payload)
      setForm(null)
      setPassword(SECRET_KEEP)
      setPortText('')
      state.reload()
      toast.success('SMTP 设置已保存，下一次发信即使用新参数')
    } catch (err) {
      setError(errorMessage(err, '保存失败，请稍后重试'))
    } finally {
      setPending(false)
    }
  }

  return (
    <Card>
      <CardHeader className="border-b">
        <CardTitle className="text-base">邮件 SMTP（email.smtp）</CardTitle>
        <CardDescription>
          通知邮件与测试邮件的发送通道；未配置时邮件通知静默跳过（站内通知不受影响）。
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <SectionError message={error} />

        <ToggleField
          id="smtp_enabled"
          label="启用邮件通知"
          description="关闭后不发送任何通知邮件（是否真的发信还取决于下方参数是否齐全）"
          value={current.enabled}
          onChange={(value) => update({ enabled: value })}
        />

        <div className="grid gap-4 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="smtp_host">SMTP 主机（不含端口）</FieldLabel>
            <Input
              id="smtp_host"
              name="smtp_host"
              placeholder="smtp.example.com"
              value={current.host}
              onChange={(event) => update({ host: event.target.value })}
            />
          </Field>

          <FilterSelect
            label="加密方式"
            value={current.encryption}
            options={ENCRYPTION_OPTIONS}
            onChange={(value) => update({ encryption: value })}
          />
        </div>

        <div className="grid gap-4 sm:grid-cols-3">
          <Field>
            <FieldLabel htmlFor="smtp_port">端口</FieldLabel>
            <Input
              id="smtp_port"
              name="smtp_port"
              inputMode="numeric"
              placeholder={`留空取缺省（当前生效 ${current.port_effective}）`}
              value={portText}
              onChange={(event) => setPortText(event.target.value)}
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="smtp_username">认证用户名（可空 = 不认证）</FieldLabel>
            <Input
              id="smtp_username"
              name="smtp_username"
              placeholder="noreply@example.com"
              value={current.username}
              onChange={(event) => update({ username: event.target.value })}
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="smtp_from">发件人地址</FieldLabel>
            <Input
              id="smtp_from"
              name="smtp_from"
              placeholder="noreply@example.com"
              value={current.from}
              onChange={(event) => update({ from: event.target.value })}
            />
          </Field>
        </div>

        <SecretInput
          label="SMTP 口令"
          name="smtp_password"
          configured={current.password_configured}
          masked={current.password_masked}
          state={password}
          onChange={setPassword}
          hint="配了认证用户名就必须有口令；不需要认证时请清空用户名与口令。"
        />

        <Field className="w-full sm:w-72">
          <FieldLabel htmlFor="smtp_from_name">发件人显示名（可空）</FieldLabel>
          <Input
            id="smtp_from_name"
            name="smtp_from_name"
            placeholder="岭云互联"
            value={current.from_name}
            onChange={(event) => update({ from_name: event.target.value })}
          />
        </Field>

        <div className="flex flex-wrap items-center gap-3">
          <Button disabled={pending} onClick={handleSave}>
            {pending ? (
              <>
                <LoaderCircleIcon className="animate-spin" data-icon="inline-start" aria-hidden />
                保存中…
              </>
            ) : (
              '保存 SMTP 设置'
            )}
          </Button>
          <UpdatedFootnote at={current.updated_at} by={current.updated_by} />
        </div>

        <div className="space-y-3 border-t border-border pt-4">
          <p className="text-sm font-medium text-foreground">发送测试邮件</p>
          <p className="text-xs text-muted-foreground">
            同步发送一封测试邮件验证参数（不受通知总开关限制，同样写入 email_logs）；
            收件地址留空时使用站点管理员邮箱。
          </p>
          <div className="flex flex-wrap items-end gap-3">
            <Field className="w-full sm:w-72">
              <FieldLabel htmlFor="test_email_to">收件地址（可空）</FieldLabel>
              <Input
                id="test_email_to"
                name="test_email_to"
                placeholder="admin@example.com"
                value={testTo}
                aria-invalid={testError ? true : undefined}
                onChange={(event) => {
                  setTestTo(event.target.value)
                  setTestError('')
                }}
              />
            </Field>
            <Button variant="outline" disabled={testing} onClick={handleTest}>
              {testing ? '正在发送…' : '发送测试邮件'}
            </Button>
          </div>
          {testResult ? (
            <Alert className="border-success/40 bg-success/5">
              <CheckCircle2Icon className="text-success" aria-hidden />
              <AlertDescription>{testResult}</AlertDescription>
            </Alert>
          ) : null}
          {testError ? (
            <Alert variant="destructive">
              <AlertCircleIcon aria-hidden />
              <AlertDescription>{testError}</AlertDescription>
            </Alert>
          ) : null}
        </div>
      </CardContent>
    </Card>
  )
}

// ---------------------------------------------------------------------------
// 通知开关（notifications）
// ---------------------------------------------------------------------------

function NotificationSection() {
  const state = useAsync(fetchNotificationSettings, [])
  const [form, setForm] = useState<NotificationSettings | null>(null)
  const [daysText, setDaysText] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  const current = form ?? state.data

  if (state.loading && !current) {
    return <LoadingBlock label="正在读取通知开关…" />
  }
  if (state.error && !current) {
    return <ErrorState message={state.error} onRetry={state.reload} />
  }
  if (!current) {
    return null
  }

  const update = (patch: Partial<NotificationSettings>) => setForm({ ...current, ...patch })

  const handleSave = async () => {
    const base = state.data
    if (!base) {
      return
    }
    const days = daysText.trim() === '' ? undefined : Number(daysText.trim())
    if (days !== undefined && (!Number.isInteger(days) || days < 1 || days > 30)) {
      setError('提醒天数需为 1-30 之间的整数')
      return
    }

    const payload: Record<string, unknown> = {}
    if (current.inapp_enabled !== base.inapp_enabled) {
      payload.inapp_enabled = current.inapp_enabled
    }
    if (current.email_enabled !== base.email_enabled) {
      payload.email_enabled = current.email_enabled
    }
    if (current.expiry_reminder_enabled !== base.expiry_reminder_enabled) {
      payload.expiry_reminder_enabled = current.expiry_reminder_enabled
    }
    if (days !== undefined && days !== base.expiry_reminder_days) {
      payload.expiry_reminder_days = days
    }

    if (Object.keys(payload).length === 0) {
      toast.info('没有需要保存的修改')
      return
    }

    setPending(true)
    setError('')
    try {
      await updateNotificationSettings(payload)
      setForm(null)
      setDaysText('')
      state.reload()
      toast.success('通知开关已保存')
    } catch (err) {
      setError(errorMessage(err, '保存失败，请稍后重试'))
    } finally {
      setPending(false)
    }
  }

  return (
    <Card>
      <CardHeader className="border-b">
        <CardTitle className="text-base">通知开关（notifications）</CardTitle>
        <CardDescription>
          站内通知、通知邮件与到期提醒的全局开关；缺省全开、提前 7 天提醒。
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <SectionError message={error} />

        <div className="space-y-3">
          <ToggleField
            id="notify_inapp"
            label="站内通知"
            description="关闭后不再写入站内通知（会员与管理员收件箱都不产生新通知）"
            value={current.inapp_enabled}
            onChange={(value) => update({ inapp_enabled: value })}
          />
          <ToggleField
            id="notify_email"
            label="邮件通知"
            description="关闭后不发送任何通知邮件（仍需 email.smtp 配置齐全才会真正发信）"
            value={current.email_enabled}
            onChange={(value) => update({ email_enabled: value })}
          />
          <ToggleField
            id="notify_expiry"
            label="到期提醒"
            description="关闭后到期扫描不产生任何提醒，也不认领去重锚点"
            value={current.expiry_reminder_enabled}
            onChange={(value) => update({ expiry_reminder_enabled: value })}
          />
        </div>

        <Field className="w-full sm:w-64">
          <FieldLabel htmlFor="expiry_reminder_days">提前提醒天数（1-30）</FieldLabel>
          <Input
            id="expiry_reminder_days"
            name="expiry_reminder_days"
            inputMode="numeric"
            placeholder={`当前 ${current.expiry_reminder_days} 天（留空保持不变）`}
            value={daysText}
            onChange={(event) => setDaysText(event.target.value)}
          />
        </Field>

        <div className="flex flex-wrap items-center gap-3">
          <Button disabled={pending} onClick={handleSave}>
            {pending ? (
              <>
                <LoaderCircleIcon className="animate-spin" data-icon="inline-start" aria-hidden />
                保存中…
              </>
            ) : (
              '保存通知开关'
            )}
          </Button>
          <UpdatedFootnote at={current.updated_at} by={current.updated_by} />
        </div>
      </CardContent>
    </Card>
  )
}

/**
 * 返佣设置（契约 18.5，R10-A）：总开关、全局默认返佣比例、自建券折扣上限。
 * 仅 admin（设置页整页闸门已挡 finance / support）；比例越界由后端 40002 判定。
 */
function ReferralSection() {
  const state = useAsync(fetchReferralSettings, [])
  const [form, setForm] = useState<ReferralSettings | null>(null)
  const [defaultPercent, setDefaultPercent] = useState('')
  const [maxCouponPercent, setMaxCouponPercent] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  const current = form ?? state.data

  if (state.loading && !current) {
    return <LoadingBlock label="正在读取返佣设置…" />
  }
  if (state.error && !current) {
    return <ErrorState message={state.error} onRetry={state.reload} />
  }
  if (!current) {
    return null
  }

  const handleSave = async () => {
    const base = state.data
    if (!base) {
      return
    }
    const defaultError = validateRebatePercent(defaultPercent)
    const maxError = validateRebatePercent(maxCouponPercent)
    const firstError = defaultError || maxError
    if (firstError) {
      setError(firstError)
      return
    }

    const payload: Record<string, unknown> = {}
    if (current.enabled !== base.enabled) {
      payload.enabled = current.enabled
    }
    const defaultTrimmed = defaultPercent.trim()
    if (defaultTrimmed && defaultTrimmed !== base.default_percent) {
      payload.default_percent = defaultTrimmed
    }
    const maxTrimmed = maxCouponPercent.trim()
    if (maxTrimmed && maxTrimmed !== base.max_coupon_percent) {
      payload.max_coupon_percent = maxTrimmed
    }

    if (Object.keys(payload).length === 0) {
      toast.info('没有需要保存的修改')
      return
    }

    setPending(true)
    setError('')
    try {
      await updateReferralSettings(payload)
      setForm(null)
      setDefaultPercent('')
      setMaxCouponPercent('')
      state.reload()
      toast.success('返佣设置已保存')
    } catch (err) {
      setError(errorMessage(err, '保存失败，请稍后重试'))
    } finally {
      setPending(false)
    }
  }

  return (
    <Card>
      <CardHeader className="border-b">
        <CardTitle className="text-base">返佣设置（referral）</CardTitle>
        <CardDescription>
          总开关、全局默认返佣比例与邀请人自建券折扣上限。改完下一笔支付即用新比例；
          自建券折扣上限 = min(此处上限, 邀请人的生效返佣比例)。
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <SectionError message={error} />

        <ToggleField
          id="referral_enabled"
          label="返佣总开关"
          description="关闭后支付成功不发放任何返佣；已发放的返佣与已生成的邀请码不受影响"
          value={current.enabled}
          onChange={(value) => setForm({ ...current, enabled: value })}
        />

        <div className="grid gap-4 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="referral_default_percent">
              全局默认返佣比例（%，留空保持不变）
            </FieldLabel>
            <Input
              id="referral_default_percent"
              name="referral_default_percent"
              inputMode="decimal"
              placeholder={`当前 ${current.default_percent}%`}
              value={defaultPercent}
              onChange={(event) => {
                setDefaultPercent(event.target.value)
                setError('')
              }}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="referral_max_coupon_percent">
              自建券折扣上限（%，留空保持不变）
            </FieldLabel>
            <Input
              id="referral_max_coupon_percent"
              name="referral_max_coupon_percent"
              inputMode="decimal"
              placeholder={`当前 ${current.max_coupon_percent}%`}
              value={maxCouponPercent}
              onChange={(event) => {
                setMaxCouponPercent(event.target.value)
                setError('')
              }}
            />
          </Field>
        </div>

        <div className="flex flex-wrap items-center gap-3">
          <Button disabled={pending} onClick={handleSave}>
            {pending ? (
              <>
                <LoaderCircleIcon className="animate-spin" data-icon="inline-start" aria-hidden />
                保存中…
              </>
            ) : (
              '保存返佣设置'
            )}
          </Button>
          <UpdatedFootnote at={current.updated_at} by={current.updated_by} />
        </div>
      </CardContent>
    </Card>
  )
}

// ---------------------------------------------------------------------------
// 系统更新（updates，R13-A / 契约 19）：当前版本、实时检查、结果区与更新源设置
// ---------------------------------------------------------------------------

type UpdateFailureCode = Exclude<UpdateStatusCode, 'ok' | 'not_checked'>

/** 失败态（契约 19.3）的短标签；ok / not_checked 不在此表内。 */
const UPDATE_FAILURE_LABELS: Record<UpdateFailureCode, string> = {
  no_release: '暂无正式版本',
  rate_limited: '访问受限',
  unauthorized: 'Token 无效',
  timeout: '连接超时',
  unreachable: '无法连接',
  bad_response: '数据异常',
}

/** 失败态色调：可自行恢复的环境类故障用警示色；Token 无效 / 数据异常需人工处理用错误色；无正式版是中性信息。 */
const UPDATE_FAILURE_TONES: Record<UpdateFailureCode, StatusTone> = {
  no_release: 'pending',
  rate_limited: 'warn',
  unauthorized: 'error',
  timeout: 'warn',
  unreachable: 'warn',
  bad_response: 'error',
}

/** 是否为失败态：失败只改写 status / message，上一次成功的结果仍保留在同一对象里（契约 19.4）。 */
function isUpdateFailure(
  status: UpdateStatus,
): status is UpdateStatus & { status: UpdateFailureCode } {
  return status.status in UPDATE_FAILURE_LABELS
}

/**
 * UpdatesSection 是「系统更新」卡片（契约 19.7）：当前版本（读缓存）、「检查更新」（实时出网）、
 * 结果区与更新源设置。仅 admin 可进（设置页整页闸门已挡 finance / support，卡片内不再额外判断）。
 * GitHub 侧失败是 200 + status：页面据 status 展示失败态与「重试」；只有 HTTP 层异常（ApiError）才 toast。
 */
function UpdatesSection() {
  const { status, hasUpdate } = useAdminUpdateStatus()
  const [checking, setChecking] = useState(false)

  const handleCheck = async () => {
    setChecking(true)
    try {
      // 检查结果同步进共享 store：侧栏「设置」红点与本卡片标题红点即时生效。
      setAdminUpdateStatus(await checkAdminUpdates())
    } catch (err) {
      toast.error(errorMessage(err, '检查更新失败，请稍后重试'))
    } finally {
      setChecking(false)
    }
  }

  return (
    <Card>
      <CardHeader className="border-b">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <div className="flex items-center gap-2">
              <CardTitle className="text-base">系统更新</CardTitle>
              {hasUpdate ? <UpdateDot /> : null}
            </div>
            <CardDescription>
              查看当前版本，并检查 GitHub 上的最新正式版。仅检测与展示，不下载、不替换、不重启服务。
            </CardDescription>
          </div>
          <Button variant="outline" size="sm" disabled={checking} onClick={handleCheck}>
            {checking ? (
              <>
                <LoaderCircleIcon className="animate-spin" data-icon="inline-start" aria-hidden />
                检查中…
              </>
            ) : (
              '检查更新'
            )}
          </Button>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <CurrentVersion status={status} />
        <UpdateResult status={status} checking={checking} onRetry={handleCheck} />
        <UpdateSourceForm />
      </CardContent>
    </Card>
  )
}

/** 当前版本（读缓存）；构建注入的 commit 与构建时间小字展示，未注入时为 unknown（原样展示）。 */
function CurrentVersion({ status }: { status: UpdateStatus | null }) {
  return (
    <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
      <span className="text-sm text-muted-foreground">当前版本</span>
      <span
        className="font-mono text-base font-semibold text-foreground"
        data-testid="update-current-version"
      >
        {status?.current_version ?? '—'}
      </span>
      {status ? (
        <span className="text-xs text-muted-foreground">
          构建 commit {status.commit} · 构建时间 {formatDateTimeOr(status.built_at, '未知')}
        </span>
      ) : null}
    </div>
  )
}

/**
 * 结果区（契约 19.7）：无数据或未检查时给出引导；否则先给失败提示（如有），再给主结论。
 * 失败尝试只改写 status / message，latest_version 仍是上一次成功的结果（19.4），因此失败态也可能与「有新版本」并存。
 */
function UpdateResult({
  status,
  checking,
  onRetry,
}: {
  status: UpdateStatus | null
  checking: boolean
  onRetry: () => void
}) {
  if (!status) {
    return (
      <p className="text-sm text-muted-foreground">暂无版本信息，点击「检查更新」获取最新版本</p>
    )
  }
  if (status.status === 'not_checked') {
    return (
      <p className="text-sm text-muted-foreground">尚未检查更新，点击「检查更新」获取最新版本</p>
    )
  }
  return (
    <div className="space-y-4">
      {isUpdateFailure(status) ? (
        <UpdateFailureNotice status={status} checking={checking} onRetry={onRetry} />
      ) : null}
      <UpdateConclusion status={status} />
    </div>
  )
}

/** 主结论（契约 19.7 优先级）：开发构建 → 有新版本 → 已是最新 → 无法比较。 */
function UpdateConclusion({ status }: { status: UpdateStatus }) {
  if (status.compare === 'dev_build') {
    // 失败态下 message 是失败原因而非开发说明（19.3），此时只给结论，原因由失败提示展示。
    return (
      <NoticeLine
        title="开发构建，无法比较版本"
        message={isUpdateFailure(status) ? '' : status.message}
      />
    )
  }
  if (status.has_update) {
    return <UpdateAvailable status={status} />
  }
  if (status.status === 'ok' && status.compare === 'ok') {
    return (
      <p className="flex items-center gap-2 text-sm font-medium text-foreground">
        <CheckCircle2Icon className={cn('size-4', STATUS_TONE_TEXT_CLASSES.ok)} aria-hidden />
        已是最新版本 {status.current_version}（上次检查：{formatDateTimeOr(status.checked_at, '—')}）
      </p>
    )
  }
  if (status.status === 'ok') {
    return <NoticeLine title="无法比较版本" message={status.message} />
  }
  return null
}

/** 中性结论提示（开发构建 / 无法比较都不是故障，因此不用状态色）。 */
function NoticeLine({ title, message }: { title: string; message: string }) {
  return (
    <div className="space-y-1 rounded-lg border border-border bg-muted/50 px-3 py-2">
      <p className="text-sm font-medium text-foreground">{title}</p>
      {message ? <p className="text-xs text-muted-foreground">{message}</p> : null}
    </div>
  )
}

/** 失败提示：状态徽标 + message（失败原因）+ 重试；有上一次成功结果时一并保留展示。 */
function UpdateFailureNotice({
  status,
  checking,
  onRetry,
}: {
  status: UpdateStatus & { status: UpdateFailureCode }
  checking: boolean
  onRetry: () => void
}) {
  const tone = UPDATE_FAILURE_TONES[status.status]
  return (
    <div
      className="space-y-2 rounded-lg border border-border bg-card p-3"
      data-testid="update-failure"
    >
      <div className="flex flex-wrap items-center gap-2">
        <StatusBadge tone={tone} label={UPDATE_FAILURE_LABELS[status.status]} />
        <span className={cn('text-sm', STATUS_TONE_TEXT_CLASSES[tone])}>
          {status.message || UPDATE_FAILURE_LABELS[status.status]}
        </span>
      </div>
      {status.latest_version ? (
        <p className="text-xs text-muted-foreground">
          上次成功结果：{status.latest_version}（{formatDateTimeOr(status.checked_at, '—')}）
        </p>
      ) : null}
      <Button variant="outline" size="sm" disabled={checking} onClick={onRetry}>
        重试
      </Button>
    </div>
  )
}

/**
 * 有新版本（契约 19.7）：醒目展示版本号、发布时间、纯文本 Release 说明、产物列表与 Release 页链接。
 * Release 说明按纯文本渲染（whitespace-pre-wrap，不用 dangerouslySetInnerHTML、不做 Markdown），防注入。
 */
function UpdateAvailable({ status }: { status: UpdateStatus }) {
  // 产物列表兜底：后端应返回 []，此处防御 null，避免单个字段异常让整块页面报错。
  const assets = status.assets ?? []
  return (
    <section
      className="space-y-4 rounded-xl border border-primary/30 bg-accent p-4"
      data-testid="update-available"
    >
      <div className="space-y-1">
        <p className="text-base font-semibold text-primary">发现新版本 {status.latest_version}</p>
        <p className="text-xs text-muted-foreground">
          发布时间：{formatDateTimeOr(status.published_at, '—')}
        </p>
      </div>

      <div className="space-y-2">
        <p className="text-xs font-medium text-foreground">Release 说明</p>
        <div
          data-testid="update-notes"
          className="max-h-64 overflow-y-auto rounded-lg border border-border bg-card p-3 text-sm break-words whitespace-pre-wrap text-foreground"
        >
          {status.notes || '该版本未填写说明'}
        </div>
        {status.notes_truncated ? (
          <p className="text-xs text-muted-foreground">
            说明已截断（超过 16 KiB），完整内容请在 Release 页查看。
          </p>
        ) : null}
      </div>

      <div className="space-y-2">
        <p className="text-xs font-medium text-foreground">产物（服务器平台：{status.platform}）</p>
        {assets.length > 0 ? (
          <ul className="space-y-2">
            {assets.map((asset) => (
              <AssetRow key={asset.name} asset={asset} />
            ))}
          </ul>
        ) : (
          <p className="text-xs text-muted-foreground">该 Release 未附带产物</p>
        )}
      </div>

      {isHTTPURL(status.html_url) ? (
        <a
          href={status.html_url}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1 text-sm font-medium text-primary underline-offset-4 hover:underline"
        >
          <ExternalLinkIcon className="size-3.5" aria-hidden />
          查看 Release 页
        </a>
      ) : null}
    </section>
  )
}

/** 单个产物：platform_match 的一项用强调底色与边框高亮并标注「本服务器平台」；.sha256 等其余产物照常列出。 */
function AssetRow({ asset }: { asset: UpdateAsset }) {
  return (
    <li
      data-testid="update-asset"
      className={cn(
        'flex flex-wrap items-center gap-x-3 gap-y-2 rounded-lg border px-3 py-2 text-sm',
        asset.platform_match ? 'border-primary bg-accent' : 'border-border bg-card',
      )}
    >
      <span className="font-mono break-all text-foreground">{asset.name}</span>
      <span className="text-xs text-muted-foreground">{formatFileSize(asset.size)}</span>
      {asset.platform_match ? (
        <Badge variant="secondary" className="bg-card text-primary">
          本服务器平台
        </Badge>
      ) : null}
      {isHTTPURL(asset.download_url) ? (
        <a
          href={asset.download_url}
          target="_blank"
          rel="noopener noreferrer"
          className="ml-auto inline-flex items-center gap-1 text-sm text-primary underline-offset-4 hover:underline"
        >
          <DownloadIcon className="size-3.5" aria-hidden />
          下载
        </a>
      ) : null}
    </li>
  )
}

/**
 * 更新源设置（契约 19.5）：GitHub 仓库 + Token（三态，沿用 SecretInput，与上游 API 密钥同口径）。
 * 只提交变化的字段；Token 只回显掩码、绝不回填明文。修改仓库时后端同请求清空检查缓存，这里同步清掉红点与旧结果。
 */
function UpdateSourceForm() {
  const state = useAsync(fetchUpdatesSettings, [])
  const [form, setForm] = useState<UpdatesSettings | null>(null)
  const [token, setToken] = useState<SecretState>(SECRET_KEEP)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  const current = form ?? state.data

  if (state.loading && !current) {
    return <LoadingBlock label="正在读取更新源设置…" />
  }
  if (state.error && !current) {
    return <ErrorState message={state.error} onRetry={state.reload} />
  }
  if (!current) {
    return null
  }

  const update = (patch: Partial<UpdatesSettings>) => setForm({ ...current, ...patch })
  // 仓库前端预校验与后端同口径（owner/name）：不合法即禁用保存并给出提示。
  const repoError = validateUpdatesRepo(current.repo)
  const repoChanged = current.repo.trim() !== (state.data?.repo ?? '')
  const dirty = repoChanged || token.mode !== 'keep'

  const handleSave = async () => {
    if (!state.data) {
      return
    }
    if (repoError) {
      setError(repoError)
      return
    }

    // 只提交变化字段：仓库未改则省略，Token 为保持不变（keep）则省略。
    const payload: UpdateUpdatesSettingsInput = {}
    if (repoChanged) {
      payload.repo = current.repo.trim()
    }
    const tokenValue = secretPayload(token)
    if (tokenValue !== undefined) {
      payload.github_token = tokenValue
    }

    if (Object.keys(payload).length === 0) {
      toast.info('没有需要保存的修改')
      return
    }

    setPending(true)
    setError('')
    try {
      await updateUpdatesSettings(payload)
      setForm(null)
      setToken(SECRET_KEEP)
      state.reload()
      if (payload.repo !== undefined) {
        // 后端已清空旧仓库的检查缓存：先清掉旧红点与旧结果，再读回缓存（当前版本仍有效）。
        setAdminUpdateStatus(null)
        await refreshAdminUpdateStatus()
        toast.success('更新源已保存，旧仓库的检查结果已清空，请重新检查')
      } else {
        toast.success('更新源设置已保存，下一次检查即使用新配置')
      }
    } catch (err) {
      setError(errorMessage(err, '保存失败，请稍后重试'))
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="space-y-3 border-t border-border pt-4">
      <div className="space-y-0.5">
        <p className="text-sm font-medium text-foreground">更新源设置</p>
        <p className="text-xs text-muted-foreground">
          检查更新访问的 GitHub 仓库与可选 Token；修改仓库会清空当前的检查结果。
        </p>
      </div>
      <SectionError message={error} />

      <div className="grid gap-4 sm:grid-cols-2">
        <Field>
          <FieldLabel htmlFor="updates_repo">GitHub 仓库（owner/name）</FieldLabel>
          <Input
            id="updates_repo"
            name="updates_repo"
            placeholder="owner/name"
            value={current.repo}
            aria-invalid={repoError ? true : undefined}
            onChange={(event) => update({ repo: event.target.value })}
          />
          {repoError ? <FieldError>{repoError}</FieldError> : null}
        </Field>

        <SecretInput
          label="GitHub Token"
          name="updates_github_token"
          configured={current.github_token_configured}
          masked={current.github_token_masked}
          state={token}
          onChange={setToken}
          hint="可选。配置后访问 GitHub 的额度更高；只存服务端，接口只回显掩码。"
        />
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <Button disabled={pending || !dirty || Boolean(repoError)} onClick={handleSave}>
          {pending ? (
            <>
              <LoaderCircleIcon className="animate-spin" data-icon="inline-start" aria-hidden />
              保存中…
            </>
          ) : (
            '保存更新源设置'
          )}
        </Button>
        <UpdatedFootnote at={current.updated_at} by={current.updated_by} />
      </div>
    </div>
  )
}
