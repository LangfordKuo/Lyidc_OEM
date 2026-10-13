import { http } from './client'
import type { UpdateStatus } from './types'

// 系统版本与更新检查接口（契约 19.3）：**仅 admin**，finance / support 一律 403（19.6）。
// GitHub 侧的失败（限流 / 超时 / 无正式版等）不走 HTTP 错误码：返回 code=0 的成功包装并带 status / message，
// 由页面据 status 展示失败态；这里抛出的只有鉴权与本地库故障（ApiError）。

/** GET /api/v1/admin/updates —— 读缓存（不出网；从未检查时 status=not_checked）。红点与卡片当前版本读的是它。 */
export function fetchAdminUpdateStatus(): Promise<UpdateStatus> {
  return http.get<UpdateStatus>('/admin/updates', { auth: 'admin' })
}

/**
 * GET /api/v1/admin/updates/check —— 实时检查（强制访问 GitHub，成功或失败都写入缓存）。
 * 超时放宽到 30 秒：单次出网上限 8 秒，且与定时检查互斥排队，最坏耗时可能超过默认的 15 秒。
 */
export function checkAdminUpdates(): Promise<UpdateStatus> {
  return http.get<UpdateStatus>('/admin/updates/check', { auth: 'admin', timeoutMs: 30_000 })
}
