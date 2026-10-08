import { Toast } from '@heroui/react'
import { RouterProvider, createBrowserRouter } from 'react-router-dom'

import { router } from './app/router'
import AdminAuthProvider from './auth/AdminAuthProvider'
import AuthProvider from './auth/AuthProvider'

type AppRouter = ReturnType<typeof createBrowserRouter>

interface AppProps {
  /** 供测试注入 Memory Router；缺省使用 Browser Router（见 src/app/router.tsx）。 */
  router?: AppRouter
}

// App 组装应用级 Provider 与路由：
//   - AuthProvider：会员登录态（token 校验、登录/登出、余额刷新）
//   - AdminAuthProvider：管理端登录态（与会员登录态完全隔离，token 键不同）
//   - Toast.Provider：HeroUI v3 的轻提示区域（使用默认渲染，无需自定义 children）
// HeroUI v3 已废除 HeroUIProvider，样式由 index.css 中的 @heroui/styles 提供。
function App({ router: routerProp }: AppProps = {}) {
  return (
    <AuthProvider>
      <AdminAuthProvider>
        <RouterProvider router={routerProp ?? router} />
        <Toast.Provider />
      </AdminAuthProvider>
    </AuthProvider>
  )
}

export default App
