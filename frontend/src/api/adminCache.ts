import { http } from './client'
import type {
  CacheFlushResult,
  CacheSettings,
  CacheStatsReset,
  CacheStatus,
  UpdateCacheSettingsInput,
} from './types'

// 缓存基础设施接口（R19 批 1，契约第 24 节）：**五个接口全部仅 admin**（含读取）。
//
// 默认是文件缓存（无需配置）；Redis 默认关闭，由管理员在设置页选择并填参数后启用。
// 设置保存后**立即生效**（不用重启）；Redis 不可用时后端自动降级到文件缓存（degraded=true）。
// 口令三态与其他密钥一致——省略 = 保持不变、给值 = 替换、空串 = 清空；接口只回掩码。

/** GET /api/v1/admin/settings/cache —— 缓存设置（驱动 + Redis 参数 + 当前运行状态）。 */
export function fetchCacheSettings(): Promise<CacheSettings> {
  return http.get<CacheSettings>('/admin/settings/cache', { auth: 'admin' })
}

/** PUT /api/v1/admin/settings/cache —— 局部更新（至少一个字段），保存即生效。 */
export function updateCacheSettings(input: UpdateCacheSettingsInput): Promise<CacheSettings> {
  return http.put<CacheSettings>('/admin/settings/cache', input, { auth: 'admin' })
}

/**
 * GET /api/v1/admin/cache/status —— 运行状态：驱动 / 降级与原因 / 键数量 / 命中率 / 连接信息。
 * 该请求同时是一次探活（降级期间 Redis 恢复可用会被自动切回）。
 */
export function fetchCacheStatus(): Promise<CacheStatus> {
  return http.get<CacheStatus>('/admin/cache/status', { auth: 'admin' })
}

/** POST /api/v1/admin/cache/reset-stats —— 重置进程内命中统计（不动缓存内容）。 */
export function resetCacheStats(): Promise<CacheStatsReset> {
  return http.post<CacheStatsReset>('/admin/cache/reset-stats', undefined, { auth: 'admin' })
}

/** POST /api/v1/admin/cache/flush —— 清空缓存（只清缓存、不碰业务数据）。 */
export function flushCache(): Promise<CacheFlushResult> {
  return http.post<CacheFlushResult>('/admin/cache/flush', undefined, { auth: 'admin' })
}
