import type { MemberStatus } from '../api/types'
import type { StatusTone } from './orderStatus'

// 会员状态文案与色调（契约 6.1：active / disabled）。

export const MEMBER_STATUS_LABELS: Record<MemberStatus, string> = {
  active: '正常',
  disabled: '已禁用',
}

export const MEMBER_STATUS_TONES: Record<MemberStatus, StatusTone> = {
  active: 'ok',
  disabled: 'error',
}

export function memberStatusLabel(status: string): string {
  return MEMBER_STATUS_LABELS[status as MemberStatus] ?? status
}

export function memberStatusTone(status: string): StatusTone {
  return MEMBER_STATUS_TONES[status as MemberStatus] ?? 'pending'
}
