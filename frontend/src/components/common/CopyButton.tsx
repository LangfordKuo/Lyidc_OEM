import { Button, toast } from '@heroui/react'

import { copyText } from '../../lib/clipboard'

// CopyButton 统一「复制 + toast 反馈」：主机 IP、账号密码、订单号等处复用。
export default function CopyButton({
  value,
  label = '复制',
  successMessage = '已复制到剪贴板',
  size = 'sm',
}: {
  value: string
  label?: string
  successMessage?: string
  size?: 'sm' | 'md'
}) {
  const handleCopy = async () => {
    const ok = await copyText(value)
    if (ok) {
      toast.success(successMessage)
    } else {
      toast.danger('复制失败，请手动选择文本复制')
    }
  }

  return (
    <Button
      size={size}
      variant="outline"
      isDisabled={!value}
      onPress={() => {
        void handleCopy()
      }}
    >
      {label}
    </Button>
  )
}
