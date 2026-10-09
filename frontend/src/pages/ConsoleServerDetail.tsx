import { useState } from 'react'
import { AlertTriangleIcon, ChevronRightIcon, InfoIcon } from 'lucide-react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { fetchInstance } from '@/api/instances'
import type { Order } from '@/api/types'
import { paths } from '@/app/paths'
import CopyButton from '@/components/common/CopyButton'
import { InfoList } from '@/components/common/InfoList'
import { EmptyBlock, ErrorState, LoadingBlock } from '@/components/common/PageState'
import SecretValue from '@/components/common/SecretValue'
import StatusBadge from '@/components/common/StatusBadge'
import CancelRequestDialog from '@/components/console/CancelRequestDialog'
import InstanceLogsCard from '@/components/console/InstanceLogsCard'
import PayOrderDialog from '@/components/console/PayOrderDialog'
import PowerActions from '@/components/console/PowerActions'
import ReinstallDialog from '@/components/console/ReinstallDialog'
import RenewDialog from '@/components/console/RenewDialog'
import ResetPasswordDialog from '@/components/console/ResetPasswordDialog'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import { useAsync } from '@/hooks/useAsync'
import { expiryColorClass, expiryState, expiryText } from '@/lib/expiry'
import { formatCycleLabel, formatDateTimeOr } from '@/lib/format'
import { cancelStatusLabel, instanceStatusLabel, instanceStatusTone } from '@/lib/instanceStatus'

type DialogKind = 'reinstall' | 'reset' | 'renew' | 'cancel' | null

// ConsoleServerDetail 是实例详情页（契约 15.2）：信息区 + 操作区（一律弹窗二次确认）+ 操作记录。
export default function ConsoleServerDetail() {
  const params = useParams<{ id: string }>()
  const navigate = useNavigate()
  const id = params.id && /^\d+$/.test(params.id) ? Number(params.id) : null

  const [refreshKey, setRefreshKey] = useState(0)
  const [dialog, setDialog] = useState<DialogKind>(null)
  const [payTarget, setPayTarget] = useState<Order | null>(null)

  const instanceState = useAsync(
    () => {
      if (id === null) {
        return Promise.reject(new Error('实例 ID 必须为正整数'))
      }
      return fetchInstance(id)
    },
    [id, refreshKey],
  )
  const instance = instanceState.data
  const refresh = () => setRefreshKey((value) => value + 1)

  if (id === null) {
    return <EmptyBlock title="实例 ID 不正确" description="请从「我的服务器」列表进入实例详情。" />
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
            <Link className="text-primary hover:underline" to={paths.consoleServers}>
              返回我的服务器
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
  const canPower = instance.status === 'active'
  const canRenew = (instance.status === 'active' || instance.status === 'suspended') && !cancelPending
  const canCancel = canRenew
  const dueState = expiryState(instance.next_due_date)
  const ips = [instance.dedicated_ip, ...instance.assigned_ips].filter(Boolean)

  return (
    <div className="space-y-5">
      <nav className="flex items-center gap-1 text-sm text-muted-foreground" aria-label="面包屑">
        <Link className="hover:text-foreground" to={paths.consoleServers}>
          我的服务器
        </Link>
        <ChevronRightIcon className="size-3.5" aria-hidden />
        <span className="text-foreground">实例详情</span>
      </nav>

      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          {/* 主标题用商品名（本身即含区域，如「香港二区 CN2 A型」）；实例名与各类 ID 降为次要行。 */}
          <h1 className="truncate text-2xl font-semibold tracking-tight text-foreground">
            {instance.product_name}
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">
            {formatCycleLabel(instance.billing_cycle)} · 到期{' '}
            {formatDateTimeOr(instance.next_due_date)}
          </p>
          <p className="mt-1 truncate font-mono text-xs text-muted-foreground">
            {instance.name} · 实例 ID {instance.id} · 上游主机 #{instance.host_id}
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
        <Alert>
          <InfoIcon aria-hidden />
          <AlertTitle>终止申请在途</AlertTitle>
          <AlertDescription>
            已提交{cancelStatusLabel('pending')}
            （{instance.cancel_type === 'end_of_billing' ? '到期终止' : '立即终止'}，提交于{' '}
            {formatDateTimeOr(instance.cancel_requested_at)}）。在途期间不可续费；上游确认删除后实例转为已终止。
          </AlertDescription>
        </Alert>
      ) : null}

      {terminated ? (
        <Alert variant="destructive">
          <AlertTriangleIcon aria-hidden />
          <AlertTitle>实例已终止</AlertTitle>
          <AlertDescription>
            主机已被上游删除，实例为终态只读；如需继续使用请重新下单购买。
          </AlertDescription>
        </Alert>
      ) : null}

      <Card>
        <CardHeader className="border-b">
          <CardTitle className="text-base">实例信息</CardTitle>
          <CardDescription>账号密码仅会员本人可见，请勿泄漏给他人。</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <InfoList
            columns={2}
            items={[
              { label: '状态', value: instanceStatusLabel(instance.status) },
              { label: '上游状态', value: instance.upstream_status || '—' },
              {
                label: '到期时间',
                value: (
                  <span className={expiryColorClass(dueState)}>
                    {formatDateTimeOr(instance.next_due_date)}
                    {instance.next_due_date ? `（${expiryText(instance.next_due_date)}）` : ''}
                  </span>
                ),
              },
              { label: '计费周期', value: formatCycleLabel(instance.billing_cycle) },
              { label: '主机 ID', value: String(instance.host_id) },
              {
                label: '来源订单',
                value: (
                  <Link className="text-primary hover:underline" to={paths.consoleOrders}>
                    #{instance.order_id}
                  </Link>
                ),
              },
              { label: '创建时间', value: formatDateTimeOr(instance.created_at) },
              { label: '更新时间', value: formatDateTimeOr(instance.updated_at) },
              { label: '端口', value: instance.port ? String(instance.port) : '—' },
              { label: 'IP 地址', value: ips.length > 0 ? ips.join('、') : '—' },
            ]}
          />

          <Separator />

          <InfoList
            items={[
              {
                label: '主机账号',
                value: (
                  <span className="inline-flex items-center justify-end gap-1">
                    <span className="font-mono text-sm">{instance.username || '—'}</span>
                    {instance.username ? (
                      <CopyButton value={instance.username} label="复制账号" />
                    ) : null}
                  </span>
                ),
              },
              {
                label: '主机密码',
                value: <SecretValue value={instance.password} />,
              },
            ]}
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="border-b">
          <CardTitle className="text-base">实例操作</CardTitle>
          <CardDescription>
            电源与重装/改密仅「运行中」可用；上游为异步受理，操作结果可在下方操作记录中查看。
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-5">
          <div>
            <p className="mb-2 text-sm font-medium text-foreground">电源操作</p>
            <PowerActions instance={instance} onDone={refresh} />
          </div>

          <Separator />

          <div className="space-y-2">
            <p className="text-sm font-medium text-foreground">系统与密码</p>
            <div className="flex flex-wrap gap-2">
              <Button
                variant="outline"
                size="sm"
                disabled={!canPower}
                onClick={() => setDialog('reinstall')}
              >
                重装系统
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={!canPower}
                onClick={() => setDialog('reset')}
              >
                重置密码
              </Button>
            </div>
          </div>

          <Separator />

          <div className="space-y-2">
            <p className="text-sm font-medium text-foreground">续费与终止</p>
            <div className="flex flex-wrap gap-2">
              <Button size="sm" disabled={!canRenew} onClick={() => setDialog('renew')}>
                续费
              </Button>
              <Button
                size="sm"
                variant="destructive"
                disabled={!canCancel}
                onClick={() => setDialog('cancel')}
              >
                <AlertTriangleIcon aria-hidden />
                申请终止
              </Button>
            </div>
            {cancelPending ? (
              <p className="text-xs text-muted-foreground">
                已有在途终止申请，续费与再次申请已禁用。
              </p>
            ) : null}
            {terminated ? (
              <p className="text-xs text-muted-foreground">已终止的实例不可续费。</p>
            ) : null}
          </div>
        </CardContent>
      </Card>

      <InstanceLogsCard key={refreshKey} instanceId={instance.id} />

      {/* 弹窗按需挂载：打开即初始化，关闭即卸载，避免跨次打开沿用上一次的输入。 */}
      {dialog === 'reinstall' ? (
        <ReinstallDialog
          instance={instance}
          onOpenChange={(open) => (!open ? setDialog(null) : undefined)}
          onDone={refresh}
        />
      ) : null}
      {dialog === 'reset' ? (
        <ResetPasswordDialog
          instance={instance}
          onOpenChange={(open) => (!open ? setDialog(null) : undefined)}
          onDone={refresh}
        />
      ) : null}
      {dialog === 'renew' ? (
        <RenewDialog
          instance={instance}
          onOpenChange={(open) => (!open ? setDialog(null) : undefined)}
          onOrderCreated={(order) => setPayTarget(order)}
        />
      ) : null}
      {dialog === 'cancel' ? (
        <CancelRequestDialog
          instance={instance}
          onOpenChange={(open) => (!open ? setDialog(null) : undefined)}
          onDone={refresh}
        />
      ) : null}
      <PayOrderDialog
        order={payTarget}
        onOpenChange={(open) => (!open ? setPayTarget(null) : undefined)}
        onPaid={() => {
          setPayTarget(null)
          refresh()
          navigate(paths.consoleOrders)
        }}
      />
    </div>
  )
}
