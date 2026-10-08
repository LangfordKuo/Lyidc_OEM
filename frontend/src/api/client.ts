// 统一响应包，字段与 backend/internal/response.Envelope 一一对应。
// 契约文档：docs/api-contract.md
export interface ApiEnvelope<T> {
  code: number
  message: string
  data: T
}

// 开发环境走 Vite 代理（/api → 127.0.0.1:8080），可用 VITE_API_BASE_URL 覆盖。
export const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? '/api/v1'

// ApiError 承载业务错误码与响应包 data，便于调用方按需展示。
export class ApiError extends Error {
  readonly code: number
  readonly data: unknown

  constructor(code: number, message: string, data: unknown = null) {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.data = data
  }
}

// request 调用后端接口并统一解析 {code, message, data}：
// code === 0 返回 data，否则抛出 ApiError。
export async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const url = `${API_BASE_URL}${path}`
  const headers = {
    Accept: 'application/json',
    ...(init.headers as Record<string, string> | undefined),
  }

  let response: Response
  try {
    response = await fetch(url, { ...init, headers })
  } catch (error) {
    throw new ApiError(0, `无法连接后端服务：${error instanceof Error ? error.message : String(error)}`)
  }

  let envelope: ApiEnvelope<T>
  try {
    envelope = (await response.json()) as ApiEnvelope<T>
  } catch {
    throw new ApiError(response.status, `响应不是合法 JSON（HTTP ${response.status}）`)
  }

  if (typeof envelope?.code !== 'number') {
    throw new ApiError(response.status, `响应缺少 code 字段（HTTP ${response.status}）`)
  }
  if (!response.ok || envelope.code !== 0) {
    throw new ApiError(envelope.code, envelope.message || `请求失败（HTTP ${response.status}）`, envelope.data)
  }
  return envelope.data
}
