// 计费周期：契约 10.2 定义的 6 个本地周期名（上游字段名只出现在上游原文里）。
export const BILLING_CYCLES = [
  'monthly',
  'quarterly',
  'semiannual',
  'annual',
  'biennial',
  'triennial',
] as const

export type BillingCycle = (typeof BILLING_CYCLES)[number]

/** 中文显示名：契约 10.2「中文显示名」列，接口不下发，由前端展示。 */
export const CYCLE_LABELS: Record<BillingCycle, string> = {
  monthly: '月付',
  quarterly: '季付',
  semiannual: '半年付',
  annual: '年付',
  biennial: '两年付',
  triennial: '三年付',
}

/** 周期月数：用于折算「月均价」与展示时长。 */
export const CYCLE_MONTHS: Record<BillingCycle, number> = {
  monthly: 1,
  quarterly: 3,
  semiannual: 6,
  annual: 12,
  biennial: 24,
  triennial: 36,
}

/** 周期价格表：六个键始终存在，null 表示该周期不可售（契约 10.3）。 */
export type CyclePrices = Record<BillingCycle, string | null>

export function isBillingCycle(value: unknown): value is BillingCycle {
  return typeof value === 'string' && (BILLING_CYCLES as readonly string[]).includes(value)
}

/** 把任意周期字符串安全转成 BillingCycle，非法值返回 null。 */
export function toBillingCycle(value: string | null | undefined): BillingCycle | null {
  return isBillingCycle(value) ? value : null
}

/** 按标准周期顺序返回可售（价格非 null）的周期。 */
export function availableCycles(prices: CyclePrices): BillingCycle[] {
  return BILLING_CYCLES.filter((cycle) => prices[cycle] !== null && prices[cycle] !== undefined)
}

/** 返回最低价周期（同价时取周期更短者），全部不可售时返回 null。 */
export function cheapestCycle(prices: CyclePrices): { cycle: BillingCycle; amount: string } | null {
  let best: { cycle: BillingCycle; amount: string } | null = null
  for (const cycle of BILLING_CYCLES) {
    const amount = prices[cycle]
    if (amount === null || amount === undefined) {
      continue
    }
    if (best === null || Number(amount) < Number(best.amount)) {
      best = { cycle, amount }
    }
  }
  return best
}
