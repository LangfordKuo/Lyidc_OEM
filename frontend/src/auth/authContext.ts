import { createContext, useContext } from 'react'

import type { Member, RegisterInput } from '../api/types'

export interface AuthContextValue {
  /** 当前登录会员（未登录为 null；首屏可能先给本地缓存占位）。 */
  member: Member | null
  /** 首屏正在用 token 校验登录态（守卫在此期间不做跳转，避免闪跳）。 */
  initializing: boolean
  isAuthenticated: boolean
  login: (username: string, password: string) => Promise<void>
  register: (input: RegisterInput) => Promise<void>
  logout: () => void
  reload: () => Promise<void>
  /** 支付/充值后同步本地余额（服务端为准）。 */
  setBalance: (balance: string) => void
}

export const AuthContext = createContext<AuthContextValue | null>(null)

export function useAuth(): AuthContextValue {
  const context = useContext(AuthContext)
  if (!context) {
    throw new Error('useAuth 必须在 AuthProvider 内使用')
  }
  return context
}
