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

/** 订单状态说明（订单详情时间线与状态提示用，契约 12.3 / 14.1）。 */
export const ORDER_STATUS_DESCRIPTIONS: Record<OrderStatus, string> = {
  pending: '订单已创建，等待支付；可继续支付或取消订单。',
  paid: '支付已到账，即将进入开通流程。',
  provisioning: '上游正在开通，通常很快完成，请稍后刷新。',
  active: '订单已交付完成，可在「我的服务器」查看实例。',
  failed: '交付失败，管理员可在后台重试；如有疑问可提交工单。',
  cancelled: '订单已取消，不可恢复。',
}

export function isOrderStatus(value: unknown): value is OrderStatus {
  return typeof value === 'string' && value in ORDER_STATUS_LABELS
}

export function orderStatusLabel(status: OrderStatus): string {
  return ORDER_STATUS_LABELS[status] ?? status
}
