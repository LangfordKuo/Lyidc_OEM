import { CYCLE_LABELS, CYCLE_MONTHS, type BillingCycle } from './cycles'

const dateTimeFormatter = new Intl.DateTimeFormat('zh-CN', {
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hour12: false,
})

/** 把 RFC3339 时间串格式化为本地时间；无法解析时原样返回。 */
export function formatDateTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return dateTimeFormatter.format(date)
}

/** 空值/未交付场景的占位版本。 */
export function formatDateTimeOr(value: string | null | undefined, fallback = '—'): string {
  return value ? formatDateTime(value) : fallback
}

/** 金额（定点小数字符串）统一显示两位小数；非法值原样返回。 */
export function formatAmount(value: string | number | null | undefined): string {
  if (value === null || value === undefined || value === '') {
    return '—'
  }
  const n = Number(value)
  if (!Number.isFinite(n)) {
    return String(value)
  }
  return n.toFixed(2)
}

/** 带货币符号的金额，用于价格与余额展示（全站人民币）。非法输入原样返回。 */
export function formatMoney(value: string | number | null | undefined): string {
  const amount = formatAmount(value)
  return Number.isFinite(Number(amount)) ? `¥${amount}` : amount
}

/** 周期中文名，未知周期原样返回。 */
export function formatCycleLabel(cycle: string): string {
  return CYCLE_LABELS[cycle as BillingCycle] ?? cycle
}

/** 按周期折算月均价（整数分计算，避免浮点误差）。 */
export function monthlyEquivalent(
  amount: string | null | undefined,
  cycle: BillingCycle,
): string | null {
  if (amount === null || amount === undefined || amount === '') {
    return null
  }
  const cents = Math.round(Number(amount) * 100)
  if (!Number.isFinite(cents)) {
    return null
  }
  const months = CYCLE_MONTHS[cycle]
  // 四舍五入到分（half-up），与后端金额口径一致。
  const perMonth = Math.round(cents / months)
  return (perMonth / 100).toFixed(2)
}

/** 周期时长文案，如「12 个月」。 */
export function formatDuration(cycle: BillingCycle): string {
  return `${CYCLE_MONTHS[cycle]} 个月`
}

/**
 * 把上游商品描述原文（含 HTML 实体与标签）转成可读纯文本。
 * 契约 10.3 备注 4：上游原文含 `<li>` 之类标签与 HTML 实体；这里只做实体反转义 + 去标签，
 * 以纯文本渲染，避免 dangerouslySetInnerHTML 带来的 XSS 面。
 */
export function decodeDescription(value: string | null | undefined): string {
  if (!value) {
    return ''
  }
  const unescaped = value
    .replace(/&lt;/gi, '<')
    .replace(/&gt;/gi, '>')
    .replace(/&quot;/gi, '"')
    .replace(/&#39;/g, "'")
    .replace(/&nbsp;/gi, ' ')
    .replace(/&amp;/gi, '&')
  return unescaped
    .replace(/<\s*br\s*\/?\s*>/gi, '\n')
    .replace(/<\/\s*(p|div|li|tr)\s*>/gi, '\n')
    .replace(/<[^>]*>/g, '')
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line.length > 0)
    .join('\n')
}
