import { Link } from 'react-router-dom'
import {
  ArrowRightIcon,
  LifeBuoyIcon,
  ReceiptTextIcon,
  ServerIcon,
  UsersIcon,
  WalletIcon,
  WifiIcon,
} from 'lucide-react'

import { listAdminInstances } from '@/api/adminInstances'
import { listAdminLedger, listAdminMembers, listAdminRecharges } from '@/api/adminMembers'
import { listAdminOrders } from '@/api/adminOrders'
import { fetchUpstreamHealth } from '@/api/adminSettings'
import { listAdminTickets } from '@/api/adminTickets'
import type { AdminOrder, AdminTicket, Recharge } from '@/api/types'
import { paths } from '@/app/paths'
import { useAdminAuth } from '@/auth/adminAuthContext'
import StatCard from '@/components/admin/StatCard'
import { ErrorState } from '@/components/common/PageState'
import StatusBadge from '@/components/common/StatusBadge'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { useAsync } from '@/hooks/useAsync'
import { ADMIN_ROLE_DESCRIPTIONS, ADMIN_ROLE_LABELS, hasPermission } from '@/lib/adminRoles'
import { formatDateTime, formatMoney } from '@/lib/format'
import { ORDER_STATUS_TONES, orderStatusLabel } from '@/lib/orderStatus'
import { ticketStatusLabel, ticketStatusTone } from '@/lib/ticketStatus'

// AdminDashboard 是管理后台仪表盘：只用**既有接口能取到的**指标。
//
// 口径说明（不造假）：
//   - 会员总数 / 实例数 / 待处理工单 / 已到账充值单 / 流水条数：由对应列表接口的 total 取得
//     （带 page_size=1 只读总数）；
//   - 订单指标：订单总数 / 待交付（paid）/ 交付失败 + 最近订单（GET /admin/orders）；
//   - 上游探活：GET /admin/upstream/health（管理端 token，三类角色都可只读探活）；
//   - 收入趋势 / 日活等统计类指标**没有接口**，本页不做估算（卡片内注明）。
// 角色矩阵：无权限的指标不发请求（useAsync 的 enabled=false），卡片显示「无权限」而非数字。
export default function AdminDashboard() {
  const { admin, role } = useAdminAuth()

  const canMembers = hasPermission(role, 'members.read')
  const canOrders = hasPermission(role, 'orders.read')
  const canInstances = hasPermission(role, 'instances.read')
  const canTickets = hasPermission(role, 'tickets.access')
  const canFinance = hasPermission(role, 'finance.reconcile')
  const canProbe = hasPermission(role, 'upstream.probe')

  const membersState = useAsync(() => listAdminMembers({ page: 1, page_size: 1 }), [], canMembers)
  const ordersState = useAsync(() => listAdminOrders({ page: 1, page_size: 1 }), [], canOrders)
  const paidOrdersState = useAsync(
    () => listAdminOrders({ page: 1, page_size: 1, status: 'paid' }),
    [],
    canOrders,
  )
  const failedOrdersState = useAsync(
    () => listAdminOrders({ page: 1, page_size: 1, status: 'failed' }),
    [],
    canOrders,
  )
  const recentOrdersState = useAsync(() => listAdminOrders({ page: 1, page_size: 5 }), [], canOrders)

  const instancesState = useAsync(() => listAdminInstances({ page: 1, page_size: 1 }), [], canInstances)
  const activeInstancesState = useAsync(
    () => listAdminInstances({ page: 1, page_size: 1, status: 'active' }),
    [],
    canInstances,
  )
  const suspendedInstancesState = useAsync(
    () => listAdminInstances({ page: 1, page_size: 1, status: 'suspended' }),
    [],
    canInstances,
  )

  const openTicketsState = useAsync(
    () => listAdminTickets({ page: 1, page_size: 1, status: 'open' }),
    [],
    canTickets,
  )
  const recentTicketsState = useAsync(() => listAdminTickets({ page: 1, page_size: 5 }), [], canTickets)

  const paidRechargesState = useAsync(
    () => listAdminRecharges({ page: 1, page_size: 1, status: 'paid' }),
    [],
    canFinance,
  )
  const recentRechargesState = useAsync(
    () => listAdminRecharges({ page: 1, page_size: 5 }),
    [],
    canFinance,
  )
  const ledgerState = useAsync(() => listAdminLedger({ page: 1, page_size: 1 }), [], canFinance)

  const upstreamState = useAsync(fetchUpstreamHealth, [], canProbe)

  const loadError =
    membersState.error ||
    ordersState.error ||
    instancesState.error ||
    openTicketsState.error ||
    paidRechargesState.error ||
    ''

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-foreground">仪表盘</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            欢迎，{admin?.nickname || admin?.username || '管理员'}
            {role ? ` · ${ADMIN_ROLE_LABELS[role]}` : ''}
          </p>
        </div>
        {role ? <Badge variant="secondary">{ADMIN_ROLE_LABELS[role]}</Badge> : null}
      </header>

      {role ? (
        <Card>
          <CardContent className="text-sm text-muted-foreground">
            当前角色权限：{ADMIN_ROLE_DESCRIPTIONS[role]}
          </CardContent>
        </Card>
      ) : null}

      {loadError ? <ErrorState message={loadError} onRetry={membersState.reload} /> : null}

      <section>
        <h2 className="mb-3 text-sm font-medium text-muted-foreground">关键指标</h2>
        {/* 指标卡布局：xl 一行 5 列；lg 前 3 张各占 1/3、后 2 张各占 1/2 铺满整行；
            sm 下第 5 张占满整行——任何断点都不留大片空白。 */}
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-6 xl:grid-cols-5">
          <StatCard
            label="会员总数"
            className="lg:col-span-2 xl:col-span-1"
            icon={UsersIcon}
            value={canMembers ? membersState.data?.total : null}
            loading={membersState.loading}
            placeholder={canMembers ? '—' : '无权限'}
            hint="全站会员账号（含已禁用）"
          />
          <StatCard
            label="订单总数"
            className="lg:col-span-2 xl:col-span-1"
            icon={ReceiptTextIcon}
            value={canOrders ? ordersState.data?.total : null}
            loading={ordersState.loading}
            placeholder={canOrders ? '—' : '无权限'}
            hint={
              canOrders ? (
                <span>
                  待交付{' '}
                  <span data-testid="stat-paid-orders">{paidOrdersState.data?.total ?? '—'}</span> ·
                  交付失败{' '}
                  <span data-testid="stat-failed-orders">{failedOrdersState.data?.total ?? '—'}</span>
                </span>
              ) : undefined
            }
          />
          <StatCard
            label="实例总数"
            className="lg:col-span-2 xl:col-span-1"
            icon={ServerIcon}
            value={canInstances ? instancesState.data?.total : null}
            loading={instancesState.loading}
            placeholder={canInstances ? '—' : '无权限'}
            hint={`运行中 ${activeInstancesState.data?.total ?? '—'} · 已暂停 ${
              suspendedInstancesState.data?.total ?? '—'
            }`}
          />
          <StatCard
            label="待处理工单"
            className="lg:col-span-3 xl:col-span-1"
            icon={LifeBuoyIcon}
            value={canTickets ? openTicketsState.data?.total : null}
            loading={openTicketsState.loading}
            placeholder={canTickets ? '—' : '无权限'}
            hint={canTickets ? '待客服处理中的工单' : undefined}
          />
          <StatCard
            label="已到账充值"
            className="sm:col-span-2 lg:col-span-3 xl:col-span-1"
            icon={WalletIcon}
            value={canFinance ? paidRechargesState.data?.total : null}
            loading={paidRechargesState.loading}
            placeholder={canFinance ? '—' : '无权限'}
            hint={canFinance ? `流水条数 ${ledgerState.data?.total ?? '—'}` : undefined}
          />
        </div>
      </section>

      <section className="grid gap-4 lg:grid-cols-2">
        {canOrders ? (
          <Card>
            <CardHeader className="border-b">
              <CardTitle className="text-base">最近订单</CardTitle>
              <CardDescription>新建在前，点击进入详情查看交付信息。</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              {recentOrdersState.loading ? <Skeleton className="h-16 w-full" /> : null}
              {!recentOrdersState.loading && (recentOrdersState.data?.items.length ?? 0) === 0 ? (
                <p className="text-sm text-muted-foreground">暂无订单。</p>
              ) : null}
              {(recentOrdersState.data?.items ?? []).map((order: AdminOrder) => (
                <div key={order.id} className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <Link
                      className="block truncate font-mono text-sm text-foreground hover:text-primary"
                      to={paths.adminOrderDetail(order.id)}
                    >
                      {order.trade_no}
                    </Link>
                    <p className="mt-0.5 text-xs text-muted-foreground">
                      {order.member ? order.member.username : `#${order.member_id}`} ·{' '}
                      {formatDateTime(order.created_at)}
                    </p>
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    <span className="text-sm text-foreground">{formatMoney(order.final_amount)}</span>
                    <StatusBadge
                      tone={ORDER_STATUS_TONES[order.status] ?? 'pending'}
                      label={orderStatusLabel(order.status)}
                    />
                  </div>
                </div>
              ))}
              <Button asChild variant="ghost" size="sm" className="w-full">
                <Link to={paths.adminOrders}>
                  查看全部订单
                  <ArrowRightIcon aria-hidden />
                </Link>
              </Button>
            </CardContent>
          </Card>
        ) : null}

        {canTickets ? (
          <Card>
            <CardHeader className="border-b">
              <CardTitle className="text-base">最近工单</CardTitle>
              <CardDescription>按最近活动排序，点击进入处理。</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              {recentTicketsState.loading ? <Skeleton className="h-16 w-full" /> : null}
              {!recentTicketsState.loading && (recentTicketsState.data?.items.length ?? 0) === 0 ? (
                <p className="text-sm text-muted-foreground">暂无工单。</p>
              ) : null}
              {(recentTicketsState.data?.items ?? []).map((ticket: AdminTicket) => (
                <div key={ticket.id} className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <Link
                      className="block truncate text-sm text-foreground hover:text-primary"
                      to={paths.adminTicketDetail(ticket.id)}
                    >
                      {ticket.subject}
                    </Link>
                    <p className="mt-0.5 text-xs text-muted-foreground">
                      <span className="font-mono">{ticket.trade_no}</span> ·{' '}
                      {formatDateTime(ticket.last_reply_at)}
                    </p>
                  </div>
                  <StatusBadge
                    tone={ticketStatusTone(ticket.status)}
                    label={ticketStatusLabel(ticket.status)}
                  />
                </div>
              ))}
              <Button asChild variant="ghost" size="sm" className="w-full">
                <Link to={paths.adminTickets}>
                  查看全部工单
                  <ArrowRightIcon aria-hidden />
                </Link>
              </Button>
            </CardContent>
          </Card>
        ) : null}

        {canFinance ? (
          <Card>
            <CardHeader className="border-b">
              <CardTitle className="text-base">最近充值单</CardTitle>
              <CardDescription>全站充值记录（对账用，明细见会员详情）。</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              {recentRechargesState.loading ? <Skeleton className="h-16 w-full" /> : null}
              {!recentRechargesState.loading &&
              (recentRechargesState.data?.items.length ?? 0) === 0 ? (
                <p className="text-sm text-muted-foreground">暂无充值单。</p>
              ) : null}
              {(recentRechargesState.data?.items ?? []).map((recharge: Recharge) => (
                <div key={recharge.id} className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <p className="truncate font-mono text-sm text-foreground">{recharge.trade_no}</p>
                    <p className="mt-0.5 text-xs text-muted-foreground">
                      会员 #{recharge.member_id} · {formatDateTime(recharge.created_at)}
                    </p>
                  </div>
                  <span className="text-sm text-foreground">{formatMoney(recharge.amount)}</span>
                </div>
              ))}
            </CardContent>
          </Card>
        ) : null}
      </section>

      <section className="grid gap-4 lg:grid-cols-2">
        {canProbe ? (
          <Card>
            <CardHeader className="border-b">
              <CardTitle className="flex items-center gap-2 text-base">
                <WifiIcon className="size-4" aria-hidden />
                上游连通性
              </CardTitle>
              <CardDescription>按当前后台设置发起一次只读探活（登录 + 查余额）。</CardDescription>
            </CardHeader>
            <CardContent className="space-y-2 text-sm">
              {upstreamState.loading ? (
                <p className="text-muted-foreground">正在探活…</p>
              ) : null}
              {upstreamState.error ? (
                <p className="text-destructive">探活失败：{upstreamState.error}</p>
              ) : null}
              {upstreamState.data ? (
                <>
                  <p className="flex items-center gap-2">
                    <StatusBadge
                      tone={upstreamState.data.connected ? 'ok' : 'error'}
                      label={upstreamState.data.connected ? '已连通' : '未连通'}
                    />
                    <span className="text-muted-foreground">
                      {upstreamState.data.base_url || '未配置上游'}
                    </span>
                  </p>
                  <p className="text-xs text-muted-foreground">
                    耗时 {upstreamState.data.latency_ms} ms · 密钥{' '}
                    {upstreamState.data.api_key_masked || '未配置'}
                  </p>
                  {upstreamState.data.error ? (
                    <p className="text-xs text-destructive">{upstreamState.data.error}</p>
                  ) : null}
                </>
              ) : null}
              <Button variant="outline" size="sm" onClick={upstreamState.reload}>
                重新探活
              </Button>
            </CardContent>
          </Card>
        ) : null}

        <Card className="bg-muted/30">
          <CardHeader className="border-b">
            <CardTitle className="text-base">暂缺的指标（接口未提供）</CardTitle>
            <CardDescription>以下数据在契约中没有接口，本页不做近似或估算。</CardDescription>
          </CardHeader>
          <CardContent className="space-y-2 text-sm text-muted-foreground">
            <p>
              <span className="text-foreground">收入趋势 / 日活</span>
              ：无统计类接口，不做前端估算（订单类指标已由管理端订单接口覆盖）。
            </p>
          </CardContent>
        </Card>
      </section>
    </div>
  )
}
