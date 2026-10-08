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

/**
 * 跳出本站前往渠道收银台（回跳地址由后端 return_url 设置决定）。
 * 抽成函数便于测试替身：单测里 mock 掉它就不会真正触发 jsdom 导航。
 */
export function gotoPayurl(payurl: string): void {
  window.location.assign(payurl)
}
