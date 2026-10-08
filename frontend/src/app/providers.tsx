import type { ReactNode } from 'react'

import AdminAuthProvider from '@/auth/AdminAuthProvider'
import AuthProvider from '@/auth/AuthProvider'
import { Toaster } from '@/components/ui/sonner'

/**
 * 应用级 Provider 集合：会员认证上下文 + 管理端认证上下文（token 与登录态完全隔离）
 * + 全局 toast。测试 harness 复用同一份。
 */
export default function AppProviders({ children }: { children: ReactNode }) {
  return (
    <AuthProvider>
      <AdminAuthProvider>
        {children}
        <Toaster position="top-center" richColors />
      </AdminAuthProvider>
    </AuthProvider>
  )
}
