// 到期时间展示口径：后端输出 RFC3339 UTC 字符串，这里只做「距今天数」的展示层判断。
// 7 天与契约 17.5 的到期提醒缺省天数（expiry_reminder_days=7）保持一致：会员端接口不下发该设置，
// 因此列表用固定 7 天做「临期」高亮，仅影响颜色，不影响任何业务判定。
export const EXPIRY_SOON_DAYS = 7

export type ExpiryState = 'none' | 'ok' | 'soon' | 'expired'

/** 距到期的整天数（向上取整）：已过期为负值；时间无法解析返回 null。 */
export function daysUntil(value: string | null | undefined, now: Date = new Date()): number | null {
  if (!value) {
    return null
  }
  const target = new Date(value)
  if (Number.isNaN(target.getTime())) {
    return null
  }
  return Math.ceil((target.getTime() - now.getTime()) / 86_400_000)
}

export function expiryState(
  value: string | null | undefined,
  soonDays = EXPIRY_SOON_DAYS,
): ExpiryState {
  const days = daysUntil(value)
  if (days === null) {
    return 'none'
  }
  if (days < 0) {
    return 'expired'
  }
  return days <= soonDays ? 'soon' : 'ok'
}

/** 到期文案：如「12 天后到期」「今天到期」「已过期 3 天」；无到期时间返回空串。 */
export function expiryText(value: string | null | undefined): string {
  const days = daysUntil(value)
  if (days === null) {
    return ''
  }
  if (days < 0) {
    return `已过期 ${Math.abs(days)} 天`
  }
  if (days === 0) {
    return '今天到期'
  }
  return `${days} 天后到期`
}

/** 到期时间的文本颜色（临期警告、过期危险）。 */
export function expiryColorClass(state: ExpiryState): string {
  switch (state) {
    case 'soon':
      return 'text-amber-600 dark:text-amber-400'
    case 'expired':
      return 'text-destructive'
    default:
      return 'text-foreground'
  }
}
