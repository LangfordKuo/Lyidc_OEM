import { Button } from '@heroui/react'
import { useState } from 'react'
import { Link, NavLink, useNavigate } from 'react-router-dom'

import { paths } from '../../app/paths'
import { useAuth } from '../../auth/authContext'
import { SITE_NAME } from '../../lib/site'
import { IconClose, IconMenu } from '../common/icons'
import UserMenu from './UserMenu'

const NAV_ITEMS = [
  { to: paths.home, label: '首页', end: true },
  { to: paths.products, label: '商品', end: false },
  { to: paths.console, label: '控制台', end: false },
]

function navLinkClass({ isActive }: { isActive: boolean }): string {
  return [
    'rounded-md px-3 py-2 text-sm font-medium transition-colors',
    isActive ? 'bg-accent-soft text-accent-soft-foreground' : 'text-muted hover:text-foreground',
  ].join(' ')
}

// SiteHeader 是官网公共顶栏：站点名 + 导航 + 登录注册/用户菜单，移动端折叠为抽屉。
export default function SiteHeader() {
  const { isAuthenticated } = useAuth()
  const navigate = useNavigate()
  const [mobileOpen, setMobileOpen] = useState(false)

  return (
    <header className="sticky top-0 z-40 border-b border-border bg-overlay/85 backdrop-blur">
      <div className="mx-auto flex h-16 max-w-6xl items-center gap-4 px-4 sm:px-6">
        <Link to={paths.home} className="flex shrink-0 items-center gap-2" aria-label={SITE_NAME}>
          <span className="grid size-8 place-items-center rounded-lg bg-accent text-sm font-semibold text-accent-foreground">
            岭
          </span>
          <span className="text-base font-semibold text-foreground">{SITE_NAME}</span>
        </Link>

        <nav className="hidden items-center gap-1 md:flex" aria-label="主导航">
          {NAV_ITEMS.map((item) => (
            <NavLink key={item.to} to={item.to} end={item.end} className={navLinkClass}>
              {item.label}
            </NavLink>
          ))}
        </nav>

        <div className="ml-auto flex items-center gap-2">
          {isAuthenticated ? (
            <UserMenu />
          ) : (
            <div className="hidden items-center gap-2 md:flex">
              <Button variant="ghost" size="sm" onPress={() => navigate(paths.login)}>
                登录
              </Button>
              <Button variant="primary" size="sm" onPress={() => navigate(paths.register)}>
                免费注册
              </Button>
            </div>
          )}

          <Button
            className="md:hidden"
            isIconOnly
            variant="ghost"
            size="sm"
            aria-label={mobileOpen ? '关闭菜单' : '打开菜单'}
            aria-expanded={mobileOpen}
            onPress={() => setMobileOpen((open) => !open)}
          >
            {mobileOpen ? <IconClose /> : <IconMenu />}
          </Button>
        </div>
      </div>

      {mobileOpen ? (
        <div className="border-t border-border bg-overlay px-4 py-3 md:hidden">
          <nav className="flex flex-col gap-1" aria-label="移动端导航">
            {NAV_ITEMS.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.end}
                className={navLinkClass}
                onClick={() => setMobileOpen(false)}
              >
                {item.label}
              </NavLink>
            ))}
          </nav>
          {isAuthenticated ? null : (
            <div className="mt-3 flex gap-2">
              <Button
                fullWidth
                variant="outline"
                size="sm"
                onPress={() => {
                  setMobileOpen(false)
                  navigate(paths.login)
                }}
              >
                登录
              </Button>
              <Button
                fullWidth
                variant="primary"
                size="sm"
                onPress={() => {
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
