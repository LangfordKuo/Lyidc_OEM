import { request } from './client'

// HealthData 是 GET /api/v1/health 响应包中的 data。
export interface HealthData {
  status: string
  db: string
  time: string
}

// fetchHealth 调用健康检查接口。
export function fetchHealth(): Promise<HealthData> {
  return request<HealthData>('/health')
}

// isHealthData 判断未知数据是否形如健康检查数据体（后端降级响应也会带上该结构）。
export function isHealthData(value: unknown): value is HealthData {
  if (typeof value !== 'object' || value === null) {
    return false
  }
  const candidate = value as Partial<HealthData>
  return (
    typeof candidate.status === 'string' &&
    typeof candidate.db === 'string' &&
    typeof candidate.time === 'string'
  )
}
