import type { EpayType, PayChannel } from '../api/types'

// 支付方式口径（契约 12.2 / 12.4）：本批在线支付仅易支付（支付宝 / 微信），另一路是余额支付。

export interface PayMethod {
  value: PayChannel
  title: string
  description: string
}

export const PAY_METHODS: PayMethod[] = [
  { value: 'epay', title: '在线支付', description: '跳转易支付收银台，支持支付宝 / 微信' },
  { value: 'balance', title: '余额支付', description: '使用账户余额即时扣款并自动开通' },
]

export const EPAY_TYPES: { value: EpayType; label: string }[] = [
  { value: 'alipay', label: '支付宝' },
  { value: 'wxpay', label: '微信支付' },
]

export function epayTypeLabel(value: string): string {
  return EPAY_TYPES.find((item) => item.value === value)?.label ?? value
}

/** payurl 被协议白名单拒绝时的可读提示（调用方用页面既有的错误提示方式展示，不静默失败）。 */
export const PAYURL_REJECTED_MESSAGE =
  '支付地址不合法（仅允许 http / https 绝对地址），已阻止跳转；请稍后重试或联系客服核对支付渠道配置'

/**
 * 判断渠道返回的 payurl 是否允许跳转：必须是**绝对地址**且协议为 `http` / `https`
 * （契约 12.2.1，P5 安全整改 finding#07）。
 *
 * 只放行这两种协议的原因：`javascript:` / `data:` / `vbscript:` 会让网关（或网关被指向的
 * 攻击者、链路中间人）在**本站源**里执行任意 JS，直接读到 localStorage 里的会员 JWT 并外带；
 * `//evil` 与 `/relative` 则是开放重定向。非字符串与空值同样拒绝。
 */
export function isAllowedPayurl(payurl: unknown): payurl is string {
  if (typeof payurl !== 'string') {
    return false
  }
  const trimmed = payurl.trim()
  if (!trimmed) {
    return false
  }
  let parsed: URL
  try {
    // 相对地址（`/x`）与协议相对地址（`//evil`）在没有 base 时直接抛错，正是我们要拒绝的形态。
    parsed = new URL(trimmed)
  } catch {
    return false
  }
  return parsed.protocol === 'http:' || parsed.protocol === 'https:'
}

/**
 * 跳出本站前往渠道收银台（回跳地址由后端 return_url 设置决定）。
 *
 * **本函数是唯一的跳转出口，因此协议白名单必须在这里兜底**：不合法一律不跳转并返回 false，
 * 由调用方给出可读提示（`PAYURL_REJECTED_MESSAGE`）。
 * 抽成函数便于测试替身：单测里 mock 掉它就不会真正触发 jsdom 导航。
 *
 * @returns true = 已发起跳转；false = 地址被拒绝，**没有**发生跳转。
 */
export function gotoPayurl(payurl: unknown): boolean {
  if (!isAllowedPayurl(payurl)) {
    return false
  }
  window.location.assign(payurl)
  return true
}
