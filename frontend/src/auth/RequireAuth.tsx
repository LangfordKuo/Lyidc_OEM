import { Spinner } from '@heroui/react'
import { Navigate, Outlet, useLocation } from 'react-router-dom'

import { buildLoginUrl } from '../lib/redirect'
import { useAuth } from './authContext'

// RequireAuth 是会员区路由守卫：未登录跳登录页并带上回跳地址。
export default function RequireAuth() {
  const { isAuthenticated, initializing } = useAuth()
  const location = useLocation()

  if (initializing) {
    return (
      <div className="flex min-h-[50vh] items-center justify-center gap-3 text-muted">
        <Spinner size="md" />
        <span className="text-sm">正在校验登录状态…</span>
      </div>
    )
  }

  if (!isAuthenticated) {
    const target = `${location.pathname}${location.search}`
    return <Navigate to={buildLoginUrl(target)} replace />
  }

  return <Outlet />
}
