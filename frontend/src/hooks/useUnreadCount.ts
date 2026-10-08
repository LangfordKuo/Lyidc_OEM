import { useCallback, useEffect, useSyncExternalStore } from 'react'

import { fetchUnreadCount } from '../api/notifications'

// 未读通知计数是「跨页面共享 + 需要即时刷新」的小状态（侧栏徽章 / 通知页）。
// 用一个极简的模块级 store + useSyncExternalStore 承载，避免为它引入 Context Provider。
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

export function setUnreadCount(value: number): void {
  const next = Math.max(0, Math.floor(value))
  if (next !== unread) {
    unread = next
    emit()
  }
}

/** 重新拉取未读数（登录后、已读操作后、进入会员区时调用）；失败静默忽略。 */
export async function refreshUnreadCount(): Promise<void> {
  try {
    const result = await fetchUnreadCount()
    setUnreadCount(result.unread)
  } catch {
    // 未登录 / 网络异常：保持现有计数，不打扰用户。
  }
}

/** 订阅未读数（组件卸载后不再接收更新）。 */
export function useUnreadCount(): number {
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot)
}

/** 首屏自动拉取一次未读数（进入会员区时调用）。 */
export function useUnreadCountEffect(enabled = true): number {
  const count = useUnreadCount()
  const reload = useCallback(() => {
    void refreshUnreadCount()
  }, [])
  useEffect(() => {
    if (enabled) {
      reload()
    }
  }, [enabled, reload])
  return count
}
