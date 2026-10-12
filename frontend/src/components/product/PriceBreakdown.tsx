import { ChevronDownIcon, ChevronUpIcon } from 'lucide-react'
import { useState } from 'react'

import type { PriceBreakdown as Breakdown } from '@/api/types'
import { Button } from '@/components/ui/button'
import { formatMoney } from '@/lib/format'
import { NO_PRICING_DATA_NOTE } from '@/lib/priceQuote'

/**
 * 成交价明细（R7，契约 10.2.1）：基础价 + 每个配置项的差价，可展开/收起。
 *
 * 只做展示；金额来自服务端下发的 `pricings` 或下单返回的 `price_breakdown`，
 * 前端不重套定价规则（见 lib/priceQuote.ts）。
 */
export default function PriceBreakdownView({
  breakdown,
  defaultOpen = false,
  className,
}: {
  breakdown: Breakdown
  defaultOpen?: boolean
  className?: string
}) {
  const [open, setOpen] = useState(defaultOpen)

  return (
    <div className={className} data-testid="price-breakdown">
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className="-mx-2 h-7 px-2 text-xs text-muted-foreground hover:text-foreground"
        aria-expanded={open}
        onClick={() => setOpen((prev) => !prev)}
      >
        {open ? (
          <ChevronUpIcon data-icon="inline-start" aria-hidden />
        ) : (
          <ChevronDownIcon data-icon="inline-start" aria-hidden />
        )}
        价格明细
      </Button>

      {open ? (
        <dl className="mt-1 space-y-1.5 text-xs" data-testid="price-breakdown-detail">
          <Row label="基础价" value={formatMoney(breakdown.base)} />
          {breakdown.deltas.map((line) => (
            <Row
              key={line.option_id}
              label={line.option_name}
              value={formatMoney(line.amount)}
              hint={
                line.note === NO_PRICING_DATA_NOTE
                  ? NO_PRICING_DATA_NOTE
                  : line.kind === 'quantity'
                    ? `${line.qty} × ${formatMoney(line.unit)}`
                    : undefined
              }
            />
          ))}
          <div className="flex items-baseline justify-between border-t border-border pt-1.5">
            <dt className="text-muted-foreground">合计</dt>
            <dd className="font-medium tabular-nums text-foreground">
              {formatMoney(breakdown.total)}
            </dd>
          </div>
        </dl>
      ) : null}
    </div>
  )
}

function Row({
  label,
  value,
  hint,
}: {
  label: string
  value: string
  hint?: string
}) {
  return (
    <div className="flex items-baseline justify-between gap-3">
      <dt className="min-w-0 truncate text-muted-foreground">
        {label}
        {hint ? <span className="ml-1.5 text-muted-foreground/70">{hint}</span> : null}
      </dt>
      <dd className="shrink-0 tabular-nums text-foreground">{value}</dd>
    </div>
  )
}
