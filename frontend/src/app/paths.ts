// 路由路径常量：全站统一引用，避免各处硬编码字符串。
// 路由命名对齐旧版（第二期范围：官网 + 认证 + 商品 + 下单支付 + 会员区）。
export const paths = {
  home: '/',
  products: '/products',
  productDetail: (id: number | string) => `/products/${id}`,
  login: '/login',
  register: '/register',
  checkout: (productId: number | string) => `/checkout/${productId}`,
  payResult: '/pay/result',

  // 会员区（控制台）
  console: '/console',
  consoleServers: '/console/servers',
  consoleServerDetail: (id: number | string) => `/console/servers/${id}`,
  consoleOrders: '/console/orders',
  consoleRecharge: '/console/recharge',
  consoleTickets: '/console/tickets',
  consoleTicketDetail: (id: number | string) => `/console/tickets/${id}`,
  consoleNotifications: '/console/notifications',
} as const

/** 官网顶栏导航项。 */
export const siteNavItems = [
  { to: paths.home, label: '首页', end: true },
  { to: paths.products, label: '商品', end: false },
] as const

/** 会员区侧栏导航项。 */
export const consoleNavItems = [
  { to: paths.console, label: '概览', end: true },
  { to: paths.consoleServers, label: '我的服务器', end: false },
  { to: paths.consoleOrders, label: '我的订单', end: false },
  { to: paths.consoleRecharge, label: '余额充值', end: false },
  { to: paths.consoleTickets, label: '工单', end: false },
  { to: paths.consoleNotifications, label: '通知', end: false },
] as const
