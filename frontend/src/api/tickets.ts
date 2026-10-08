import { http } from './client'
import type {
  CreateTicketInput,
  Paged,
  Ticket,
  TicketCloseResult,
  TicketDetail,
  TicketMutationResult,
  TicketReplyResult,
  TicketStatus,
} from './types'

// 会员端工单接口（契约 16.3）：仅本人（他人工单与不存在的工单统一 404 工单不存在）。

/** POST /api/v1/tickets —— 提交工单（同事务写入首条消息），返回 {ticket, message}。 */
export function createTicket(input: CreateTicketInput): Promise<TicketMutationResult> {
  return http.post<TicketMutationResult>('/tickets', input, { auth: 'member' })
}

/** GET /api/v1/tickets —— 本人工单分页（last_reply_at 降序），可按 status 过滤。 */
export function listTickets(
  params: { page?: number; page_size?: number; status?: TicketStatus } = {},
): Promise<Paged<Ticket>> {
  return http.get<Paged<Ticket>>('/tickets', { auth: 'member', query: { ...params } })
}

/** GET /api/v1/tickets/:id —— 工单详情（平铺字段）+ 消息流（不含内部备注）。 */
export function fetchTicket(id: number): Promise<TicketDetail> {
  return http.get<TicketDetail>(`/tickets/${id}`, { auth: 'member' })
}

/** POST /api/v1/tickets/:id/reply —— 会员回复（状态回 open；已关闭返回 40002）。 */
export function replyTicket(id: number, content: string): Promise<TicketReplyResult> {
  return http.post<TicketReplyResult>(`/tickets/${id}/reply`, { content }, { auth: 'member' })
}

/** POST /api/v1/tickets/:id/close —— 关闭工单（幂等：重复关闭 already_closed=true）。 */
export function closeTicket(id: number): Promise<TicketCloseResult> {
  return http.post<TicketCloseResult>(`/tickets/${id}/close`, undefined, { auth: 'member' })
}
