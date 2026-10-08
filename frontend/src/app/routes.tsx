import type { RouteObject } from 'react-router-dom'

import RequireAuth from '../auth/RequireAuth'
import ConsoleLayout from '../components/layout/ConsoleLayout'
import SiteLayout from '../components/layout/SiteLayout'
import Checkout from '../pages/Checkout'
import Home from '../pages/Home'
import Login from '../pages/Login'
import NotFound from '../pages/NotFound'
import PayResult from '../pages/PayResult'
import ProductDetail from '../pages/ProductDetail'
import Products from '../pages/Products'
import Register from '../pages/Register'
import ConsoleNotifications from '../pages/console/ConsoleNotifications'
import ConsoleOrders from '../pages/console/ConsoleOrders'
import ConsoleOverview from '../pages/console/ConsoleOverview'
import ConsoleRecharge from '../pages/console/ConsoleRecharge'
import ConsoleServers from '../pages/console/ConsoleServers'
import ConsoleTickets from '../pages/console/ConsoleTickets'
import { paths } from './paths'

// 路由表：官网公共布局（SiteLayout）+ 会员区（RequireAuth 守卫 + ConsoleLayout）。
// 抽成数组便于测试用 createMemoryRouter 复用同一套路由。
export const routes: RouteObject[] = [
  {
    path: paths.home,
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
      {
        path: 'console',
        element: <RequireAuth />,
        children: [
          {
            element: <ConsoleLayout />,
            children: [
              { index: true, element: <ConsoleOverview /> },
              { path: 'servers', element: <ConsoleServers /> },
              { path: 'orders', element: <ConsoleOrders /> },
              { path: 'recharge', element: <ConsoleRecharge /> },
              { path: 'tickets', element: <ConsoleTickets /> },
              { path: 'notifications', element: <ConsoleNotifications /> },
            ],
          },
        ],
      },
      { path: '*', element: <NotFound /> },
    ],
  },
]
