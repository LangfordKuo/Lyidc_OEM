import { Alert, Button, Card, Separator } from '@heroui/react'
import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { fetchInstance } from '../../api/instances'
import type { Order } from '../../api/types'
import { paths } from '../../app/paths'
import StatusBadge from '../../components/StatusBadge'
import CopyButton from '../../components/common/CopyButton'
import { InfoList } from '../../components/common/InfoList'
import { EmptyBlock, ErrorState, LoadingBlock } from '../../components/common/PageState'
import SecretValue from '../../components/common/SecretValue'
import CancelRequestDialog from '../../components/instance/CancelRequestDialog'
import InstanceLogsCard from '../../components/instance/InstanceLogsCard'
import PowerActions from '../../components/instance/PowerActions'
import ReinstallDialog from '../../components/instance/ReinstallDialog'
import RenewDialog from '../../components/instance/RenewDialog'
import ResetPasswordDialog from '../../components/instance/ResetPasswordDialog'
import PayOrderDialog from '../../components/order/PayOrderDialog'
import { useAsync } from '../../hooks/useAsync'
import { expiryColorClass, expiryState, expiryText } from '../../lib/expiry'
import { formatCycleLabel, formatDateTimeOr } from '../../lib/format'
import { cancelStatusLabel, instanceStatusLabel, instanceStatusTone } from '../../lib/instanceStatus'

type DialogKind = 'reinstall' | 'reset' | 'renew' | 'cancel' | null

// ConsoleServerDetail 是实例详情页（契约 15.2）：信息区 + 操作区（全部走弹窗二次确认）+ 操作记录。
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
    const missing = instanceState.error.includes('实例不存在')
    if (missing) {
      return (
        <EmptyBlock
          title="实例不存在"
          description={
            <Link className="text-accent hover:underline" to={paths.consoleServers}>
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
  const canCancel =
    (instance.status === 'active' || instance.status === 'suspended') && !cancelPending
  const dueState = expiryState(instance.next_due_date)
  const ips = [instance.dedicated_ip, ...instance.assigned_ips].filter(Boolean)

  return (
    <div className="space-y-5">
      <nav className="text-sm text-muted" aria-label="面包屑">
        <Link className="hover:text-foreground" to={paths.consoleServers}>
          我的服务器
        </Link>
        <span className="mx-2">/</span>
        <span className="text-foreground">实例详情</span>
      </nav>

      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="truncate font-mono text-lg font-semibold text-foreground">
            {instance.name}
          </h1>
          <p className="mt-1 text-sm text-muted">
            {instance.product_name} · {formatCycleLabel(instance.billing_cycle)} · 实例 ID{' '}
            {instance.id}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <StatusBadge
            tone={instanceStatusTone(instance.status)}
            label={instanceStatusLabel(instance.status)}
          />
          {cancelPending ? <StatusBadge tone="warn" label={cancelStatusLabel('pending')} /> : null}
          <Button variant="outline" size="sm" onPress={refresh}>
            刷新
          </Button>
        </div>
      </header>

      {cancelPending ? (
        <Alert status="warning">
          <Alert.Indicator />
          <Alert.Content>
            <Alert.Title>终止申请在途</Alert.Title>
            <Alert.Description>
              已提交{cancelStatusLabel('pending')}
              （{instance.cancel_type === 'end_of_billing' ? '到期终止' : '立即终止'}，提交于{' '}
              {formatDateTimeOr(instance.cancel_requested_at)}）。在途期间不可续费；上游确认删除后实例转为已终止。
            </Alert.Description>
          </Alert.Content>
        </Alert>
      ) : null}

      {terminated ? (
        <Alert status="danger">
          <Alert.Indicator />
          <Alert.Content>
            <Alert.Title>实例已终止</Alert.Title>
            <Alert.Description>
              主机已被上游删除，实例为终态只读；如需继续使用请重新下单购买。
            </Alert.Description>
          </Alert.Content>
        </Alert>
      ) : null}

      <Card>
        <Card.Header>
          <Card.Title className="text-base">实例信息</Card.Title>
          <Card.Description>账号密码仅会员本人可见，请勿泄漏给他人。</Card.Description>
        </Card.Header>
        <Card.Content className="space-y-4">
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
                  <Link className="text-accent hover:underline" to={paths.consoleOrders}>
                    #{instance.order_id}
                  </Link>
                ),
              },
              { label: '创建时间', value: formatDateTimeOr(instance.created_at) },
              { label: '更新时间', value: formatDateTimeOr(instance.updated_at) },
              { label: '端口', value: instance.port ? String(instance.port) : '—' },
              {
                label: 'IP 地址',
                value: ips.length > 0 ? ips.join('、') : '—',
              },
            ]}
          />

          <Separator />

          <InfoList
            items={[
              {
                label: '主机账号',
                value: (
                  <span className="flex flex-wrap items-center justify-end gap-2">
                    <span className="font-mono text-sm">{instance.username || '—'}</span>
                    {instance.username ? <CopyButton value={instance.username} label="复制账号" /> : null}
                  </span>
                ),
              },
              {
                label: '主机密码',
                value: <SecretValue value={instance.password} />,
              },
            ]}
          />
        </Card.Content>
      </Card>

      <Card>
        <Card.Header>
          <Card.Title className="text-base">实例操作</Card.Title>
          <Card.Description>
            电源与重装/改密仅「运行中」可用；上游为异步受理，操作结果可在下方操作记录中查看。
          </Card.Description>
        </Card.Header>
        <Card.Content className="space-y-5">
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
                isDisabled={!canPower}
                onPress={() => setDialog('reinstall')}
              >
                重装系统
              </Button>
              <Button
                variant="outline"
                size="sm"
                isDisabled={!canPower}
                onPress={() => setDialog('reset')}
              >
                重置密码
              </Button>
            </div>
          </div>

          <Separator />

          <div className="space-y-2">
            <p className="text-sm font-medium text-foreground">续费与终止</p>
            <div className="flex flex-wrap gap-2">
              <Button
                variant="primary"
                size="sm"
                isDisabled={!canRenew}
                onPress={() => setDialog('renew')}
              >
                续费
              </Button>
              <Button
                variant="danger-soft"
                size="sm"
                isDisabled={!canCancel}
                onPress={() => setDialog('cancel')}
              >
                申请终止
              </Button>
            </div>
            {cancelPending ? (
              <p className="text-xs text-muted">已有在途终止申请，续费与再次申请已禁用。</p>
            ) : null}
            {!canRenew && !cancelPending && instance.status === 'terminated' ? (
              <p className="text-xs text-muted">已终止的实例不可续费。</p>
            ) : null}
          </div>
        </Card.Content>
      </Card>

      <InstanceLogsCard key={refreshKey} instanceId={instance.id} />

      {/* 弹窗按需挂载：打开即初始化，关闭即卸载，避免跨次打开沿用上一次的输入。 */}
      {dialog === 'reinstall' ? (
        <ReinstallDialog instance={instance} onClose={() => setDialog(null)} onDone={refresh} />
      ) : null}
      {dialog === 'reset' ? (
        <ResetPasswordDialog instance={instance} onClose={() => setDialog(null)} onDone={refresh} />
      ) : null}
      {dialog === 'renew' ? (
        <RenewDialog
          instance={instance}
          onClose={() => setDialog(null)}
          onOrderCreated={(order) => setPayTarget(order)}
        />
      ) : null}
      {dialog === 'cancel' ? (
        <CancelRequestDialog instance={instance} onClose={() => setDialog(null)} onDone={refresh} />
      ) : null}
      {payTarget ? (
        <PayOrderDialog
          order={payTarget}
          onClose={() => setPayTarget(null)}
          onPaid={() => {
            refresh()
            navigate(paths.consoleOrders)
          }}
        />
      ) : null}
    </div>
  )
}
