import { http } from './client'
import type { ClearOperationLogsResult, OperationLog, OplogChannel, OplogStatus, Paged } from './types'

// 管理端操作日志接口（契约第 23 节）。
// 权限：列表与清空**都仅 admin**（finance / support 403、会员/匿名 401）——
// 界面侧按同一矩阵隐藏入口并不发越权请求（23.8 / 23.11）。
// 硬约束：**没有**按条删除或修改的接口，只允许整体清空（23.4.3）。

/** 列表查询参数（契约 23.4.1；未提供的参数即「不筛选」）。 */
export interface OperationLogQuery {
  page?: number
  page_size?: number
  channel?: OplogChannel
  /** 0 = 系统 / 定时任务 / 未登录尝试。 */
  actor_id?: number
  status?: OplogStatus
  /** 全文关键字：**至少 2 个字符**，否则后端 40001（契约 23.5）。 */
  keyword?: string
  /** 时间范围（闭区间）：RFC3339 或纯日期；纯日期的 end 按当日 23:59:59 解释。 */
  start?: string
  end?: string
}

/** GET /api/v1/admin/operation-logs —— 操作日志分页（新记录在前，高级搜索见 23.4.1）。 */
export function listOperationLogs(params: OperationLogQuery = {}): Promise<Paged<OperationLog>> {
  return http.get<Paged<OperationLog>>('/admin/operation-logs', { auth: 'admin', query: { ...params } })
}

/**
 * DELETE /api/v1/admin/operation-logs —— 清空（按通道或全部，契约 23.4.2）。
 * 清空前后端会先冲出在途缓冲，因此返回时被清通道确实为空；清空动作本身会留痕。
 */
export function clearOperationLogs(
  channel: OplogChannel | 'all',
): Promise<ClearOperationLogsResult> {
  return http.del<ClearOperationLogsResult>('/admin/operation-logs', {
    auth: 'admin',
    query: { channel },
  })
}
