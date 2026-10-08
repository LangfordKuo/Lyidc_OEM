import { Button } from '@heroui/react'

import { BILLING_CYCLES, type BillingCycle, type CyclePrices } from '../../lib/cycles'
import { formatCycleLabel, formatMoney, monthlyEquivalent } from '../../lib/format'

// CycleSelector 是六周期选择器：不可售周期（价格 null，契约 10.2）置灰不可选。
export default function CycleSelector({
  prices,
  value,
  onChange,
  size = 'md',
}: {
  prices: CyclePrices
  value: BillingCycle
  onChange: (cycle: BillingCycle) => void
  size?: 'sm' | 'md'
}) {
  return (
    <div role="radiogroup" aria-label="计费周期" className="grid grid-cols-2 gap-2 sm:grid-cols-3">
      {BILLING_CYCLES.map((cycle) => {
        const amount = prices[cycle]
        const disabled = amount === null || amount === undefined
        const active = cycle === value && !disabled
        const perMonth = amount ? monthlyEquivalent(amount, cycle) : null

        return (
          <Button
            key={cycle}
            isDisabled={disabled}
            variant={active ? 'primary' : 'outline'}
            size={size}
            className="h-auto flex-col items-start gap-0.5 py-2.5"
            onPress={() => onChange(cycle)}
          >
            <span className="text-sm font-medium">{formatCycleLabel(cycle)}</span>
            {disabled ? (
              <span className="text-xs opacity-70">不可售</span>
            ) : (
              <span className="text-xs opacity-80">
                {formatMoney(amount)}
                {perMonth && cycle !== 'monthly' ? ` · 约 ¥${perMonth}/月` : ''}
              </span>
            )}
          </Button>
        )
      })}
    </div>
  )
}
