import { createContext, useContext } from 'react'

import type { Member, RegisterInput } from '../api/types'

// 会员登录态上下文。Provider 实现见 src/auth/AuthProvider.tsx
// （组件与 hook 分文件，避免 react/only-export-components 告警）。
export interface AuthContextValue {
  /** 当前登录会员；未登录为 null。 */
  member: Member | null
  /** 首次进入时带 token 校验登录态/拉取资料的加载态。 */
  initializing: boolean
  isAuthenticated: boolean
  login: (username: string, password: string) => Promise<void>
  /** 注册成功后自动登录（契约 6.2 的注册接口不返回 token）。 */
  register: (input: RegisterInput) => Promise<void>
  logout: () => void
  /** 重新拉取会员资料（如支付后再取余额）。 */
  reload: () => Promise<void>
  /** 本地更新余额显示（余额支付成功后用 pay.balance_after 覆盖）。 */
  setBalance: (balance: string) => void
}

export const AuthContext = createContext<AuthContextValue | null>(null)

export function useAuth(): AuthContextValue {
  const value = useContext(AuthContext)
  if (!value) {
    throw new Error('useAuth 必须在 <AuthProvider> 内使用')
  }
  return value
}
