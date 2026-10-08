import { Alert, Button, Card, Separator } from '@heroui/react'
import { useMemo } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'

import { fetchOrder } from '../api/orders'
import type { Order, OrderStatus } from '../api/types'
import { paths } from '../app/paths'
import { EmptyBlock, ErrorState, LoadingBlock } from '../components/common/PageState'
import { IconAlert, IconCheck, IconClock } from '../components/common/icons'
import StatusBadge from '../components/StatusBadge'
import { useAsync } from '../hooks/useAsync'
import { findRememberedOrderByTradeNo } from '../lib/checkout'
import { formatCycleLabel, formatDateTimeOr, formatMoney } from '../lib/format'
import { ORDER_STATUS_LABELS, ORDER_STATUS_TONES } from '../lib/orderStatus'

// 各订单状态在结果页的呈现口径（契约 12.2.3：同步跳转不参与入账，以异步通知为准）。
const RESULT_TONE: Record<OrderStatus, 'success' | 'warning' | 'danger'> = {
  pending: 'warning',
  paid: 'success',
  provisioning: 'success',
  active: 'success',
  failed: 'danger',
  cancelled: 'warning',
}

function ResultHeadline({ order }: { order: Order }) {
  const tone = RESULT_TONE[order.status]
  const Icon = tone === 'success' ? IconCheck : tone === 'danger' ? IconAlert : IconClock
  const title =
    order.status === 'pending'
      ? '支付结果确认中'
      : order.status === 'cancelled'
        ? '订单已取消'
        : order.status === 'failed'
          ? '支付成功，但开通失败'
          : '支付成功'

  const description =
    order.status === 'pending'
      ? '我们尚未收到支付渠道的异步通知（通常几秒内到达）。若您已付款，可稍后点击「重新查询」；长时间未更新请提交工单。'
      : order.status === 'cancelled'
        ? '该订单已取消（未支付订单可由本人取消）。如需购买请重新下单。'
        : order.status === 'failed'
          ? '支付已到账，自动开通未成功，管理员可重试交付；也可提交工单由客服协助处理。'
          : order.status === 'active'
            ? '实例已开通，可在会员区「我的服务器」查看并使用。'
            : '支付已到账，系统正在向上游提交开通请求。'

  return (
    <div className="flex flex-col items-center text-center">
      <span
        className={[
          'grid size-12 place-items-center rounded-full',
          tone === 'success'
            ? 'bg-success-soft text-success-soft-foreground'
            : tone === 'danger'
              ? 'bg-danger-soft text-danger-soft-foreground'
              : 'bg-warning-soft text-warning-soft-foreground',
        ].join(' ')}
      >
        <Icon className="size-6" />
      </span>
      <h1 className="mt-4 text-xl font-semibold text-foreground">{title}</h1>
      <p className="mt-2 max-w-xl text-sm text-muted">{description}</p>
    </div>
  )
}

// PayResult 是支付结果页：优先按 order 参数查询订单，其次按渠道回跳的 out_trade_no
// 匹配本地记录的最近订单；都拿不到时给出引导（不做猜测，避免误报支付状态）。
export default function PayResult() {
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()

  const orderParam = searchParams.get('order')
  const tradeNoParam = searchParams.get('out_trade_no') ?? searchParams.get('trade_no')

  const orderId = useMemo(() => {
    if (orderParam && /^\d+$/.test(orderParam)) {
      return Number(orderParam)
    }
    const remembered = findRememberedOrderByTradeNo(tradeNoParam)
    return remembered?.id ?? null
  }, [orderParam, tradeNoParam])

  const { data, loading, error, reload } = useAsync<Order>(
    () => {
      if (orderId === null) {
        return Promise.reject(new Error('缺少订单信息'))
      }
      return fetchOrder(orderId)
    },
    [orderId],
    orderId !== null,
  )

  if (orderId === null) {
    return (
      <div className="mx-auto max-w-3xl px-4 py-16 sm:px-6">
        <EmptyBlock
          title="未找到对应的订单信息"
          description="支付回跳参数缺少订单标识，可能是浏览器会话已过期。请前往会员区「我的订单」查看最新状态。"
        />
        <div className="mt-6 flex justify-center gap-3">
          <Button variant="primary" onPress={() => navigate(paths.consoleOrders)}>
            查看我的订单
          </Button>
          <Button variant="outline" onPress={() => navigate(paths.home)}>
            返回首页
          </Button>
        </div>
      </div>
    )
  }

  if (loading) {
    return <LoadingBlock label="正在查询订单状态…" />
  }

  if (error || !data) {
    return (
      <div className="mx-auto max-w-3xl px-4 py-16 sm:px-6">
        <ErrorState message={error || '订单查询失败'} onRetry={reload} />
        <div className="mt-6 flex justify-center gap-3">
          <Button variant="outline" onPress={() => navigate(paths.consoleOrders)}>
            查看我的订单
          </Button>
        </div>
      </div>
    )
  }

  const order = data

  return (
    <div className="mx-auto max-w-2xl px-4 py-12 sm:px-6">
      <ResultHeadline order={order} />

      <Card className="mt-8">
        <Card.Header>
          <div className="flex w-full items-center justify-between gap-3">
            <Card.Title className="text-base">订单信息</Card.Title>
            <StatusBadge tone={ORDER_STATUS_TONES[order.status]} label={ORDER_STATUS_LABELS[order.status]} />
          </div>
        </Card.Header>
        <Card.Content className="space-y-2 text-sm">
          <div className="flex justify-between gap-4">
            <span className="text-muted">订单号</span>
            <span className="font-mono text-xs text-foreground sm:text-sm">{order.trade_no}</span>
          </div>
          <div className="flex justify-between gap-4">
            <span className="text-muted">商品</span>
            <span className="text-right text-foreground">{order.product_name}</span>
          </div>
          <div className="flex justify-between gap-4">
            <span className="text-muted">周期 / 数量</span>
            <span className="text-foreground">
              {formatCycleLabel(order.cycle)} × {order.qty}
            </span>
          </div>
          <div className="flex justify-between gap-4">
            <span className="text-muted">原价 / 优惠</span>
            <span className="text-foreground">
              {formatMoney(order.amount)}
              {Number(order.discount_amount) > 0 ? ` / -${formatMoney(order.discount_amount)}` : ''}
            </span>
          </div>
          <div className="flex justify-between gap-4">
            <span className="text-muted">应付金额</span>
            <span className="font-medium text-foreground">{formatMoney(order.final_amount)}</span>
          </div>
          <Separator className="my-2" />
          <div className="flex justify-between gap-4">
            <span className="text-muted">支付渠道</span>
            <span className="text-foreground">{order.pay_channel || '—'}</span>
          </div>
          <div className="flex justify-between gap-4">
            <span className="text-muted">支付时间</span>
            <span className="text-foreground">{formatDateTimeOr(order.pay_time)}</span>
          </div>
          <div className="flex justify-between gap-4">
            <span className="text-muted">创建时间</span>
            <span className="text-foreground">{formatDateTimeOr(order.created_at)}</span>
          </div>
        </Card.Content>
      </Card>

      {order.status === 'pending' ? (
        <Alert status="warning" className="mt-6">
          <Alert.Indicator />
          <Alert.Content>
            <Alert.Description>
              支付结果以渠道异步通知为准，到账后会立即自动开通并发送站内通知。
            </Alert.Description>
          </Alert.Content>
        </Alert>
      ) : null}

      <div className="mt-8 flex flex-wrap justify-center gap-3">
        <Button variant="primary" onPress={() => navigate(paths.consoleOrders)}>
          查看订单
        </Button>
        {order.status === 'pending' ? (
          <Button variant="outline" onPress={reload}>
            重新查询
          </Button>
        ) : (
          <Button variant="outline" onPress={() => navigate(paths.consoleServers)}>
            我的服务器
          </Button>
        )}
        <Button variant="ghost" onPress={() => navigate(paths.products)}>
          继续选购
        </Button>
      </div>
    </div>
  )
}
