import { useState } from 'react'
import { AlertCircleIcon, AlertTriangleIcon, LoaderCircleIcon } from 'lucide-react'
import { toast } from 'sonner'

import { errorMessage } from '@/api/client'
import { cancelInstance } from '@/api/instances'
import type { CancelType, InstanceSummary } from '@/api/types'
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
import { Textarea } from '@/components/ui/textarea'
import { validateCancelReason } from '@/lib/validate'
import { cn } from '@/lib/utils'

// CancelRequestDialog：申请终止（取消）实例（契约 15.8.2）。
// 提交后本地只记「申请在途」标记，主机继续运行直到上游确认删除；已有在途申请时幂等返回。
export default function CancelRequestDialog({
  instance,
  onOpenChange,
  onDone,
}: {
  instance: InstanceSummary
  onOpenChange: (open: boolean) => void
  onDone: () => void
}) {
  const [type, setType] = useState<CancelType>('immediate')
  const [reason, setReason] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  const reasonError = validateCancelReason(reason)

  const handleConfirm = async () => {
    if (reasonError) {
      setError(reasonError)
      return
    }
    setPending(true)
    setError('')
    try {
      const result = await cancelInstance(instance.id, {
        type,
        ...(reason.trim() ? { reason: reason.trim() } : {}),
      })
      toast.success(
        result.duplicate
          ? '该实例已有在途终止申请，无需重复提交'
          : result.message || '取消申请已提交',
      )
      onOpenChange(false)
      onDone()
    } catch (err) {
      setError(errorMessage(err, '提交取消申请失败'))
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => (!pending ? onOpenChange(open) : undefined)}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>申请终止实例</DialogTitle>
          <DialogDescription>
            实例 <span className="font-mono">{instance.name}</span>
          </DialogDescription>
        </DialogHeader>

        <Alert variant="destructive">
          <AlertTriangleIcon aria-hidden />
          <AlertTitle>终止后不可恢复</AlertTitle>
          <AlertDescription>
            本系统通过「提交申请 → 上游处理 → 本地收敛」完成终止：提交后主机仍会运行一小段时间，
            上游确认删除后实例转为「已终止」。终止会删除主机与数据，且不支持撤销申请。
          </AlertDescription>
        </Alert>

        <RadioGroup
          value={type}
          onValueChange={(value) => setType(value as CancelType)}
          aria-label="终止方式"
          className="gap-2"
        >
          <Label
            className={cn(
              'flex cursor-pointer items-start gap-3 rounded-lg border p-3 transition-colors',
              type === 'immediate'
                ? 'border-primary bg-primary/5'
                : 'border-border hover:bg-muted/50',
            )}
          >
            <RadioGroupItem value="immediate" className="mt-0.5" />
            <span className="flex flex-col gap-0.5">
              <span className="text-sm font-medium text-foreground">立即终止</span>
              <span className="text-xs text-muted-foreground">上游受理后尽快删除主机</span>
            </span>
          </Label>
          <Label
            className={cn(
              'flex cursor-pointer items-start gap-3 rounded-lg border p-3 transition-colors',
              type === 'end_of_billing'
                ? 'border-primary bg-primary/5'
                : 'border-border hover:bg-muted/50',
            )}
          >
            <RadioGroupItem value="end_of_billing" className="mt-0.5" />
            <span className="flex flex-col gap-0.5">
              <span className="text-sm font-medium text-foreground">到期终止</span>
              <span className="text-xs text-muted-foreground">
                到当前账单周期结束后再删除主机
              </span>
            </span>
          </Label>
        </RadioGroup>

        <div className="space-y-1.5">
          <Label htmlFor="cancel_reason">申请原因（可选）</Label>
          <Textarea
            id="cancel_reason"
            rows={3}
            value={reason}
            placeholder="如：业务迁移，不再需要该主机"
            aria-invalid={Boolean(reason && reasonError)}
            onChange={(event) => {
              setReason(event.target.value)
              setError('')
            }}
          />
          {reason && reasonError ? (
            <p className="text-xs text-destructive">{reasonError}</p>
          ) : null}
        </div>

        {error ? (
          <Alert variant="destructive">
            <AlertCircleIcon aria-hidden />
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}

        <DialogFooter className="gap-2">
          <Button variant="outline" disabled={pending} onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button variant="destructive" disabled={pending} onClick={handleConfirm}>
            {pending ? (
              <>
                <LoaderCircleIcon className="animate-spin" data-icon="inline-start" aria-hidden />
                正在提交…
              </>
            ) : (
              '提交终止申请'
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
