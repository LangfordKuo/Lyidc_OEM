import { useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { listOrders } from '@/api/orders'
import type { Order, OrderStatus } from '@/api/types'
import { paths } from '@/app/paths'
import Pager from '@/components/common/Pager'
import { EmptyBlock, ErrorState, LoadingBlock } from '@/components/common/PageState'
import StatusBadge from '@/components/common/StatusBadge'
import StatusFilter from '@/components/common/StatusFilter'
import OrderDetailPanel from '@/components/console/OrderDetailPanel'
import PayOrderDialog from '@/components/console/PayOrderDialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { useAsync } from '@/hooks/useAsync'
import { formatCycleLabel, formatDateTime, formatMoney } from '@/lib/format'
import { ORDER_STATUS_LABELS, ORDER_STATUS_TONES } from '@/lib/orderStatus'
import { CONSOLE_PAGE_SIZE } from '@/lib/pagination'

type StatusFilterValue = '' | OrderStatus

const STATUS_OPTIONS: { value: StatusFilterValue; label: string }[] = (
  Object.entries(ORDER_STATUS_LABELS) as [OrderStatus, string][]
).map(([value, label]) => ({ value, label }))

// ConsoleOrders 是会员区「我的订单」（契约 12.4）：状态筛选 + 分页 + 行内展开详情（含继续支付与取消）。
export default function ConsoleOrders() {
  const navigate = useNavigate()
  const [status, setStatus] = useState<StatusFilterValue>('')
  const [page, setPage] = useState(1)
  const [expandedId, setExpandedId] = useState<number | null>(null)
  const [payTarget, setPayTarget] = useState<Order | null>(null)
  const [refreshKey, setRefreshKey] = useState(0)

  const ordersState = useAsync(
    () =>
      listOrders({
        page,
        page_size: CONSOLE_PAGE_SIZE,
        ...(status ? { status } : {}),
      }),
    [page, status, refreshKey],
  )
  const orders = ordersState.data?.items ?? []

  const changeStatus = (next: StatusFilterValue) => {
    setStatus(next)
    setPage(1)
    setExpandedId(null)
  }

  const refresh = () => setRefreshKey((value) => value + 1)

  return (
    <div className="space-y-5">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold text-foreground">我的订单</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            查看新购与续费订单的支付、交付进度；待支付订单可继续支付或取消。
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={refresh}>
            刷新
          </Button>
          <Button size="sm" onClick={() => navigate(paths.products)}>
            去选购商品
          </Button>
        </div>
      </header>

      <StatusFilter
        label="按状态筛选"
        options={STATUS_OPTIONS}
        value={status}
        onChange={changeStatus}
      />

      {ordersState.loading ? <LoadingBlock label="正在读取订单…" /> : null}
      {ordersState.error ? (
        <ErrorState message={ordersState.error} onRetry={ordersState.reload} />
      ) : null}

      {!ordersState.loading && !ordersState.error && orders.length === 0 ? (
        <EmptyBlock
          title={status ? '该状态下没有订单' : '还没有订单'}
          description={
            status ? (
              '换个状态筛选看看。'
            ) : (
              <span>
                去
                <button
                  type="button"
                  className="mx-1 text-primary hover:underline"
                  onClick={() => navigate(paths.products)}
                >
                  商品列表
                </button>
                选购第一台服务器吧。
              </span>
            )
          }
        />
      ) : null}

      <div className="space-y-3">
        {orders.map((order) => (
          <div key={order.id} className="space-y-3">
            <Card data-testid={`order-card-${order.id}`}>
              <CardContent className="space-y-3">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium text-foreground">
                      {order.product_name}
                    </p>
                    <p className="mt-0.5 font-mono text-xs text-muted-foreground">
                      {order.trade_no}
                    </p>
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    <Badge variant="secondary">{order.type === 'renew' ? '续费' : '新购'}</Badge>
                    <StatusBadge
                      tone={ORDER_STATUS_TONES[order.status]}
                      label={ORDER_STATUS_LABELS[order.status]}
                    />
                  </div>
                </div>

                <div className="grid gap-1.5 text-xs text-muted-foreground sm:grid-cols-3">
                  <span>
                    周期：<span className="text-foreground">{formatCycleLabel(order.cycle)}</span>
                  </span>
                  <span>
                    应付：
                    <span className="font-medium text-foreground">
                      {formatMoney(order.final_amount)}
                    </span>
                    {Number(order.discount_amount) > 0 ? (
                      <span className="ml-1">（已优惠 {formatMoney(order.discount_amount)}）</span>
                    ) : null}
                  </span>
                  <span>
                    创建：<span className="text-foreground">{formatDateTime(order.created_at)}</span>
                  </span>
                </div>

                <div className="flex flex-wrap gap-2">
                  <Button
                    size="sm"
                    variant="outline"
                    aria-expanded={expandedId === order.id}
                    onClick={() => setExpandedId(expandedId === order.id ? null : order.id)}
                  >
                    {expandedId === order.id ? '收起详情' : '查看详情'}
                  </Button>
                  {order.status === 'pending' ? (
                    <Button size="sm" onClick={() => setPayTarget(order)}>
                      继续支付
                    </Button>
                  ) : null}
                  {order.status === 'active' && order.type === 'new' ? (
                    <Button size="sm" variant="outline" onClick={() => navigate(paths.consoleServers)}>
                      查看实例
                    </Button>
                  ) : null}
                </div>
              </CardContent>
            </Card>

            {expandedId === order.id ? (
              <OrderDetailPanel
                key={order.id}
                orderId={order.id}
                onClose={() => setExpandedId(null)}
                onPay={(target) => setPayTarget(target)}
                onCancelled={() => {
                  setExpandedId(null)
                  refresh()
                }}
              />
            ) : null}
          </div>
        ))}
      </div>

      {ordersState.data ? (
        <Pager
          page={page}
          total={ordersState.data.total}
          pageSize={ordersState.data.page_size || CONSOLE_PAGE_SIZE}
          onChange={(next) => {
            setPage(next)
            setExpandedId(null)
          }}
        />
      ) : null}

      <PayOrderDialog
        order={payTarget}
        onOpenChange={(open) => (!open ? setPayTarget(null) : undefined)}
        onPaid={() => {
          setPayTarget(null)
          refresh()
        }}
      />
    </div>
  )
}
