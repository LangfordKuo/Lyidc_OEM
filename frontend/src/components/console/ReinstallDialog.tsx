import { useState } from 'react'
import { AlertTriangleIcon, LoaderCircleIcon } from 'lucide-react'
import { toast } from 'sonner'

import { errorMessage } from '@/api/client'
import { fetchReinstallOptions, reinstallInstance } from '@/api/instances'
import type { InstanceSummary, ReinstallOption } from '@/api/types'
import ChoiceChips from '@/components/common/ChoiceChips'
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
import { useAsync } from '@/hooks/useAsync'

// ReinstallDialog：选择系统 + 二次确认后发起重装（契约 15.2）。
// 可选系统在打开时按需拉取；上游为异步执行，返回成功仅表示已受理。
//
// R6：系统选择为两级（大类 → 版本），分组取上游 /host/cloudos 的 group 字段；
// 数据无分组信息时退化为单级列表（历史兼容）。

interface ReinstallOsGroup {
  name: string
  options: ReinstallOption[]
}

/** 按上游分组名聚合（保持出现顺序）；无分组名的项归入「其它」。 */
function groupOptions(options: ReinstallOption[]): ReinstallOsGroup[] {
  const groups: ReinstallOsGroup[] = []
  const index = new Map<string, ReinstallOsGroup>()
  for (const option of options) {
    const name = option.group.trim() || '其它'
    let group = index.get(name)
    if (!group) {
      group = { name, options: [] }
      index.set(name, group)
      groups.push(group)
    }
    group.options.push(option)
  }
  return groups
}

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
  const [activeGroup, setActiveGroup] = useState('')
  // 记忆每个大类下最后选择的版本：大类间来回切换时不丢选择。
  const [picks, setPicks] = useState<Record<string, string>>({})
  const [pending, setPending] = useState(false)

  const optionsState = useAsync(() => fetchReinstallOptions(instance.id), [instance.id])
  const options = optionsState.data?.os ?? []
  const groups = groupOptions(options)
  // 上游带分组信息时走两级选择；全无分组名（异常数据）时保持单级平铺。
  const twoLevel = options.some((item) => item.group.trim() !== '')
  const activeOptions = groups.find((group) => group.name === activeGroup)?.options ?? []
  const selected = options.find((item) => String(item.id) === osId) ?? null

  const handleSelectGroup = (name: string) => {
    setActiveGroup(name)
    // 恢复该大类上次选的版本；没选过则清空待重选（新大类不沿用旧版本的选中态）。
    setOsId(picks[name] ?? '')
  }

  const handleSelectOs = (id: string) => {
    setOsId(id)
    setPicks((prev) => ({ ...prev, [activeGroup]: id }))
  }

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

        {options.length > 0 && twoLevel ? (
          <div className="space-y-4">
            <div className="space-y-2">
              <p className="text-xs text-muted-foreground">系统大类</p>
              <ChoiceChips
                items={groups.map((group) => ({ value: group.name, label: group.name }))}
                value={activeGroup}
                onChange={handleSelectGroup}
                ariaLabel="系统大类"
                idPrefix="reinstall-group"
              />
            </div>
            <div className="space-y-2">
              <p className="text-xs text-muted-foreground">具体版本</p>
              {activeGroup ? (
                <div className="max-h-64 overflow-y-auto pr-1">
                  <ChoiceChips
                    items={activeOptions.map((option) => ({
                      value: String(option.id),
                      label: option.name,
                    }))}
                    value={osId}
                    onChange={handleSelectOs}
                    ariaLabel="系统版本"
                    idPrefix="reinstall-os"
                  />
                </div>
              ) : (
                <p className="text-sm text-muted-foreground">请先选择系统大类</p>
              )}
            </div>
          </div>
        ) : null}

        {options.length > 0 && !twoLevel ? (
          <div className="max-h-72 overflow-y-auto pr-1">
            <ChoiceChips
              items={options.map((option) => ({ value: String(option.id), label: option.name }))}
              value={osId}
              onChange={setOsId}
              ariaLabel="选择操作系统"
              idPrefix="reinstall-os"
            />
          </div>
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
