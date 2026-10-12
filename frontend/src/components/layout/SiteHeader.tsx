import { useState } from 'react'
import { Link, NavLink, useNavigate } from 'react-router-dom'
import { MenuIcon, ServerIcon, XIcon } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { paths } from '@/app/paths'
import { useAuth } from '@/auth/authContext'
import { SITE_NAME } from '@/lib/site'
import { cn } from '@/lib/utils'
import UserMenu from './UserMenu'

function navLinkClass({ isActive }: { isActive: boolean }): string {
  return cn(
    'rounded-lg px-3 py-2 text-sm font-medium transition-colors',
    isActive ? 'bg-accent text-primary' : 'text-muted-foreground hover:bg-muted hover:text-foreground',
  )
}

/** 官网公共顶栏：站点名 + 导航 + 登录注册/用户菜单，移动端折叠为下拉面板。 */
export default function SiteHeader() {
  const { isAuthenticated } = useAuth()
  const navigate = useNavigate()
  const [mobileOpen, setMobileOpen] = useState(false)

  // 主导航仅公开项；「控制台」是登录后入口，放右侧操作区（移动端走下拉面板）。
  const navItems = [
    { to: paths.home, label: '首页', end: true },
    { to: paths.products, label: '商品', end: false },
  ]

  return (
    <header className="sticky top-0 z-40 border-b border-border bg-card/90 backdrop-blur">
      <div className="mx-auto flex h-16 max-w-6xl items-center gap-4 px-4 sm:px-6">
        <Link to={paths.home} className="flex shrink-0 items-center gap-2" aria-label={SITE_NAME}>
          <span className="grid size-8 place-items-center rounded-lg bg-primary text-primary-foreground">
            <ServerIcon className="size-4.5" aria-hidden />
          </span>
          <span className="text-base font-semibold text-foreground">{SITE_NAME}</span>
        </Link>

        <nav className="hidden items-center gap-1 md:flex" aria-label="主导航">
          {navItems.map((item) => (
            <NavLink key={item.label} to={item.to} end={item.end} className={navLinkClass}>
              {item.label}
            </NavLink>
          ))}
        </nav>

        <div className="ml-auto flex items-center gap-2" data-testid="header-actions">
          {isAuthenticated ? (
            <>
              {/* 「控制台」登录后才显示：桌面端在此（移动端入口在下拉面板，避免重复）。 */}
              <NavLink
                to={paths.console}
                className={(state) => cn(navLinkClass(state), 'hidden md:inline-flex')}
              >
                控制台
              </NavLink>
              <UserMenu />
            </>
          ) : (
            <div className="hidden items-center gap-2 md:flex">
              <Button variant="ghost" size="sm" onClick={() => navigate(paths.login)}>
                登录
              </Button>
              <Button size="sm" onClick={() => navigate(paths.register)}>
                免费注册
              </Button>
            </div>
          )}

          <Button
            className="md:hidden"
            variant="ghost"
            size="icon-sm"
            aria-label={mobileOpen ? '关闭菜单' : '打开菜单'}
            aria-expanded={mobileOpen}
            onClick={() => setMobileOpen((open) => !open)}
          >
            {mobileOpen ? <XIcon aria-hidden /> : <MenuIcon aria-hidden />}
          </Button>
        </div>
      </div>

      {mobileOpen ? (
        <div className="border-t border-border bg-card px-4 py-3 md:hidden">
          <nav className="flex flex-col gap-1" aria-label="移动端导航">
            {navItems.map((item) => (
              <NavLink
                key={item.label}
                to={item.to}
                end={item.end}
                className={navLinkClass}
                onClick={() => setMobileOpen(false)}
              >
                {item.label}
              </NavLink>
            ))}
            {isAuthenticated ? (
              <NavLink to={paths.console} className={navLinkClass} onClick={() => setMobileOpen(false)}>
                控制台
              </NavLink>
            ) : null}
          </nav>
          {isAuthenticated ? null : (
            <div className="mt-3 flex gap-2">
              <Button
                className="flex-1"
                variant="outline"
                size="sm"
                onClick={() => {
                  setMobileOpen(false)
                  navigate(paths.login)
                }}
              >
                登录
              </Button>
              <Button
                className="flex-1"
                size="sm"
                onClick={() => {
                  setMobileOpen(false)
                  navigate(paths.register)
                }}
              >
                免费注册
              </Button>
            </div>
          )}
        </div>
      ) : null}
    </header>
  )
}
