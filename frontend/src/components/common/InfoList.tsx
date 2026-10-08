import type { ReactNode } from 'react'

// InfoList 是详情面板的「标签 / 值」列表：订单、实例、工单等处复用同一排版。
export interface InfoItem {
  label: string
  value: ReactNode
}

export function InfoList({ items, columns = 1 }: { items: InfoItem[]; columns?: 1 | 2 }) {
  return (
    <dl className={`grid gap-x-6 gap-y-2 text-sm ${columns === 2 ? 'sm:grid-cols-2' : ''}`}>
      {items.map((item) => (
        <div key={item.label} className="flex items-start justify-between gap-4">
          <dt className="shrink-0 text-muted">{item.label}</dt>
          <dd className="min-w-0 text-right text-foreground">{item.value}</dd>
        </div>
      ))}
    </dl>
  )
}

/** 时间线（订单流转 / 实例操作记录）：左侧圆点 + 竖线，右侧标题与说明。 */
export interface TimelineItem {
  key: string
  title: ReactNode
  time?: string
  description?: ReactNode
  tone?: 'default' | 'success' | 'danger'
}

const TONE_DOT: Record<string, string> = {
  default: 'bg-accent',
  success: 'bg-success',
  danger: 'bg-danger',
}

export function Timeline({ items }: { items: TimelineItem[] }) {
  return (
    <ol className="space-y-4">
      {items.map((item, index) => (
        <li key={item.key} className="flex gap-3">
          <span className="flex flex-col items-center pt-1.5">
            <span className={`size-2 rounded-full ${TONE_DOT[item.tone ?? 'default']}`} />
            {index < items.length - 1 ? <span className="mt-1 w-px flex-1 bg-border" /> : null}
          </span>
          <span className="min-w-0 flex-1 pb-1">
            <span className="flex flex-wrap items-baseline justify-between gap-2">
              <span className="text-sm text-foreground">{item.title}</span>
              {item.time ? <span className="text-xs text-muted">{item.time}</span> : null}
            </span>
            {item.description ? (
              <span className="mt-0.5 block text-xs text-muted">{item.description}</span>
            ) : null}
          </span>
        </li>
      ))}
    </ol>
  )
}
