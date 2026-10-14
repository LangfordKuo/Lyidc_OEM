import { useState } from 'react'
import { AlertCircleIcon, AlertTriangleIcon, LoaderCircleIcon, RefreshCwIcon, Trash2Icon } from 'lucide-react'
import { toast } from 'sonner'

import {
  fetchCacheSettings,
  fetchCacheStatus,
  flushCache,
  resetCacheStats,
  updateCacheSettings,
} from '@/api/adminCache'
import { errorMessage } from '@/api/client'
import type { CacheDriver, CacheSettings } from '@/api/types'
import SecretInput from '@/components/admin/SecretInput'
import ChoiceChips from '@/components/common/ChoiceChips'
import ConfirmDialog from '@/components/common/ConfirmDialog'
import { ErrorState, LoadingBlock } from '@/components/common/PageState'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { useAsync } from '@/hooks/useAsync'
import { formatDateTimeOr } from '@/lib/format'
import { SECRET_KEEP, secretPayload, type SecretState } from '@/lib/secret'

/**
 * CachePanel 是设置页的「缓存（cache）」分区（R19 批 1，契约第 24 节）。
 *
 * 只消费 24.3 / 24.4 的四个接口，不新增端点：
 *   - 驱动选择：文件缓存（默认，无需配置）/ Redis（默认关闭，填参数后启用）；
 *   - 运行状态：生效驱动、键数量、命中率与命中 / 未命中、文件目录或 Redis 连接信息（**不含口令**）；
 *   - **启用 Redis 但不可用**时标红并显示可读原因（后端自动降级到文件缓存，业务不中断）；
 *   - 重置命中统计（不动缓存内容）与清空缓存（只清缓存、不碰业务数据，二次确认）。
 *
 * 整页仅 admin 可进（整页闸门已挡 finance / support），因此这里不额外判断角色、也不发越权请求。
 */

/** 驱动选项（默认文件缓存；Redis 默认关闭）。 */
const DRIVER_ITEMS = [
  { value: 'file', label: '文件缓存（默认）' },
  { value: 'redis', label: 'Redis' },
]

/** 表单草稿：以字符串承接输入（保存前统一校验并转成数字）。 */
interface CacheDraft {
  driver: CacheDriver
  host: string
  port: string
  db: string
  keyPrefix: string
  timeout: string
}

function draftOf(settings: CacheSettings): CacheDraft {
  return {
    driver: settings.driver,
    host: settings.redis.host,
    port: String(settings.redis.port),
    db: String(settings.redis.db),
    keyPrefix: settings.redis.key_prefix,
    timeout: String(settings.redis.timeout_seconds),
  }
}

/** 就地校验（与后端 24.3 的口径一致；前端只做快速反馈，最终以后端 40002 为准）。 */
function validateDraft(draft: CacheDraft): string {
  if (draft.driver === 'redis' && !draft.host.trim()) {
    return '启用 Redis 时必须填写主机（host）'
  }
  const port = Number(draft.port)
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    return '端口需为 1-65535 的整数'
  }
  const db = Number(draft.db)
  if (!Number.isInteger(db) || db < 0 || db > 15) {
    return '数据库序号（db）需为 0-15 的整数'
  }
  const timeout = Number(draft.timeout)
  if (!Number.isInteger(timeout) || timeout < 1 || timeout > 10) {
    return '超时（timeout_seconds）需为 1-10 秒的整数'
  }
  if (draft.keyPrefix.trim().length > 64) {
    return '键前缀不能超过 64 个字符'
  }
  return ''
}

export default function CachePanel() {
  const settingsState = useAsync(fetchCacheSettings, [])
  const statusState = useAsync(fetchCacheStatus, [])
  const [draft, setDraft] = useState<CacheDraft | null>(null)
  const [password, setPassword] = useState<SecretState>(SECRET_KEEP)
  const [error, setError] = useState('')
  const [pending, setPending] = useState(false)
  const [resetting, setResetting] = useState(false)
  const [flushOpen, setFlushOpen] = useState(false)
  const [flushing, setFlushing] = useState(false)

  const current = settingsState.data
  const status = statusState.data

  if (settingsState.loading && !current) {
    return <LoadingBlock label="正在读取缓存设置…" />
  }
  if (settingsState.error && !current) {
    return <ErrorState message={settingsState.error} onRetry={settingsState.reload} />
  }
  if (!current) {
    return null
  }

  const form = draft ?? draftOf(current)
  const update = (patch: Partial<CacheDraft>) => {
    setDraft({ ...form, ...patch })
    setError('')
  }

  const handleSave = async () => {
    const message = validateDraft(form)
    if (message) {
      // 就地拒绝：不发保存请求（与后端同一套口径）。
      setError(message)
      return
    }
    setPending(true)
    setError('')
    try {
      const saved = await updateCacheSettings({
        driver: form.driver,
        redis: {
          host: form.host.trim(),
          port: Number(form.port),
          // 口令三态：keep 时该键不发送（省略 = 保持不变）。
          password: secretPayload(password),
          db: Number(form.db),
          key_prefix: form.keyPrefix.trim(),
          timeout_seconds: Number(form.timeout),
        },
      })
      setDraft(null)
      setPassword(SECRET_KEEP)
      settingsState.reload()
      statusState.reload()
      if (saved.degraded) {
        toast.warning(`缓存设置已保存并生效，但 Redis 不可用，已降级到文件缓存：${saved.degraded_reason}`)
      } else {
        toast.success(`缓存设置已保存并生效（驱动：${saved.effective_driver}）`)
      }
    } catch (err) {
      setError(errorMessage(err, '保存失败，请稍后重试'))
    } finally {
      setPending(false)
    }
  }

  const handleResetStats = async () => {
    setResetting(true)
    try {
      await resetCacheStats()
      statusState.reload()
      toast.success('命中统计已重置（缓存内容不受影响）')
    } catch (err) {
      toast.error(errorMessage(err, '重置统计失败，请稍后重试'))
    } finally {
      setResetting(false)
    }
  }

  const handleFlush = async () => {
    setFlushing(true)
    try {
      const result = await flushCache()
      setFlushOpen(false)
      statusState.reload()
      toast.success(`缓存已清空（生效驱动：${result.driver}，当前键数量 ${result.keys}）`)
    } catch (err) {
      // 失败保留弹窗（与备份恢复同口径），原因由后端给出。
      toast.error(errorMessage(err, '清空缓存失败，请稍后重试'))
    } finally {
      setFlushing(false)
    }
  }

  return (
    <Card>
      <CardHeader className="border-b">
        <CardTitle className="text-base">缓存（cache）</CardTitle>
        <CardDescription>
          默认使用文件缓存（系统临时目录，无需配置）；也可启用 Redis。
          保存后立即生效、不用重启；启用 Redis 但连接不可用时会自动降级到文件缓存并在下方标红。
          本页只提供缓存基础设施与排障手段——缓存内容可由「清空缓存」清掉，业务数据一概不动。
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        {error ? (
          <Alert variant="destructive">
            <AlertCircleIcon aria-hidden />
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}

        {/* 降级标红：启用 Redis 但不可用（连接被拒绝 / 认证失败 / 超时 / PING 失败）。 */}
        {current.degraded ? (
          <Alert variant="destructive" data-testid="cache-degraded">
            <AlertTriangleIcon aria-hidden />
            <AlertTitle>Redis 不可用，已降级到文件缓存</AlertTitle>
            <AlertDescription>
              原因：{current.degraded_reason || '连接不可用'}。业务不受影响，缓存读写继续在文件缓存上进行；
              Redis 恢复后，下一次读取设置或刷新状态会自动切回（不必重启）。
            </AlertDescription>
          </Alert>
        ) : null}

        {/* 运行状态（契约 24.4）。 */}
        <div className="space-y-3 rounded-lg border p-4">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-sm font-medium">运行状态</span>
            <Badge variant="secondary">
              配置驱动：{current.driver === 'redis' ? 'Redis' : '文件缓存'}
            </Badge>
            <Badge variant="secondary">
              当前生效：{current.effective_driver === 'redis' ? 'Redis' : '文件缓存'}
            </Badge>
            {current.degraded ? (
              <Badge variant="destructive">已降级</Badge>
            ) : (
              <Badge variant="secondary" className="bg-accent text-primary">
                正常
              </Badge>
            )}
          </div>

          {statusState.error && !status ? (
            <p className="text-sm text-destructive">读取运行状态失败：{statusState.error}</p>
          ) : null}

          {status ? (
            <>
              <p className="text-sm" data-testid="cache-status">
                键数量 {status.keys} · 命中 {status.hits} · 未命中 {status.misses} · 命中率{' '}
                {status.hit_rate}%
              </p>
              <p className="text-xs text-muted-foreground">
                统计自 {formatDateTimeOr(status.stats_since)}（进程内累计，重启归零、可重置）
              </p>
              {status.effective_driver === 'file' ? (
                <p className="text-xs break-all text-muted-foreground">
                  文件缓存目录：{status.file.dir || current.file_dir}
                </p>
              ) : (
                <p className="text-xs break-all text-muted-foreground">
                  Redis 连接：{status.redis.host}:{status.redis.port}（db {status.redis.db}，前缀{' '}
                  {status.redis.key_prefix}，超时 {status.redis.timeout_seconds} 秒
                  {status.redis.version ? `，版本 ${status.redis.version}` : ''}
                  {status.redis.latency_ms > 0 ? `，连接耗时 ${status.redis.latency_ms} ms` : ''}）
                </p>
              )}
            </>
          ) : null}

          <div className="flex flex-wrap items-center gap-2">
            <Button
              size="sm"
              variant="outline"
              disabled={statusState.loading}
              onClick={() => statusState.reload()}
            >
              <RefreshCwIcon data-icon="inline-start" aria-hidden />
              刷新状态
            </Button>
            <Button size="sm" variant="outline" disabled={resetting} onClick={handleResetStats}>
              {resetting ? (
                <LoaderCircleIcon className="animate-spin" data-icon="inline-start" aria-hidden />
              ) : null}
              重置统计
            </Button>
            <Button
              size="sm"
              variant="outline"
              data-testid="cache-flush-open"
              onClick={() => setFlushOpen(true)}
            >
              <Trash2Icon data-icon="inline-start" aria-hidden />
              清空缓存
            </Button>
          </div>
        </div>

        {/* 设置表单（契约 24.3）。 */}
        <div className="space-y-4">
          <ChoiceChips
            items={DRIVER_ITEMS}
            value={form.driver}
            onChange={(value) => update({ driver: value as CacheDriver })}
            ariaLabel="缓存驱动"
            idPrefix="cache-driver"
          />

          <div className="grid gap-4 sm:grid-cols-2">
            <Field>
              <FieldLabel htmlFor="cache_host">Redis 主机（host）</FieldLabel>
              <Input
                id="cache_host"
                name="cache_host"
                placeholder="如 127.0.0.1"
                value={form.host}
                onChange={(event) => update({ host: event.target.value })}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="cache_port">端口（port）</FieldLabel>
              <Input
                id="cache_port"
                name="cache_port"
                inputMode="numeric"
                placeholder="缺省 6379"
                value={form.port}
                onChange={(event) => update({ port: event.target.value })}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="cache_db">数据库序号（db）</FieldLabel>
              <Input
                id="cache_db"
                name="cache_db"
                inputMode="numeric"
                placeholder="缺省 0（0-15）"
                value={form.db}
                onChange={(event) => update({ db: event.target.value })}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="cache_key_prefix">键前缀（key_prefix）</FieldLabel>
              <Input
                id="cache_key_prefix"
                name="cache_key_prefix"
                placeholder="缺省 lyidc:"
                value={form.keyPrefix}
                onChange={(event) => update({ keyPrefix: event.target.value })}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="cache_timeout">超时（timeout_seconds）</FieldLabel>
              <Input
                id="cache_timeout"
                name="cache_timeout"
                inputMode="numeric"
                placeholder="缺省 1 秒（1-10）"
                value={form.timeout}
                onChange={(event) => update({ timeout: event.target.value })}
              />
            </Field>
          </div>

          <SecretInput
            label="Redis 口令（password）"
            name="cache_password"
            configured={current.redis.password_configured}
            masked={current.redis.password_masked}
            state={password}
            onChange={setPassword}
            hint="可空（不认证时留空）。只进库与内存，接口永不回显明文；留空表示保持不变。"
          />

          <div className="flex flex-wrap items-center gap-3">
            <Button disabled={pending} onClick={handleSave}>
              {pending ? (
                <>
                  <LoaderCircleIcon className="animate-spin" data-icon="inline-start" aria-hidden />
                  保存中…
                </>
              ) : (
                '保存缓存设置'
              )}
            </Button>
            <span className="text-xs text-muted-foreground">
              最近更新：{formatDateTimeOr(current.updated_at, '未设置')}
              {current.updated_by ? ` · 操作人 #${current.updated_by}` : ''}
            </span>
          </div>
        </div>

        <ConfirmDialog
          open={flushOpen}
          onOpenChange={setFlushOpen}
          title="清空缓存？"
          description="只清缓存键，不触碰任何业务数据（商品、订单、余额、日志一概不动）。清空后少量请求会回源一次，属预期行为。"
          confirmLabel="确认清空"
          danger
          pending={flushing}
          onConfirm={handleFlush}
        />
      </CardContent>
    </Card>
  )
}
