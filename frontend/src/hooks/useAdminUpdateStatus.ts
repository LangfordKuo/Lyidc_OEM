import { useEffect, useSyncExternalStore } from 'react'

import { fetchAdminUpdateStatus } from '../api/adminUpdates'
import type { UpdateStatus } from '../api/types'

// 管理端系统更新状态（契约 19.7）：侧栏「设置」红点与设置页「系统更新」卡片标题红点的共同数据源。
// 与 useAdminUnreadCount **同构但完全独立**（两个 store 分开，避免切换登录态时串号）。
// 红点只读缓存（GET /admin/updates），**读取不触发出网**；手动检查的结果也经这里写入，红点即时生效。
let latest: UpdateStatus | null = null
// 写入序号：读缓存期间若已有新的写入（手动检查结果、修改仓库后的清空、登出），迟到的旧缓存快照不得覆盖它。
let writeSeq = 0
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

function getSnapshot(): UpdateStatus | null {
  return latest
}

/** 写入最新更新状态：手动检查、读缓存、清空都经这里，保证侧栏红点与设置页卡片一致。 */
export function setAdminUpdateStatus(next: UpdateStatus | null): void {
  writeSeq += 1
  if (next !== latest) {
    latest = next
    emit()
  }
}

/** 登出时清空（避免下一位登录者看到上一位管理员留下的更新状态）。 */
export function resetAdminUpdateStatus(): void {
  setAdminUpdateStatus(null)
}

/** 重新读取缓存（进入后台布局时调用，不出网）；失败静默忽略：不 toast、不改动现有状态、不置红点。 */
export async function refreshAdminUpdateStatus(): Promise<void> {
  const seq = writeSeq
  try {
    const result = await fetchAdminUpdateStatus()
    // 读取期间已有新写入（更新的检查结果或登出清空）：这份缓存快照可能更旧，丢弃。
    if (seq === writeSeq) {
      setAdminUpdateStatus(result)
    }
  } catch {
    // 未登录 / 网络异常 / 无权限：保持现有状态，不打扰用户。
  }
}

export interface AdminUpdateState {
  /** 最近一次读到的更新状态（从未读到时为 null）。 */
  status: UpdateStatus | null
  /** 红点判定：仅 has_update === true 为真（未检查 / 失败 / 读取失败均为假）。 */
  hasUpdate: boolean
}

function toState(status: UpdateStatus | null): AdminUpdateState {
  return { status, hasUpdate: status?.has_update === true }
}

/** 订阅更新状态（只读内存 store，不发请求）。 */
export function useAdminUpdateStatus(): AdminUpdateState {
  return toState(useSyncExternalStore(subscribe, getSnapshot, getSnapshot))
}

/** 首屏自动读取一次缓存（进入后台布局时调用）；enabled 为假（非 admin）时不发任何请求。 */
export function useAdminUpdateStatusEffect(enabled = true): AdminUpdateState {
  const state = useAdminUpdateStatus()
  useEffect(() => {
    if (enabled) {
      void refreshAdminUpdateStatus()
    }
  }, [enabled])
  return state
}
