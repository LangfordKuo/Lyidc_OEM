import { Alert, Button, Skeleton, Spinner } from '@heroui/react'
import type { ReactNode } from 'react'

// 页面通用的加载/错误/空态占位，统一各页面的排版与文案位置。

export function LoadingBlock({ label = '加载中…' }: { label?: string }) {
  return (
    <div className="flex items-center justify-center gap-3 py-16 text-muted">
      <Spinner size="md" />
      <span className="text-sm">{label}</span>
    </div>
  )
}

/** 卡片骨架屏：用于商品列表的加载态。 */
export function CardSkeletonGrid({ count = 6 }: { count?: number }) {
  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      {Array.from({ length: count }, (_, index) => (
        <div key={index} className="space-y-3 rounded-xl border border-border bg-surface p-5">
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
}: {
  message: string
  onRetry?: () => void
  retryLabel?: string
}) {
  return (
    <Alert status="danger" className="my-6">
      <Alert.Indicator />
      <Alert.Content>
        <Alert.Title>加载失败</Alert.Title>
        <Alert.Description>{message}</Alert.Description>
      </Alert.Content>
      {onRetry ? (
        <Button variant="outline" size="sm" onPress={onRetry}>
          {retryLabel}
        </Button>
      ) : null}
    </Alert>
  )
}

export function EmptyBlock({ title, description }: { title: string; description?: ReactNode }) {
  return (
    <div className="rounded-xl border border-dashed border-border bg-surface-secondary/40 px-6 py-14 text-center">
      <p className="text-base font-medium text-foreground">{title}</p>
      {description ? <p className="mt-2 text-sm text-muted">{description}</p> : null}
    </div>
  )
}
