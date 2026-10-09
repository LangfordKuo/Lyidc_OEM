import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'

// 信息列表与时间线：实例信息、订单明细、操作记录共用的展示原语。

export interface InfoItem {
  label: string
  value: ReactNode
}

/** 键值信息列表（label 左、value 右）。 */
export function InfoList({
  items,
  columns = 1,
  className,
}: {
  items: InfoItem[]
  columns?: 1 | 2
  className?: string
}) {
  return (
    <dl
      className={cn(
        'grid gap-x-8 gap-y-2.5 text-sm',
        columns === 2 && 'sm:grid-cols-2',
        className,
      )}
    >
      {items.map((item) => (
        <div key={item.label} className="flex items-start justify-between gap-4">
          <dt className="shrink-0 text-muted-foreground">{item.label}</dt>
          {/* 空值占位「—」统一降为次要灰色，避免与真实值同等醒目。 */}
          <dd
            className={cn(
              'min-w-0 text-right',
              item.value === '—' ? 'text-muted-foreground' : 'text-foreground',
            )}
          >
            {item.value}
          </dd>
        </div>
      ))}
    </dl>
  )
}

export interface TimelineItem {
  key: string
  title: ReactNode
  time?: string
  description?: ReactNode
  tone?: 'default' | 'success' | 'danger'
}

const DOT_CLASSES: Record<NonNullable<TimelineItem['tone']>, string> = {
  default: 'bg-muted-foreground/40',
  success: 'bg-success',
  danger: 'bg-destructive',
}

/** 竖向时间线：左侧圆点 + 竖线，右侧标题/时间/描述。 */
export function Timeline({ items, className }: { items: TimelineItem[]; className?: string }) {
  return (
    <ol className={cn('relative space-y-4', className)}>
      {items.map((item, index) => (
        <li key={item.key} className="relative pl-6">
          {index < items.length - 1 ? (
            <span className="absolute top-3 left-[5px] h-full w-px bg-border" aria-hidden />
          ) : null}
          <span
            className={cn(
              'absolute top-1.5 left-0 size-2.5 rounded-full',
              DOT_CLASSES[item.tone ?? 'default'],
            )}
            aria-hidden
          />
          <div className="flex flex-wrap items-baseline justify-between gap-2">
            <span className="text-sm font-medium text-foreground">{item.title}</span>
            {item.time ? <span className="text-xs text-muted-foreground">{item.time}</span> : null}
          </div>
          {item.description ? (
            <p className="mt-0.5 text-xs text-muted-foreground">{item.description}</p>
          ) : null}
        </li>
      ))}
    </ol>
  )
}
