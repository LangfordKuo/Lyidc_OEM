import { clearMemberToken, getMemberToken } from './tokens'

// 统一响应包，字段与 backend/internal/response.Envelope 一一对应。
// 契约文档：docs/api-contract.md 第 2 节。
export interface ApiEnvelope<T> {
  code: number
  message: string
  data: T
}

// 开发环境走 Vite 代理（/api → 127.0.0.1:8080），可用 VITE_API_BASE_URL 覆盖。
export const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? '/api/v1'

// 默认请求超时：避免后端未启动时页面无限转圈。
export const DEFAULT_TIMEOUT_MS = 15000

export type HttpMethod = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
export type QueryValue = string | number | boolean | null | undefined
export type QueryParams = Record<string, QueryValue>

export interface RequestOptions {
  method?: HttpMethod
  /** 请求体：自动 JSON 序列化并补 Content-Type。 */
  body?: unknown
  /** 查询参数：null / undefined 会被跳过。 */
  query?: QueryParams
  /** 是否注入会员 token（Authorization: Bearer）。 */
  auth?: boolean
  headers?: Record<string, string>
  signal?: AbortSignal
  timeoutMs?: number
}

/** ApiError 承载业务错误码与响应包 data，便于调用方按需展示（契约第 2/3 节）。 */
export class ApiError extends Error {
  readonly code: number
  readonly data: unknown
  readonly status: number

  constructor(code: number, message: string, data: unknown = null, status = 0) {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.data = data
    this.status = status
  }

  /** 未认证/凭证失效（契约错误码 401）。 */
  get isUnauthorized(): boolean {
    return this.code === 401
  }

  /** 账号被禁用或权限不足（契约错误码 403）。 */
  get isForbidden(): boolean {
    return this.code === 403
  }

  /** 资源不存在（契约错误码 404）。 */
  get isNotFound(): boolean {
    return this.code === 404
  }

  /** 参数/业务校验失败（40001 / 40002 / 40003）：message 可直接展示给用户。 */
  get isValidation(): boolean {
    return this.code === 40001 || this.code === 40002 || this.code === 40003
  }
}

// ---------------------------------------------------------------------------
// 未认证回调：会员 token 失效（401）时清本地 token 并通知上层清登录态（路由守卫据此跳登录页）。
// ---------------------------------------------------------------------------
type UnauthorizedListener = () => void

const unauthorizedListeners = new Set<UnauthorizedListener>()

export function onUnauthorized(listener: UnauthorizedListener): () => void {
  unauthorizedListeners.add(listener)
  return () => {
    unauthorizedListeners.delete(listener)
  }
}

function notifyUnauthorized(): void {
  clearMemberToken()
  for (const listener of unauthorizedListeners) {
    listener()
  }
}

// ---------------------------------------------------------------------------
// 请求实现
// ---------------------------------------------------------------------------
function buildUrl(path: string, query?: QueryParams): string {
  const url = `${API_BASE_URL}${path}`
  if (!query) {
    return url
  }
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null) {
      continue
    }
    search.append(key, String(value))
  }
  const qs = search.toString()
  return qs ? `${url}?${qs}` : url
}

function buildHeaders(options: RequestOptions): Headers {
  const headers = new Headers({ Accept: 'application/json' })
  if (options.body !== undefined) {
    headers.set('Content-Type', 'application/json')
  }
  if (options.auth) {
    const token = getMemberToken()
    if (token) {
      headers.set('Authorization', `Bearer ${token}`)
    }
  }
  for (const [key, value] of Object.entries(options.headers ?? {})) {
    headers.set(key, value)
  }
  return headers
}

async function readEnvelope<T>(response: Response): Promise<ApiEnvelope<T>> {
  let text: string
  try {
    text = await response.text()
  } catch (error) {
    throw new ApiError(
      -1,
      `读取响应失败：${error instanceof Error ? error.message : String(error)}`,
      null,
      response.status,
    )
  }
  if (!text) {
    // 空响应体：HTTP 成功时按「无数据」处理（本系统接口一律带响应包，属兜底分支）。
    if (response.ok) {
      return { code: 0, message: 'ok', data: null as T }
    }
    throw new ApiError(response.status, `请求失败（HTTP ${response.status}）`, null, response.status)
  }
  try {
    return JSON.parse(text) as ApiEnvelope<T>
  } catch {
    throw new ApiError(-1, `响应不是合法 JSON（HTTP ${response.status}）`, null, response.status)
  }
}

/**
 * request 调用后端接口并统一解析 `{code, message, data}`：
 * `code === 0` 返回 `data`，否则抛出 ApiError（携带 code / message / data / HTTP 状态）。
 */
export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { auth, method = 'GET', timeoutMs = DEFAULT_TIMEOUT_MS } = options

  if (auth && !getMemberToken()) {
    // 未登录时不必打网络：直接给出与后端一致的 401 语义。
    throw new ApiError(401, '请先登录后再操作', null, 401)
  }

  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeoutMs)
  const externalSignal = options.signal
  const abortFromExternal = () => controller.abort()
  externalSignal?.addEventListener('abort', abortFromExternal)

  let response: Response
  try {
    response = await fetch(buildUrl(path, options.query), {
      method,
      headers: buildHeaders(options),
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
      signal: controller.signal,
    })
  } catch (error) {
    if (controller.signal.aborted && !externalSignal?.aborted) {
      throw new ApiError(-1, `请求超时（${timeoutMs / 1000}s），请稍后重试`)
    }
    if (externalSignal?.aborted) {
      throw new ApiError(-1, '请求已取消')
    }
    throw new ApiError(
      -1,
      `无法连接后端服务：${error instanceof Error ? error.message : String(error)}`,
    )
  } finally {
    clearTimeout(timer)
    externalSignal?.removeEventListener('abort', abortFromExternal)
  }

  const envelope = await readEnvelope<T>(response)

  if (typeof envelope?.code !== 'number') {
    throw new ApiError(-1, `响应缺少 code 字段（HTTP ${response.status}）`, null, response.status)
  }
  if (!response.ok || envelope.code !== 0) {
    const error = new ApiError(
      envelope.code,
      envelope.message || `请求失败（HTTP ${response.status}）`,
      envelope.data,
      response.status,
    )
    if (error.isUnauthorized && auth) {
      notifyUnauthorized()
    }
    throw error
  }
  return envelope.data
}

/** http 是 request 的薄封装，按方法直取，减少每次调用都要写 method 的样板。 */
export const http = {
  get: <T>(path: string, options: Omit<RequestOptions, 'method' | 'body'> = {}) =>
    request<T>(path, { ...options, method: 'GET' }),
  post: <T>(path: string, body?: unknown, options: Omit<RequestOptions, 'method' | 'body'> = {}) =>
    request<T>(path, { ...options, method: 'POST', body }),
  put: <T>(path: string, body?: unknown, options: Omit<RequestOptions, 'method' | 'body'> = {}) =>
    request<T>(path, { ...options, method: 'PUT', body }),
  del: <T>(path: string, options: Omit<RequestOptions, 'method' | 'body'> = {}) =>
    request<T>(path, { ...options, method: 'DELETE' }),
}

/** 把任意异常转成可直接展示的中文提示。 */
export function errorMessage(error: unknown, fallback = '操作失败，请稍后重试'): string {
  if (error instanceof ApiError) {
    return error.message || fallback
  }
  if (error instanceof Error) {
    return error.message || fallback
  }
  return fallback
}
