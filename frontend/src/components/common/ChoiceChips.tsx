import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { cn } from '@/lib/utils'

export interface ChoiceChipItem {
  value: string
  label: string
}

/**
 * 隐藏但仍可聚焦的单选项（无障碍语义用）。
 *
 * 不能只写 `sr-only`：Tailwind v4 的 utilities 里 `.sr-only` 生成在 `.relative` **之前**，
 * 而 shadcn 的 `RadioGroupItem` 基类带 `relative size-4`；二者同特异性、后者胜出——
 * `position` 被打回 `relative`、尺寸仍是 16px，于是这个「隐藏」的单选成了**文档流里的 flex 项**，
 * 再吃上 Label 的 `gap-2`，每个选项文字前多出 23px 幽灵空白
 * （实测 16px 尺寸 − 1px margin + 8px gap = 23px）。
 *
 * 这里显式写 `absolute`：`cn`（tailwind-merge 等价物）会把它与 `relative` 归入同一冲突组
 * 并**替换掉** `relative`，从而真正脱离文档流；`sr-only` 继续负责视觉隐藏（clip / 1px）。
 */
export const HIDDEN_RADIO_CLASS = 'absolute sr-only'

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
            <RadioGroupItem
              id={chipId}
              value={item.value}
              aria-label={item.label}
              className={HIDDEN_RADIO_CLASS}
            />
            {item.label}
          </Label>
        )
      })}
    </RadioGroup>
  )
}
