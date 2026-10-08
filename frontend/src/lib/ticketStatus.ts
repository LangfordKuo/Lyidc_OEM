import type { TicketCategory, TicketStatus } from '../api/types'
import type { StatusTone } from '../components/StatusBadge'

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
