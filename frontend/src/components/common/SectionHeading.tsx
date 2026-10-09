import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'

/** 区块标题：标题 + 描述 + 右侧动作，页面内多处复用。 */
export default function SectionHeading({
  title,
  description,
  action,
  className,
}: {
  title: string
  description?: string
  action?: ReactNode
  className?: string
}) {
  return (
    <div className={cn('mb-6 flex flex-wrap items-end justify-between gap-3', className)}>
      <div>
        <h2 className="text-2xl font-semibold tracking-tight text-foreground sm:text-[1.75rem]">
          {title}
        </h2>
        {description ? (
          <p className="mt-1.5 text-sm text-muted-foreground sm:text-base">{description}</p>
        ) : null}
      </div>
      {action}
    </div>
  )
}
