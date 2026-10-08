import { useState } from 'react'
import { toast } from 'sonner'

import { errorMessage } from '@/api/client'
import { powerInstance } from '@/api/instances'
import type { InstanceSummary } from '@/api/types'
import ConfirmDialog from '@/components/common/ConfirmDialog'
import { Button } from '@/components/ui/button'
import { POWER_ACTIONS, type PowerAction } from '@/lib/instanceStatus'

// PowerActions 是实例电源操作区（契约 15.1）：全部按钮走二次确认弹窗，
// hard_* 强制操作额外标注风险文案；仅 active 状态可执行，操作后回调刷新详情。
export default function PowerActions({
  instance,
  onDone,
}: {
  instance: InstanceSummary
  onDone: () => void
}) {
  const [target, setTarget] = useState<PowerAction | null>(null)
  const [pending, setPending] = useState(false)

  const disabled = instance.status !== 'active'

  const handleConfirm = async () => {
    if (!target) {
      return
    }
    setPending(true)
    try {
      const result = await powerInstance(instance.id, target.op)
      toast.success(result.message || `${target.label}指令已提交`)
      setTarget(null)
      onDone()
    } catch (error) {
      toast.error(errorMessage(error, `${target.label}失败`))
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap gap-2">
        {POWER_ACTIONS.map((action) => (
          <Button
            key={action.op}
            size="sm"
            variant={action.danger ? 'destructive' : 'outline'}
            disabled={disabled}
            title={action.description}
            onClick={() => setTarget(action)}
          >
            {action.label}
          </Button>
        ))}
      </div>
      <p className="text-xs text-muted-foreground">
        {disabled
          ? `仅「运行中」的实例可执行电源操作（当前状态：${instance.status}）。`
          : '上游为异步受理：返回成功表示指令已提交，电源状态随后由上游完成。'}
      </p>

      <ConfirmDialog
        open={target !== null}
        onOpenChange={(open) => (!open ? setTarget(null) : undefined)}
        title={target?.confirmTitle ?? ''}
        description={target?.confirmText}
        confirmLabel={target?.label}
        danger={target?.danger ?? false}
        pending={pending}
        onConfirm={handleConfirm}
      />
    </div>
  )
}
