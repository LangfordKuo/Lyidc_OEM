import { useMemo, useState } from 'react'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { CheckCircle2Icon, ClockIcon, RotateCcwIcon, XCircleIcon } from 'lucide-react'

import { fetchOrder } from '@/api/orders'
import type { Order } from '@/api/types'
import { paths } from '@/app/paths'
import { useAuth } from '@/auth/authContext'
import { buildLoginUrl } from '@/lib/redirect'
import PayDialog from '@/components/checkout/PayDialog'
import { EmptyBlock, ErrorState, LoadingBlock } from '@/components/common/PageState'
import StatusBadge from '@/components/common/StatusBadge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import { useAsync } from '@/hooks/useAsync'
import { findRememberedOrderByTradeNo, readRememberedOrder } from '@/lib/checkout'
import { formatCycleLabel, formatDateTimeOr, formatMoney } from '@/lib/format'
import { ORDER_STATUS_LABELS, ORDER_STATUS_TONES } from '@/lib/orderStatus'

type ResultTone = 'success' | 'warning' | 'danger'

// 各订单状态在结果页的呈现口径（契约 12.2.3：同步跳转不参与入账，以异步通知为准）。
const RESULT_TONE: Record<Order['status'], ResultTone> = {
  pending: 'warning',
  paid: 'success',
  provisioning: 'success',
  active: 'success',
  failed: 'danger',
  cancelled: 'warning',
}

function resultCopy(order: Order): { title: string; description: string } {
  switch (order.status) {
    case 'pending':
      return {
        title: '支付结果确认中',
        description:
          '尚未收到支付渠道的异步通知（通常几秒内到达）。若您已付款，可稍后点击「重新查询」；若还未支付，可点击「继续支付」。',
      }
    case 'cancelled':
      return {
        title: '订单已取消',
        description: '该订单已取消（未支付订单可由本人取消）。如需购买请重新下单。',
      }
    case 'failed':
      return {
        title: '支付成功，但开通失败',
        description: '支付已到账，自动开通未成功。管理员可重试交付，届时您会收到站内通知。',
      }
    case 'active':
      return {
        title: '支付成功，实例已开通',
        description: '实例已交付完成，开通结果已通过站内通知发送。',
      }
    case 'provisioning':
      return { title: '支付成功，正在开通', description: '系统正在向上游提交开通请求，请稍候刷新查看结果。' }
    default:
      return { title: '支付成功', description: '支付已到账，系统将自动完成开通并发送站内通知。' }
  }
}

/**
 * 支付结果页：优先按 order 参数查询订单，其次按渠道回跳的 out_trade_no
 * 匹配本地记录的最近订单；都拿不到时给出引导（不做猜测，避免误报支付状态）。
 */
export default function PayResult() {
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const location = useLocation()
  const { member, initializing, setBalance } = useAuth()
  const [payOpen, setPayOpen] = useState(false)

  const orderParam = searchParams.get('order')
  const tradeNoParam = searchParams.get('out_trade_no') ?? searchParams.get('trade_no')

  const orderId = useMemo(() => {
    if (orderParam && /^\d+$/.test(orderParam)) {
      return Number(orderParam)
    }
    // 渠道回跳带 out_trade_no / trade_no：按单号匹配本地记录。
    const byTradeNo = findRememberedOrderByTradeNo(tradeNoParam)
    if (byTradeNo) {
      return byTradeNo.id
    }
    // 真实渠道（及 mock 网关）同步回跳可能完全不带参数：回退到最近一次下单/发起支付的记录。
    // 这是「用户刚从收银台跳回」的高置信场景，且订单查询要求会员 token（只能查本人订单），无越权面。
    if (!orderParam && !tradeNoParam) {
      return readRememberedOrder()?.id ?? null
    }
    return null
  }, [orderParam, tradeNoParam])

  // 订单查询要求会员 token：未登录时先展示登录引导（登录后带 redirect 回到本页）。
  const canQuery = orderId !== null && (member !== null || initializing)
  const { data, loading, error, reload } = useAsync<Order>(
    () => {
      if (orderId === null) {
        return Promise.reject(new Error('缺少订单信息'))
      }
      return fetchOrder(orderId)
    },
    [orderId, member?.id],
    canQuery,
  )

  if (orderId !== null && !initializing && !member) {
    const target = `${location.pathname}${location.search}`
    return (
      <div className="mx-auto max-w-3xl px-4 py-16 sm:px-6">
        <EmptyBlock
          title="请先登录后查看订单状态"
          description="订单详情仅对下单会员本人可见；登录后会自动回到本页。"
        />
        <div className="mt-6 flex justify-center gap-3">
          <Button onClick={() => navigate(buildLoginUrl(target))}>登录后查看</Button>
          <Button variant="outline" onClick={() => navigate(paths.products)}>
            继续选购
          </Button>
        </div>
      </div>
    )
  }

  if (orderId === null) {
    return (
      <div className="mx-auto max-w-3xl px-4 py-16 sm:px-6">
        <EmptyBlock
          title="未找到对应的订单信息"
          description="支付回跳参数缺少订单标识，可能是浏览器会话已过期。请登录后前往会员区「我的订单」查看最新状态。"
        />
        <div className="mt-6 flex justify-center gap-3">
          <Button onClick={() => navigate(paths.home)}>返回首页</Button>
          <Button variant="outline" onClick={() => navigate(paths.products)}>
            继续选购
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
          <Button variant="outline" onClick={() => navigate(paths.products)}>
            继续选购
          </Button>
        </div>
      </div>
    )
  }

  const order = data
  const tone = RESULT_TONE[order.status]
  const copy = resultCopy(order)
  const ToneIcon = tone === 'success' ? CheckCircle2Icon : tone === 'danger' ? XCircleIcon : ClockIcon

  return (
    <div className="mx-auto max-w-2xl px-4 py-12 sm:px-6">
      <div className="flex flex-col items-center text-center">
        <span
          className={[
            'grid size-12 place-items-center rounded-full',
            tone === 'success'
              ? 'bg-success/10 text-success dark:bg-green-500/15 dark:text-green-400'
              : tone === 'danger'
                ? 'bg-destructive/10 text-destructive'
                : 'bg-warning/10 text-warning dark:bg-amber-500/15 dark:text-amber-400',
          ].join(' ')}
        >
          <ToneIcon className="size-6" aria-hidden />
        </span>
        <h1 className="mt-4 text-xl font-semibold text-foreground">{copy.title}</h1>
        <p className="mt-2 max-w-xl text-sm text-muted-foreground">{copy.description}</p>
      </div>

      <Card className="mt-8">
        <CardHeader>
          <div className="flex w-full items-center justify-between gap-3">
            <CardTitle className="text-base">订单信息</CardTitle>
            <StatusBadge tone={ORDER_STATUS_TONES[order.status]} label={ORDER_STATUS_LABELS[order.status]} />
          </div>
        </CardHeader>
        <CardContent className="space-y-2 text-sm">
          <div className="flex justify-between gap-4">
            <span className="text-muted-foreground">订单号</span>
            <span className="font-mono text-xs text-foreground sm:text-sm">{order.trade_no}</span>
          </div>
          <div className="flex justify-between gap-4">
            <span className="text-muted-foreground">商品</span>
            <span className="text-right text-foreground">{order.product_name}</span>
          </div>
          <div className="flex justify-between gap-4">
            <span className="text-muted-foreground">周期 / 数量</span>
            <span className="text-foreground">
              {formatCycleLabel(order.cycle)} × {order.qty}
            </span>
          </div>
          <div className="flex justify-between gap-4">
            <span className="text-muted-foreground">原价 / 优惠</span>
            <span className="text-foreground">
              {formatMoney(order.amount)}
              {Number(order.discount_amount) > 0 ? ` / -${formatMoney(order.discount_amount)}` : ''}
            </span>
          </div>
          <div className="flex justify-between gap-4">
            <span className="text-muted-foreground">应付金额</span>
            <span className="font-medium text-foreground">{formatMoney(order.final_amount)}</span>
          </div>
          <Separator className="my-2" />
          <div className="flex justify-between gap-4">
            <span className="text-muted-foreground">支付渠道</span>
            <span className="text-foreground">{order.pay_channel || '—'}</span>
          </div>
          <div className="flex justify-between gap-4">
            <span className="text-muted-foreground">支付时间</span>
            <span className="text-foreground">{formatDateTimeOr(order.pay_time)}</span>
          </div>
          <div className="flex justify-between gap-4">
            <span className="text-muted-foreground">创建时间</span>
            <span className="text-foreground">{formatDateTimeOr(order.created_at)}</span>
          </div>
        </CardContent>
      </Card>

      <div className="mt-8 flex flex-wrap justify-center gap-3">
        {order.status === 'pending' ? (
          <>
            <Button onClick={() => setPayOpen(true)}>继续支付</Button>
            <Button variant="outline" onClick={reload}>
              <RotateCcwIcon data-icon="inline-start" aria-hidden />
              重新查询
            </Button>
          </>
        ) : (
          <Button variant="outline" onClick={reload}>
            <RotateCcwIcon data-icon="inline-start" aria-hidden />
            刷新状态
          </Button>
        )}
        <Button variant="ghost" onClick={() => navigate(paths.products)}>
          继续选购
        </Button>
      </div>

      <PayDialog
        order={order}
        open={payOpen}
        onOpenChange={setPayOpen}
        balance={member?.balance ?? '0.00'}
        onPaid={(result) => {
          if (result.pay.balance_after) {
            setBalance(result.pay.balance_after)
          }
          setPayOpen(false)
          reload()
        }}
      />
    </div>
  )
}
