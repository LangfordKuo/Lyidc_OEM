import { Button, Chip } from '@heroui/react'
import { Link, NavLink, Outlet, useNavigate } from 'react-router-dom'

import { adminNavItems, paths } from '../../app/paths'
import { useAdminAuth } from '../../auth/adminAuthContext'
import { useAdminUnreadCountEffect } from '../../hooks/useAdminUnreadCount'
import { ADMIN_ROLE_LABELS, canAccessAdminPage } from '../../lib/adminRoles'
import { SITE_NAME } from '../../lib/site'
import {
  IconBell,
  IconDashboard,
  IconLogout,
  IconOrders,
  IconProduct,
  IconServer,
  IconSettings,
  IconTicket,
  IconUsers,
} from '../common/icons'

const NAV_ICONS: Record<string, typeof IconDashboard> = {
  [paths.admin]: IconDashboard,
  [paths.adminProducts]: IconProduct,
  [paths.adminOrders]: IconOrders,
  [paths.adminMembers]: IconUsers,
  [paths.adminInstances]: IconServer,
  [paths.adminTickets]: IconTicket,
  [paths.adminSettings]: IconSettings,
  [paths.adminNotifications]: IconBell,
}

function sidebarLinkClass({ isActive }: { isActive: boolean }): string {
  return [
    'flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm transition-colors',
    isActive
      ? 'bg-accent-soft font-medium text-accent-soft-foreground'
      : 'text-muted hover:bg-surface-secondary hover:text-foreground',
  ].join(' ')
}

/** 未读通知徽章：0 条时不渲染。 */
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

// AdminLayout 是管理后台布局：顶栏（后台标识 + 当前账号 + 退出）+ 侧栏 + 内容区。
// 侧栏按**角色矩阵**过滤：无权限的整页（工单 / 设置）直接不渲染入口。
export default function AdminLayout() {
  const { admin, role, logout } = useAdminAuth()
  const navigate = useNavigate()
  const unread = useAdminUnreadCountEffect(true)

  const navItems = adminNavItems.filter((item) => canAccessAdminPage(role, item.page))

  const handleLogout = () => {
    logout()
    navigate(paths.adminLogin, { replace: true })
  }

  return (
    <div className="flex min-h-screen flex-col bg-background">
      <header className="sticky top-0 z-40 border-b border-border bg-overlay/85 backdrop-blur">
        <div className="mx-auto flex h-16 w-full max-w-7xl items-center gap-3 px-4 sm:px-6">
          <Link to={paths.admin} className="flex shrink-0 items-center gap-2">
            <span className="grid size-8 place-items-center rounded-lg bg-accent text-sm font-semibold text-accent-foreground">
              领
            </span>
            <span className="text-base font-semibold text-foreground">{SITE_NAME}</span>
            <Chip size="sm" variant="soft" color="accent">
              管理后台
            </Chip>
          </Link>

          <div className="ml-auto flex items-center gap-2">
            {admin ? (
              <>
                <span className="hidden text-sm text-muted sm:inline">
                  {admin.nickname || admin.username}
                </span>
                <Chip size="sm" variant="soft" color="default">
                  {ADMIN_ROLE_LABELS[admin.role] ?? admin.role}
                </Chip>
              </>
            ) : null}
            <Button variant="ghost" size="sm" onPress={() => navigate(paths.home)}>
              官网
            </Button>
            <Button variant="outline" size="sm" onPress={handleLogout}>
              <IconLogout className="size-4" />
              退出
            </Button>
          </div>
        </div>
      </header>

      <div className="mx-auto flex w-full max-w-7xl flex-1 gap-6 px-4 py-6 sm:px-6">
        <aside className="hidden w-52 shrink-0 md:block">
          <nav className="sticky top-24 space-y-1" aria-label="管理后台导航">
            {navItems.map((item) => {
              const Icon = NAV_ICONS[item.to] ?? IconDashboard
              return (
                <NavLink key={item.to} to={item.to} end={item.end} className={sidebarLinkClass}>
                  <Icon className="size-4.5" />
                  {item.label}
                  {item.to === paths.adminNotifications ? <UnreadBadge count={unread} /> : null}
                </NavLink>
              )
            })}
          </nav>
        </aside>

        <div className="min-w-0 flex-1">
          <nav
            className="-mx-4 mb-4 flex gap-2 overflow-x-auto px-4 pb-1 md:hidden"
            aria-label="管理后台导航（移动端）"
          >
            {navItems.map((item) => (
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
                {item.to === paths.adminNotifications ? (
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
