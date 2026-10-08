// 路由路径常量：全站统一引用，避免各处硬编码字符串。
export const paths = {
  home: '/',
  products: '/products',
  productDetail: (id: number | string) => `/products/${id}`,
  login: '/login',
  register: '/register',
  checkout: (productId: number | string) => `/checkout/${productId}`,
  payResult: '/pay/result',

  // 会员区（7b 填充内容，本批为骨架 + 占位）
  console: '/console',
  consoleServers: '/console/servers',
  consoleOrders: '/console/orders',
  consoleRecharge: '/console/recharge',
  consoleTickets: '/console/tickets',
  consoleNotifications: '/console/notifications',
} as const

// 会员区侧栏导航项（骨架期即按 7b 的最终结构固定下来）。
export const consoleNavItems = [
  { to: paths.console, label: '概览', end: true, description: '账户与资源总览' },
  { to: paths.consoleServers, label: '我的服务器', end: false, description: '实例列表与操作' },
  { to: paths.consoleOrders, label: '我的订单', end: false, description: '订单查询与支付' },
  { to: paths.consoleRecharge, label: '余额充值', end: false, description: '在线充值与流水' },
  { to: paths.consoleTickets, label: '工单', end: false, description: '提交与跟踪工单' },
  { to: paths.consoleNotifications, label: '通知', end: false, description: '站内通知与提醒' },
] as const
