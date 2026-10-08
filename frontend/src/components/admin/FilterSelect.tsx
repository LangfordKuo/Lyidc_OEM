import type { ReactNode } from 'react'

// FilterSelect 是管理后台列表页的下拉筛选（状态 / 分组 / 类型等）：
// 受控的原生 select，样式对齐 HeroUI 字段（圆角、描边、焦点色），
// 语义简单（role=combobox）便于测试与键盘操作。
export interface FilterSelectOption<T extends string> {
  value: T
  label: string
}

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
    <label className={`flex items-center gap-2 text-xs text-muted ${className ?? ''}`}>
      <span className="shrink-0">{label}</span>
      <select
        className="h-9 rounded-lg border border-border bg-field-background px-2.5 text-sm text-foreground outline-none focus:border-accent"
        value={value}
        onChange={(event) => onChange(event.target.value as T)}
        aria-label={label}
      >
        {options.map((option) => (
          <option key={option.value || 'all'} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    </label>
  )
}

/** AdminFilterBar 统一列表页筛选区排版（左筛选、右操作）。 */
export function AdminFilterBar({ children, actions }: { children: ReactNode; actions?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-end justify-between gap-3">
      <div className="flex flex-wrap items-center gap-3">{children}</div>
      {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
    </div>
  )
}
