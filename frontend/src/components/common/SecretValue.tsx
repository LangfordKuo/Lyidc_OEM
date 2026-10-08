import { useState } from 'react'
import { EyeIcon, EyeOffIcon } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import CopyButton from './CopyButton'

/**
 * 敏感值展示（主机密码等）：默认遮蔽，可显示/隐藏并复制。
 * 契约 14.2：账号密码仅会员本人可见——页面上默认不直接暴露明文。
 */
export default function SecretValue({
  value,
  label = '密码',
  className,
}: {
  value: string
  label?: string
  className?: string
}) {
  const [visible, setVisible] = useState(false)

  if (!value) {
    return <span className="text-muted-foreground">—</span>
  }

  return (
    <span className={cn('inline-flex flex-wrap items-center justify-end gap-1', className)}>
      <span className="font-mono text-sm break-all">
        {visible ? value : '••••••••••'}
      </span>
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        className="text-muted-foreground hover:text-foreground"
        aria-label={visible ? `隐藏${label}` : `显示${label}`}
        onClick={() => setVisible((current) => !current)}
      >
        {visible ? <EyeOffIcon aria-hidden /> : <EyeIcon aria-hidden />}
      </Button>
      {visible ? <CopyButton value={value} label={`复制${label}`} /> : null}
    </span>
  )
}
