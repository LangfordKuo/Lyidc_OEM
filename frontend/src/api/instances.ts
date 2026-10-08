import type { BillingCycle } from '../lib/cycles'
import { http } from './client'
import type {
  CancelInstanceInput,
  CancelResult,
  InstanceActionResult,
  InstanceDetail,
  InstanceLog,
  InstanceStatus,
  InstanceSummary,
  Order,
  Paged,
  PowerOp,
  ReinstallOptions,
} from './types'

// 会员端实例接口（契约 14.4 / 15.2 / 15.8）：全部要求会员 token，且只操作本人实例
// （他人实例与不存在的实例统一 404 实例不存在）。

/** GET /api/v1/instances —— 本人实例分页（新建在前），可按 status 过滤。 */
export function listInstances(
  params: { page?: number; page_size?: number; status?: InstanceStatus } = {},
): Promise<Paged<InstanceSummary>> {
  return http.get<Paged<InstanceSummary>>('/instances', { auth: 'member', query: { ...params } })
}

/** GET /api/v1/instances/:id —— 本人实例详情（含主机账号密码等敏感字段）。 */
export function fetchInstance(id: number): Promise<InstanceDetail> {
  return http.get<InstanceDetail>(`/instances/${id}`, { auth: 'member' })
}

/** POST /api/v1/instances/:id/power —— 电源操作（上游异步受理，返回仅表示指令已提交）。 */
export function powerInstance(id: number, op: PowerOp): Promise<InstanceActionResult> {
  return http.post<InstanceActionResult>(`/instances/${id}/power`, { op }, { auth: 'member' })
}

/** GET /api/v1/instances/:id/reinstall-options —— 重装可选系统列表。 */
export function fetchReinstallOptions(id: number): Promise<ReinstallOptions> {
  return http.get<ReinstallOptions>(`/instances/${id}/reinstall-options`, { auth: 'member' })
}

/** POST /api/v1/instances/:id/reinstall —— 发起重装（os_id 取自可选系统列表）。 */
export function reinstallInstance(
  id: number,
  input: { os_id: number; port?: number },
): Promise<InstanceActionResult> {
  return http.post<InstanceActionResult>(`/instances/${id}/reinstall`, input, { auth: 'member' })
}

/**
 * POST /api/v1/instances/:id/reset-password —— 重置密码。
 * password 省略/空串 = 服务端生成 16 位强密码，最终密码在响应中回带。
 */
export function resetInstancePassword(
  id: number,
  password?: string,
): Promise<InstanceActionResult> {
  const body = password ? { password } : {}
  return http.post<InstanceActionResult>(`/instances/${id}/reset-password`, body, {
    auth: 'member',
  })
}

/** POST /api/v1/instances/:id/renew —— 续费下单（返回 type=renew 的待支付订单，不支持优惠码）。 */
export function renewInstance(id: number, cycle: BillingCycle): Promise<Order> {
  return http.post<Order>(`/instances/${id}/renew`, { cycle }, { auth: 'member' })
}

/** POST /api/v1/instances/:id/cancel —— 提交取消（终止）申请；已有在途申请时幂等返回。 */
export function cancelInstance(id: number, input: CancelInstanceInput): Promise<CancelResult> {
  return http.post<CancelResult>(`/instances/${id}/cancel`, input, { auth: 'member' })
}

/** GET /api/v1/instances/:id/logs —— 实例操作记录分页（新记录在前）。 */
export function listInstanceLogs(
  id: number,
  params: { page?: number; page_size?: number } = {},
): Promise<Paged<InstanceLog>> {
  return http.get<Paged<InstanceLog>>(`/instances/${id}/logs`, {
    auth: 'member',
    query: { ...params },
  })
}
