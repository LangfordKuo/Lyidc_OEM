import { Navigate, Outlet, useLocation } from 'react-router-dom'

import { LoadingBlock } from '../components/common/PageState'
import { buildLoginUrl } from '../lib/redirect'
import { useAuth } from './authContext'

// RequireAuth 是会员路由守卫：未登录跳登录页并带 redirect 回跳参数。
// 首屏校验中先展示加载态，避免「有 token 但资料未拉到」时闪跳登录页。
export default function RequireAuth() {
  const { isAuthenticated, initializing } = useAuth()
  const location = useLocation()

  if (initializing) {
    return <LoadingBlock label="正在校验登录状态…" />
  }

  if (!isAuthenticated) {
    const target = `${location.pathname}${location.search}`
    return <Navigate to={buildLoginUrl(target)} replace />
  }

  return <Outlet />
}
