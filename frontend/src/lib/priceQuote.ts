import type {
  ConfigGroup,
  ConfigOption,
  ConfigOptionValue,
  PriceBreakdown,
  PriceDeltaLine,
} from '@/api/types'
import type { BillingCycle } from './cycles'
import { isQuantityOption } from './configControl'
import { optionLabel } from './productText'

/**
 * 配置差价实时预览（R7，契约 10.2.1）。
 *
 * 成交价 = 基础价 + Σ 配置差价；差价已随商品定价模式换算（后端在 `values[].pricings`
 * 里下发的是**已调整**的单位差价），前端只做累加，**不重套定价规则**。
 *
 * 数量型差价 = 单位差价 × 可计费数量，可计费数量 = 数量 − 低一档的 qty_maximum
 * （无低一档时为 0）。单档位时即退化为「单位差价 × 数量」。
 *
 * 全程整数分，与后端逐分一致；展示价只是预览，下单仍以服务端为准。
 */

/** 无差价数据标注（契约 10.2.1）：该项按 0.00 计，不阻断下单。 */
export const NO_PRICING_DATA_NOTE = '无差价数据'

/** 把金额字符串解析为整数分；非法/空值返回 null。 */
export function toCents(amount: string | null | undefined): number | null {
  if (amount === null || amount === undefined) {
    return null
  }
  const text = String(amount).trim()
  if (!/^\d+(\.\d{1,2})?$/.test(text)) {
    return null
  }
  const [intPart, fracPart = ''] = text.split('.')
  const frac = (fracPart + '00').slice(0, 2)
  const cents = Number(intPart) * 100 + Number(frac)
  return Number.isFinite(cents) ? cents : null
}

/** 整数分格式化为两位小数字符串（与后端 FormatAmount 一致）。 */
export function fromCents(cents: number): string {
  const sign = cents < 0 ? '-' : ''
  const abs = Math.abs(Math.trunc(cents))
  return `${sign}${Math.floor(abs / 100)}.${String(abs % 100).padStart(2, '0')}`
}

/**
 * 数量型的可计费数量：数量 − 低一档的 qty_maximum。
 * 档位区间取自该配置项全部值的 qty_minimum/qty_maximum（后端下发的并集）。
 */
export function billableQuantity(
  option: ConfigOption,
  selectedValue: ConfigOptionValue | undefined,
  qty: number,
): number {
  if (!selectedValue) {
    return 0
  }
  // 低一档上限 = 所有「上界 < 当前档下界」的档位的最大上界；无低一档时为 0。
  let floor = 0
  const currentMin = selectedValue.qty_minimum ?? 0
  for (const value of option.values) {
    const max = value.qty_maximum ?? 0
    if (max < currentMin && max > floor) {
      floor = max
    }
  }
  return Math.max(0, qty - floor)
}

/** 取某配置值在某周期的单位差价；无差价数据返回 null。 */
export function unitDelta(
  value: ConfigOptionValue | undefined,
  cycle: BillingCycle,
): { cents: number; hasData: boolean } {
  const raw = value?.pricings?.[cycle]
  if (raw === null || raw === undefined) {
    return { cents: 0, hasData: false }
  }
  const cents = toCents(raw)
  if (cents === null || cents < 0) {
    return { cents: 0, hasData: false }
  }
  return { cents, hasData: true }
}

/**
 * 按「基础价 + Σ 配置差价」生成成交价预览与明细。
 *
 * `base` 是该周期基础价（product.prices[cycle]），可为 null（不可售）。
 * 未知配置项/未知值跳过（与后端一致）；明细按配置项出现顺序输出，便于对照控件。
 */
export function quotePrice(
  groups: ConfigGroup[],
  config: Record<string, string>,
  cycle: BillingCycle,
  base: string | null,
): PriceBreakdown {
  const baseCents = toCents(base) ?? 0
  const deltas: PriceDeltaLine[] = []
  let total = baseCents

  for (const group of groups) {
    for (const option of group.options) {
      const raw = config[String(option.id)]
      if (raw === undefined || raw === '') {
        continue
      }
      const label = optionLabel(option.name)

      if (isQuantityOption(option)) {
        const qty = Number.parseInt(raw, 10)
        if (!Number.isFinite(qty) || qty < 0) {
          continue
        }
        // 档位按 qty 落点选取；找不到（异常数据）按无差价数据降级。
        const tier =
          option.values.find(
            (value) => qty >= (value.qty_minimum ?? 0) && qty <= (value.qty_maximum ?? 0),
          ) ?? option.values[0]
        const { cents, hasData } = unitDelta(tier, cycle)
        const billable = billableQuantity(option, tier, qty)
        const amount = cents * billable
        total += amount
        deltas.push({
          option_id: option.id,
          option_name: label,
          kind: 'quantity',
          value_id: tier?.id,
          qty,
          unit: fromCents(hasData ? cents : 0),
          billable,
          amount: fromCents(hasData ? amount : 0),
          note: hasData ? '' : NO_PRICING_DATA_NOTE,
        })
        continue
      }

      const value = option.values.find((item) => String(item.id) === raw)
      if (!value) {
        continue
      }
      const { cents, hasData } = unitDelta(value, cycle)
      total += hasData ? cents : 0
      deltas.push({
        option_id: option.id,
        option_name: label,
        kind: 'select',
        value_id: value.id,
        unit: fromCents(hasData ? cents : 0),
        amount: fromCents(hasData ? cents : 0),
        note: hasData ? '' : NO_PRICING_DATA_NOTE,
      })
    }
  }

  return { base: fromCents(baseCents), deltas, total: fromCents(total) }
}

/** 明细里有差价的项数（用于决定是否默认展开）。 */
export function nonZeroDeltaCount(breakdown: PriceBreakdown): number {
  return breakdown.deltas.filter((line) => toCents(line.amount) !== 0).length
}
