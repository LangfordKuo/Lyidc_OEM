import type { ReactNode } from 'react'
import { Skeleton } from '@/components/ui/skeleton'
import { Card, CardContent } from '@/components/ui/card'

/**
 * StatCard 是后台仪表盘的关键指标卡：标题 + 数值 + 说明（副标题）。
 * 数值缺失（接口未提供 / 无权限）时展示 placeholder，绝不编造数字。
 */
export default function StatCard({
  label,
  value,
  hint,
  loading = false,
  placeholder = '—',
  action,
}: {
  label: string
  value: ReactNode
  hint?: ReactNode
  loading?: boolean
  placeholder?: string
  action?: ReactNode
}) {
  return (
    <Card>
      <CardContent className="space-y-1.5">
        <div className="flex items-center justify-between gap-2">
          <p className="text-xs text-muted-foreground">{label}</p>
          {action}
        </div>
        {loading ? (
          <Skeleton className="h-7 w-16 rounded-md" />
        ) : (
          <p className="text-2xl font-semibold text-foreground">{value ?? placeholder}</p>
        )}
        {hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
      </CardContent>
    </Card>
  )
}
