import { Button } from '@heroui/react'
import { useState } from 'react'

import CopyButton from './CopyButton'

// SecretValue 展示实例账号密码等敏感值：默认遮蔽，可「显示/隐藏」与「复制」。
// 明文只在本地组件状态中短暂存在，不写日志、不进 URL。
export default function SecretValue({
  value,
  label = '密码',
  emptyText = '未记录',
}: {
  value: string
  label?: string
  emptyText?: string
}) {
  const [visible, setVisible] = useState(false)

  if (!value) {
    return <span className="text-muted">{emptyText}</span>
  }

  return (
    <span className="flex flex-wrap items-center gap-2">
      <span className="font-mono text-sm break-all text-foreground">
        {visible ? value : '••••••••'}
      </span>
      <Button size="sm" variant="ghost" onPress={() => setVisible((prev) => !prev)}>
        {visible ? '隐藏' : '显示'}
      </Button>
      <CopyButton value={value} label={`复制${label}`} successMessage={`${label}已复制`} />
    </span>
  )
}
