import { BILLING_CYCLES, type BillingCycle, type CyclePrices } from '@/lib/cycles'
import { formatCycleLabel, formatMoney, monthlyEquivalent } from '@/lib/format'
import { cn } from '@/lib/utils'

/**
 * 六周期选择器：不可售周期（价格 null，契约 10.2）置灰不可选。
 * 用按钮组实现（radiogroup 语义），保留键盘可达性。
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
  return (
    <div role="radiogroup" aria-label="计费周期" className={cn('grid grid-cols-2 gap-2 sm:grid-cols-3', className)}>
      {BILLING_CYCLES.map((cycle) => {
        const amount = prices[cycle]
        const disabled = amount === null || amount === undefined
        const active = cycle === value && !disabled
        const perMonth = amount ? monthlyEquivalent(amount, cycle) : null

        return (
          <button
            key={cycle}
            type="button"
            role="radio"
            aria-checked={active}
            disabled={disabled}
            onClick={() => onChange(cycle)}
            className={cn(
              'flex flex-col items-start gap-0.5 rounded-lg border px-3 py-2.5 text-left transition-colors',
              'focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none',
              disabled
                ? 'cursor-not-allowed border-border bg-muted/40 opacity-60'
                : active
                  ? 'border-primary bg-primary/5 ring-1 ring-primary'
                  : 'border-border bg-background hover:border-primary/40 hover:bg-muted/40',
            )}
          >
            <span className={cn('text-sm font-medium', active ? 'text-primary' : 'text-foreground')}>
              {formatCycleLabel(cycle)}
            </span>
            {disabled ? (
              <span className="text-xs text-muted-foreground">不可售</span>
            ) : (
              <span className="text-xs text-muted-foreground">
                {formatMoney(amount)}
                {perMonth && cycle !== 'monthly' ? ` · 约 ¥${perMonth}/月` : ''}
              </span>
            )}
          </button>
        )
      })}
    </div>
  )
}
