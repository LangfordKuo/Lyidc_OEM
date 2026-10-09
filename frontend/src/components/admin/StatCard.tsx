import type { ComponentType, ReactNode } from 'react'
import { Skeleton } from '@/components/ui/skeleton'
import { Card, CardContent } from '@/components/ui/card'

/**
 * StatCard 是后台仪表盘的关键指标卡：浅蓝图标块 + 标题 + 大号数值 + 说明（副标题）。
 * 数值缺失（接口未提供 / 无权限）时展示 placeholder，绝不编造数字。
 */
export default function StatCard({
  label,
  value,
  hint,
  icon: Icon,
  loading = false,
  placeholder = '—',
  action,
  className,
}: {
  label: string
  value: ReactNode
  hint?: ReactNode
  /** 指标图标（lucide），渲染在浅蓝圆角方块里。 */
  icon?: ComponentType<{ className?: string; 'aria-hidden'?: boolean }>
  loading?: boolean
  placeholder?: string
  action?: ReactNode
  /** 外部布局类（网格跨列等）。 */
  className?: string
}) {
  return (
    <Card className={className}>
      <CardContent className="flex items-start gap-3">
        {Icon ? (
          <span className="grid size-10 shrink-0 place-items-center rounded-xl bg-accent text-primary">
            <Icon className="size-5" aria-hidden />
          </span>
        ) : null}
        <div className="min-w-0 flex-1 space-y-1">
          <div className="flex items-center justify-between gap-2">
            <p className="text-xs text-muted-foreground">{label}</p>
            {action}
          </div>
          {loading ? (
            <Skeleton className="h-8 w-16 rounded-md" />
          ) : (
            <p className="text-3xl leading-none font-semibold tabular-nums text-foreground">
              {value ?? placeholder}
            </p>
          )}
          {hint ? <p className="pt-1 text-xs text-muted-foreground">{hint}</p> : null}
        </div>
      </CardContent>
    </Card>
  )
}
