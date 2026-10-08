import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'

import { fetchMe, loginMember, registerMember } from '../api/auth'
import { onUnauthorized } from '../api/client'
import {
  clearMemberToken,
  getMemberProfile,
  getMemberToken,
  setMemberProfile,
  setMemberToken,
} from '../api/tokens'
import type { Member, RegisterInput } from '../api/types'
import { AuthContext, type AuthContextValue } from './authContext'

// AuthProvider 维护会员登录态：
//   1. 首次进入：有 token 就校验并拉取 /members/me（本地缓存仅作首屏占位）；
//   2. 登录/注册：签发 token 后立即拉取完整会员对象；
//   3. 任意请求返回 401：api 层清 token 并通知这里同步清空登录态（守卫据此跳登录页）。
export default function AuthProvider({ children }: { children: ReactNode }) {
  // 初始值直接取本地缓存（首屏不闪烁）；没有缓存就是未登录。
  const [member, setMember] = useState<Member | null>(() => {
    const cached = getMemberProfile<Member>()
    return cached?.id ? cached : null
  })
  const [initializing, setInitializing] = useState(() => Boolean(getMemberToken()))

  const applyMember = useCallback((next: Member) => {
    setMember(next)
    setMemberProfile(next)
  }, [])

  const clearMember = useCallback(() => {
    clearMemberToken()
    setMember(null)
  }, [])

  const reload = useCallback(async () => {
    if (!getMemberToken()) {
      setMember(null)
      return
    }
    try {
      applyMember(await fetchMe())
    } catch (error) {
      // 401/403（token 失效、账号被禁用）→ 清登录态；网络异常保留本地缓存不误踢。
      const code = (error as { code?: number }).code
      if (code === 401 || code === 403) {
        clearMember()
      }
    }
  }, [applyMember, clearMember])

  // 首次挂载：带 token 时校验一次（缓存资料已在上面的 useState 初始值里生效）。
  const bootstrapped = useRef(false)
  useEffect(() => {
    if (bootstrapped.current || !getMemberToken()) {
      return
    }
    bootstrapped.current = true
    void reload().finally(() => setInitializing(false))
  }, [reload])

  // token 失效通知：清空登录态。
  useEffect(
    () =>
      onUnauthorized(() => {
        setMember(null)
      }),
    [],
  )

  const login = useCallback(
    async (username: string, password: string) => {
      const result = await loginMember(username, password)
      setMemberToken(result.token)
      const profile = await fetchMe()
      applyMember(profile)
    },
    [applyMember],
  )

  const register = useCallback(
    async (input: RegisterInput) => {
      await registerMember(input)
      // 注册接口不签发 token，紧接着登录一次拿到登录态。
      await login(input.username, input.password)
    },
    [login],
  )

  const logout = useCallback(() => {
    clearMember()
  }, [clearMember])

  const setBalance = useCallback((balance: string) => {
    setMember((prev) => {
      if (!prev) {
        return prev
      }
      const next = { ...prev, balance }
      setMemberProfile(next)
      return next
    })
  }, [])

  const value = useMemo<AuthContextValue>(
    () => ({
      member,
      initializing,
      isAuthenticated: member !== null,
      login,
      register,
      logout,
      reload,
      setBalance,
    }),
    [member, initializing, login, register, logout, reload, setBalance],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}
