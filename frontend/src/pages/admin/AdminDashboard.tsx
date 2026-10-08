import { Card, Chip } from '@heroui/react'
import { Link } from 'react-router-dom'

import { fetchUpstreamHealth } from '../../api/adminSettings'
import { listAdminInstances } from '../../api/adminInstances'
import { listAdminLedger, listAdminMembers, listAdminRecharges } from '../../api/adminMembers'
import { listAdminOrders } from '../../api/adminOrders'
import { listAdminTickets } from '../../api/adminTickets'
import type { AdminOrder, Recharge, Ticket } from '../../api/types'
import { paths } from '../../app/paths'
import { useAdminAuth } from '../../auth/adminAuthContext'
import StatCard from '../../components/admin/StatCard'
import { ErrorState } from '../../components/common/PageState'
import { useAsync } from '../../hooks/useAsync'
import { ADMIN_ROLE_DESCRIPTIONS, ADMIN_ROLE_LABELS, hasPermission } from '../../lib/adminRoles'
import { formatDateTime, formatMoney } from '../../lib/format'
import { ORDER_STATUS_TONES, orderStatusLabel } from '../../lib/orderStatus'
import { ticketStatusLabel, ticketStatusTone } from '../../lib/ticketStatus'
import StatusBadge from '../../components/StatusBadge'

// AdminDashboard 是管理后台仪表盘：只用**既有接口能取到的**指标。
//
// 口径说明（不造假）：
//   - 会员总数 / 订单数 / 实例数 / 待处理工单 / 已到账充值单 / 流水条数：均由对应列表接口的
//     total 取得（带 page_size=1 只读总数）；
//   - 订单指标（阶段 8b 补齐）：订单总数 / 待支付 / 交付失败 + 最近订单（GET /admin/orders）；
//   - 上游探活：GET /admin/upstream/health（仅 admin）；
//   - 收入趋势 / 日活等统计类指标仍**没有接口**，本页不做估算（页脚注明）。
export default function AdminDashboard() {
  const { admin, role } = useAdminAuth()

  const canTickets = hasPermission(role, 'tickets.access')
  const canFinance = hasPermission(role, 'finance.reconcile')

  // 会员总数（三类角色均可读）。
  const membersState = useAsync(
    () => listAdminMembers({ page: 1, page_size: 1 }),
    [],
    hasPermission(role, 'members.read'),
  )
  // 实例指标：总数 + 运行中 + 已暂停（三次轻量查询，只看 total）。
  const instancesState = useAsync(() => listAdminInstances({ page: 1, page_size: 1 }), [], Boolean(role))
  const activeInstancesState = useAsync(
    () => listAdminInstances({ page: 1, page_size: 1, status: 'active' }),
    [],
    Boolean(role),
  )
  const suspendedInstancesState = useAsync(
    () => listAdminInstances({ page: 1, page_size: 1, status: 'suspended' }),
    [],
    Boolean(role),
  )
  // 订单指标（阶段 8b）：总数 / 待交付（paid，尚未触发交付）/ 交付失败 + 最近订单
  // （查看类接口，三角色均可读）。
  const canOrders = hasPermission(role, 'orders.read')
  const ordersState = useAsync(
    () => listAdminOrders({ page: 1, page_size: 1 }),
    [],
    canOrders,
  )
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
  const recentOrdersState = useAsync(
    () => listAdminOrders({ page: 1, page_size: 5 }),
    [],
    canOrders,
  )
  // 待处理工单（客服域：仅 admin / support）。
  const openTicketsState = useAsync(
    () => listAdminTickets({ page: 1, page_size: 1, status: 'open' }),
    [],
    canTickets,
  )
  // 最近工单（取 5 条，给客服一个待办入口）。
  const recentTicketsState = useAsync(
    () => listAdminTickets({ page: 1, page_size: 5 }),
    [],
    canTickets,
  )
  // 财务：已到账充值单总数 + 最近充值单。
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
  // 上游连通性：契约第 9 节的鉴权口径与 /admin/profile 一致（三类角色都可只读探活）。
  const upstreamState = useAsync(fetchUpstreamHealth, [], Boolean(role))

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
          <h1 className="text-xl font-semibold text-foreground">仪表盘</h1>
          <p className="mt-1 text-sm text-muted">
            欢迎，{admin?.nickname || admin?.username || '管理员'}
            {role ? ` · ${ADMIN_ROLE_LABELS[role]}` : ''}
          </p>
        </div>
        {role ? (
          <Chip size="sm" variant="soft" color="accent">
            {role}
          </Chip>
        ) : null}
      </header>

      {role ? (
        <Card variant="secondary">
          <Card.Content>
            <p className="text-sm text-muted">
              当前角色权限：{ADMIN_ROLE_DESCRIPTIONS[role]}
            </p>
          </Card.Content>
        </Card>
      ) : null}

      {loadError ? <ErrorState message={loadError} onRetry={membersState.reload} /> : null}

      <section>
        <h2 className="mb-3 text-sm font-medium text-muted">关键指标</h2>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <StatCard
            label="会员总数"
            value={membersState.data?.total}
            loading={membersState.loading}
            hint="全站会员账号（含已禁用）"
          />
          <StatCard
            label="订单总数"
            value={canOrders ? ordersState.data?.total : null}
            loading={ordersState.loading}
            placeholder={canOrders ? '—' : '无权限'}
            hint={
              canOrders
                ? `待交付 ${paidOrdersState.data?.total ?? '—'} · 交付失败 ${
                    failedOrdersState.data?.total ?? '—'
                  }`
                : undefined
            }
          />
          <StatCard
            label="实例总数"
            value={instancesState.data?.total}
            loading={instancesState.loading}
            hint={`运行中 ${activeInstancesState.data?.total ?? '—'} · 已暂停 ${
              suspendedInstancesState.data?.total ?? '—'
            }`}
          />
          <StatCard
            label="待处理工单"
            value={canTickets ? openTicketsState.data?.total : null}
            loading={openTicketsState.loading}
            placeholder={canTickets ? '—' : '无权限'}
          />
          <StatCard
            label="已到账充值"
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
            <Card.Header>
              <Card.Title className="text-base">最近订单</Card.Title>
              <Card.Description>新建在前，点击进入详情查看交付信息。</Card.Description>
            </Card.Header>
            <Card.Content className="space-y-3">
              {recentOrdersState.loading ? (
                <p className="text-sm text-muted">正在读取订单…</p>
              ) : null}
              {!recentOrdersState.loading && (recentOrdersState.data?.items.length ?? 0) === 0 ? (
                <p className="text-sm text-muted">暂无订单。</p>
              ) : null}
              {(recentOrdersState.data?.items ?? []).map((order: AdminOrder) => (
                <div key={order.id} className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <Link
                      className="block truncate font-mono text-sm text-foreground hover:text-accent"
                      to={paths.adminOrderDetail(order.id)}
                    >
                      {order.trade_no}
                    </Link>
                    <p className="mt-0.5 text-xs text-muted">
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
            </Card.Content>
          </Card>
        ) : null}

        {canTickets ? (
          <Card>
            <Card.Header>
              <Card.Title className="text-base">最近工单</Card.Title>
              <Card.Description>按最近活动排序，点击进入处理。</Card.Description>
            </Card.Header>
            <Card.Content className="space-y-3">
              {recentTicketsState.loading ? (
                <p className="text-sm text-muted">正在读取工单…</p>
              ) : null}
              {!recentTicketsState.loading && (recentTicketsState.data?.items.length ?? 0) === 0 ? (
                <p className="text-sm text-muted">暂无工单。</p>
              ) : null}
              {(recentTicketsState.data?.items ?? []).map((ticket: Ticket) => (
                <div key={ticket.id} className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <Link
                      className="block truncate text-sm text-foreground hover:text-accent"
                      to={paths.adminTicketDetail(ticket.id)}
                    >
                      {ticket.subject}
                    </Link>
                    <p className="mt-0.5 text-xs text-muted">
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
            </Card.Content>
          </Card>
        ) : null}

        {canFinance ? (
          <Card>
            <Card.Header>
              <Card.Title className="text-base">最近充值单</Card.Title>
              <Card.Description>全站充值记录（对账用）。</Card.Description>
            </Card.Header>
            <Card.Content className="space-y-3">
              {recentRechargesState.loading ? (
                <p className="text-sm text-muted">正在读取充值单…</p>
              ) : null}
              {!recentRechargesState.loading &&
              (recentRechargesState.data?.items.length ?? 0) === 0 ? (
                <p className="text-sm text-muted">暂无充值单。</p>
              ) : null}
              {(recentRechargesState.data?.items ?? []).map((recharge: Recharge) => (
                <div key={recharge.id} className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <p className="truncate font-mono text-sm text-foreground">{recharge.trade_no}</p>
                    <p className="mt-0.5 text-xs text-muted">
                      会员 #{recharge.member_id} · {formatDateTime(recharge.created_at)}
                    </p>
                  </div>
                  <span className="text-sm text-foreground">{formatMoney(recharge.amount)}</span>
                </div>
              ))}
            </Card.Content>
          </Card>
        ) : null}
      </section>

      <section className="grid gap-4 lg:grid-cols-2">
        {role ? (
          <Card>
            <Card.Header>
              <Card.Title className="text-base">上游连通性</Card.Title>
              <Card.Description>按当前后台设置发起一次只读探活（登录 + 查余额）。</Card.Description>
            </Card.Header>
            <Card.Content className="space-y-2 text-sm">
              {upstreamState.loading ? <p className="text-muted">正在探活…</p> : null}
              {upstreamState.error ? (
                <p className="text-danger">探活失败：{upstreamState.error}</p>
              ) : null}
              {upstreamState.data ? (
                <>
                  <p className="flex items-center gap-2">
                    <StatusBadge
                      tone={upstreamState.data.connected ? 'ok' : 'error'}
                      label={upstreamState.data.connected ? '已连通' : '未连通'}
                    />
                    <span className="text-muted">{upstreamState.data.base_url || '未配置上游'}</span>
                  </p>
                  <p className="text-xs text-muted">
                    耗时 {upstreamState.data.latency_ms} ms · 密钥{' '}
                    {upstreamState.data.api_key_masked || '未配置'}
                  </p>
                  {upstreamState.data.error ? (
                    <p className="text-xs text-danger">{upstreamState.data.error}</p>
                  ) : null}
                </>
              ) : null}
            </Card.Content>
          </Card>
        ) : null}

        <Card variant="secondary">
          <Card.Header>
            <Card.Title className="text-base">暂缺的指标（接口未提供）</Card.Title>
            <Card.Description>以下数据在契约中没有接口，本页不做近似或估算。</Card.Description>
          </Card.Header>
          <Card.Content className="space-y-2 text-sm text-muted">
            <p>
              <span className="text-foreground">收入趋势 / 日活</span>
              ：无统计类接口，不做前端估算（订单类指标已由阶段 8b 的管理端订单接口补齐）。
            </p>
          </Card.Content>
        </Card>
      </section>
    </div>
  )
}
