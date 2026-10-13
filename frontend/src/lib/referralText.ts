import type { StatusTone } from './orderStatus'

// 邀请返佣文案（契约 18）：出账申请状态、类型、渠道、返佣状态、比例来源的中文名与色调。
// 状态机与枚举以契约 18.1 / 18.1.1 为权威；未知取值原样透出，避免前端吞掉后端新增枚举。

export const PAYOUT_STATUS_LABELS: Record<string, string> = {
  pending: '待审核',
  approved: '已通过',
  rejected: '已驳回',
  paid: '已打款',
}

export const PAYOUT_STATUS_TONES: Record<string, StatusTone> = {
  pending: 'warn',
  approved: 'ok',
  rejected: 'error',
  paid: 'ok',
}

export function payoutStatusLabel(status: string): string {
  return PAYOUT_STATUS_LABELS[status] ?? status
}

export function payoutStatusTone(status: string): StatusTone {
  return PAYOUT_STATUS_TONES[status] ?? 'pending'
}

export const PAYOUT_KIND_LABELS: Record<string, string> = {
  withdraw: '提现',
  convert: '转余额',
}

export function payoutKindLabel(kind: string): string {
  return PAYOUT_KIND_LABELS[kind] ?? kind
}

export const PAYOUT_CHANNEL_LABELS: Record<string, string> = {
  wechat: '微信',
  alipay: '支付宝',
}

export function payoutChannelLabel(channel: string): string {
  return PAYOUT_CHANNEL_LABELS[channel] ?? channel
}

export const REFERRAL_REBATE_STATUS_LABELS: Record<string, string> = {
  granted: '已发放',
  rolled_back: '已冲回',
}

export const REFERRAL_REBATE_STATUS_TONES: Record<string, StatusTone> = {
  granted: 'ok',
  rolled_back: 'error',
}

export function referralRebateStatusLabel(status: string): string {
  return REFERRAL_REBATE_STATUS_LABELS[status] ?? status
}

export function referralRebateStatusTone(status: string): StatusTone {
  return REFERRAL_REBATE_STATUS_TONES[status] ?? 'pending'
}

export const RATIO_SOURCE_LABELS: Record<string, string> = {
  product: '商品级',
  member: '会员特批',
  global: '全局默认',
}

export function ratioSourceLabel(source: string): string {
  return RATIO_SOURCE_LABELS[source] ?? source
}

export const COUPON_STATUS_LABELS: Record<string, string> = {
  on: '启用',
  off: '停用',
}

export const COUPON_STATUS_TONES: Record<string, StatusTone> = {
  on: 'ok',
  off: 'error',
}

export function couponStatusLabel(status: string): string {
  return COUPON_STATUS_LABELS[status] ?? status
}

export function couponStatusTone(status: string): StatusTone {
  return COUPON_STATUS_TONES[status] ?? 'pending'
}
