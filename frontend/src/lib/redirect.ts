// 登录回跳参数处理：只接受站内相对路径，避免开放重定向。
export const DEFAULT_REDIRECT = '/'

/** 校验 redirect 参数；非法（外站、协议相对 URL、空值）时返回 fallback。 */
export function safeRedirect(value: string | null | undefined, fallback = DEFAULT_REDIRECT): string {
  if (!value) {
    return fallback
  }
  // 只允许以单个 "/" 开头的站内路径：排除 "//evil.com"、"https://evil.com"、"javascript:"。
  if (!value.startsWith('/') || value.startsWith('//')) {
    return fallback
  }
  return value
}

/** 生成带 redirect 参数的登录链接（保留查询串）。 */
export function buildLoginUrl(redirect: string, fallback = DEFAULT_REDIRECT): string {
  const target = safeRedirect(redirect, fallback)
  return `/login?redirect=${encodeURIComponent(target)}`
}

/** 生成带 redirect 参数的注册链接。 */
export function buildRegisterUrl(redirect: string, fallback = DEFAULT_REDIRECT): string {
  const target = safeRedirect(redirect, fallback)
  return `/register?redirect=${encodeURIComponent(target)}`
}
