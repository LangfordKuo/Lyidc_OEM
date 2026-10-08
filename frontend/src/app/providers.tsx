import type { ReactNode } from 'react'

import AuthProvider from '@/auth/AuthProvider'
import { Toaster } from '@/components/ui/sonner'

/** 应用级 Provider 集合：认证上下文 + 全局 toast。测试 harness 复用同一份。 */
export default function AppProviders({ children }: { children: ReactNode }) {
  return (
    <AuthProvider>
      {children}
      <Toaster position="top-center" richColors />
    </AuthProvider>
  )
}
