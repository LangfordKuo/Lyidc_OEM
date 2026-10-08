import { NavLink, Outlet } from 'react-router-dom'

import { consoleNavItems, paths } from '../../app/paths'
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

// ConsoleLayout 是会员区骨架布局：顶栏 + 侧栏（移动端改为横向滚动导航）+ 内容区。
// 7a 只提供骨架与占位路由，7b 在各路由内填充真实功能。
export default function ConsoleLayout() {
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
                    'shrink-0 rounded-full border px-3 py-1.5 text-xs',
                    isActive
                      ? 'border-transparent bg-accent text-accent-foreground'
                      : 'border-border text-muted',
                  ].join(' ')
                }
              >
                {item.label}
              </NavLink>
            ))}
          </nav>
          <Outlet />
        </div>
      </div>
    </div>
  )
}
