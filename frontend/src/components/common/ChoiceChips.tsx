import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { cn } from '@/lib/utils'

export interface ChoiceChipItem {
  value: string
  label: string
}

/**
 * 单选横条组（R6）：整块可点，选中态沿用 R4 蓝白灰（primary 描边 + 浅底）。
 * 配置项控件与重装系统选择共用；无障碍语义走 Radix RadioGroup（role=radio + aria-checked）。
 */
export default function ChoiceChips({
  items,
  value,
  onChange,
  ariaLabel,
  idPrefix,
  className,
}: {
  items: ChoiceChipItem[]
  value: string
  onChange: (value: string) => void
  ariaLabel: string
  idPrefix: string
  className?: string
}) {
  return (
    <RadioGroup
      value={value}
      onValueChange={onChange}
      aria-label={ariaLabel}
      className={cn('flex flex-wrap gap-2', className)}
    >
      {items.map((item) => {
        const chipId = `${idPrefix}-${item.value}`
        const active = value === item.value
        return (
          <Label
            key={item.value}
            htmlFor={chipId}
            className={cn(
              'cursor-pointer rounded-lg border px-3 py-1.5 text-sm font-normal transition-colors focus-within:ring-2 focus-within:ring-ring',
              active
                ? 'border-primary bg-primary/5 text-primary'
                : 'border-border text-foreground hover:bg-muted/50',
            )}
          >
            <RadioGroupItem id={chipId} value={item.value} aria-label={item.label} className="sr-only" />
            {item.label}
          </Label>
        )
      })}
    </RadioGroup>
  )
}
