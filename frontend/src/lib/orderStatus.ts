import type { OrderStatus } from '../api/types'

export type StatusTone = 'ok' | 'warn' | 'error' | 'pending'

// 订单状态文案与色调（契约 12.3 / 14.1 的 6 态状态机）。
export const ORDER_STATUS_LABELS: Record<OrderStatus, string> = {
  pending: '待支付',
  paid: '已支付',
  provisioning: '开通中',
  active: '已开通',
  failed: '交付失败',
  cancelled: '已取消',
}

export const ORDER_STATUS_TONES: Record<OrderStatus, StatusTone> = {
  pending: 'warn',
  paid: 'ok',
  provisioning: 'pending',
  active: 'ok',
  failed: 'error',
  cancelled: 'pending',
}

export function isOrderStatus(value: unknown): value is OrderStatus {
  return typeof value === 'string' && value in ORDER_STATUS_LABELS
}

export function orderStatusLabel(status: OrderStatus): string {
  return ORDER_STATUS_LABELS[status] ?? status
}
