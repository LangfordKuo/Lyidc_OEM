import { useCallback, useEffect, useSyncExternalStore } from 'react'

import { fetchAdminUnreadCount } from '../api/adminNotifications'

// 管理端未读通知计数：与会员侧 useUnreadCount 同构（模块级 store + useSyncExternalStore），
// 但读取的是**当前管理员的本人收件箱**，且与会员计数完全独立（两套登录态互不影响）。
let unread = 0
const listeners = new Set<() => void>()

function emit(): void {
  for (const listener of listeners) {
    listener()
  }
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

function getSnapshot(): number {
  return unread
}

export function setAdminUnreadCount(value: number): void {
  const next = Math.max(0, Math.floor(value))
  if (next !== unread) {
    unread = next
    emit()
  }
}

/** 重新拉取未读数（登录后、已读操作后、进入后台时调用）；失败静默忽略。 */
export async function refreshAdminUnreadCount(): Promise<void> {
  try {
    const result = await fetchAdminUnreadCount()
    setAdminUnreadCount(result.unread)
  } catch {
    // 未登录 / 网络异常 / 无权限：保持现有计数，不打扰用户
  }
}

/** 订阅未读数。 */
export function useAdminUnreadCount(): number {
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot)
}

/** 首屏自动拉取一次未读数。 */
export function useAdminUnreadCountEffect(enabled = true): number {
  const count = useAdminUnreadCount()
  const reload = useCallback(() => {
    void refreshAdminUnreadCount()
  }, [])
  useEffect(() => {
    if (enabled) {
      reload()
    }
  }, [enabled, reload])
  return count
}
