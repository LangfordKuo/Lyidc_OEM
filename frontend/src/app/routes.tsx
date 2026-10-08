import type { RouteObject } from 'react-router-dom'

import RequireAdmin from '@/auth/RequireAdmin'
import RequireAuth from '@/auth/RequireAuth'
import AdminLayout from '@/components/admin/AdminLayout'
import ConsoleLayout from '@/components/layout/ConsoleLayout'
import SiteLayout from '@/components/layout/SiteLayout'
import Checkout from '@/pages/Checkout'
import ConsoleNotifications from '@/pages/ConsoleNotifications'
import ConsoleOrders from '@/pages/ConsoleOrders'
import ConsoleOverview from '@/pages/ConsoleOverview'
import ConsoleRecharge from '@/pages/ConsoleRecharge'
import ConsoleServerDetail from '@/pages/ConsoleServerDetail'
import ConsoleServers from '@/pages/ConsoleServers'
import ConsoleTicketDetail from '@/pages/ConsoleTicketDetail'
import ConsoleTickets from '@/pages/ConsoleTickets'
import Home from '@/pages/Home'
import Login from '@/pages/Login'
import NotFound from '@/pages/NotFound'
import PayResult from '@/pages/PayResult'
import ProductDetail from '@/pages/ProductDetail'
import Products from '@/pages/Products'
import Register from '@/pages/Register'
import AdminDashboard from '@/pages/admin/AdminDashboard'
import AdminInstanceDetail from '@/pages/admin/AdminInstanceDetail'
import AdminInstances from '@/pages/admin/AdminInstances'
import AdminLogin from '@/pages/admin/AdminLogin'
import AdminMembers from '@/pages/admin/AdminMembers'
import AdminNotifications from '@/pages/admin/AdminNotifications'
import AdminOrderDetail from '@/pages/admin/AdminOrderDetail'
import AdminOrders from '@/pages/admin/AdminOrders'
import AdminProducts from '@/pages/admin/AdminProducts'
import AdminSettings from '@/pages/admin/AdminSettings'
import AdminTicketDetail from '@/pages/admin/AdminTicketDetail'
import AdminTickets from '@/pages/admin/AdminTickets'

// 路由表：官网公共布局（SiteLayout）+ 会员区（RequireAuth 守卫 + ConsoleLayout）
// + 管理后台（/admin，独立布局，不套官网顶栏/页脚；登录页开放，其余由 RequireAdmin 守卫）。
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
      {
        path: 'console',
        element: <RequireAuth />,
        children: [
          {
            element: <ConsoleLayout />,
            children: [
              { index: true, element: <ConsoleOverview /> },
              { path: 'servers', element: <ConsoleServers /> },
              { path: 'servers/:id', element: <ConsoleServerDetail /> },
              { path: 'orders', element: <ConsoleOrders /> },
              { path: 'recharge', element: <ConsoleRecharge /> },
              { path: 'tickets', element: <ConsoleTickets /> },
              { path: 'tickets/:id', element: <ConsoleTicketDetail /> },
              { path: 'notifications', element: <ConsoleNotifications /> },
            ],
          },
        ],
      },
      { path: '*', element: <NotFound /> },
    ],
  },
  {
    // 管理后台：登录页不设守卫（否则会无限跳转），其余页面统一走 RequireAdmin。
    // 角色维度的可见性由页面内的权限闸门处理（隐藏入口 + 无权提示），不做重定向。
    path: 'admin',
    children: [
      { path: 'login', element: <AdminLogin /> },
      {
        element: <RequireAdmin />,
        children: [
          {
            element: <AdminLayout />,
            children: [
              { index: true, element: <AdminDashboard /> },
              { path: 'products', element: <AdminProducts /> },
              { path: 'orders', element: <AdminOrders /> },
              { path: 'orders/:id', element: <AdminOrderDetail /> },
              { path: 'members', element: <AdminMembers /> },
              { path: 'instances', element: <AdminInstances /> },
              { path: 'instances/:id', element: <AdminInstanceDetail /> },
              { path: 'tickets', element: <AdminTickets /> },
              { path: 'tickets/:id', element: <AdminTicketDetail /> },
              { path: 'settings', element: <AdminSettings /> },
              { path: 'notifications', element: <AdminNotifications /> },
            ],
          },
        ],
      },
      // 后台内的未知路径：仍落在后台分支（避免命中 /admin 之外的兜底路由）。
      { path: '*', element: <NotFound /> },
    ],
  },
]
