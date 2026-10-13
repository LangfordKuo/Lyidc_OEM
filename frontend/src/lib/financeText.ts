import type { LedgerType, RechargeStatus } from '../api/types'
import type { StatusTone } from './orderStatus'

// 充值单状态（契约 12.3）与余额流水类型（契约 12.3 的 ledger.type）文案。

export const RECHARGE_STATUS_LABELS: Record<RechargeStatus, string> = {
  pending: '待支付',
  paid: '已到账',
  closed: '已关闭',
}

export const RECHARGE_STATUS_TONES: Record<RechargeStatus, StatusTone> = {
  pending: 'warn',
  paid: 'ok',
  closed: 'pending',
}

export const LEDGER_TYPE_LABELS: Record<LedgerType, string> = {
  recharge: '充值入账',
  order_pay: '订单支付',
  refund: '退款',
  adjust: '人工调整',
  // 契约 18.1：返佣发放/冲回/提现冻结/退回（金额有符号）；会员端余额流水不过滤前不展示它。
  referral_rebate: '邀请返佣',
}

export function rechargeStatusLabel(status: string): string {
  return RECHARGE_STATUS_LABELS[status as RechargeStatus] ?? status
}

export function rechargeStatusTone(status: string): StatusTone {
  return RECHARGE_STATUS_TONES[status as RechargeStatus] ?? 'pending'
}

export function ledgerTypeLabel(type: string): string {
  return LEDGER_TYPE_LABELS[type as LedgerType] ?? type
}
