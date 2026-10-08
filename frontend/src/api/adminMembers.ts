import { http } from './client'
import type {
  LedgerEntry,
  LedgerType,
  Member,
  MemberStatus,
  Paged,
  Recharge,
  RechargeStatus,
} from './types'

// 管理端会员与对账接口（契约 6.3 / 12.6）。
// 角色矩阵：会员列表三类角色均可读；改状态（禁用/启用）与对账（充值单/流水）为 admin + finance，
// support 一律 403（界面侧按同一矩阵隐藏/禁用入口，并不发起无权限请求）。

/** GET /api/v1/admin/members —— 会员分页（username/email 模糊 + status 过滤）。 */
export function listAdminMembers(
  params: {
    page?: number
    page_size?: number
    username?: string
    email?: string
    status?: MemberStatus
  } = {},
): Promise<Paged<Member>> {
  return http.get<Paged<Member>>('/admin/members', { auth: 'admin', query: { ...params } })
}

/** PUT /api/v1/admin/members/:id/status —— 启用/禁用会员（admin / finance）。 */
export function updateMemberStatus(id: number, status: MemberStatus): Promise<Member> {
  return http.put<Member>(`/admin/members/${id}/status`, { status }, { auth: 'admin' })
}

/** GET /api/v1/admin/ledger —— 全站余额流水分页（admin / finance；可按会员与类型筛选）。 */
export function listAdminLedger(
  params: { page?: number; page_size?: number; member_id?: number; type?: LedgerType } = {},
): Promise<Paged<LedgerEntry>> {
  return http.get<Paged<LedgerEntry>>('/admin/ledger', { auth: 'admin', query: { ...params } })
}

/** GET /api/v1/admin/recharges —— 全站充值单分页（admin / finance；可按会员与状态筛选）。 */
export function listAdminRecharges(
  params: { page?: number; page_size?: number; member_id?: number; status?: RechargeStatus } = {},
): Promise<Paged<Recharge>> {
  return http.get<Paged<Recharge>>('/admin/recharges', { auth: 'admin', query: { ...params } })
}
