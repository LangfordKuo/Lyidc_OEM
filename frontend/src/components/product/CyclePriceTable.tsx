import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { BILLING_CYCLES, type BillingCycle, type CyclePrices } from '@/lib/cycles'
import { formatCycleLabel, formatDuration, formatMoney, monthlyEquivalent } from '@/lib/format'
import { cn } from '@/lib/utils'

/** 以表格列出六个周期的价格（含月均价折算），用于商品详情页价格一览。 */
export default function CyclePriceTable({
  prices,
  highlight,
}: {
  prices: CyclePrices
  highlight?: BillingCycle
}) {
  return (
    <div className="overflow-hidden rounded-xl border border-border">
      <Table>
        <TableHeader>
          <TableRow className="bg-muted/50">
            <TableHead>周期</TableHead>
            <TableHead className="text-right">价格</TableHead>
            <TableHead className="hidden text-right sm:table-cell">折合月均</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {BILLING_CYCLES.map((cycle) => {
            const amount = prices[cycle]
            const perMonth = amount ? monthlyEquivalent(amount, cycle) : null
            return (
              <TableRow key={cycle} className={cn(cycle === highlight && 'bg-primary/5 font-medium')}>
                <TableCell>
                  {formatCycleLabel(cycle)}
                  <span className="ml-1.5 text-xs text-muted-foreground">({formatDuration(cycle)})</span>
                </TableCell>
                <TableCell className="text-right">
                  {amount ? formatMoney(amount) : <span className="text-muted-foreground">不可售</span>}
                </TableCell>
                <TableCell className="hidden text-right text-muted-foreground sm:table-cell">
                  {perMonth && cycle !== 'monthly' ? formatMoney(perMonth) : '—'}
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </div>
  )
}
