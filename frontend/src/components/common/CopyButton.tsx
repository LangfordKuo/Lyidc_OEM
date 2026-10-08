import { useEffect, useState } from 'react'
import { CheckIcon, CopyIcon } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { copyText } from '@/lib/clipboard'
import { cn } from '@/lib/utils'

/** 复制按钮：点击复制文本到剪贴板，成功后短暂显示对勾。 */
export default function CopyButton({
  value,
  label = '复制',
  className,
}: {
  value: string
  label?: string
  className?: string
}) {
  const [copied, setCopied] = useState(false)

  // 复制成功后 1.5s 恢复图标；组件卸载后不再 setState。
  useEffect(() => {
    if (!copied) {
      return
    }
    const timer = setTimeout(() => setCopied(false), 1500)
    return () => clearTimeout(timer)
  }, [copied])

  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-sm"
      className={cn('text-muted-foreground hover:text-foreground', className)}
      aria-label={copied ? '已复制' : label}
      disabled={!value}
      onClick={async () => {
        const success = await copyText(value)
        setCopied(success)
        if (!success) {
          toast.error('复制失败，请手动选择文本复制')
        }
      }}
    >
      {copied ? <CheckIcon className="text-emerald-600" aria-hidden /> : <CopyIcon aria-hidden />}
    </Button>
  )
}
