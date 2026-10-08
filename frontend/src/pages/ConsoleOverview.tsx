import { Link } from 'react-router-dom'
import {
  ArrowRightIcon,
  BellIcon,
  LayoutDashboardIcon,
  LifeBuoyIcon,
  ReceiptTextIcon,
  ServerIcon,
  WalletIcon,
} from 'lucide-react'

import { fetchBalance } from '@/api/finance'
import { listInstances } from '@/api/instances'
import { listOrders } from '@/api/orders'
import { listTickets } from '@/api/tickets'
import { paths } from '@/app/paths'
import { useAuth } from '@/auth/authContext'
import { EmptyBlock, ErrorState, LoadingBlock } from '@/components/common/PageState'
import StatusBadge from '@/components/common/StatusBadge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import { useAsync } from '@/hooks/useAsync'
import { useUnreadCountEffect } from '@/hooks/useUnreadCount'
import { formatMoney } from '@/lib/format'
import { ORDER_STATUS_LABELS, ORDER_STATUS_TONES } from '@/lib/orderStatus'
import { ticketStatusLabel, ticketStatusTone } from '@/lib/ticketStatus'

const SHORTCUTS = [
  { to: paths.consoleServers, label: '我的服务器', icon: ServerIcon, hint: '实例状态与操作' },
  { to: paths.consoleOrders, label: '我的订单', icon: ReceiptTextIcon, hint: '支付与交付进度' },
  { to: paths.consoleRecharge, label: '余额充值', icon: WalletIcon, hint: '在线充值入账' },
  { to: paths.consoleTickets, label: '工单', icon: LifeBuoyIcon, hint: '问题反馈与跟踪' },
  { to: paths.consoleNotifications, label: '通知', icon: BellIcon, hint: '站内通知与提醒' },
] as const

// ConsoleOverview 是会员区概览页：账户摘要（余额 / 未读 / 实例与订单计数）+ 最近订单与工单 + 快捷入口。
// 每个数据块独立请求（取不到就显示错误态，不伪造数据）。
export default function ConsoleOverview() {
  const { member } = useAuth()
  const unread = useUnreadCountEffect(true)

  const balanceState = useAsync(fetchBalance, [])
  const balance = balanceState.data?.balance ?? member?.balance ?? '0.00'

  // 计数用 page_size=1 只取 total，避免拉全量数据。
  const instancesState = useAsync(() => listInstances({ page: 1, page_size: 1 }), [])
  const ordersState = useAsync(() => listOrders({ page: 1, page_size: 5 }), [])
  const ticketsState = useAsync(() => listTickets({ page: 1, page_size: 5 }), [])

  const recentOrders = ordersState.data?.items ?? []
  const recentTickets = ticketsState.data?.items ?? []

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <CardTitle className="text-lg">
                你好，{member?.nickname || member?.username || '会员'}
              </CardTitle>
              <CardDescription>欢迎回到会员控制台</CardDescription>
            </div>
            <span className="rounded-full bg-primary/10 px-2.5 py-1 text-xs font-medium text-primary">
              会员 ID {member?.id ?? '—'}
            </span>
          </div>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex flex-wrap items-end justify-between gap-4">
            <div>
              <p className="text-sm text-muted-foreground">账户余额</p>
              <p className="mt-1 text-2xl font-semibold text-foreground">{formatMoney(balance)}</p>
              {balanceState.error ? (
                <p className="mt-1 text-xs text-destructive">
                  余额读取失败：{balanceState.error}
                </p>
              ) : null}
            </div>
            <div className="flex gap-2">
              <Button size="sm" asChild>
                <Link to={paths.consoleRecharge}>去充值</Link>
              </Button>
              <Button variant="outline" size="sm" asChild>
                <Link to={paths.products}>选购商品</Link>
              </Button>
            </div>
          </div>

          <Separator />

          <div className="grid gap-4 sm:grid-cols-3">
            <div className="rounded-lg border border-border p-3">
              <p className="text-xs text-muted-foreground">实例数</p>
              <p className="mt-1 text-xl font-semibold text-foreground">
                {instancesState.loading ? '…' : (instancesState.data?.total ?? '—')}
              </p>
              {instancesState.error ? (
                <p className="mt-0.5 text-xs text-destructive">读取失败</p>
              ) : null}
            </div>
            <div className="rounded-lg border border-border p-3">
              <p className="text-xs text-muted-foreground">订单数</p>
              <p className="mt-1 text-xl font-semibold text-foreground">
                {ordersState.loading ? '…' : (ordersState.data?.total ?? '—')}
              </p>
              {ordersState.error ? (
                <p className="mt-0.5 text-xs text-destructive">读取失败</p>
              ) : null}
            </div>
            <div className="rounded-lg border border-border p-3">
              <p className="text-xs text-muted-foreground">未读通知</p>
              <p className="mt-1 text-xl font-semibold text-foreground">{unread}</p>
            </div>
          </div>
        </CardContent>
      </Card>

      <div className="grid gap-5 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <div className="flex items-center justify-between gap-2">
              <CardTitle className="text-base">最近订单</CardTitle>
              <Button variant="ghost" size="sm" asChild>
                <Link to={paths.consoleOrders}>
                  全部订单
                  <ArrowRightIcon data-icon="inline-end" aria-hidden />
                </Link>
              </Button>
            </div>
          </CardHeader>
          <CardContent className="space-y-3">
            {ordersState.loading ? <LoadingBlock label="正在读取订单…" /> : null}
            {ordersState.error ? (
              <ErrorState message={ordersState.error} onRetry={ordersState.reload} />
            ) : null}
            {!ordersState.loading && !ordersState.error && recentOrders.length === 0 ? (
              <EmptyBlock title="还没有订单" className="my-0" />
            ) : null}
            {recentOrders.map((order) => (
              <Link
                key={order.id}
                to={paths.consoleOrders}
                className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-border px-3 py-2.5 transition-colors hover:bg-muted/50"
              >
                <div className="min-w-0">
                  <p className="truncate text-sm text-foreground">{order.product_name}</p>
                  <p className="font-mono text-xs text-muted-foreground">{order.trade_no}</p>
                </div>
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium text-foreground">
                    {formatMoney(order.final_amount)}
                  </span>
                  <StatusBadge
                    tone={ORDER_STATUS_TONES[order.status]}
                    label={ORDER_STATUS_LABELS[order.status]}
                  />
                </div>
              </Link>
            ))}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <div className="flex items-center justify-between gap-2">
              <CardTitle className="text-base">最近工单</CardTitle>
              <Button variant="ghost" size="sm" asChild>
                <Link to={paths.consoleTickets}>
                  全部工单
                  <ArrowRightIcon data-icon="inline-end" aria-hidden />
                </Link>
              </Button>
            </div>
          </CardHeader>
          <CardContent className="space-y-3">
            {ticketsState.loading ? <LoadingBlock label="正在读取工单…" /> : null}
            {ticketsState.error ? (
              <ErrorState message={ticketsState.error} onRetry={ticketsState.reload} />
            ) : null}
            {!ticketsState.loading && !ticketsState.error && recentTickets.length === 0 ? (
              <EmptyBlock title="还没有工单" className="my-0" />
            ) : null}
            {recentTickets.map((ticket) => (
              <Link
                key={ticket.id}
                to={paths.consoleTicketDetail(ticket.id)}
                className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-border px-3 py-2.5 transition-colors hover:bg-muted/50"
              >
                <div className="min-w-0">
                  <p className="truncate text-sm text-foreground">{ticket.subject}</p>
                  <p className="font-mono text-xs text-muted-foreground">{ticket.trade_no}</p>
                </div>
                <StatusBadge
                  tone={ticketStatusTone(ticket.status)}
                  label={ticketStatusLabel(ticket.status)}
                />
              </Link>
            ))}
          </CardContent>
        </Card>
      </div>

      <section>
        <h2 className="mb-3 text-sm font-medium text-muted-foreground">快捷入口</h2>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {SHORTCUTS.map(({ to, label, icon: Icon, hint }) => (
            <Link
              key={to}
              to={to}
              className="flex items-center gap-3 rounded-xl border border-border bg-card p-4 transition-colors hover:bg-muted/50"
            >
              <span className="grid size-9 place-items-center rounded-lg bg-primary/10 text-primary">
                <Icon className="size-4.5" aria-hidden />
              </span>
              <span className="min-w-0 flex-1">
                <span className="block text-sm font-medium text-foreground">{label}</span>
                <span className="block truncate text-xs text-muted-foreground">{hint}</span>
              </span>
              <ArrowRightIcon className="size-4 text-muted-foreground" aria-hidden />
            </Link>
          ))}
        </div>
      </section>

      <Card className="bg-muted/30">
        <CardContent className="flex items-start gap-3">
          <LayoutDashboardIcon className="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-hidden />
          <p className="text-sm text-muted-foreground">
            会员区覆盖服务器（开关机 / 重装 / 改密 / 续费 / 终止申请 / 操作记录）、订单与继续支付、
            余额充值、工单与站内通知。遇到问题可提交工单，我们会通过站内通知与邮件回复。
          </p>
        </CardContent>
      </Card>
    </div>
  )
}
