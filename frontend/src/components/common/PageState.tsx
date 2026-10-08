import type { ReactNode } from 'react'
import { AlertTriangleIcon, InboxIcon, LoaderCircleIcon } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

// 页面通用的加载/错误/空态占位，统一各页面的排版与文案位置。

export function LoadingBlock({ label = '加载中…', className }: { label?: string; className?: string }) {
  return (
    <div className={cn('flex items-center justify-center gap-3 py-16 text-muted-foreground', className)}>
      <LoaderCircleIcon className="size-5 animate-spin" aria-hidden />
      <span className="text-sm">{label}</span>
    </div>
  )
}

/** 卡片骨架屏：用于商品网格的加载态。 */
export function CardSkeletonGrid({ count = 6 }: { count?: number }) {
  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3" data-testid="card-skeleton-grid">
      {Array.from({ length: count }, (_, index) => (
        <div key={index} className="space-y-3 rounded-xl border border-border bg-card p-5">
          <Skeleton className="h-5 w-2/3 rounded-md" />
          <Skeleton className="h-4 w-1/3 rounded-md" />
          <Skeleton className="h-8 w-1/2 rounded-md" />
          <Skeleton className="h-9 w-full rounded-md" />
        </div>
      ))}
    </div>
  )
}

export function ErrorState({
  message,
  onRetry,
  retryLabel = '重新加载',
  className,
}: {
  message: string
  onRetry?: () => void
  retryLabel?: string
  className?: string
}) {
  return (
    <div
      role="alert"
      className={cn(
        'my-6 flex flex-col items-center gap-3 rounded-xl border border-destructive/30 bg-destructive/5 px-6 py-10 text-center',
        className,
      )}
    >
      <span className="grid size-10 place-items-center rounded-full bg-destructive/10 text-destructive">
        <AlertTriangleIcon className="size-5" aria-hidden />
      </span>
      <div>
        <p className="text-sm font-medium text-foreground">加载失败</p>
        <p className="mt-1 text-sm text-muted-foreground">{message}</p>
      </div>
      {onRetry ? (
        <Button variant="outline" size="sm" onClick={onRetry}>
          {retryLabel}
        </Button>
      ) : null}
    </div>
  )
}

export function EmptyBlock({
  title,
  description,
  action,
  className,
}: {
  title: string
  description?: ReactNode
  action?: ReactNode
  className?: string
}) {
  return (
    <div
      className={cn(
        'my-6 flex flex-col items-center gap-3 rounded-xl border border-dashed border-border px-6 py-12 text-center',
        className,
      )}
    >
      <span className="grid size-10 place-items-center rounded-full bg-muted text-muted-foreground">
        <InboxIcon className="size-5" aria-hidden />
      </span>
      <div>
        <p className="text-sm font-medium text-foreground">{title}</p>
        {description ? <p className="mt-1 text-sm text-muted-foreground">{description}</p> : null}
      </div>
      {action}
    </div>
  )
}
