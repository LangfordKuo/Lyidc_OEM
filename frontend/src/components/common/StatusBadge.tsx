import { Badge } from '@/components/ui/badge'
import type { StatusTone } from '@/lib/orderStatus'
import { STATUS_TONE_BADGE_CLASSES } from '@/lib/statusTone'
import { cn } from '@/lib/utils'

/** 订单/资源/工单状态徽标：色调由 StatusTone 统一映射（见 lib/statusTone）。 */
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
    <Badge
      variant="secondary"
      className={cn('border-transparent', STATUS_TONE_BADGE_CLASSES[tone], className)}
    >
      {label}
    </Badge>
  )
}
