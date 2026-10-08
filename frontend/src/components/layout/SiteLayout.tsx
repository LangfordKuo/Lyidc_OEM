import { Outlet, useLocation } from 'react-router-dom'

import SiteFooter from './SiteFooter'
import SiteHeader from './SiteHeader'

/**
 * SiteLayout 是官网公共布局：顶栏 + 内容区 + 页脚，内容区撑满剩余高度。
 * 会员区（/console）与管理后台（/admin）不展示官网页脚——页脚只保留给官网公开页。
 * （管理后台自带独立布局，不经本组件；这里同时兜住 /admin 前缀以防路由调整。）
 */
export default function SiteLayout() {
  const { pathname } = useLocation()
  const hideFooter = pathname.startsWith('/console') || pathname.startsWith('/admin')

  return (
    <div className="flex min-h-svh flex-col">
      <SiteHeader />
      <main className="flex-1">
        <Outlet />
      </main>
      {hideFooter ? null : <SiteFooter />}
    </div>
  )
}
