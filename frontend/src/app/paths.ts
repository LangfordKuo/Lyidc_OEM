import type { OplogChannel } from '@/api/types'

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
  consoleReferral: '/console/referral',
  consoleTickets: '/console/tickets',
  consoleTicketDetail: (id: number | string) => `/console/tickets/${id}`,
  consoleNotifications: '/console/notifications',

  // 管理后台（第三期）
  adminLogin: '/admin/login',
  admin: '/admin',
  // 商品与分类合并页（R9-B）：分类能力作为页内页签，查询参数便于分享与刷新保持。
  adminProducts: '/admin/products',
  adminProductsCategories: '/admin/products?tab=categories',
  // 旧地址仅作重定向目标（routes.tsx → adminProductsCategories），不再有独立分类页。
  adminCategories: '/admin/categories',
  adminOrders: '/admin/orders',
  adminOrderDetail: (id: number | string) => `/admin/orders/${id}`,
  adminMembers: '/admin/members',
  adminReferral: '/admin/referral',
  adminInstances: '/admin/instances',
  adminInstanceDetail: (id: number | string) => `/admin/instances/${id}`,
  adminTickets: '/admin/tickets',
  adminTicketDetail: (id: number | string) => `/admin/tickets/${id}`,
  adminSettings: '/admin/settings',
  adminNotifications: '/admin/notifications',
  // 操作日志页（R18 批 2，契约 23.11）：三个通道共用同一个页面，通道由查询参数预置
  // （可分享、可刷新），侧栏「日志」展开后的三项就是它的三个带参链接。
  adminOperationLogs: '/admin/logs',
  adminOperationLogsOf: (channel: OplogChannel) => `/admin/logs?channel=${channel}`,
  // 会员页的「日志」入口（契约 23.11）：预置 channel=member 与该会员的 actor_id。
  adminMemberOperationLogs: (memberId: number | string) =>
    `/admin/logs?channel=member&actor_id=${memberId}`,
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
  // 邀请有礼紧挨余额充值：两者都是「钱」的入口（返现余额可提现/转余额），语义上同一组。
  { to: paths.consoleReferral, label: '邀请有礼', end: false },
  { to: paths.consoleTickets, label: '工单', end: false },
  { to: paths.consoleNotifications, label: '通知', end: false },
] as const

// 管理后台侧栏导航项：page 字段与 src/lib/adminRoles.ts 的角色矩阵对应，
// 无权限的整页（工单仅 admin/support、设置仅 admin）直接不渲染入口。
// 商品与分类合并为一项（R9-B）：分类在商品页的页签里，不再有独立导航入口。
export const adminNavItems = [
  { to: paths.admin, label: '仪表盘', end: true, page: 'dashboard', description: '全站关键指标' },
  {
    to: paths.adminProducts,
    label: '商品与分类',
    end: false,
    page: 'products',
    description: '分类树、定价、上下架与导入',
  },
  { to: paths.adminOrders, label: '订单', end: false, page: 'orders', description: '订单查询与交付处置' },
  { to: paths.adminMembers, label: '会员', end: false, page: 'members', description: '会员资料与余额流水' },
  // 返佣管理紧挨会员：审核对象是会员的提现/转余额申请，特批比例也在会员页。
  {
    to: paths.adminReferral,
    label: '返佣管理',
    end: false,
    page: 'referral',
    description: '提现/转余额审核与返佣流水',
  },
  { to: paths.adminInstances, label: '实例', end: false, page: 'instances', description: '实例状态与操作' },
  { to: paths.adminTickets, label: '工单', end: false, page: 'tickets', description: '客服工单处理' },
  { to: paths.adminSettings, label: '设置', end: false, page: 'settings', description: '支付、上游、邮件与通知' },
  // 日志（R18 批 2）：侧栏的**可展开入口**，展开后是三个通道各自的链接（见 adminOplogNavItems）。
  { to: paths.adminOperationLogs, label: '日志', end: false, page: 'logs', description: '操作留痕查询与清空' },
  { to: paths.adminNotifications, label: '通知', end: false, page: 'notifications', description: '我的收件箱' },
] as const

/**
 * 侧栏「日志」展开后的三项（R18 批 2，契约 23.11）：三项进入**同一个日志页**，
 * 只是预置的 `channel` 不同；`end` 无意义（同一条路径），选中态由页面按 channel 自行判定。
 */
export const adminOplogNavItems = [
  { to: paths.adminOperationLogsOf('member'), channel: 'member', label: '用户日志' },
  { to: paths.adminOperationLogsOf('scheduler'), channel: 'scheduler', label: '定时任务日志' },
  { to: paths.adminOperationLogsOf('admin'), channel: 'admin', label: '管理员日志' },
] as const
