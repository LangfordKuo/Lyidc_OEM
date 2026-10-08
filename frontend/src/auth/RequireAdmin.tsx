import { Navigate, Outlet, useLocation } from 'react-router-dom'

import { LoadingBlock } from '../components/common/PageState'
import { buildAdminLoginUrl } from '../lib/redirect'
import { useAdminAuth } from './adminAuthContext'

// RequireAdmin 是管理后台路由守卫：未登录（或 admin token 失效）跳后台登录页并带回跳地址。
// 注意：它只管「有没有登录」，**不管角色**——角色维度由页面内的权限闸门渲染（隐藏/禁用/无权提示）。
export default function RequireAdmin() {
  const { isAuthenticated, initializing } = useAdminAuth()
  const location = useLocation()

  if (initializing) {
    return <LoadingBlock label="正在校验管理员登录状态…" className="py-24" />
  }

  if (!isAuthenticated) {
    const target = `${location.pathname}${location.search}`
    return <Navigate to={buildAdminLoginUrl(target)} replace />
  }

  return <Outlet />
}
