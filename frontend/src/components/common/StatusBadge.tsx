import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import type { StatusTone } from '@/lib/orderStatus'

const TONE_CLASSES: Record<StatusTone, string> = {
  ok: 'bg-emerald-600/10 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-400',
  warn: 'bg-amber-500/10 text-amber-700 dark:bg-amber-500/15 dark:text-amber-400',
  error: 'bg-destructive/10 text-destructive dark:bg-destructive/20',
  pending: 'bg-muted text-muted-foreground',
}

/** 订单/资源状态徽标：色调由 StatusTone 统一映射。 */
export default function StatusBadge({
  tone,
  label,
  className,
}: {
  tone: StatusTone
  label: string
  className?: string
}) {
  return (
    <Badge variant="secondary" className={cn('border-transparent', TONE_CLASSES[tone], className)}>
      {label}
    </Badge>
  )
}
