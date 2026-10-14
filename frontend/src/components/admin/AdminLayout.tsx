import { Fragment, useState } from 'react'
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import {
  BellIcon,
  ChevronDownIcon,
  ExternalLinkIcon,
  LayoutDashboardIcon,
  LifeBuoyIcon,
  LogOutIcon,
  PackageIcon,
  ReceiptTextIcon,
  ScrollTextIcon,
  ServerIcon,
  SettingsIcon,
  UsersIcon,
} from 'lucide-react'

import { adminNavItems, adminOplogNavItems, paths } from '@/app/paths'
import { useAdminAuth } from '@/auth/adminAuthContext'
import UpdateDot from '@/components/admin/UpdateDot'
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
import { resetAdminUpdateStatus, useAdminUpdateStatusEffect } from '@/hooks/useAdminUpdateStatus'
import {
  ADMIN_ROLE_LABELS,
  ADMIN_ROLE_DESCRIPTIONS,
  canAccessAdminPage,
  hasPermission,
} from '@/lib/adminRoles'
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
  [paths.adminOperationLogs]: ScrollTextIcon,
  [paths.adminNotifications]: BellIcon,
}

// 侧栏选中态：3px 蓝色左边条 + 浅蓝底 + 蓝字（未选中项保留同宽透明边条，保证文字对齐不位移）。
function sidebarLinkClass({ isActive }: { isActive: boolean }): string {
  return cn(
    'flex items-center gap-2.5 rounded-lg border-l-[3px] px-3 py-2 text-sm transition-colors',
    isActive
      ? 'border-primary bg-accent font-medium text-primary'
      : 'border-transparent text-muted-foreground hover:bg-muted hover:text-foreground',
  )
}

/**
 * OplogNavGroup 是侧栏的「日志」入口（R18 批 2，契约 23.11）：**点击展开**三个通道子项
 * （用户日志 / 定时任务日志 / 管理员日志），三项进入同一个日志页、只是预置的 `channel` 不同。
 * 展开状态默认跟随当前地址（在日志页时自动展开），也允许手动折叠/展开。
 */
function OplogNavGroup() {
  const location = useLocation()
  const onLogsPage = location.pathname === paths.adminOperationLogs
  const currentChannel = new URLSearchParams(location.search).get('channel') ?? ''
  const [manual, setManual] = useState<boolean | null>(null)
  const expanded = manual ?? onLogsPage

  return (
    <div>
      <button
        type="button"
        aria-expanded={expanded}
        onClick={() => setManual(!expanded)}
        className={cn(
          'flex w-full items-center gap-2.5 rounded-lg border-l-[3px] px-3 py-2 text-sm transition-colors',
          onLogsPage
            ? 'border-primary bg-accent font-medium text-primary'
            : 'border-transparent text-muted-foreground hover:bg-muted hover:text-foreground',
        )}
      >
        <ScrollTextIcon className="size-4.5" aria-hidden />
        日志
        <ChevronDownIcon
          className={cn('ml-auto size-3.5 transition-transform', expanded && 'rotate-180')}
          aria-hidden
        />
      </button>
      {expanded ? (
        <div className="mt-1 space-y-1 pl-4">
          {adminOplogNavItems.map((item) => {
            const active = onLogsPage && currentChannel === item.channel
            return (
              <Link
                key={item.channel}
                to={item.to}
                aria-current={active ? 'page' : undefined}
                className={cn(
                  'block rounded-lg border-l-[3px] px-3 py-1.5 text-sm transition-colors',
                  active
                    ? 'border-primary bg-accent font-medium text-primary'
                    : 'border-transparent text-muted-foreground hover:bg-muted hover:text-foreground',
                )}
              >
                {item.label}
              </Link>
            )
          })}
        </div>
      ) : null}
    </div>
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
  // 系统更新红点（契约 19.7）：仅 admin（settings.access）读取更新缓存，其他角色不发任何更新相关请求（19.6）。
  const { hasUpdate } = useAdminUpdateStatusEffect(hasPermission(role, 'settings.access'))

  const navItems = adminNavItems.filter((item) => canAccessAdminPage(role, item.page))
  const displayName = admin?.nickname || admin?.username || '管理员'

  const handleLogout = () => {
    logout()
    // 清空未读徽章，避免下一位登录者看到上一位管理员的未读数。
    resetAdminUnreadCount()
    // 同理清空系统更新红点状态，避免下一位登录者沿用上一位的更新结果。
    resetAdminUpdateStatus()
    navigate(paths.adminLogin, { replace: true })
  }

  return (
    <div className="flex min-h-svh flex-col bg-background">
      <header className="sticky top-0 z-40 border-b border-border bg-card/90 backdrop-blur">
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
                  {/* 昵称与角色名相同时只显示一次，避免顶栏出现两个「超级管理员」。 */}
                  {role && ADMIN_ROLE_LABELS[role] !== displayName ? (
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
              if (item.page === 'logs') {
                // 「日志」是**可展开入口**（点击展开三个通道），不是一个链接。
                return <OplogNavGroup key={item.to} />
              }
              const Icon = NAV_ICONS[item.to] ?? LayoutDashboardIcon
              return (
                <NavLink key={item.to} to={item.to} end={item.end} className={sidebarLinkClass}>
                  <Icon className="size-4.5" aria-hidden />
                  {item.label}
                  {item.to === paths.adminNotifications ? <UnreadBadge count={unread} /> : null}
                  {item.to === paths.adminSettings && hasUpdate ? (
                    <UpdateDot className="ml-auto" />
                  ) : null}
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
            {navItems.map((item) =>
              item.page === 'logs' ? (
                // 移动端是横向滚动条：折叠面板不好用，直接把三个通道入口平铺出来。
                <Fragment key={item.to}>
                  {adminOplogNavItems.map((sub) => (
                    <NavLink
                      key={sub.channel}
                      to={sub.to}
                      className={({ isActive }) =>
                        cn(
                          'flex shrink-0 items-center gap-1.5 rounded-full border px-3 py-1.5 text-xs',
                          isActive
                            ? 'border-transparent bg-primary text-primary-foreground'
                            : 'border-border text-muted-foreground',
                        )
                      }
                    >
                      {sub.label}
                    </NavLink>
                  ))}
                </Fragment>
              ) : (
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
                  {item.to === paths.adminSettings && hasUpdate ? <UpdateDot /> : null}
                </NavLink>
              ),
            )}
          </nav>
          <Outlet />
        </div>
      </div>
    </div>
  )
}
