import { http } from './client'
import type { AdminAccount, AdminLoginResult } from './types'

// 管理端认证（契约 6.3）：登录开放，其余管理端接口一律带管理员 token（aud=admin）。
// 登录成功后由调用方（src/auth/AdminAuthProvider.tsx）负责落 token，api 层不写存储。

/** POST /api/v1/admin/auth/login —— 管理员登录，返回 {token, expires_at, admin}。 */
export function loginAdmin(username: string, password: string): Promise<AdminLoginResult> {
  return http.post<AdminLoginResult>('/admin/auth/login', { username, password })
}

/** GET /api/v1/admin/profile —— 当前登录管理员（含 role，界面按角色矩阵渲染权限）。 */
export function fetchAdminProfile(): Promise<AdminAccount> {
  return http.get<AdminAccount>('/admin/profile', { auth: 'admin' })
}
