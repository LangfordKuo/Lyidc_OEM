import { Chip } from '@heroui/react'

export type StatusTone = 'ok' | 'warn' | 'error' | 'pending'

const colorByTone = {
  ok: 'success',
  warn: 'warning',
  error: 'danger',
  pending: 'default',
} as const

export interface StatusBadgeProps {
  tone: StatusTone
  label: string
}

// StatusBadge 用 HeroUI Chip 统一展示状态文案。
export default function StatusBadge({ tone, label }: StatusBadgeProps) {
  return (
    <Chip color={colorByTone[tone]} variant="soft" size="sm">
      {label}
    </Chip>
  )
}
