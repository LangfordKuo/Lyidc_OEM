import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

/**
 * 状态筛选条：一组互斥按钮（含「全部」）。
 * 泛型 T 的空字符串取值表示「全部」，与后端 status 查询参数的省略语义对应。
 */
export default function StatusFilter<T extends string>({
  label,
  options,
  value,
  onChange,
  className,
}: {
  label: string
  options: { value: T; label: string }[]
  value: T
  onChange: (value: T) => void
  className?: string
}) {
  return (
    <div
      role="group"
      aria-label={label}
      className={cn('flex flex-wrap items-center gap-2', className)}
    >
      <Button
        variant={value === '' ? 'default' : 'outline'}
        size="sm"
        aria-pressed={value === ''}
        onClick={() => onChange('' as T)}
      >
        全部
      </Button>
      {options.map((option) => (
        <Button
          key={option.value}
          variant={value === option.value ? 'default' : 'outline'}
          size="sm"
          aria-pressed={value === option.value}
          onClick={() => onChange(option.value)}
        >
          {option.label}
        </Button>
      ))}
    </div>
  )
}
