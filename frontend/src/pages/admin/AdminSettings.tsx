import { useState } from 'react'
import { AlertCircleIcon, CheckCircle2Icon, LoaderCircleIcon } from 'lucide-react'
import { toast } from 'sonner'

import { errorMessage } from '@/api/client'
import {
  fetchEmailSMTPSettings,
  fetchEpaySettings,
  fetchNotificationSettings,
  fetchUpstreamHealth,
  fetchUpstreamSettings,
  sendTestEmail,
  updateEmailSMTPSettings,
  updateEpaySettings,
  updateNotificationSettings,
  updateUpstreamSettings,
} from '@/api/adminSettings'
import type {
  EmailSMTPSettings,
  EpaySettings,
  NotificationSettings,
  SMTPEncryption,
  UpstreamHealth,
  UpstreamSettings,
} from '@/api/types'
import { useAdminAuth } from '@/auth/adminAuthContext'
import FilterSelect from '@/components/admin/FilterSelect'
import NoPermission from '@/components/admin/NoPermission'
import SecretInput from '@/components/admin/SecretInput'
import CopyButton from '@/components/common/CopyButton'
import { ErrorState, LoadingBlock } from '@/components/common/PageState'
import StatusBadge from '@/components/common/StatusBadge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { useAsync } from '@/hooks/useAsync'
import { hasPermission } from '@/lib/adminRoles'
import { formatDateTimeOr } from '@/lib/format'
import { SECRET_KEEP, secretPayload, type SecretState } from '@/lib/secret'
import { validateEmail } from '@/lib/validate'

/**
 * AdminSettings 是管理后台「设置」页（契约 12.1 / 17.3）：易支付、上游对接、SMTP、通知开关。
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
          支付渠道、上游对接、邮件与通知开关。保存后立即生效（无需重启服务）；密钥与口令永不回显明文。
        </p>
      </header>

      <EpaySection />
      <UpstreamSection />
      <SMTPSection />
      <NotificationSection />
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
