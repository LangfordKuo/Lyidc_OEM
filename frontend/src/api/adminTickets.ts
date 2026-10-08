import { http } from './client'
import type {
  AdminReplyTicketInput,
  AdminTicket,
  AdminTicketCloseResult,
  AdminTicketDetail,
  AdminTicketMutationResult,
  Paged,
  TicketCategory,
  TicketStatus,
} from './types'

// 管理端工单接口（契约 16.3）：工单域是**客服域**——admin + support 全权，finance 一律 403。
// 管理端详情包含内部备注（internal=true）；内部备注不影响工单状态且不会发给会员。

/** GET /api/v1/admin/tickets —— 全站工单分页（status / category / member_id / keyword 过滤）。 */
export function listAdminTickets(
  params: {
    page?: number
    page_size?: number
    status?: TicketStatus
    category?: TicketCategory
    member_id?: number
    keyword?: string
  } = {},
): Promise<Paged<AdminTicket>> {
  return http.get<Paged<AdminTicket>>('/admin/tickets', { auth: 'admin', query: { ...params } })
}

/** GET /api/v1/admin/tickets/:id —— 工单详情 `{ticket, messages}`（消息流含内部备注）。 */
export function fetchAdminTicket(id: number): Promise<AdminTicketDetail> {
  return http.get<AdminTicketDetail>(`/admin/tickets/${id}`, { auth: 'admin' })
}

/**
 * POST /api/v1/admin/tickets/:id/reply —— 客服回复。
 * internal=false → 状态转 replied（并通知会员）；internal=true → 内部备注，状态不变、不通知。
 */
export function replyAdminTicket(
  id: number,
  input: AdminReplyTicketInput,
): Promise<AdminTicketMutationResult> {
  return http.post<AdminTicketMutationResult>(`/admin/tickets/${id}/reply`, input, {
    auth: 'admin',
  })
}

/** POST /api/v1/admin/tickets/:id/close —— 关闭工单（幂等：重复关闭 already_closed=true）。 */
export function closeAdminTicket(id: number): Promise<AdminTicketCloseResult> {
  return http.post<AdminTicketCloseResult>(`/admin/tickets/${id}/close`, undefined, {
    auth: 'admin',
  })
}
