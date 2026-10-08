import type { NotificationEvent } from '../api/types'
import type { StatusTone } from '../components/StatusBadge'

// 站内通知事件文案（契约 17.1 的 9 个事件枚举）与点击后的跳转建议。

export const NOTIFICATION_EVENT_LABELS: Record<NotificationEvent, string> = {
  order_delivered: '订单开通',
  order_failed: '交付失败',
  renew_succeeded: '续费成功',
  instance_suspended: '实例暂停',
  instance_terminated: '实例终止',
  ticket_created: '工单',
  ticket_replied: '工单回复',
  ticket_closed: '工单关闭',
  expiry_reminder: '到期提醒',
}

export const NOTIFICATION_EVENT_TONES: Record<NotificationEvent, StatusTone> = {
  order_delivered: 'ok',
  order_failed: 'error',
  renew_succeeded: 'ok',
  instance_suspended: 'warn',
  instance_terminated: 'error',
  ticket_created: 'pending',
  ticket_replied: 'pending',
  ticket_closed: 'pending',
  expiry_reminder: 'warn',
}

export function notificationEventLabel(event: string): string {
  return NOTIFICATION_EVENT_LABELS[event as NotificationEvent] ?? event
}

export function notificationEventTone(event: string): StatusTone {
  return NOTIFICATION_EVENT_TONES[event as NotificationEvent] ?? 'pending'
}
