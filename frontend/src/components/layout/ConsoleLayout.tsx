import { NavLink, Outlet } from 'react-router-dom'
import {
  BellIcon,
  LayoutDashboardIcon,
  LifeBuoyIcon,
  ReceiptTextIcon,
  ServerIcon,
  WalletIcon,
} from 'lucide-react'

import { consoleNavItems, paths } from '@/app/paths'
import { useUnreadCountEffect } from '@/hooks/useUnreadCount'
import { cn } from '@/lib/utils'

const NAV_ICONS: Record<string, typeof LayoutDashboardIcon> = {
  [paths.console]: LayoutDashboardIcon,
  [paths.consoleServers]: ServerIcon,
  [paths.consoleOrders]: ReceiptTextIcon,
  [paths.consoleRecharge]: WalletIcon,
  [paths.consoleTickets]: LifeBuoyIcon,
  [paths.consoleNotifications]: BellIcon,
}

function sidebarLinkClass({ isActive }: { isActive: boolean }): string {
  return cn(
    'flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm transition-colors',
    isActive
      ? 'bg-primary/10 font-medium text-primary'
      : 'text-muted-foreground hover:bg-muted hover:text-foreground',
  )
}

/** 未读通知徽章：0 条时不渲染，避免侧栏出现无意义的「0」。 */
function UnreadBadge({ count }: { count: number }) {
  if (count <= 0) {
    return null
  }
  return (
    <span className="ml-auto rounded-full bg-destructive px-1.5 py-0.5 text-[10px] leading-4 font-medium text-white">
      {count > 99 ? '99+' : count}
    </span>
  )
}

/**
 * ConsoleLayout 是会员区布局：官网顶栏（SiteLayout 提供）+ 侧栏 + 内容区。
 * 移动端侧栏折叠为横向滚动导航；「通知」入口带未读徽标（进入会员区时拉取一次，
 * 已读操作后由通知页刷新）。
 */
export default function ConsoleLayout() {
  const unread = useUnreadCountEffect(true)

  return (
    <div className="mx-auto flex w-full max-w-6xl gap-6 px-4 py-6 sm:px-6">
      <aside className="hidden w-56 shrink-0 md:block">
        <nav className="sticky top-20 space-y-1" aria-label="会员区导航">
          {consoleNavItems.map((item) => {
            const Icon = NAV_ICONS[item.to] ?? LayoutDashboardIcon
            return (
              <NavLink key={item.to} to={item.to} end={item.end} className={sidebarLinkClass}>
                <Icon className="size-4.5" aria-hidden />
                {item.label}
                {item.to === paths.consoleNotifications ? <UnreadBadge count={unread} /> : null}
              </NavLink>
            )
          })}
        </nav>
      </aside>

      <div className="min-w-0 flex-1">
        <nav
          className="mb-4 flex gap-2 overflow-x-auto pb-1 md:hidden"
          aria-label="会员区导航（移动端）"
        >
          {consoleNavItems.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.end}
              className={({ isActive }) =>
                cn(
                  'flex shrink-0 items-center gap-1.5 rounded-full border px-3 py-1.5 text-xs',
                  isActive
                    ? 'border-transparent bg-primary text-primary-foreground'
                    : 'border-border text-muted-foreground',
                )
              }
            >
              {item.label}
              {item.to === paths.consoleNotifications ? (
                <UnreadBadge count={unread} />
              ) : null}
            </NavLink>
          ))}
        </nav>
        <Outlet />
      </div>
    </div>
  )
}
