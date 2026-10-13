import { http } from './client'
import type { ApplyUpdateInput, UpdateJob, UpdateStatus } from './types'

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

/**
 * POST /api/v1/admin/updates/apply —— 发起一键升级（契约 20.3）：二次确认 + 目标版本必须等于
 * 当前检查到的最新正式版。成功 HTTP 202 返回任务视图；前置检查拒绝走业务错误码
 * （40001 参数 / 40002 规则 / 409 已有任务进行中），由页面据 ApiError 展示原因。
 * 超时放宽到 30 秒：同步阶段包含一次「取 .sha256」的出网预检。
 */
export function applyAdminUpdate(input: ApplyUpdateInput): Promise<UpdateJob> {
  return http.post<UpdateJob>('/admin/updates/apply', input, { auth: 'admin', timeoutMs: 30_000 })
}

/**
 * GET /api/v1/admin/updates/job —— 读取当前/最近一次升级任务（契约 20.4）；
 * 从未发起过升级时返回 null。读取不发任何出网请求，也不改状态。
 */
export function fetchAdminUpdateJob(): Promise<UpdateJob | null> {
  return http.get<UpdateJob | null>('/admin/updates/job', { auth: 'admin' })
}
