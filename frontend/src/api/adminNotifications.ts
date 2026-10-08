import { http } from './client'
import type {
  NotificationPage,
  NotificationReadAllResult,
  NotificationReadResult,
  NotificationUnread,
} from './types'

// 管理端站内通知接口（契约 17.2）：与会员端**同构**，读的是当前管理员的**本人收件箱**
// （三类角色都可访问；扇出通常只覆盖 admin + support，finance 的收件箱一般是空的）。

/** GET /api/v1/admin/notifications —— 本人通知分页（新建在前），unread=true 只看未读。 */
export function listAdminNotifications(
  params: { page?: number; page_size?: number; unread?: boolean } = {},
): Promise<NotificationPage> {
  return http.get<NotificationPage>('/admin/notifications', {
    auth: 'admin',
    query: { ...params },
  })
}

/** GET /api/v1/admin/notifications/unread-count —— 未读数（侧栏徽章）。 */
export function fetchAdminUnreadCount(): Promise<NotificationUnread> {
  return http.get<NotificationUnread>('/admin/notifications/unread-count', { auth: 'admin' })
}

/** POST /api/v1/admin/notifications/:id/read —— 单条已读（幂等）。 */
export function readAdminNotification(id: number): Promise<NotificationReadResult> {
  return http.post<NotificationReadResult>(`/admin/notifications/${id}/read`, undefined, {
    auth: 'admin',
  })
}

/** POST /api/v1/admin/notifications/read-all —— 全部已读（幂等，返回本次置为已读条数）。 */
export function readAllAdminNotifications(): Promise<NotificationReadAllResult> {
  return http.post<NotificationReadAllResult>('/admin/notifications/read-all', undefined, {
    auth: 'admin',
  })
}
