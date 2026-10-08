import type { OrderStatus } from '../api/types'
import type { StatusTone } from '../components/StatusBadge'

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

export const ORDER_STATUS_DESCRIPTIONS: Record<OrderStatus, string> = {
  pending: '订单已创建，等待支付。可继续发起支付或取消订单。',
  paid: '支付已到账，正在排队自动开通。',
  provisioning: '上游正在开通实例，请稍候刷新查看结果。',
  active: '实例已开通，可在会员区「我的服务器」查看。',
  failed: '自动开通失败，管理员可重试交付；如需协助请提交工单。',
  cancelled: '订单已取消（未支付订单可由本人取消）。',
}

export function isOrderStatus(value: unknown): value is OrderStatus {
  return typeof value === 'string' && value in ORDER_STATUS_LABELS
}

export function orderStatusLabel(status: OrderStatus): string {
  return ORDER_STATUS_LABELS[status] ?? status
}
