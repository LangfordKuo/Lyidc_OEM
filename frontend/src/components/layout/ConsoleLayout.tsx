import { NavLink, Outlet } from 'react-router-dom'

import { consoleNavItems, paths } from '../../app/paths'
import { useUnreadCountEffect } from '../../hooks/useUnreadCount'
import {
  IconBell,
  IconDashboard,
  IconOrders,
  IconServer,
  IconTicket,
  IconWallet,
} from '../common/icons'
import SiteHeader from './SiteHeader'

const NAV_ICONS: Record<string, typeof IconDashboard> = {
  [paths.console]: IconDashboard,
  [paths.consoleServers]: IconServer,
  [paths.consoleOrders]: IconOrders,
  [paths.consoleRecharge]: IconWallet,
  [paths.consoleTickets]: IconTicket,
  [paths.consoleNotifications]: IconBell,
}

function sidebarLinkClass({ isActive }: { isActive: boolean }): string {
  return [
    'flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm transition-colors',
    isActive
      ? 'bg-accent-soft font-medium text-accent-soft-foreground'
      : 'text-muted hover:bg-surface-secondary hover:text-foreground',
  ].join(' ')
}

/** 未读通知徽章：0 条时不渲染，避免侧栏出现无意义的「0」。 */
function UnreadBadge({ count }: { count: number }) {
  if (count <= 0) {
    return null
  }
  return (
    <span className="ml-auto rounded-full bg-danger px-1.5 py-0.5 text-[10px] leading-4 font-medium text-danger-foreground">
      {count > 99 ? '99+' : count}
    </span>
  )
}

// ConsoleLayout 是会员区布局：顶栏 + 侧栏（移动端改为横向滚动导航）+ 内容区。
// 侧栏「通知」入口带未读徽章（进入会员区时拉取一次，已读操作后由通知页刷新）。
export default function ConsoleLayout() {
  const unread = useUnreadCountEffect(true)

  return (
    <div className="flex min-h-screen flex-col bg-background">
      <SiteHeader />
      <div className="mx-auto flex w-full max-w-6xl flex-1 gap-6 px-4 py-6 sm:px-6">
        <aside className="hidden w-56 shrink-0 md:block">
          <nav className="sticky top-24 space-y-1" aria-label="会员区导航">
            {consoleNavItems.map((item) => {
              const Icon = NAV_ICONS[item.to] ?? IconDashboard
              return (
                <NavLink key={item.to} to={item.to} end={item.end} className={sidebarLinkClass}>
                  <Icon className="size-4.5" />
                  {item.label}
                  {item.to === paths.consoleNotifications ? <UnreadBadge count={unread} /> : null}
                </NavLink>
              )
            })}
          </nav>
        </aside>

        <div className="min-w-0 flex-1">
          <nav
            className="-mx-4 mb-4 flex gap-2 overflow-x-auto px-4 pb-1 md:hidden"
            aria-label="会员区导航（移动端）"
          >
            {consoleNavItems.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.end}
                className={({ isActive }) =>
                  [
                    'flex shrink-0 items-center gap-1.5 rounded-full border px-3 py-1.5 text-xs',
                    isActive
                      ? 'border-transparent bg-accent text-accent-foreground'
                      : 'border-border text-muted',
                  ].join(' ')
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
    </div>
  )
}
