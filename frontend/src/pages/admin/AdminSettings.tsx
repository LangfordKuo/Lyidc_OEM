import { Alert, Button, Card, Chip, Input, Label, Switch, TextField, toast } from '@heroui/react'
import { useState } from 'react'

import { errorMessage } from '../../api/client'
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
} from '../../api/adminSettings'
import type {
  EmailSMTPSettings,
  EpaySettings,
  NotificationSettings,
  SMTPEncryption,
  UpstreamHealth,
  UpstreamSettings,
} from '../../api/types'
import { useAdminAuth } from '../../auth/adminAuthContext'
import NoPermission from '../../components/admin/NoPermission'
import CopyButton from '../../components/common/CopyButton'
import { ErrorState, LoadingBlock } from '../../components/common/PageState'
import FilterSelect from '../../components/admin/FilterSelect'
import { useAsync } from '../../hooks/useAsync'
import { hasPermission } from '../../lib/adminRoles'
import { formatDateTimeOr } from '../../lib/format'
import { validateEmail } from '../../lib/validate'

// AdminSettings 是管理后台「设置」页（契约 12.1 / 17.3）：易支付、上游对接、SMTP、通知开关。
//
// 权限：**仅 admin 角色，含读取**（finance / support 一律 403，页面直接给无权提示）。
// 密钥 / 口令三态：留空 = 保持不变、输入 = 替换、点「清空」= 清空（保存后生效）；
// 接口只回显配置标志与掩码，前端任何地方都不保存明文。
export default function AdminSettings() {
  const { role } = useAdminAuth()

  if (!hasPermission(role, 'settings.access')) {
    return <NoPermission permission="settings.access" title="无权访问后台设置" />
  }

  return (
    <div className="space-y-5">
      <header>
        <h1 className="text-xl font-semibold text-foreground">设置</h1>
        <p className="mt-1 text-sm text-muted">
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
// 通用小组件：开关与密钥输入（三态）
// ---------------------------------------------------------------------------

function ToggleField({
  label,
  description,
  value,
  onChange,
}: {
  label: string
  description?: string
  value: boolean
  onChange: (value: boolean) => void
}) {
  return (
    <Switch isSelected={value} onChange={onChange} aria-label={label}>
      <Switch.Content>
        <Switch.Control>
          <Switch.Thumb />
        </Switch.Control>
        <span className="flex flex-col text-left">
          <span className="text-sm text-foreground">{label}</span>
          {description ? <span className="text-xs text-muted">{description}</span> : null}
        </span>
      </Switch.Content>
    </Switch>
  )
}

/** 密钥三态值：keep = 保持、replace = 用 value 替换、clear = 清空。 */
type SecretMode = 'keep' | 'replace' | 'clear'

interface SecretValue {
  mode: SecretMode
  value: string
}

const SECRET_KEEP: SecretValue = { mode: 'keep', value: '' }

function SecretInput({
  label,
  name,
  configured,
  masked,
  state,
  onChange,
  placeholder,
  hint,
}: {
  label: string
  name: string
  configured: boolean
  masked: string
  state: SecretValue
  onChange: (next: SecretValue) => void
  placeholder?: string
  hint?: string
}) {
  return (
    <div className="space-y-2">
      <TextField
        name={name}
        type="password"
        value={state.value}
        onChange={(value) => onChange(value ? { mode: 'replace', value } : SECRET_KEEP)}
        isDisabled={state.mode === 'clear'}
        autoComplete="new-password"
      >
        <Label>{label}</Label>
        <Input
          placeholder={
            state.mode === 'clear'
              ? '已标记清空（取消清空后可重新输入）'
              : (placeholder ??
                (configured ? `已配置（${masked}）· 留空保持不变` : '未配置 · 输入以设置'))
          }
        />
      </TextField>
      <div className="flex flex-wrap items-center gap-2 text-xs">
        <span className="text-muted">
          当前状态：{configured ? `已配置（${masked}）` : '未配置'}
        </span>
        {state.mode === 'replace' ? (
          <Chip size="sm" variant="soft" color="accent">
            保存后替换
          </Chip>
        ) : null}
        {state.mode === 'clear' ? (
          <>
            <Chip size="sm" variant="soft" color="danger">
              保存后清空
            </Chip>
            <Button size="sm" variant="ghost" onPress={() => onChange(SECRET_KEEP)}>
              取消清空
            </Button>
          </>
        ) : null}
        {configured && state.mode !== 'clear' ? (
          <Button
            size="sm"
            variant="ghost"
            onPress={() => onChange({ mode: 'clear', value: '' })}
          >
            清空{label}
          </Button>
        ) : null}
      </div>
      {hint ? <p className="text-xs text-muted">{hint}</p> : null}
    </div>
  )
}

/** 把三态值转成请求体字段（keep 时返回 undefined，不发送该键）。 */
function secretPayload(state: SecretValue): string | undefined {
  if (state.mode === 'keep') {
    return undefined
  }
  return state.mode === 'clear' ? '' : state.value
}

function SectionError({ message }: { message: string }) {
  if (!message) {
    return null
  }
  return (
    <Alert status="danger">
      <Alert.Indicator />
      <Alert.Content>
        <Alert.Description>{message}</Alert.Description>
      </Alert.Content>
    </Alert>
  )
}

const isHTTPURL = (value: string) => /^https?:\/\/\S+$/i.test(value.trim())

// ---------------------------------------------------------------------------
// 易支付（payment.epay）
// ---------------------------------------------------------------------------

function EpaySection() {
  const state = useAsync(fetchEpaySettings, [])
  const [form, setForm] = useState<EpaySettings | null>(null)
  const [key, setKey] = useState<SecretValue>(SECRET_KEEP)
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
      <Card.Header>
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <Card.Title className="text-base">易支付（payment.epay）</Card.Title>
            <Card.Description>
              彩虹标准协议；启用时网关 / PID / 密钥 / 异步通知地址缺一不可。
            </Card.Description>
          </div>
          <Chip size="sm" variant="soft" color={current.enabled ? 'success' : 'default'}>
            {current.enabled ? '已启用' : '未启用'}
          </Chip>
        </div>
      </Card.Header>
      <Card.Content className="space-y-4">
        <SectionError message={error} />

        <ToggleField
          label="启用易支付"
          description="关闭后下单接口不再提供在线支付渠道（余额支付不受影响）"
          value={current.enabled}
          onChange={(value) => update({ enabled: value })}
        />

        <div className="grid gap-4 sm:grid-cols-2">
          <TextField
            name="epay_gateway"
            value={current.gateway}
            onChange={(value) => update({ gateway: value })}
          >
            <Label>网关地址</Label>
            <Input placeholder="https://pay.example.com/" />
          </TextField>

          <TextField name="epay_pid" value={current.pid} onChange={(value) => update({ pid: value })}>
            <Label>商户 PID</Label>
            <Input placeholder="1001" />
          </TextField>
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
          <TextField
            name="epay_notify_url"
            value={current.notify_url}
            onChange={(value) => update({ notify_url: value })}
          >
            <Label>异步通知地址（notify_url）</Label>
            <Input placeholder={current.notify_url_recommended} />
          </TextField>

          <TextField
            name="epay_return_url"
            value={current.return_url}
            onChange={(value) => update({ return_url: value })}
          >
            <Label>同步跳转地址（return_url）</Label>
            <Input placeholder="https://example.com/pay/result" />
          </TextField>
        </div>

        {current.notify_url_recommended ? (
          <div className="flex flex-wrap items-center gap-2 text-xs text-muted">
            <span>推荐回调地址（按当前访问域名推导）：</span>
            <span className="font-mono text-foreground">{current.notify_url_recommended}</span>
            <CopyButton
              value={current.notify_url_recommended}
              label="复制"
              size="sm"
              successMessage="回调地址已复制"
            />
          </div>
        ) : null}

        <div className="flex flex-wrap items-center gap-3">
          <Button variant="primary" isDisabled={pending} onPress={handleSave}>
            {pending ? '保存中…' : '保存易支付设置'}
          </Button>
          <span className="text-xs text-muted">
            最近更新：{formatDateTimeOr(current.updated_at, '未设置')}
            {current.updated_by ? ` · 操作人 #${current.updated_by}` : ''}
          </span>
        </div>
      </Card.Content>
    </Card>
  )
}

// ---------------------------------------------------------------------------
// 上游对接（upstream）
// ---------------------------------------------------------------------------

function UpstreamSection() {
  const state = useAsync(fetchUpstreamSettings, [])
  const [form, setForm] = useState<UpstreamSettings | null>(null)
  const [apiKey, setAPIKey] = useState<SecretValue>(SECRET_KEEP)
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

  const handleProbe = async () => {
    setProbing(true)
    try {
      setProbe(await fetchUpstreamHealth())
    } catch (err) {
      toast.danger(errorMessage(err, '探活失败'))
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
      <Card.Header>
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <Card.Title className="text-base">上游对接（upstream）</Card.Title>
            <Card.Description>
              商品导入、实例开通与所有上游操作都使用这里的参数；不落 config.yaml。
            </Card.Description>
          </div>
          <Button variant="outline" size="sm" isDisabled={probing} onPress={handleProbe}>
            {probing ? '探活中…' : '测试连通性'}
          </Button>
        </div>
      </Card.Header>
      <Card.Content className="space-y-4">
        <SectionError message={error} />

        {probe ? (
          <Alert status={probe.connected ? 'success' : 'warning'}>
            <Alert.Indicator />
            <Alert.Content>
              <Alert.Title>{probe.connected ? '上游连通正常' : '上游未连通'}</Alert.Title>
              <Alert.Description>
                {probe.connected
                  ? `${probe.base_url} · 耗时 ${probe.latency_ms} ms`
                  : (probe.error || '请检查地址、用户名与密钥')}
              </Alert.Description>
            </Alert.Content>
          </Alert>
        ) : null}

        <div className="grid gap-4 sm:grid-cols-2">
          <TextField
            name="upstream_base_url"
            value={current.base_url}
            onChange={(value) => update({ base_url: value })}
          >
            <Label>上游地址（base_url）</Label>
            <Input placeholder="https://idc.example.com/" />
          </TextField>

          <TextField
            name="upstream_username"
            value={current.username}
            onChange={(value) => update({ username: value })}
          >
            <Label>上游用户名</Label>
            <Input placeholder="对接账号" />
          </TextField>
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

        <TextField
          name="upstream_timeout"
          value={timeoutText}
          onChange={setTimeoutText}
          className="w-full sm:w-56"
        >
          <Label>单次请求超时（秒，1-120）</Label>
          <Input placeholder={`当前 ${current.timeout_seconds}（留空保持不变）`} inputMode="numeric" />
        </TextField>

        <div className="flex flex-wrap items-center gap-3">
          <Button variant="primary" isDisabled={pending} onPress={handleSave}>
            {pending ? '保存中…' : '保存上游设置'}
          </Button>
          <span className="text-xs text-muted">
            最近更新：{formatDateTimeOr(current.updated_at, '未设置')}
            {current.updated_by ? ` · 操作人 #${current.updated_by}` : ''}
          </span>
        </div>
      </Card.Content>
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
  const [password, setPassword] = useState<SecretValue>(SECRET_KEEP)
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
      <Card.Header>
        <Card.Title className="text-base">邮件 SMTP（email.smtp）</Card.Title>
        <Card.Description>
          通知邮件与测试邮件的发送通道；未配置时邮件通知静默跳过（站内通知不受影响）。
        </Card.Description>
      </Card.Header>
      <Card.Content className="space-y-4">
        <SectionError message={error} />

        <ToggleField
          label="启用邮件通知"
          description="关闭后不发送任何通知邮件（是否真的发信还取决于下方参数是否齐全）"
          value={current.enabled}
          onChange={(value) => update({ enabled: value })}
        />

        <div className="grid gap-4 sm:grid-cols-2">
          <TextField
            name="smtp_host"
            value={current.host}
            onChange={(value) => update({ host: value })}
          >
            <Label>SMTP 主机（不含端口）</Label>
            <Input placeholder="smtp.example.com" />
          </TextField>

          <FilterSelect
            label="加密方式"
            value={current.encryption}
            options={ENCRYPTION_OPTIONS}
            onChange={(value) => update({ encryption: value })}
          />
        </div>

        <div className="grid gap-4 sm:grid-cols-3">
          <TextField name="smtp_port" value={portText} onChange={setPortText}>
            <Label>端口</Label>
            <Input
              placeholder={`留空取缺省（当前生效 ${current.port_effective}）`}
              inputMode="numeric"
            />
          </TextField>

          <TextField
            name="smtp_username"
            value={current.username}
            onChange={(value) => update({ username: value })}
          >
            <Label>认证用户名（可空 = 不认证）</Label>
            <Input placeholder="noreply@example.com" />
          </TextField>

          <TextField
            name="smtp_from"
            value={current.from}
            onChange={(value) => update({ from: value })}
          >
            <Label>发件人地址</Label>
            <Input placeholder="noreply@example.com" />
          </TextField>
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

        <TextField
          name="smtp_from_name"
          value={current.from_name}
          onChange={(value) => update({ from_name: value })}
          className="w-full sm:w-72"
        >
          <Label>发件人显示名（可空）</Label>
          <Input placeholder="岭云互联" />
        </TextField>

        <div className="flex flex-wrap items-center gap-3">
          <Button variant="primary" isDisabled={pending} onPress={handleSave}>
            {pending ? '保存中…' : '保存 SMTP 设置'}
          </Button>
          <span className="text-xs text-muted">
            最近更新：{formatDateTimeOr(current.updated_at, '未设置')}
            {current.updated_by ? ` · 操作人 #${current.updated_by}` : ''}
          </span>
        </div>

        <div className="space-y-3 border-t border-border pt-4">
          <p className="text-sm font-medium text-foreground">发送测试邮件</p>
          <p className="text-xs text-muted">
            同步发送一封测试邮件验证参数（不受通知总开关限制，同样写入 email_logs）；
            收件地址留空时使用站点管理员邮箱。
          </p>
          <div className="flex flex-wrap items-end gap-3">
            <TextField
              name="test_email_to"
              value={testTo}
              onChange={(value) => {
                setTestTo(value)
                setTestError('')
              }}
              isInvalid={Boolean(testError)}
              className="w-full sm:w-72"
            >
              <Label>收件地址（可空）</Label>
              <Input placeholder="admin@example.com" />
            </TextField>
            <Button variant="outline" isDisabled={testing} onPress={handleTest}>
              {testing ? '正在发送…' : '发送测试邮件'}
            </Button>
          </div>
          {testResult ? (
            <Alert status="success">
              <Alert.Indicator />
              <Alert.Content>
                <Alert.Description>{testResult}</Alert.Description>
              </Alert.Content>
            </Alert>
          ) : null}
          {testError ? (
            <Alert status="danger">
              <Alert.Indicator />
              <Alert.Content>
                <Alert.Description>{testError}</Alert.Description>
              </Alert.Content>
            </Alert>
          ) : null}
        </div>
      </Card.Content>
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
      <Card.Header>
        <Card.Title className="text-base">通知开关（notifications）</Card.Title>
        <Card.Description>
          站内通知、通知邮件与到期提醒的全局开关；缺省全开、提前 7 天提醒。
        </Card.Description>
      </Card.Header>
      <Card.Content className="space-y-4">
        <SectionError message={error} />

        <div className="space-y-3">
          <ToggleField
            label="站内通知"
            description="关闭后不再写入站内通知（会员与管理员收件箱都不产生新通知）"
            value={current.inapp_enabled}
            onChange={(value) => update({ inapp_enabled: value })}
          />
          <ToggleField
            label="邮件通知"
            description="关闭后不发送任何通知邮件（仍需 email.smtp 配置齐全才会真正发信）"
            value={current.email_enabled}
            onChange={(value) => update({ email_enabled: value })}
          />
          <ToggleField
            label="到期提醒"
            description="关闭后到期扫描不产生任何提醒，也不认领去重锚点"
            value={current.expiry_reminder_enabled}
            onChange={(value) => update({ expiry_reminder_enabled: value })}
          />
        </div>

        <TextField
          name="expiry_reminder_days"
          value={daysText}
          onChange={setDaysText}
          className="w-full sm:w-64"
        >
          <Label>提前提醒天数（1-30）</Label>
          <Input
            placeholder={`当前 ${current.expiry_reminder_days} 天（留空保持不变）`}
            inputMode="numeric"
          />
        </TextField>

        <div className="flex flex-wrap items-center gap-3">
          <Button variant="primary" isDisabled={pending} onPress={handleSave}>
            {pending ? '保存中…' : '保存通知开关'}
          </Button>
          <span className="text-xs text-muted">
            最近更新：{formatDateTimeOr(current.updated_at, '未设置')}
            {current.updated_by ? ` · 操作人 #${current.updated_by}` : ''}
          </span>
        </div>
      </Card.Content>
    </Card>
  )
}
