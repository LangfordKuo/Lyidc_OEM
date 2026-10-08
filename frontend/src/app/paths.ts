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

  // 管理后台（第三期）
  adminLogin: '/admin/login',
  admin: '/admin',
  adminProducts: '/admin/products',
  adminOrders: '/admin/orders',
  adminOrderDetail: (id: number | string) => `/admin/orders/${id}`,
  adminMembers: '/admin/members',
  adminInstances: '/admin/instances',
  adminInstanceDetail: (id: number | string) => `/admin/instances/${id}`,
  adminTickets: '/admin/tickets',
  adminTicketDetail: (id: number | string) => `/admin/tickets/${id}`,
  adminSettings: '/admin/settings',
  adminNotifications: '/admin/notifications',
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

// 管理后台侧栏导航项：page 字段与 src/lib/adminRoles.ts 的角色矩阵对应，
// 无权限的整页（工单仅 admin/support、设置仅 admin）直接不渲染入口。
export const adminNavItems = [
  { to: paths.admin, label: '仪表盘', end: true, page: 'dashboard', description: '全站关键指标' },
  { to: paths.adminProducts, label: '商品', end: false, page: 'products', description: '定价、上下架与导入' },
  { to: paths.adminOrders, label: '订单', end: false, page: 'orders', description: '订单查询与交付处置' },
  { to: paths.adminMembers, label: '会员', end: false, page: 'members', description: '会员资料与余额流水' },
  { to: paths.adminInstances, label: '实例', end: false, page: 'instances', description: '实例状态与操作' },
  { to: paths.adminTickets, label: '工单', end: false, page: 'tickets', description: '客服工单处理' },
  { to: paths.adminSettings, label: '设置', end: false, page: 'settings', description: '支付、上游、邮件与通知' },
  { to: paths.adminNotifications, label: '通知', end: false, page: 'notifications', description: '我的收件箱' },
] as const
