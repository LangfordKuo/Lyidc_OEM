import type { ReactNode } from 'react'

import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { cn } from '@/lib/utils'

/** 选项值为空串（= 不筛选）时用该哨兵值承载（Radix Select 不接受空串 value）。 */
const ALL_VALUE = '__all__'

export interface FilterSelectOption<T extends string> {
  value: T
  label: string
}

/**
 * FilterSelect 是管理后台列表页的下拉筛选（状态 / 分组 / 类型等），基于 shadcn Select。
 * 泛型 T 的空字符串取值表示「全部」，与后端「省略该查询参数」的语义对应。
 */
export default function FilterSelect<T extends string>({
  label,
  value,
  options,
  onChange,
  className,
}: {
  label: string
  value: T
  options: FilterSelectOption<T>[]
  onChange: (value: T) => void
  className?: string
}) {
  return (
    <div className={cn('flex items-center gap-2', className)}>
      <Label className="shrink-0 text-xs text-muted-foreground">{label}</Label>
      <Select
        value={value === '' ? ALL_VALUE : value}
        onValueChange={(next) => onChange((next === ALL_VALUE ? '' : next) as T)}
      >
        <SelectTrigger size="sm" aria-label={label} className="min-w-32">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {options.map((option) => (
            <SelectItem
              key={option.value === '' ? ALL_VALUE : option.value}
              value={option.value === '' ? ALL_VALUE : option.value}
            >
              {option.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )
}

/** AdminFilterBar 统一列表页筛选区排版（左筛选、右操作）。 */
export function AdminFilterBar({ children, actions }: { children: ReactNode; actions?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-end justify-between gap-3">
      <div className="flex flex-wrap items-end gap-3">{children}</div>
      {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
    </div>
  )
}
