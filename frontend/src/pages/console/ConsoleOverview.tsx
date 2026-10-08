import { Button, Card, Chip, Separator } from '@heroui/react'
import { useNavigate } from 'react-router-dom'

import { fetchBalance } from '../../api/finance'
import { paths } from '../../app/paths'
import { useAuth } from '../../auth/authContext'
import HealthPanel from '../../components/HealthPanel'
import {
  IconArrowRight,
  IconBell,
  IconOrders,
  IconServer,
  IconTicket,
  IconWallet,
} from '../../components/common/icons'
import { useAsync } from '../../hooks/useAsync'
import { formatMoney } from '../../lib/format'

const SHORTCUTS = [
  { to: paths.consoleServers, label: '我的服务器', icon: IconServer, hint: '实例状态与操作' },
  { to: paths.consoleOrders, label: '我的订单', icon: IconOrders, hint: '支付与交付进度' },
  { to: paths.consoleRecharge, label: '余额充值', icon: IconWallet, hint: '在线充值入账' },
  { to: paths.consoleTickets, label: '工单', icon: IconTicket, hint: '问题反馈与跟踪' },
  { to: paths.consoleNotifications, label: '通知', icon: IconBell, hint: '站内通知与提醒' },
]

// ConsoleOverview 是会员区概览页：账户信息 + 余额（实时读取）+ 快捷入口 + 服务状态。
export default function ConsoleOverview() {
  const { member } = useAuth()
  const navigate = useNavigate()
  const balanceState = useAsync(fetchBalance, [])

  const balance = balanceState.data?.balance ?? member?.balance ?? '0.00'

  return (
    <div className="space-y-6">
      <Card>
        <Card.Header>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <Card.Title className="text-lg">
                你好，{member?.nickname || member?.username || '会员'}
              </Card.Title>
              <Card.Description>欢迎回到会员控制台</Card.Description>
            </div>
            <Chip size="sm" variant="soft" color="accent">
              会员 ID {member?.id ?? '—'}
            </Chip>
          </div>
        </Card.Header>
        <Card.Content className="space-y-4">
          <div className="flex flex-wrap items-end justify-between gap-4">
            <div>
              <p className="text-sm text-muted">账户余额</p>
              <p className="mt-1 text-2xl font-semibold text-foreground">{formatMoney(balance)}</p>
              {balanceState.error ? (
                <p className="mt-1 text-xs text-danger">余额读取失败：{balanceState.error}</p>
              ) : null}
            </div>
            <div className="flex gap-2">
              <Button variant="primary" size="sm" onPress={() => navigate(paths.consoleRecharge)}>
                去充值
              </Button>
              <Button variant="outline" size="sm" onPress={() => navigate(paths.products)}>
                选购商品
              </Button>
            </div>
          </div>

          <Separator />

          <div className="grid gap-3 sm:grid-cols-2">
            <div className="text-sm">
              <span className="text-muted">邮箱：</span>
              <span className="text-foreground">{member?.email ?? '—'}</span>
            </div>
            <div className="text-sm">
              <span className="text-muted">账号状态：</span>
              <span className="text-foreground">
                {member?.status === 'active' ? '正常' : (member?.status ?? '—')}
              </span>
            </div>
          </div>
        </Card.Content>
      </Card>

      <section>
        <h2 className="mb-3 text-sm font-medium text-muted">快捷入口</h2>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {SHORTCUTS.map(({ to, label, icon: Icon, hint }) => (
            <button
              key={to}
              type="button"
              onClick={() => navigate(to)}
              className="flex items-center gap-3 rounded-xl border border-border bg-surface p-4 text-left transition-colors hover:bg-surface-secondary"
            >
              <span className="grid size-9 place-items-center rounded-lg bg-accent-soft text-accent-soft-foreground">
                <Icon className="size-4.5" />
              </span>
              <span className="min-w-0 flex-1">
                <span className="block text-sm font-medium text-foreground">{label}</span>
                <span className="block truncate text-xs text-muted">{hint}</span>
              </span>
              <IconArrowRight className="size-4 text-muted" />
            </button>
          ))}
        </div>
      </section>

      <section>
        <h2 className="mb-3 text-sm font-medium text-muted">服务状态</h2>
        <HealthPanel />
      </section>

      <Card variant="secondary">
        <Card.Content>
          <p className="text-sm text-muted">
            当前为前端一期（阶段 7a）：官网、认证、商品与下单支付已可用；会员区各功能页将在阶段 7b 填充。
          </p>
        </Card.Content>
      </Card>
    </div>
  )
}
