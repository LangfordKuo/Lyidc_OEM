import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import {
  BellIcon,
  ExternalLinkIcon,
  LayoutDashboardIcon,
  LifeBuoyIcon,
  LogOutIcon,
  PackageIcon,
  ReceiptTextIcon,
  ServerIcon,
  SettingsIcon,
  UsersIcon,
} from 'lucide-react'

import { adminNavItems, paths } from '@/app/paths'
import { useAdminAuth } from '@/auth/adminAuthContext'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { resetAdminUnreadCount, useAdminUnreadCountEffect } from '@/hooks/useAdminUnreadCount'
import { ADMIN_ROLE_LABELS, ADMIN_ROLE_DESCRIPTIONS, canAccessAdminPage } from '@/lib/adminRoles'
import { SITE_NAME } from '@/lib/site'
import { cn } from '@/lib/utils'

const NAV_ICONS: Record<string, typeof LayoutDashboardIcon> = {
  [paths.admin]: LayoutDashboardIcon,
  [paths.adminProducts]: PackageIcon,
  [paths.adminOrders]: ReceiptTextIcon,
  [paths.adminMembers]: UsersIcon,
  [paths.adminInstances]: ServerIcon,
  [paths.adminTickets]: LifeBuoyIcon,
  [paths.adminSettings]: SettingsIcon,
  [paths.adminNotifications]: BellIcon,
}

function sidebarLinkClass({ isActive }: { isActive: boolean }): string {
  return cn(
    'flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm transition-colors',
    isActive
      ? 'bg-primary/10 font-medium text-primary'
      : 'text-muted-foreground hover:bg-muted hover:text-foreground',
  )
}

/** 未读通知徽章：0 条时不渲染。 */
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
 * AdminLayout 是管理后台的**独立布局**（不套官网顶栏/页脚）：顶栏（后台标识 + 账号菜单）
 * + 左侧栏 + 内容区。侧栏按**角色矩阵**过滤：无权限的整页（工单 / 设置）不渲染入口。
 * 移动端侧栏折叠为横向滚动导航（管理端以桌面为主，移动端保证可用）。
 */
export default function AdminLayout() {
  const { admin, role, logout } = useAdminAuth()
  const navigate = useNavigate()
  const unread = useAdminUnreadCountEffect(true)

  const navItems = adminNavItems.filter((item) => canAccessAdminPage(role, item.page))
  const displayName = admin?.nickname || admin?.username || '管理员'

  const handleLogout = () => {
    logout()
    // 清空未读徽章，避免下一位登录者看到上一位管理员的未读数。
    resetAdminUnreadCount()
    navigate(paths.adminLogin, { replace: true })
  }

  return (
    <div className="flex min-h-svh flex-col bg-muted/20">
      <header className="sticky top-0 z-40 border-b border-border bg-background/85 backdrop-blur">
        <div className="mx-auto flex h-14 w-full max-w-[1600px] items-center gap-3 px-4 sm:px-6">
          <NavLink to={paths.admin} className="flex shrink-0 items-center gap-2">
            <span className="grid size-8 place-items-center rounded-lg bg-primary text-sm font-semibold text-primary-foreground">
              领
            </span>
            <span className="text-base font-semibold text-foreground">{SITE_NAME}</span>
            <Badge variant="secondary" className="hidden sm:inline-flex">
              管理后台
            </Badge>
          </NavLink>

          <div className="ml-auto flex items-center gap-2">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => navigate(paths.home)}
              aria-label="返回官网"
            >
              <ExternalLinkIcon aria-hidden />
              <span className="hidden sm:inline">官网</span>
            </Button>

            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="sm" className="gap-2 px-1.5" aria-label="管理端账号菜单">
                  <Avatar className="size-7">
                    <AvatarFallback className="bg-primary text-xs font-medium text-primary-foreground">
                      {displayName.slice(0, 1).toUpperCase()}
                    </AvatarFallback>
                  </Avatar>
                  <span className="hidden max-w-28 truncate sm:inline">{displayName}</span>
                  {role ? (
                    <Badge variant="secondary" className="hidden md:inline-flex">
                      {ADMIN_ROLE_LABELS[role]}
                    </Badge>
                  ) : null}
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-64">
                <DropdownMenuLabel>
                  <span className="block truncate text-sm font-medium">{displayName}</span>
                  <span className="mt-0.5 block text-xs font-normal text-muted-foreground">
                    {admin?.username}
                    {role ? ` · ${ADMIN_ROLE_LABELS[role]}` : ''}
                  </span>
                  {role ? (
                    <span className="mt-1 block text-xs font-normal text-muted-foreground">
                      {ADMIN_ROLE_DESCRIPTIONS[role]}
                    </span>
                  ) : null}
                </DropdownMenuLabel>
                <DropdownMenuSeparator />
                <DropdownMenuItem onSelect={() => navigate(paths.home)}>
                  <ExternalLinkIcon aria-hidden />
                  返回官网
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={handleLogout}>
                  <LogOutIcon aria-hidden />
                  退出登录
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
      </header>

      <div className="mx-auto flex w-full max-w-[1600px] flex-1 gap-6 px-4 py-6 sm:px-6">
        <aside className="hidden w-52 shrink-0 md:block">
          <nav className="sticky top-20 space-y-1" aria-label="管理后台导航">
            {navItems.map((item) => {
              const Icon = NAV_ICONS[item.to] ?? LayoutDashboardIcon
              return (
                <NavLink key={item.to} to={item.to} end={item.end} className={sidebarLinkClass}>
                  <Icon className="size-4.5" aria-hidden />
                  {item.label}
                  {item.to === paths.adminNotifications ? <UnreadBadge count={unread} /> : null}
                </NavLink>
              )
            })}
          </nav>
        </aside>

        <div className="min-w-0 flex-1">
          <nav
            className="mb-4 flex gap-2 overflow-x-auto pb-1 md:hidden"
            aria-label="管理后台导航（移动端）"
          >
            {navItems.map((item) => (
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
                {item.to === paths.adminNotifications ? <UnreadBadge count={unread} /> : null}
              </NavLink>
            ))}
          </nav>
          <Outlet />
        </div>
      </div>
    </div>
  )
}
