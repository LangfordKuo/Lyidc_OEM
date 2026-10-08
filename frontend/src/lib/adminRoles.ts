import type { AdminRole } from '../api/types'

// 管理端角色矩阵（契约 12.7 为权威，另有 6.4 / 10.5 / 15.3 / 16.3 / 17.3 的分域矩阵）。
//
// 界面按角色矩阵渲染：**无权限的入口隐藏或禁用**（禁用时给出原因），且**不发起无权限请求**
// （查询类用 useAsync 的 enabled 参数关闭，避免无谓的 403）；服务端仍会独立校验
// （越权返回 403，由统一的错误提示兜底）。这里只描述「谁能做什么」，不承载任何请求逻辑。

export const ADMIN_ROLES: AdminRole[] = ['admin', 'finance', 'support']

export const ADMIN_ROLE_LABELS: Record<AdminRole, string> = {
  admin: '超级管理员',
  finance: '财务',
  support: '客服',
}

export const ADMIN_ROLE_DESCRIPTIONS: Record<AdminRole, string> = {
  admin: '全部功能，含设置、实例暂停/终止与订单重试交付',
  finance: '商品定价与导入、财务对账、会员禁用；不含设置与实例硬操作',
  support: '工单全权、实例只读与同步；不含财务对账、商品写操作与设置',
}

/** 管理端权限点：与契约各分域的角色矩阵一一对应。 */
export type AdminPermission =
  // 会员（契约 6.4）
  | 'members.read'
  | 'members.status'
  // 商品（契约 10.5）
  | 'products.read'
  | 'products.write'
  | 'products.import'
  | 'groups.write'
  // 订单（契约 12.6 查看类 / 14.4 重试交付）
  | 'orders.read'
  | 'orders.retry'
  // 实例（契约 14.4 / 15.3）
  | 'instances.read'
  | 'instances.sync'
  | 'instances.suspend'
  | 'instances.cancel'
  // 工单（契约 16.3，客服域：admin + support，finance 403）
  | 'tickets.access'
  // 财务对账（契约 12.6）
  | 'finance.reconcile'
  // 后台设置（契约 12.1 / 17.3，含读取）
  | 'settings.access'
  // 上游探活（契约第 9 节：管理端 token 即可，三类角色都可只读探活）
  | 'upstream.probe'
  // 本人通知收件箱（契约 17.2）
  | 'notifications.read'

const PERMISSION_ROLES: Record<AdminPermission, AdminRole[]> = {
  'members.read': ['admin', 'finance', 'support'],
  'members.status': ['admin', 'finance'],

  'products.read': ['admin', 'finance', 'support'],
  'products.write': ['admin', 'finance'],
  'products.import': ['admin', 'finance'],
  'groups.write': ['admin', 'finance'],

  'orders.read': ['admin', 'finance', 'support'],
  'orders.retry': ['admin'],

  'instances.read': ['admin', 'finance', 'support'],
  'instances.sync': ['admin', 'finance', 'support'],
  'instances.suspend': ['admin'],
  'instances.cancel': ['admin'],

  'tickets.access': ['admin', 'support'],

  'finance.reconcile': ['admin', 'finance'],

  'settings.access': ['admin'],

  'upstream.probe': ['admin', 'finance', 'support'],

  'notifications.read': ['admin', 'finance', 'support'],
}

/** 权限点说明：用于禁用态提示（如「仅超级管理员可执行」）。 */
const PERMISSION_LABELS: Record<AdminPermission, string> = {
  'members.read': '查看会员',
  'members.status': '禁用/启用会员',
  'products.read': '查看商品',
  'products.write': '修改定价或上下架',
  'products.import': '上游导入商品',
  'groups.write': '修改商品分组',
  'orders.read': '查看订单',
  'orders.retry': '重试订单交付',
  'instances.read': '查看实例',
  'instances.sync': '同步实例状态',
  'instances.suspend': '暂停/恢复实例',
  'instances.cancel': '提交实例终止申请',
  'tickets.access': '处理工单',
  'finance.reconcile': '财务对账（充值单与流水）',
  'settings.access': '后台设置',
  'upstream.probe': '上游探活',
  'notifications.read': '站内通知',
}

/** hasPermission 判断角色是否拥有某权限点（未知角色一律无权限）。 */
export function hasPermission(
  role: AdminRole | null | undefined,
  permission: AdminPermission,
): boolean {
  if (!role) {
    return false
  }
  return PERMISSION_ROLES[permission]?.includes(role) ?? false
}

/** allowedRoles 返回拥有该权限点的角色列表（用于提示文案）。 */
export function allowedRoles(permission: AdminPermission): AdminRole[] {
  return PERMISSION_ROLES[permission] ?? []
}

/** permissionLabel 返回权限点的中文名。 */
export function permissionLabel(permission: AdminPermission): string {
  return PERMISSION_LABELS[permission] ?? permission
}

/**
 * permissionHint 生成禁用态提示，如「仅超级管理员可执行」「需要超级管理员或财务角色」。
 * 用于按钮禁用时的说明文字。
 */
export function permissionHint(permission: AdminPermission): string {
  const roles = allowedRoles(permission)
  const names = roles.map((role) => ADMIN_ROLE_LABELS[role])
  if (roles.length === 1) {
    return `仅${names[0]}可执行`
  }
  return `需要${names.join('或')}角色`
}

/** 后台页面标识（与侧栏导航项一一对应）。 */
export type AdminPage =
  | 'dashboard'
  | 'products'
  | 'orders'
  | 'members'
  | 'instances'
  | 'tickets'
  | 'settings'
  | 'notifications'

/**
 * canAccessAdminPage 判断角色能否进入某个后台页面（整页 403 的域）：
 * 工单页仅 admin/support、设置页仅 admin，其余页面三类角色均可。
 */
export function canAccessAdminPage(role: AdminRole | null | undefined, page: AdminPage): boolean {
  switch (page) {
    case 'tickets':
      return hasPermission(role, 'tickets.access')
    case 'settings':
      return hasPermission(role, 'settings.access')
    default:
      return Boolean(role)
  }
}
