// 登录回跳参数处理：只接受站内相对路径，避免开放重定向。

/** safeRedirect 校验 redirect 参数；非法（外站、协议相对 URL、空值）时返回 fallback。 */
export function safeRedirect(value: string | null | undefined, fallback = '/console'): string {
  if (!value) {
    return fallback
  }
  // 只允许以单个 "/" 开头的站内路径：排除 "//evil.com"、"https://evil.com"、"javascript:"
  if (!value.startsWith('/') || value.startsWith('//')) {
    return fallback
  }
  return value
}

/** buildLoginUrl 生成带 redirect 参数的登录链接（保留查询串）。 */
export function buildLoginUrl(redirect: string, fallback = '/console'): string {
  const target = safeRedirect(redirect, fallback)
  return `/login?redirect=${encodeURIComponent(target)}`
}

/** buildRegisterUrl 生成带 redirect 参数的注册链接。 */
export function buildRegisterUrl(redirect: string, fallback = '/console'): string {
  const target = safeRedirect(redirect, fallback)
  return `/register?redirect=${encodeURIComponent(target)}`
}

/**
 * buildAdminLoginUrl 生成后台登录链接（回跳默认回后台首页，不落到会员区）。
 * 与会员登录共用 safeRedirect 校验：只接受站内相对路径，避免开放重定向。
 */
export function buildAdminLoginUrl(redirect: string, fallback = '/admin'): string {
  const target = safeRedirect(redirect, fallback)
  return `/admin/login?redirect=${encodeURIComponent(target)}`
}
