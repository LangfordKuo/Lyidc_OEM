import { http } from './client'
import type {
  DbBackup,
  DbBackupList,
  DbBackupRestoreResult,
  DeleteDbBackupResult,
  RestoreDbBackupInput,
} from './types'

// 数据库备份与恢复接口（契约 21.3，R14）：**全部仅 admin 角色**（含读取）。
//
// 只输出元信息——**没有任何接口能读到备份文件内容**（契约 21.4：不可下载）。
// 恢复与删除只接受备份 ID（形状白名单），不接受任何路径参数。
// 超时放宽：导出与恢复是同步操作，大库可能耗时较久（导出 5 分钟 / 列表 30 秒）。

/** GET /api/v1/admin/db-backups —— 目录状态 + 备份列表（元信息）+ 总占用。 */
export function fetchDbBackups(): Promise<DbBackupList> {
  return http.get<DbBackupList>('/admin/db-backups', { auth: 'admin', timeoutMs: 30_000 })
}

/**
 * POST /api/v1/admin/db-backups —— 立即备份一次（同步返回元信息）。
 * 同一时刻至多一个写操作，重复提交后端返回 409。
 */
export function createDbBackup(): Promise<DbBackup> {
  return http.post<DbBackup>('/admin/db-backups', undefined, {
    auth: 'admin',
    timeoutMs: 300_000,
  })
}

/**
 * POST /api/v1/admin/db-backups/:id/restore —— **覆盖式**恢复（不可撤销）。
 * 需要 confirm=true 与当前管理员密码；后端拒绝时抛 ApiError（40002 带可读原因）。
 */
export function restoreDbBackup(
  id: string,
  input: RestoreDbBackupInput,
): Promise<DbBackupRestoreResult> {
  return http.post<DbBackupRestoreResult>(
    `/admin/db-backups/${encodeURIComponent(id)}/restore`,
    input,
    { auth: 'admin', timeoutMs: 300_000 },
  )
}

/** DELETE /api/v1/admin/db-backups/:id —— 删除一条备份（文件 + 元信息）。 */
export function deleteDbBackup(id: string): Promise<DeleteDbBackupResult> {
  return http.del<DeleteDbBackupResult>(`/admin/db-backups/${encodeURIComponent(id)}`, {
    auth: 'admin',
  })
}
