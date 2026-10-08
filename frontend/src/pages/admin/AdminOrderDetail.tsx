import { Alert, Button, Card, toast } from '@heroui/react'
import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'

import { errorMessage } from '../../api/client'
import { fetchAdminOrder, retryOrderDelivery } from '../../api/adminOrders'
import { paths } from '../../app/paths'
import { useAdminAuth } from '../../auth/adminAuthContext'
import StatusBadge from '../../components/StatusBadge'
import ConfirmDialog from '../../components/common/ConfirmDialog'
import { InfoList, Timeline, type TimelineItem } from '../../components/common/InfoList'
import { EmptyBlock, ErrorState, LoadingBlock } from '../../components/common/PageState'
import { useAsync } from '../../hooks/useAsync'
import { hasPermission, permissionHint } from '../../lib/adminRoles'
import { formatCycleLabel, formatDateTime, formatDateTimeOr, formatMoney } from '../../lib/format'
import { ORDER_STATUS_DESCRIPTIONS, ORDER_STATUS_TONES, orderStatusLabel } from '../../lib/orderStatus'

// AdminOrderDetail 是管理后台订单详情页（阶段 8b 新增接口 GET /admin/orders/:id）：
// 订单全字段 + 会员概要 + 交付信息（host_id / provision_error / delivered_at）+ 订单时间线。
// 重试交付（会真实调用上游开通）按契约 14.4 仅 admin 角色可执行，且需二次确认。
export default function AdminOrderDetail() {
  const params = useParams<{ id: string }>()
  const id = params.id && /^\d+$/.test(params.id) ? Number(params.id) : null
  const { role } = useAdminAuth()
  const canRetryRole = hasPermission(role, 'orders.retry')

  const [refreshKey, setRefreshKey] = useState(0)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [retrying, setRetrying] = useState(false)

  const orderState = useAsync(
    () => {
      if (id === null) {
        return Promise.reject(new Error('订单 ID 必须为正整数'))
      }
      return fetchAdminOrder(id)
    },
    [id, refreshKey],
  )
  const order = orderState.data
  const refresh = () => setRefreshKey((value) => value + 1)

  if (id === null) {
    return <EmptyBlock title="订单 ID 不正确" description="请从订单列表进入详情。" />
  }

  if (orderState.loading) {
    return <LoadingBlock label="正在读取订单信息…" />
  }

  if (orderState.error) {
    if (orderState.error.includes('订单不存在')) {
      return (
        <EmptyBlock
          title="订单不存在"
          description={
            <Link className="text-accent hover:underline" to={paths.adminOrders}>
              返回订单列表
            </Link>
          }
        />
      )
    }
    return <ErrorState message={orderState.error} onRetry={orderState.reload} />
  }

  if (!order) {
    return null
  }

  // 可重试交付的状态（契约 14.4）：paid（未触发过交付）与 failed（交付失败）。
  const retryable = order.status === 'paid' || order.status === 'failed'
  const configEntries = Object.entries(order.config ?? {})

  const handleRetry = async () => {
    if (id === null) {
      return
    }
    setRetrying(true)
    try {
      const result = await retryOrderDelivery(id)
      if (result.status === 'active') {
        toast.success('交付完成，订单已开通')
      } else if (result.status === 'failed') {
        toast.danger(`交付仍未成功：${result.provision_error || '上游返回失败'}`)
      } else {
        toast.info(`订单当前状态：${orderStatusLabel(result.status)}`)
      }
      setConfirmOpen(false)
      refresh()
    } catch (err) {
      // 40002（未支付 / 正在交付中 / 已交付 / 已取消）、404（订单不存在）等由后端给出中文 message。
      toast.danger(errorMessage(err, '重试交付失败，请稍后重试'))
      setConfirmOpen(false)
      refresh()
    } finally {
      setRetrying(false)
    }
  }

  const timeline: TimelineItem[] = [
    {
      key: 'created',
      title: '创建订单',
      time: formatDateTime(order.created_at),
      description: `${order.type === 'renew' ? '续费' : '新购'} · ${order.product_name || `商品 #${order.product_id}`}`,
    },
  ]
  if (order.status === 'cancelled') {
    timeline.push({
      key: 'cancelled',
      title: '订单已取消',
      time: formatDateTime(order.updated_at),
      description: '未支付订单可由会员本人取消（取消不占用优惠码次数）。',
    })
  } else if (order.pay_time) {
    timeline.push({
      key: 'paid',
      title: `支付到账（${order.pay_channel === 'balance' ? '余额支付' : '在线支付'}）`,
      time: formatDateTimeOr(order.pay_time),
      tone: 'success',
      description: order.channel_trade_no ? `渠道单号 ${order.channel_trade_no}` : undefined,
    })
  } else {
    timeline.push({ key: 'unpaid', title: '等待支付', description: ORDER_STATUS_DESCRIPTIONS.pending })
  }
  if (order.delivered_at) {
    timeline.push({
      key: 'delivered',
      title: order.type === 'renew' ? '续费完成' : '交付完成',
      time: formatDateTime(order.delivered_at),
      tone: 'success',
      description: order.host_id ? `上游主机 ID ${order.host_id}` : undefined,
    })
  } else if (order.status === 'failed') {
    timeline.push({
      key: 'failed',
      title: '交付失败',
      time: formatDateTime(order.updated_at),
      tone: 'danger',
      description: order.provision_error || '上游开通失败（原因未返回）',
    })
  } else if (order.status === 'provisioning') {
    timeline.push({ key: 'provisioning', title: '交付中', description: '上游正在开通，稍后刷新查看结果。' })
  }

  return (
    <div className="space-y-5">
      <nav className="text-sm text-muted" aria-label="面包屑">
        <Link className="hover:text-foreground" to={paths.adminOrders}>
          订单
        </Link>
        <span className="mx-2">/</span>
        <span className="text-foreground">订单详情</span>
      </nav>

      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="truncate font-mono text-lg font-semibold text-foreground">{order.trade_no}</h1>
          <p className="mt-1 text-sm text-muted">
            {order.product_name || `商品 #${order.product_id}`} · {formatCycleLabel(order.cycle)} · 订单 ID{' '}
            {order.id} · 会员{' '}
            {order.member ? (
              <span className="text-foreground">
                {order.member.username}
                <span className="ml-1 text-muted">#{order.member.id}</span>
              </span>
            ) : (
              <span className="text-muted">#{order.member_id}（资料缺失）</span>
            )}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <StatusBadge
            tone={ORDER_STATUS_TONES[order.status] ?? 'pending'}
            label={orderStatusLabel(order.status)}
          />
          <Button variant="outline" size="sm" onPress={refresh}>
            刷新
          </Button>
        </div>
      </header>

      {order.status === 'failed' && order.provision_error ? (
        <Alert status="danger">
          <Alert.Indicator />
          <Alert.Content>
            <Alert.Title>交付失败</Alert.Title>
            <Alert.Description>{order.provision_error}</Alert.Description>
          </Alert.Content>
        </Alert>
      ) : null}

      <section className="grid gap-4 lg:grid-cols-2">
        <Card>
          <Card.Header>
            <Card.Title className="text-base">交付信息</Card.Title>
            <Card.Description>上游开通结果；失败时可重试交付（同步调用上游）。</Card.Description>
          </Card.Header>
          <Card.Content className="space-y-4">
            <InfoList
              columns={1}
              items={[
                { label: '订单状态', value: orderStatusLabel(order.status) },
                { label: '上游主机 ID', value: order.host_id ?? '—' },
                {
                  label: '关联实例',
                  value: order.instance_id ? (
                    <Link
                      className="text-accent hover:underline"
                      to={paths.adminInstanceDetail(order.instance_id)}
                    >
                      #{order.instance_id}
                    </Link>
                  ) : (
                    '—'
                  ),
                },
                {
                  label: '交付时间',
                  value: formatDateTimeOr(order.delivered_at, '未交付'),
                },
                {
                  label: '失败原因',
                  value: order.provision_error || '—',
                },
              ]}
            />
            <p className="text-xs text-muted">{ORDER_STATUS_DESCRIPTIONS[order.status] ?? ''}</p>
            <div className="flex flex-wrap items-center gap-2">
              <Button
                variant="primary"
                size="sm"
                isDisabled={!canRetryRole || !retryable || retrying}
                onPress={() => setConfirmOpen(true)}
              >
                {retrying ? '正在交付…' : '重试交付'}
              </Button>
              {!canRetryRole ? (
                <span className="text-xs text-muted">
                  {permissionHint('orders.retry')}（当前角色不可执行）
                </span>
              ) : !retryable ? (
                <span className="text-xs text-muted">
                  仅「已支付」与「交付失败」的订单可重试（当前：{orderStatusLabel(order.status)}）
                </span>
              ) : null}
            </div>
          </Card.Content>
        </Card>

        <Card>
          <Card.Header>
            <Card.Title className="text-base">订单流转</Card.Title>
            <Card.Description>按下单、支付与交付时间线展示。</Card.Description>
          </Card.Header>
          <Card.Content>
            <Timeline items={timeline} />
          </Card.Content>
        </Card>
      </section>

      <Card>
        <Card.Header>
          <Card.Title className="text-base">订单信息</Card.Title>
          <Card.Description>下单时的快照字段（金额为定点小数字符串）。</Card.Description>
        </Card.Header>
        <Card.Content className="space-y-4">
          <InfoList
            columns={2}
            items={[
              { label: '订单 ID', value: order.id },
              { label: '订单号', value: <span className="font-mono">{order.trade_no}</span> },
              { label: '类型', value: order.type === 'renew' ? '续费' : '新购' },
              { label: '商品', value: `${order.product_name || '—'}（#${order.product_id}）` },
              { label: '计费周期', value: formatCycleLabel(order.cycle) },
              { label: '数量', value: String(order.qty) },
              { label: '原价', value: formatMoney(order.amount) },
              { label: '优惠金额', value: formatMoney(order.discount_amount) },
              { label: '实付金额', value: formatMoney(order.final_amount) },
              { label: '优惠码', value: order.coupon_code || '—' },
              {
                label: '支付渠道',
                value: order.pay_channel === 'balance' ? '余额支付' : order.pay_channel || '—',
              },
              { label: '渠道单号', value: order.channel_trade_no || '—' },
              { label: '支付时间', value: formatDateTimeOr(order.pay_time, '未支付') },
              {
                label: '会员',
                value: order.member
                  ? `${order.member.username}（#${order.member.id} · ${order.member.email}）`
                  : `#${order.member_id}（资料缺失）`,
              },
              { label: '下单时间', value: formatDateTime(order.created_at) },
              { label: '更新时间', value: formatDateTime(order.updated_at) },
            ]}
          />
          {configEntries.length > 0 ? (
            <div className="space-y-2">
              <p className="text-sm font-medium text-foreground">配置快照</p>
              <InfoList
                items={configEntries.map(([key, value]) => ({
                  label: `配置项 #${key}`,
                  value: value ? `值 #${value}` : '—',
                }))}
              />
              <p className="text-xs text-muted">
                配置项与取值为上游本地 ID（下单时原样快照，交付时按此拼装 configoption）。
              </p>
            </div>
          ) : null}
        </Card.Content>
      </Card>

      <ConfirmDialog
        isOpen={confirmOpen}
        title="确认重试交付？"
        description="将立即向上游提交一次开通请求（可能真实扣费），并回读结果写入订单。执行期间请勿关闭页面。"
        confirmLabel="开始重试交付"
        pending={retrying}
        onCancel={() => setConfirmOpen(false)}
        onConfirm={handleRetry}
      />
    </div>
  )
}
