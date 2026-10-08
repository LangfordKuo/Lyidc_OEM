import type { RouteObject } from 'react-router-dom'

import RequireAuth from '@/auth/RequireAuth'
import SiteLayout from '@/components/layout/SiteLayout'
import Checkout from '@/pages/Checkout'
import Home from '@/pages/Home'
import Login from '@/pages/Login'
import NotFound from '@/pages/NotFound'
import PayResult from '@/pages/PayResult'
import ProductDetail from '@/pages/ProductDetail'
import Products from '@/pages/Products'
import Register from '@/pages/Register'

// 路由表：官网公共布局（SiteLayout）+ 需登录的结算页（RequireAuth 守卫）。
// 抽成数组便于测试用 createMemoryRouter 复用同一套路由。
export const routes: RouteObject[] = [
  {
    path: '/',
    element: <SiteLayout />,
    children: [
      { index: true, element: <Home /> },
      { path: 'products', element: <Products /> },
      { path: 'products/:id', element: <ProductDetail /> },
      { path: 'login', element: <Login /> },
      { path: 'register', element: <Register /> },
      {
        path: 'checkout/:productId',
        element: <RequireAuth />,
        children: [{ index: true, element: <Checkout /> }],
      },
      { path: 'pay/result', element: <PayResult /> },
      { path: '*', element: <NotFound /> },
    ],
  },
]
