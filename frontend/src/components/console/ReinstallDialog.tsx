import { useState } from 'react'
import { AlertTriangleIcon, LoaderCircleIcon } from 'lucide-react'
import { toast } from 'sonner'

import { errorMessage } from '@/api/client'
import { fetchReinstallOptions, reinstallInstance } from '@/api/instances'
import type { InstanceSummary } from '@/api/types'
import { ErrorState, LoadingBlock } from '@/components/common/PageState'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { useAsync } from '@/hooks/useAsync'
import { cn } from '@/lib/utils'

// ReinstallDialog：选择系统 + 二次确认后发起重装（契约 15.2）。
// 可选系统在打开时按需拉取；上游为异步执行，返回成功仅表示已受理。
export default function ReinstallDialog({
  instance,
  onOpenChange,
  onDone,
}: {
  instance: InstanceSummary
  onOpenChange: (open: boolean) => void
  onDone: () => void
}) {
  const [osId, setOsId] = useState('')
  const [pending, setPending] = useState(false)

  const optionsState = useAsync(() => fetchReinstallOptions(instance.id), [instance.id])
  const options = optionsState.data?.os ?? []
  const selected = options.find((item) => String(item.id) === osId) ?? null

  const handleConfirm = async () => {
    if (!selected) {
      return
    }
    setPending(true)
    try {
      const result = await reinstallInstance(instance.id, { os_id: selected.id })
      toast.success(result.message || '重装指令已提交')
      onOpenChange(false)
      onDone()
    } catch (error) {
      toast.error(errorMessage(error, '重装失败'))
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => (!pending ? onOpenChange(open) : undefined)}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>重装系统</DialogTitle>
          <DialogDescription>
            实例 <span className="font-mono">{instance.name}</span>
          </DialogDescription>
        </DialogHeader>

        <Alert variant="destructive">
          <AlertTriangleIcon aria-hidden />
          <AlertTitle>重装会清空系统盘数据</AlertTitle>
          <AlertDescription>
            重装将格式化系统盘并安装所选系统，操作不可撤销；请先确认已备份重要数据。
          </AlertDescription>
        </Alert>

        {optionsState.loading ? <LoadingBlock label="正在读取可选系统…" /> : null}
        {optionsState.error ? (
          <ErrorState message={optionsState.error} onRetry={optionsState.reload} />
        ) : null}
        {!optionsState.loading && !optionsState.error && options.length === 0 ? (
          <p className="text-sm text-muted-foreground">该商品未配置可选操作系统，请联系客服。</p>
        ) : null}

        {options.length > 0 ? (
          <RadioGroup
            value={osId}
            onValueChange={setOsId}
            aria-label="选择操作系统"
            className="max-h-72 gap-2 overflow-y-auto pr-1"
          >
            {options.map((option) => (
              <Label
                key={option.id}
                className={cn(
                  'flex cursor-pointer items-start gap-3 rounded-lg border p-3 transition-colors',
                  osId === String(option.id)
                    ? 'border-primary bg-primary/5'
                    : 'border-border hover:bg-muted/50',
                )}
              >
                <RadioGroupItem value={String(option.id)} className="mt-0.5" />
                <span className="flex flex-col gap-0.5">
                  <span className="text-sm text-foreground">{option.name}</span>
                  {option.group ? (
                    <span className="text-xs text-muted-foreground">{option.group}</span>
                  ) : null}
                </span>
              </Label>
            ))}
          </RadioGroup>
        ) : null}

        <DialogFooter className="gap-2">
          <Button variant="outline" disabled={pending} onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button variant="destructive" disabled={pending || !selected} onClick={handleConfirm}>
            {pending ? (
              <>
                <LoaderCircleIcon className="animate-spin" data-icon="inline-start" aria-hidden />
                正在提交…
              </>
            ) : (
              '确认重装'
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
