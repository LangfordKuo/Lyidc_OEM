import { http } from './client'
import type {
  NotificationPage,
  NotificationReadAllResult,
  NotificationReadResult,
  NotificationUnread,
} from './types'

// 会员端站内通知接口（契约 17.2）：个人收件箱，仅本人。

/** GET /api/v1/notifications —— 本人通知分页（新建在前），unread=true 只看未读；回带未读数。 */
export function listNotifications(
  params: { page?: number; page_size?: number; unread?: boolean } = {},
): Promise<NotificationPage> {
  return http.get<NotificationPage>('/notifications', {
    auth: 'member',
    query: { ...params },
  })
}

/** GET /api/v1/notifications/unread-count —— 未读数（侧栏徽章）。 */
export function fetchUnreadCount(): Promise<NotificationUnread> {
  return http.get<NotificationUnread>('/notifications/unread-count', { auth: 'member' })
}

/** POST /api/v1/notifications/:id/read —— 单条已读（幂等，不覆盖首次 read_at）。 */
export function readNotification(id: number): Promise<NotificationReadResult> {
  return http.post<NotificationReadResult>(`/notifications/${id}/read`, undefined, {
    auth: 'member',
  })
}

/** POST /api/v1/notifications/read-all —— 全部已读（返回本次置为已读的条数）。 */
export function readAllNotifications(): Promise<NotificationReadAllResult> {
  return http.post<NotificationReadAllResult>('/notifications/read-all', undefined, {
    auth: 'member',
  })
}
