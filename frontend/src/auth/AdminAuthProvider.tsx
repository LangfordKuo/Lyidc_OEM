import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'

import { fetchAdminProfile, loginAdmin } from '../api/adminAuth'
import { onUnauthorized } from '../api/client'
import {
  clearAdminToken,
  getAdminProfile,
  getAdminToken,
  setAdminProfile,
  setAdminToken,
} from '../api/tokens'
import type { AdminAccount } from '../api/types'
import { AdminAuthContext, type AdminAuthContextValue } from './adminAuthContext'

// AdminAuthProvider 维护管理端登录态（与会员侧 AuthProvider 同构，互不影响）：
//   1. 首次进入：有 admin token 就拉取 /admin/profile（缓存资料仅作首屏占位）；
//   2. 登录：签发 token 后立即拉取管理员对象（含 role，权限视图据此渲染）；
//   3. 管理端接口返回 401：api 层清 admin token 并通知这里清空登录态。
export default function AdminAuthProvider({ children }: { children: ReactNode }) {
  const [admin, setAdmin] = useState<AdminAccount | null>(() => {
    const cached = getAdminProfile<AdminAccount>()
    return cached?.id ? cached : null
  })
  const [initializing, setInitializing] = useState(() => Boolean(getAdminToken()))

  const applyAdmin = useCallback((next: AdminAccount) => {
    setAdmin(next)
    setAdminProfile(next)
  }, [])

  const clearAdmin = useCallback(() => {
    clearAdminToken()
    setAdmin(null)
  }, [])

  const reload = useCallback(async () => {
    if (!getAdminToken()) {
      setAdmin(null)
      return
    }
    try {
      applyAdmin(await fetchAdminProfile())
    } catch (error) {
      // 401（token 失效）、403（账号被禁用）→ 清登录态；网络异常保留缓存不误踢。
      const code = (error as { code?: number }).code
      if (code === 401 || code === 403) {
        clearAdmin()
      }
    }
  }, [applyAdmin, clearAdmin])

  const bootstrapped = useRef(false)
  useEffect(() => {
    if (bootstrapped.current || !getAdminToken()) {
      return
    }
    bootstrapped.current = true
    void reload().finally(() => setInitializing(false))
  }, [reload])

  useEffect(
    () =>
      onUnauthorized((scope) => {
        if (scope === 'admin') {
          setAdmin(null)
        }
      }),
    [],
  )

  const login = useCallback(
    async (username: string, password: string) => {
      const result = await loginAdmin(username, password)
      setAdminToken(result.token)
      // 登录响应里的 admin 已含 role；再拉一次 profile 以拿到最新状态（失败不阻断登录）。
      try {
        applyAdmin(await fetchAdminProfile())
      } catch {
        applyAdmin(result.admin)
      }
    },
    [applyAdmin],
  )

  const logout = useCallback(() => {
    clearAdmin()
  }, [clearAdmin])

  const value = useMemo<AdminAuthContextValue>(
    () => ({
      admin,
      role: admin?.role ?? null,
      initializing,
      isAuthenticated: admin !== null,
      login,
      logout,
      reload,
    }),
    [admin, initializing, login, logout, reload],
  )

  return <AdminAuthContext.Provider value={value}>{children}</AdminAuthContext.Provider>
}
