import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { AlertTriangleIcon, ChevronRightIcon } from 'lucide-react'
import { toast } from 'sonner'

import { errorMessage } from '@/api/client'
import {
  cancelAdminInstance,
  fetchAdminInstance,
  listAdminInstanceLogs,
  suspendAdminInstance,
  syncAdminInstance,
  unsuspendAdminInstance,
} from '@/api/adminInstances'
import type { CancelType, InstanceLog } from '@/api/types'
import { paths } from '@/app/paths'
import { useAdminAuth } from '@/auth/adminAuthContext'
import AdminTable, { type AdminColumn } from '@/components/admin/AdminTable'
import ConfirmDialog from '@/components/common/ConfirmDialog'
import { InfoList } from '@/components/common/InfoList'
import { EmptyBlock, ErrorState, LoadingBlock } from '@/components/common/PageState'
import Pager from '@/components/common/Pager'
import SecretValue from '@/components/common/SecretValue'
import StatusBadge from '@/components/common/StatusBadge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Textarea } from '@/components/ui/textarea'
import { useAsync } from '@/hooks/useAsync'
import { permissionHint } from '@/lib/adminRoles'
import { expiryColorClass, expiryState, expiryText } from '@/lib/expiry'
import { formatCycleLabel, formatDateTimeOr } from '@/lib/format'
import {
  ACTOR_TYPE_LABELS,
  cancelStatusLabel,
  instanceActionLabel,
  instanceStatusLabel,
  instanceStatusTone,
} from '@/lib/instanceStatus'
import { ADMIN_PAGE_SIZE } from '@/lib/pagination'
import { validateCancelReason } from '@/lib/validate'

type DialogKind = 'suspend' | 'unsuspend' | 'cancel' | null

/**
 * AdminInstanceDetail 是管理后台实例详情页（契约 14.4 / 15.3）：
 * 字段与会员端详情一致（含主机账号密码，默认遮蔽），额外展示归属会员；
 * 操作按角色矩阵控制：同步所有角色可用；暂停/恢复/终止申请仅 admin（按钮禁用 + 原因提示）。
 */
export default function AdminInstanceDetail() {
  const params = useParams<{ id: string }>()
  const id = params.id && /^\d+$/.test(params.id) ? Number(params.id) : null
  const { role } = useAdminAuth()
  const isAdmin = role === 'admin'

  const [refreshKey, setRefreshKey] = useState(0)
  const [dialog, setDialog] = useState<DialogKind>(null)
  const [logsPage, setLogsPage] = useState(1)
  const [syncing, setSyncing] = useState(false)
  const [syncMessage, setSyncMessage] = useState('')

  const instanceState = useAsync(
    () => {
      if (id === null) {
        return Promise.reject(new Error('实例 ID 必须为正整数'))
      }
      return fetchAdminInstance(id)
    },
    [id, refreshKey],
  )
  const logsState = useAsync(
    () =>
      id === null
        ? Promise.resolve(null)
        : listAdminInstanceLogs(id, { page: logsPage, page_size: ADMIN_PAGE_SIZE }),
    [id, logsPage, refreshKey],
  )
  const instance = instanceState.data
  const refresh = () => setRefreshKey((value) => value + 1)

  if (id === null) {
    return <EmptyBlock title="实例 ID 不正确" description="请从实例列表进入详情。" />
  }

  if (instanceState.loading) {
    return <LoadingBlock label="正在读取实例信息…" />
  }

  if (instanceState.error) {
    if (instanceState.error.includes('实例不存在')) {
      return (
        <EmptyBlock
          title="实例不存在"
          description={
            <Link className="text-primary hover:underline" to={paths.adminInstances}>
              返回实例列表
            </Link>
          }
        />
      )
    }
    return <ErrorState message={instanceState.error} onRetry={instanceState.reload} />
  }

  if (!instance) {
    return null
  }

  const cancelPending = instance.cancel_status === 'pending'
  const terminated = instance.status === 'terminated'
  // 状态约束（契约 15.1）：暂停仅 active；恢复仅 suspended；终止申请 active / suspended。
  const canSuspend = isAdmin && instance.status === 'active'
  const canUnsuspend = isAdmin && instance.status === 'suspended'
  const canCancel =
    isAdmin && (instance.status === 'active' || instance.status === 'suspended') && !cancelPending

  const handleSync = async () => {
    setSyncing(true)
    setSyncMessage('')
    try {
      const result = await syncAdminInstance(instance.id)
      setSyncMessage(
        result.terminated
          ? '上游主机已不存在，本地实例已收敛为「已终止」。'
          : `${result.message}${result.status_changed ? '（本地状态已随上游收敛）' : ''}`,
      )
      toast.success('同步完成')
      refresh()
    } catch (err) {
      toast.error(errorMessage(err, '同步失败，请稍后重试'))
    } finally {
      setSyncing(false)
    }
  }

  const logColumns: AdminColumn<InstanceLog>[] = [
    {
      key: 'time',
      header: '时间',
      cell: (log) => <span className="text-xs">{formatDateTimeOr(log.created_at)}</span>,
    },
    {
      key: 'action',
      header: '操作',
      cell: (log) => <span className="text-sm">{instanceActionLabel(log.action)}</span>,
    },
    {
      key: 'actor',
      header: '来源',
      cell: (log) => (
        <span className="text-xs text-muted-foreground">
          {ACTOR_TYPE_LABELS[log.actor_type] ?? log.actor_type}
          {log.actor_id ? ` #${log.actor_id}` : ''}
        </span>
      ),
    },
    {
      key: 'status',
      header: '结果',
      cell: (log) => (
        <StatusBadge
          tone={log.status === 'success' ? 'ok' : 'error'}
          label={log.status === 'success' ? '成功' : '失败'}
        />
      ),
    },
    {
      key: 'message',
      header: '说明',
      cell: (log) => <span className="text-xs text-muted-foreground">{log.message || '—'}</span>,
    },
  ]

  const dueState = expiryState(instance.next_due_date)

  return (
    <div className="space-y-5">
      <nav className="flex items-center gap-1 text-sm text-muted-foreground" aria-label="面包屑">
        <Link className="hover:text-foreground" to={paths.adminInstances}>
          实例
        </Link>
        <ChevronRightIcon className="size-3.5" aria-hidden />
        <span className="text-foreground">实例详情</span>
      </nav>

      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="truncate font-mono text-lg font-semibold text-foreground">
            {instance.name}
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">
            {instance.product_name} · {formatCycleLabel(instance.billing_cycle)} · 实例 ID{' '}
            {instance.id} · 会员 <span className="text-foreground">#{instance.member_id}</span>
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <StatusBadge
            tone={instanceStatusTone(instance.status)}
            label={instanceStatusLabel(instance.status)}
          />
          {cancelPending ? <StatusBadge tone="warn" label={cancelStatusLabel('pending')} /> : null}
          <Button variant="outline" size="sm" onClick={refresh}>
            刷新
          </Button>
        </div>
      </header>

      {cancelPending ? (
        <Alert className="border-warning/40 bg-warning/5">
          <AlertTriangleIcon className="text-warning" aria-hidden />
          <AlertTitle>终止申请在途</AlertTitle>
          <AlertDescription>
            {instance.cancel_type === 'end_of_billing' ? '到期终止' : '立即终止'}申请已提交
            {instance.cancel_requested_at ? `（${formatDateTimeOr(instance.cancel_requested_at)}）` : ''}
            。在途期间不可续费；上游确认删除后实例转为已终止（可用「同步状态」触发收敛）。
          </AlertDescription>
        </Alert>
      ) : null}

      {terminated ? (
        <Alert variant="destructive">
          <AlertTriangleIcon aria-hidden />
          <AlertTitle>实例已终止</AlertTitle>
          <AlertDescription>
            实例为终态只读，如需继续使用请让会员重新下单。
          </AlertDescription>
        </Alert>
      ) : null}

      <Card>
        <CardHeader className="border-b">
          <CardTitle className="text-base">实例信息</CardTitle>
          <CardDescription>
            管理端详情与会员端详情字段一致（含主机凭据），敏感字段仅在管理后台内部可见。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <InfoList
            columns={2}
            items={[
              { label: '实例 ID', value: instance.id },
              { label: '归属会员', value: `#${instance.member_id}` },
              { label: '订单 ID', value: instance.order_id },
              { label: '上游主机 ID', value: instance.host_id },
              { label: '商品', value: instance.product_name || `#${instance.product_id}` },
              { label: '计费周期', value: formatCycleLabel(instance.billing_cycle) },
              {
                label: '到期时间',
                value: (
                  <span className={expiryColorClass(dueState)}>
                    {formatDateTimeOr(instance.next_due_date)}
                    {instance.next_due_date ? ` · ${expiryText(instance.next_due_date)}` : ''}
                  </span>
                ),
              },
              { label: '上游状态', value: instance.upstream_status || '—' },
              { label: '创建时间', value: formatDateTimeOr(instance.created_at) },
              { label: '更新时间', value: formatDateTimeOr(instance.updated_at) },
            ]}
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="border-b">
          <CardTitle className="text-base">主机凭据</CardTitle>
          <CardDescription>仅管理后台可见；请勿截图外传。</CardDescription>
        </CardHeader>
        <CardContent>
          <InfoList
            items={[
              {
                label: '主 IP',
                value: <span className="font-mono">{instance.dedicated_ip || '—'}</span>,
              },
              {
                label: '附加 IP',
                value: (
                  <span className="font-mono">
                    {instance.assigned_ips.length > 0 ? instance.assigned_ips.join('、') : '—'}
                  </span>
                ),
              },
              { label: '端口', value: instance.port || '—' },
              {
                label: '账号',
                value: <span className="font-mono">{instance.username || '—'}</span>,
              },
              { label: '密码', value: <SecretValue value={instance.password} /> },
            ]}
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="border-b">
          <CardTitle className="text-base">实例操作</CardTitle>
          <CardDescription>
            角色矩阵（契约 15.3）：同步状态所有角色可用；暂停/恢复/终止申请仅超级管理员可用。
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          {syncMessage ? (
            <Alert>
              <AlertDescription>{syncMessage}</AlertDescription>
            </Alert>
          ) : null}

          <div className="flex flex-wrap items-center gap-2">
            <Button variant="outline" disabled={syncing} onClick={handleSync}>
              {syncing ? '正在同步…' : '同步状态'}
            </Button>

            <Button
              variant="outline"
              disabled={!canSuspend}
              title={canSuspend ? undefined : '仅「运行中」的实例可暂停'}
              onClick={() => setDialog('suspend')}
            >
              暂停实例
            </Button>

            <Button
              variant="outline"
              disabled={!canUnsuspend}
              title={canUnsuspend ? undefined : '仅「已暂停」的实例可恢复'}
              onClick={() => setDialog('unsuspend')}
            >
              恢复实例
            </Button>

            <Button
              variant="destructive"
              disabled={!canCancel}
              title={canCancel ? undefined : '仅 admin 可对运行中/已暂停且无在途申请的实例提交终止'}
              onClick={() => setDialog('cancel')}
            >
              提交终止申请
            </Button>
          </div>

          {!isAdmin ? (
            <p className="text-xs text-muted-foreground">
              当前角色为 {role ?? '未知'}：暂停 / 恢复 / 终止申请
              {permissionHint('instances.suspend')}；同步状态仍可用。
            </p>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="border-b">
          <CardTitle className="text-base">操作记录</CardTitle>
          <CardDescription>含失败尝试与系统自动操作（message 已脱敏）。</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          {logsState.loading ? <LoadingBlock label="正在读取操作记录…" /> : null}
          {logsState.error ? <ErrorState message={logsState.error} onRetry={logsState.reload} /> : null}
          {!logsState.loading && !logsState.error ? (
            <>
              <AdminTable
                ariaLabel="实例操作记录"
                columns={logColumns}
                rows={logsState.data?.items ?? []}
                rowKey={(log) => log.id}
                empty={<EmptyBlock title="暂无操作记录" />}
              />
              {logsState.data && logsState.data.total > 0 ? (
                <Pager
                  page={logsPage}
                  total={logsState.data.total}
                  pageSize={logsState.data.page_size || ADMIN_PAGE_SIZE}
                  onChange={setLogsPage}
                />
              ) : null}
            </>
          ) : null}
        </CardContent>
      </Card>

      {dialog === 'suspend' ? (
        <SuspendDialog
          instanceId={instance.id}
          onClose={() => setDialog(null)}
          onDone={() => {
            setDialog(null)
            refresh()
          }}
        />
      ) : null}

      {dialog === 'unsuspend' ? (
        <UnsuspendDialog
          instanceId={instance.id}
          onClose={() => setDialog(null)}
          onDone={() => {
            setDialog(null)
            refresh()
          }}
        />
      ) : null}

      {dialog === 'cancel' ? (
        <CancelDialog
          instanceId={instance.id}
          onClose={() => setDialog(null)}
          onDone={() => {
            setDialog(null)
            refresh()
          }}
        />
      ) : null}
    </div>
  )
}

/** 暂停实例：reason 必填（≤200 字符，同原因提交上游并写入审计）。 */
function SuspendDialog({
  instanceId,
  onClose,
  onDone,
}: {
  instanceId: number
  onClose: () => void
  onDone: () => void
}) {
  const [reason, setReason] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  const handleConfirm = async () => {
    const trimmed = reason.trim()
    if (!trimmed) {
      setError('请填写暂停原因（会同步提交给上游）')
      return
    }
    if ([...trimmed].length > 200) {
      setError('原因最多 200 个字符')
      return
    }
    setPending(true)
    setError('')
    try {
      const result = await suspendAdminInstance(instanceId, trimmed)
      toast.success(result.message || '实例已暂停')
      onDone()
    } catch (err) {
      setError(errorMessage(err, '暂停失败，请稍后重试'))
    } finally {
      setPending(false)
    }
  }

  return (
    <ConfirmDialog
      open
      onOpenChange={(open) => (!open && !pending ? onClose() : undefined)}
      title="确认暂停实例？"
      description="暂停后会员的服务将不可用（上游同步执行），恢复前不会自动开机。"
      confirmLabel="暂停实例"
      danger
      pending={pending}
      onConfirm={handleConfirm}
    >
      <div className="space-y-2">
        <Label htmlFor="suspend_reason">暂停原因（必填，将提交上游并写入审计）</Label>
        <Textarea
          id="suspend_reason"
          name="suspend_reason"
          rows={3}
          placeholder="例如：涉嫌滥用资源 / 欠费催缴 / 违规内容"
          value={reason}
          aria-invalid={error ? true : undefined}
          onChange={(event) => {
            setReason(event.target.value)
            setError('')
          }}
        />
        {error ? <p className="text-xs text-destructive">{error}</p> : null}
      </div>
    </ConfirmDialog>
  )
}

/** 恢复实例：无请求体。 */
function UnsuspendDialog({
  instanceId,
  onClose,
  onDone,
}: {
  instanceId: number
  onClose: () => void
  onDone: () => void
}) {
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  const handleConfirm = async () => {
    setPending(true)
    setError('')
    try {
      const result = await unsuspendAdminInstance(instanceId)
      toast.success(result.message || '实例已恢复')
      onDone()
    } catch (err) {
      setError(errorMessage(err, '恢复失败，请稍后重试'))
    } finally {
      setPending(false)
    }
  }

  return (
    <ConfirmDialog
      open
      onOpenChange={(open) => (!open && !pending ? onClose() : undefined)}
      title="确认恢复实例？"
      description="将向上游提交恢复请求，会员服务随之恢复。"
      confirmLabel="恢复实例"
      pending={pending}
      onConfirm={handleConfirm}
    >
      {error ? <p className="text-xs text-destructive">{error}</p> : null}
    </ConfirmDialog>
  )
}

/** 终止申请：type 必选（immediate / end_of_billing）+ reason 必填。 */
function CancelDialog({
  instanceId,
  onClose,
  onDone,
}: {
  instanceId: number
  onClose: () => void
  onDone: () => void
}) {
  const [type, setType] = useState<CancelType>('immediate')
  const [reason, setReason] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  const handleConfirm = async () => {
    const trimmed = reason.trim()
    if (!trimmed) {
      setError('管理端终止必须填写原因（会写入审计记录）')
      return
    }
    const reasonError = validateCancelReason(trimmed)
    if (reasonError) {
      setError(reasonError)
      return
    }
    setPending(true)
    setError('')
    try {
      const result = await cancelAdminInstance(instanceId, { type, reason: trimmed })
      toast.success(
        result.duplicate
          ? '该实例已有在途终止申请，本次未重复提交上游'
          : '终止申请已提交，上游确认后实例转为已终止（可用「同步状态」触发收敛）',
      )
      onDone()
    } catch (err) {
      setError(errorMessage(err, '提交终止申请失败，请稍后重试'))
    } finally {
      setPending(false)
    }
  }

  return (
    <ConfirmDialog
      open
      onOpenChange={(open) => (!open && !pending ? onClose() : undefined)}
      title="确认提交终止申请？"
      description="终止不可逆：上游删除主机后数据不可恢复。到期终止会在到期后由上游执行。"
      confirmLabel="提交终止申请"
      danger
      pending={pending}
      onConfirm={handleConfirm}
    >
      <div className="space-y-3">
        <div>
          <p className="mb-2 text-sm font-medium text-foreground">终止方式</p>
          <RadioGroup
            aria-label="终止方式"
            value={type}
            onValueChange={(value) => setType(value as CancelType)}
            className="grid-flow-col justify-start gap-4"
          >
            <div className="flex items-center gap-2">
              <RadioGroupItem value="immediate" id="cancel-immediate" />
              <Label htmlFor="cancel-immediate" className="text-sm font-normal">
                立即终止
              </Label>
            </div>
            <div className="flex items-center gap-2">
              <RadioGroupItem value="end_of_billing" id="cancel-end-of-billing" />
              <Label htmlFor="cancel-end-of-billing" className="text-sm font-normal">
                到期终止
              </Label>
            </div>
          </RadioGroup>
        </div>

        <div className="space-y-2">
          <Label htmlFor="cancel_reason">终止原因（必填，≤200 字符）</Label>
          <Textarea
            id="cancel_reason"
            name="cancel_reason"
            rows={3}
            placeholder="例如：会员申请退款终止 / 违规处置"
            value={reason}
            aria-invalid={error ? true : undefined}
            onChange={(event) => {
              setReason(event.target.value)
              setError('')
            }}
          />
        </div>

        {error ? <p className="text-xs text-destructive">{error}</p> : null}
      </div>
    </ConfirmDialog>
  )
}
