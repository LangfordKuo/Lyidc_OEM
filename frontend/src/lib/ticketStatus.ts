import type { TicketCategory, TicketStatus } from '../api/types'
import type { StatusTone } from './orderStatus'

// 工单状态与分类文案（契约 16.2 / 16.3）。

export const TICKET_STATUS_LABELS: Record<TicketStatus, string> = {
  open: '待客服处理',
  replied: '待会员回复',
  closed: '已关闭',
}

export const TICKET_STATUS_TONES: Record<TicketStatus, StatusTone> = {
  open: 'warn',
  replied: 'ok',
  closed: 'pending',
}

/** 工单列表可用的后端状态筛选值（契约 16.3 查询参数）。 */
export const TICKET_STATUS_FILTERS: { value: TicketStatus; label: string }[] = [
  { value: 'open', label: '待客服处理' },
  { value: 'replied', label: '待会员回复' },
  { value: 'closed', label: '已关闭' },
]

export const TICKET_CATEGORY_LABELS: Record<TicketCategory, string> = {
  technical: '技术',
  billing: '财务',
  other: '其他',
}

export const TICKET_CATEGORIES: TicketCategory[] = ['technical', 'billing', 'other']

export function ticketStatusLabel(status: string): string {
  return TICKET_STATUS_LABELS[status as TicketStatus] ?? status
}

export function ticketStatusTone(status: string): StatusTone {
  return TICKET_STATUS_TONES[status as TicketStatus] ?? 'pending'
}

export function ticketCategoryLabel(category: string): string {
  return TICKET_CATEGORY_LABELS[category as TicketCategory] ?? category
}
