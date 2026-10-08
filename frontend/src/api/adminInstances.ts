import { http } from './client'
import type {
  AdminInstance,
  AdminInstanceDetail,
  CancelResult,
  CancelType,
  InstanceActionResult,
  InstanceLog,
  InstanceStatus,
  InstanceSyncResult,
  Paged,
} from './types'

// 管理端实例接口（契约 14.4 / 15.3 / 15.8）。
//
// 角色矩阵：
//   - 列表 / 详情 / 操作记录 / 同步（sync）：admin + finance + support 均可；
//   - 暂停（suspend）/ 恢复（unsuspend）/ 终止申请（cancel）：**仅 admin**（其余角色 403）。
// 界面侧按同一矩阵隐藏或禁用入口，服务端仍会独立校验（403 统一处理）。

/** GET /api/v1/admin/instances —— 全站实例分页（member_id/status 过滤，列表不含敏感字段）。 */
export function listAdminInstances(
  params: { page?: number; page_size?: number; member_id?: number; status?: InstanceStatus } = {},
): Promise<Paged<AdminInstance>> {
  return http.get<Paged<AdminInstance>>('/admin/instances', { auth: 'admin', query: { ...params } })
}

/** GET /api/v1/admin/instances/:id —— 实例详情（阶段 8 新增：含主机账号密码与 member_id）。 */
export function fetchAdminInstance(id: number): Promise<AdminInstanceDetail> {
  return http.get<AdminInstanceDetail>(`/admin/instances/${id}`, { auth: 'admin' })
}

/** GET /api/v1/admin/instances/:id/logs —— 操作记录分页（新记录在前）。 */
export function listAdminInstanceLogs(
  id: number,
  params: { page?: number; page_size?: number } = {},
): Promise<Paged<InstanceLog>> {
  return http.get<Paged<InstanceLog>>(`/admin/instances/${id}/logs`, {
    auth: 'admin',
    query: { ...params },
  })
}

/** POST /api/v1/admin/instances/:id/suspend —— 暂停实例（仅 admin；reason 必填，同原因提交上游并审计）。 */
export function suspendAdminInstance(id: number, reason: string): Promise<InstanceActionResult> {
  return http.post<InstanceActionResult>(`/admin/instances/${id}/suspend`, { reason }, { auth: 'admin' })
}

/** POST /api/v1/admin/instances/:id/unsuspend —— 恢复实例（仅 admin）。 */
export function unsuspendAdminInstance(id: number): Promise<InstanceActionResult> {
  return http.post<InstanceActionResult>(`/admin/instances/${id}/unsuspend`, undefined, {
    auth: 'admin',
  })
}

/** POST /api/v1/admin/instances/:id/sync —— 回读上游并收敛本地状态（所有角色）。 */
export function syncAdminInstance(id: number): Promise<InstanceSyncResult> {
  return http.post<InstanceSyncResult>(`/admin/instances/${id}/sync`, undefined, {
    auth: 'admin',
    // 同步会回读上游 hostinfo + 电源状态，超时放宽到 1 分钟。
    timeoutMs: 60_000,
  })
}

/** POST /api/v1/admin/instances/:id/cancel —— 管理员代客/强制提交终止申请（仅 admin；reason 必填）。 */
export function cancelAdminInstance(
  id: number,
  input: { type: CancelType; reason: string },
): Promise<CancelResult> {
  return http.post<CancelResult>(`/admin/instances/${id}/cancel`, input, { auth: 'admin' })
}
