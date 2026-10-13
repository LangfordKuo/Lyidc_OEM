import { BILLING_CYCLES, type BillingCycle, type CyclePrices } from '@/lib/cycles'
import { formatCycleLabel, formatMoney, monthlyEquivalent } from '@/lib/format'
import { cn } from '@/lib/utils'

/**
 * 周期选择器：**只渲染可售周期**（价格非 null，契约 10.2）。
 *
 * R11：不可售周期直接不显示（此前是置灰）。结算页与商品详情页共用本控件。
 * 全部不可售时降级为空态提示，不渲染空的 radiogroup。
 * 按钮组实现保留 radiogroup 语义与键盘可达性。
 */
export default function CycleSelector({
  prices,
  value,
  onChange,
  className,
}: {
  prices: CyclePrices
  value: BillingCycle
  onChange: (cycle: BillingCycle) => void
  className?: string
}) {
  const sellable = BILLING_CYCLES.filter(
    (cycle) => prices[cycle] !== null && prices[cycle] !== undefined,
  )

  if (sellable.length === 0) {
    return (
      <div
        className={cn(
          'rounded-lg border border-dashed border-border px-3 py-4 text-center text-sm text-muted-foreground',
          className,
        )}
        data-testid="cycle-selector-empty"
      >
        暂无可售的计费周期
      </div>
    )
  }

  return (
    <div
      role="radiogroup"
      aria-label="计费周期"
      className={cn('grid grid-cols-2 gap-2 sm:grid-cols-3', className)}
    >
      {sellable.map((cycle) => {
        const amount = prices[cycle]
        const active = cycle === value
        const perMonth = amount ? monthlyEquivalent(amount, cycle) : null

        return (
          <button
            key={cycle}
            type="button"
            role="radio"
            aria-checked={active}
            onClick={() => onChange(cycle)}
            className={cn(
              'flex flex-col items-start gap-0.5 rounded-lg border px-3 py-2.5 text-left transition-colors',
              'focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none',
              active
                ? 'border-primary bg-primary/5 ring-1 ring-primary'
                : 'border-border bg-background hover:border-primary/40 hover:bg-muted/40',
            )}
          >
            <span className={cn('text-sm font-medium', active ? 'text-primary' : 'text-foreground')}>
              {formatCycleLabel(cycle)}
            </span>
            <span className="text-xs text-muted-foreground">
              {formatMoney(amount)}
              {perMonth && cycle !== 'monthly' ? ` · 约 ¥${perMonth}/月` : ''}
            </span>
          </button>
        )
      })}
    </div>
  )
}
