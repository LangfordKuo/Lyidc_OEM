import { Outlet } from 'react-router-dom'

import SiteFooter from './SiteFooter'
import SiteHeader from './SiteHeader'

// SiteLayout 是官网公共布局：顶栏 + 内容区 + 页脚，内容区撑满剩余高度。
export default function SiteLayout() {
  return (
    <div className="flex min-h-svh flex-col">
      <SiteHeader />
      <main className="flex-1">
        <Outlet />
      </main>
      <SiteFooter />
    </div>
  )
}
