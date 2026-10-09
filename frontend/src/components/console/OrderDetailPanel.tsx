import { useState } from 'react'
import { AlertCircleIcon } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'

import { errorMessage } from '@/api/client'
import { cancelOrder, fetchOrder } from '@/api/orders'
import { fetchProductDetail } from '@/api/products'
import type { Order, ProductDetail } from '@/api/types'
import { paths } from '@/app/paths'
import ConfirmDialog from '@/components/common/ConfirmDialog'
import { InfoList, Timeline, type TimelineItem } from '@/components/common/InfoList'
import { ErrorState, LoadingBlock } from '@/components/common/PageState'
import StatusBadge from '@/components/common/StatusBadge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import { useAsync } from '@/hooks/useAsync'
import { formatCycleLabel, formatDateTime, formatDateTimeOr, formatMoney } from '@/lib/format'
import { describeOrderConfig } from '@/lib/orderConfig'
import {
  ORDER_STATUS_DESCRIPTIONS,
  ORDER_STATUS_LABELS,
  ORDER_STATUS_TONES,
} from '@/lib/orderStatus'

// OrderDetailPanel 是订单详情面板（列表内展开）：配置快照、金额明细、时间线与交付结果。
// 商品详情可能因下架取不到（此时配置项退化为 id 展示），因此两条请求相互独立、互不阻断。
export default function OrderDetailPanel({
  orderId,
  onClose,
  onPay,
  onCancelled,
}: {
  orderId: number
  onClose: () => void
  onPay: (order: Order) => void
  onCancelled: () => void
}) {
  const navigate = useNavigate()
  const [cancelOpen, setCancelOpen] = useState(false)
  const [pending, setPending] = useState(false)

  const orderState = useAsync(() => fetchOrder(orderId), [orderId])
  const order = orderState.data

  // 商品详情只用于翻译配置项名称，失败不影响详情展示。
  const productState = useAsync(
    () => (order ? fetchProductDetail(order.product_id) : Promise.resolve(null)),
    [order?.product_id],
    Boolean(order),
  )
  const product: ProductDetail | null = productState.data ?? null

  if (orderState.loading) {
    return <LoadingBlock label="正在读取订单详情…" />
  }
  if (orderState.error) {
    return <ErrorState message={orderState.error} onRetry={orderState.reload} />
  }
  if (!order) {
    return null
  }

  const config = describeOrderConfig(product, order.config)
  const items: TimelineItem[] = [
    {
      key: 'created',
      title: '创建订单',
      time: formatDateTime(order.created_at),
      description: `订单号 ${order.trade_no}`,
    },
  ]
  if (order.pay_time) {
    items.push({
      key: 'paid',
      title: `支付成功（${order.pay_channel === 'balance' ? '余额支付' : '在线支付'}）`,
      time: formatDateTime(order.pay_time),
      tone: 'success',
    })
  } else if (order.status === 'cancelled') {
    items.push({ key: 'cancelled', title: '订单已取消', time: formatDateTime(order.updated_at) })
  } else {
    items.push({ key: 'unpaid', title: '等待支付', description: ORDER_STATUS_DESCRIPTIONS.pending })
  }
  if (order.delivered_at) {
    items.push({
      key: 'delivered',
      title: order.type === 'renew' ? '续费完成' : '实例已开通',
      time: formatDateTime(order.delivered_at),
      tone: 'success',
      description: order.type === 'renew' ? '到期时间已顺延' : '可在「我的服务器」查看实例',
    })
  } else if (order.status === 'provisioning') {
    items.push({ key: 'provisioning', title: '交付中', description: '上游正在处理，请稍后刷新' })
  }

  return (
    <Card className="border-primary/30 bg-muted/30" data-testid="order-detail-panel">
      <CardHeader className="border-b">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <CardTitle className="text-base">订单详情</CardTitle>
            <p className="mt-1 text-sm text-muted-foreground">
              <span className="font-mono">{order.trade_no}</span> ·{' '}
              {order.type === 'renew' ? '续费' : '新购'}
            </p>
          </div>
          <div className="flex items-center gap-2">
            <StatusBadge
              tone={ORDER_STATUS_TONES[order.status]}
              label={ORDER_STATUS_LABELS[order.status]}
            />
            <Button size="sm" variant="ghost" onClick={onClose}>
              收起
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        {order.status === 'failed' && order.provision_error ? (
          <Alert variant="destructive">
            <AlertCircleIcon aria-hidden />
            <AlertTitle>交付失败</AlertTitle>
            <AlertDescription>
              {order.provision_error}
              <br />
              管理员可在后台重试交付；如需协助也可提交工单，我们会尽快处理。
            </AlertDescription>
            <Button
              size="sm"
              variant="outline"
              onClick={() =>
                navigate(
                  `${paths.consoleTickets}?compose=1&subject=${encodeURIComponent(
                    `订单交付失败：${order.trade_no}`,
                  )}&content=${encodeURIComponent(
                    `订单 ${order.trade_no}（${order.product_name}）交付失败：${order.provision_error}`,
                  )}`,
                )
              }
            >
              提交工单
            </Button>
          </Alert>
        ) : null}

        <div className="grid gap-6 lg:grid-cols-2">
          <div className="space-y-3">
            <p className="text-sm font-medium text-foreground">配置快照</p>
            <InfoList
              items={[
                { label: '商品', value: order.product_name },
                { label: '计费周期', value: formatCycleLabel(order.cycle) },
                { label: '数量', value: String(order.qty) },
                ...config.map((entry) => ({ label: entry.label, value: entry.value })),
              ]}
            />
            {config.length > 0 && !product ? (
              <p className="text-xs text-muted-foreground">
                商品详情不可用（可能已下架），配置项以 ID 展示。
              </p>
            ) : null}
          </div>

          <div className="space-y-3">
            <p className="text-sm font-medium text-foreground">金额明细</p>
            <InfoList
              items={[
                { label: '原价', value: formatMoney(order.amount) },
                {
                  label: '优惠码',
                  value: order.coupon_code
                    ? `${order.coupon_code}（-${formatMoney(order.discount_amount)}）`
                    : '未使用',
                },
                {
                  label: '应付金额',
                  value: <span className="font-semibold">{formatMoney(order.final_amount)}</span>,
                },
                {
                  label: '交付时间',
                  value: formatDateTimeOr(order.delivered_at, '未交付'),
                },
              ]}
            />
            {order.status === 'pending' ? (
              <div className="flex flex-wrap gap-2">
                <Button size="sm" onClick={() => onPay(order)}>
                  继续支付
                </Button>
                <Button size="sm" variant="outline" onClick={() => setCancelOpen(true)}>
                  取消订单
                </Button>
              </div>
            ) : null}
          </div>
        </div>

        <Separator />

        <div className="space-y-3">
          <p className="text-sm font-medium text-foreground">订单时间线</p>
          <Timeline items={items} />
        </div>
      </CardContent>

      <ConfirmDialog
        open={cancelOpen}
        onOpenChange={setCancelOpen}
        title="确认取消订单？"
        description={`取消后订单 ${order.trade_no} 不可恢复；如需继续购买请重新下单。`}
        confirmLabel="取消订单"
        cancelLabel="再想想"
        danger
        pending={pending}
        onConfirm={async () => {
          setPending(true)
          try {
            await cancelOrder(order.id)
            toast.success('订单已取消')
            setCancelOpen(false)
            onCancelled()
          } catch (error) {
            toast.error(errorMessage(error, '取消订单失败，请稍后重试'))
          } finally {
            setPending(false)
          }
        }}
      />
    </Card>
  )
}
