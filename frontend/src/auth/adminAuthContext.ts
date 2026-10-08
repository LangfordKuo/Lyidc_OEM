import { createContext, useContext } from 'react'

import type { AdminAccount, AdminRole } from '../api/types'

// 管理端登录态上下文（与会员登录态**完全隔离**：token 键不同、Provider 不同）。
// Provider 实现见 src/auth/AdminAuthProvider.tsx（组件与 hook 分文件，避免 react/only-export-components 告警）。
export interface AdminAuthContextValue {
  /** 当前登录管理员；未登录为 null。 */
  admin: AdminAccount | null
  /** 当前角色（未登录为 null）——界面按角色矩阵渲染权限。 */
  role: AdminRole | null
  /** 首次进入时带 token 校验登录态 / 拉取资料的加载态。 */
  initializing: boolean
  isAuthenticated: boolean
  login: (username: string, password: string) => Promise<void>
  logout: () => void
  /** 重新拉取管理员资料（如切换角色后刷新权限视图）。 */
  reload: () => Promise<void>
}

export const AdminAuthContext = createContext<AdminAuthContextValue | null>(null)

export function useAdminAuth(): AdminAuthContextValue {
  const value = useContext(AdminAuthContext)
  if (!value) {
    throw new Error('useAdminAuth 必须在 <AdminAuthProvider> 内使用')
  }
  return value
}
