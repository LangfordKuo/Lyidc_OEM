import { BILLING_CYCLES, type BillingCycle, type CyclePrices } from '../../lib/cycles'
import { formatCycleLabel, formatDuration, formatMoney, monthlyEquivalent } from '../../lib/format'

// CyclePriceTable 以表格形式列出六个周期的价格（含月均价折算），用于商品详情页价格一览。
export default function CyclePriceTable({
  prices,
  highlight,
}: {
  prices: CyclePrices
  highlight?: BillingCycle
}) {
  return (
    <div className="overflow-hidden rounded-xl border border-border">
      <table className="w-full text-sm">
        <thead className="bg-surface-secondary/60 text-muted">
          <tr>
            <th className="px-4 py-2 text-left font-medium">周期</th>
            <th className="px-4 py-2 text-right font-medium">价格</th>
            <th className="hidden px-4 py-2 text-right font-medium sm:table-cell">折合月均</th>
          </tr>
        </thead>
        <tbody>
          {BILLING_CYCLES.map((cycle) => {
            const amount = prices[cycle]
            const perMonth = amount ? monthlyEquivalent(amount, cycle) : null
            return (
              <tr
                key={cycle}
                className={[
                  'border-t border-border',
                  cycle === highlight ? 'bg-accent-soft/60 font-medium' : '',
                ].join(' ')}
              >
                <td className="px-4 py-2">
                  {formatCycleLabel(cycle)}
                  <span className="ml-1.5 text-xs text-muted">({formatDuration(cycle)})</span>
                </td>
                <td className="px-4 py-2 text-right">
                  {amount ? formatMoney(amount) : <span className="text-muted">不可售</span>}
                </td>
                <td className="hidden px-4 py-2 text-right text-muted sm:table-cell">
                  {perMonth && cycle !== 'monthly' ? formatMoney(perMonth) : '—'}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
