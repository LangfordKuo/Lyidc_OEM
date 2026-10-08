import { Card, Chip } from '@heroui/react'
import type { ReactNode } from 'react'

// ConsolePlaceholder 是会员区 7b 功能的占位卡片：
// 本批（7a）只交付骨架与路由，具体功能在 7b 填充，占位页明确列出计划内容，避免误导。
export default function ConsolePlaceholder({
  title,
  description,
  planned,
  plannedLabel = '阶段 7b 计划内容',
  extra,
}: {
  title: string
  description: ReactNode
  planned: string[]
  plannedLabel?: string
  extra?: ReactNode
}) {
  return (
    <div className="space-y-5">
      <header className="flex flex-wrap items-center gap-3">
        <h1 className="text-xl font-semibold text-foreground">{title}</h1>
        <Chip size="sm" variant="soft" color="warning">
          占位页
        </Chip>
      </header>

      <Card>
        <Card.Header>
          <Card.Title className="text-base">本页功能待建设</Card.Title>
          <Card.Description>{description}</Card.Description>
        </Card.Header>
        <Card.Content>
          <p className="text-sm font-medium text-foreground">{plannedLabel}</p>
          <ul className="mt-3 space-y-2 text-sm text-muted">
            {planned.map((item) => (
              <li key={item} className="flex gap-2">
                <span className="mt-2 size-1.5 shrink-0 rounded-full bg-accent" />
                <span>{item}</span>
              </li>
            ))}
          </ul>
        </Card.Content>
      </Card>

      {extra}
    </div>
  )
}
