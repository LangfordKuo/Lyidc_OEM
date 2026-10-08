import { Button, Card, Chip } from '@heroui/react'
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { listOrders } from '../../api/orders'
import type { Order, OrderStatus } from '../../api/types'
import { paths } from '../../app/paths'
import StatusBadge from '../../components/StatusBadge'
import Pager from '../../components/common/Pager'
import { EmptyBlock, ErrorState, LoadingBlock } from '../../components/common/PageState'
import StatusFilter from '../../components/common/StatusFilter'
import OrderDetailPanel from '../../components/order/OrderDetailPanel'
import PayOrderDialog from '../../components/order/PayOrderDialog'
import { useAsync } from '../../hooks/useAsync'
import { formatCycleLabel, formatDateTime, formatMoney } from '../../lib/format'
import { ORDER_STATUS_LABELS, ORDER_STATUS_TONES } from '../../lib/orderStatus'
import { DEFAULT_PAGE_SIZE } from '../../lib/pagination'

type StatusFilterValue = '' | OrderStatus

const STATUS_OPTIONS: { value: StatusFilterValue; label: string }[] = [
  { value: '', label: '全部' },
  { value: 'pending', label: '待支付' },
  { value: 'paid', label: '已支付' },
  { value: 'provisioning', label: '开通中' },
  { value: 'active', label: '已开通' },
  { value: 'failed', label: '交付失败' },
  { value: 'cancelled', label: '已取消' },
]

// ConsoleOrders 是会员区「我的订单」：状态筛选 + 分页列表 + 行内展开详情（含继续支付与取消）。
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
        page_size: DEFAULT_PAGE_SIZE,
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
          <p className="mt-1 text-sm text-muted">
            查看新购与续费订单的支付、交付进度；待支付订单可继续支付或取消。
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onPress={refresh}>
            刷新
          </Button>
          <Button variant="primary" size="sm" onPress={() => navigate(paths.products)}>
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
                  className="mx-1 text-accent hover:underline"
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
          <Card key={order.id}>
            <Card.Content className="space-y-3">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium text-foreground">
                    {order.product_name}
                  </p>
                  <p className="mt-0.5 font-mono text-xs text-muted">{order.trade_no}</p>
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  <Chip size="sm" variant="soft" color={order.type === 'renew' ? 'accent' : 'default'}>
                    {order.type === 'renew' ? '续费' : '新购'}
                  </Chip>
                  <StatusBadge
                    tone={ORDER_STATUS_TONES[order.status]}
                    label={ORDER_STATUS_LABELS[order.status]}
                  />
                </div>
              </div>

              <div className="grid gap-1.5 text-xs text-muted sm:grid-cols-3">
                <span>
                  周期：<span className="text-foreground">{formatCycleLabel(order.cycle)}</span>
                </span>
                <span>
                  应付：
                  <span className="font-medium text-foreground">{formatMoney(order.final_amount)}</span>
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
                  onPress={() => setExpandedId(expandedId === order.id ? null : order.id)}
                >
                  {expandedId === order.id ? '收起详情' : '查看详情'}
                </Button>
                {order.status === 'pending' ? (
                  <Button size="sm" variant="primary" onPress={() => setPayTarget(order)}>
                    继续支付
                  </Button>
                ) : null}
                {order.status === 'active' && order.type === 'new' ? (
                  <Button
                    size="sm"
                    variant="outline"
                    onPress={() => navigate(paths.consoleServers)}
                  >
                    查看实例
                  </Button>
                ) : null}
              </div>
            </Card.Content>
          </Card>
        ))}
      </div>

      {expandedId !== null ? (
        <OrderDetailPanel
          key={expandedId}
          orderId={expandedId}
          onClose={() => setExpandedId(null)}
          onPay={(order) => setPayTarget(order)}
          onCancelled={() => {
            setExpandedId(null)
            refresh()
          }}
        />
      ) : null}

      {ordersState.data && ordersState.data.total > 0 ? (
        <Pager
          page={page}
          total={ordersState.data.total}
          pageSize={ordersState.data.page_size || DEFAULT_PAGE_SIZE}
          onChange={(next) => {
            setPage(next)
            setExpandedId(null)
          }}
        />
      ) : null}

      {payTarget ? (
        <PayOrderDialog
          order={payTarget}
          onClose={() => setPayTarget(null)}
          onPaid={() => {
            setPayTarget(null)
            refresh()
          }}
        />
      ) : null}
    </div>
  )
}
