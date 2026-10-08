import { Button } from '@heroui/react'

// StatusFilter 是列表页的筛选条（状态/分类）：值为空串表示「全部」。
export interface FilterOption<T extends string> {
  value: T
  label: string
}

export default function StatusFilter<T extends string>({
  options,
  value,
  onChange,
  label = '筛选',
}: {
  options: FilterOption<T>[]
  value: T
  onChange: (value: T) => void
  label?: string
}) {
  return (
    <div className="flex flex-wrap gap-2" role="group" aria-label={label}>
      {options.map((option) => {
        const active = option.value === value
        return (
          <Button
            key={option.value || 'all'}
            size="sm"
            variant={active ? 'primary' : 'outline'}
            onPress={() => onChange(option.value)}
          >
            {option.label}
          </Button>
        )
      })}
    </div>
  )
}
