import type { OperationLog, OplogChannel, OplogStatus } from '@/api/types'
import type { StatusTone } from '@/lib/orderStatus'

// 操作日志（契约第 23 节）的文案与色调映射：通道名、结果、以及展示口径。

/** 通道中文名（与侧栏「日志」三项一一对应，契约 23.11）。 */
export const OPLOG_CHANNEL_LABELS: Record<OplogChannel, string> = {
  admin: '管理员日志',
  member: '用户日志',
  scheduler: '定时任务日志',
}

/** 结果中文名（成功 / 失败）。 */
export const OPLOG_STATUS_LABELS: Record<OplogStatus, string> = {
  success: '成功',
  failed: '失败',
}

/** 结果色调：失败标红，成功用 ok 色（色调只从 statusTone 出口取，不在页面硬编码颜色）。 */
export function oplogStatusTone(status: OplogStatus): StatusTone {
  return status === 'failed' ? 'error' : 'ok'
}

/**
 * 操作人展示：`actor_name` 是**用户名快照**（账号改名或删除后日志仍可读），
 * 空串表示「没有具体操作人」——定时任务与被鉴权拒绝的尝试都是空串，统一显示占位符。
 */
export function oplogActorName(log: Pick<OperationLog, 'actor_name'>): string {
  return log.actor_name || '—'
}

/**
 * 「命中较多」提示阈值：关键字检索命中这么多条以后，建议加时间窗或通道过滤
 * （契约 23.10 的实测结论：耗时与命中集合大小成正比，命中数万条的泛词搜索没有参考价值）。
 */
export const OPLOG_LARGE_RESULT_THRESHOLD = 1000

/** 命中较多的提示文案（N = 命中条数）。 */
export function oplogLargeResultHint(total: number): string {
  return `命中 ${total} 条，建议加时间窗或通道过滤以加快检索，或换更精确的关键字`
}
