import { useMemo, useState } from 'react'
import { AlertCircleIcon, CheckCircle2Icon, LoaderCircleIcon } from 'lucide-react'

import { importAdminProducts } from '@/api/adminProducts'
import { errorMessage } from '@/api/client'
import type { AdminProductGroup, ProductImportResult } from '@/api/types'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

// 上游导入弹窗（R12，契约 10.4）：勾选要导入的上游分组（默认全选），提交后展示导入计数。
//
// 请求体语义：**全选（或还没有任何分组）时省略 `upstream_group_ids`**——保持「缺省=全量导入」，
// 与 R11 前的调用完全一致；部分选择时提交选中的上游分组 ID；一个都不选时禁用确认并提示原因。
// 写权限由页面按 products.import 闸门控制挂载，这里不做重复校验。

export default function ProductImportDialog({
  groups,
  groupsLoading,
  groupsError,
  onClose,
  onImported,
}: {
  /** 上游分组清单（数据来源 GET /admin/product-groups）。 */
  groups: AdminProductGroup[]
  groupsLoading: boolean
  groupsError: string
  onClose: () => void
  /** 导入成功后的回调（页面刷新商品列表与分组）。 */
  onImported: () => void
}) {
  // 用「未勾选的集合」表示选择状态：默认空 = 全选，且不必在分组异步到达后再回填默认值。
  const [deselected, setDeselected] = useState<Set<number>>(() => new Set())
  const [pending, setPending] = useState(false)
  const [result, setResult] = useState<ProductImportResult | null>(null)
  const [error, setError] = useState('')

  const selectedIds = useMemo(
    () => groups.map((group) => group.upstream_group_id).filter((id) => !deselected.has(id)),
    [groups, deselected],
  )
  const allSelected = deselected.size === 0
  const noneSelected = groups.length > 0 && selectedIds.length === 0

  const toggle = (id: number, checked: boolean) => {
    setDeselected((prev) => {
      const next = new Set(prev)
      if (checked) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }

  const handleImport = async () => {
    if (noneSelected) {
      return
    }
    setPending(true)
    setError('')
    setResult(null)
    try {
      // 全选（或还没有分级可选）时省略请求体 = 全量导入（契约 10.4）。
      const view = await importAdminProducts(
        allSelected ? undefined : { upstream_group_ids: selectedIds },
      )
      setResult(view)
      onImported()
    } catch (err) {
      // 上游未配置 / 目录拉取失败等（500）原样展示后端 message。
      setError(errorMessage(err, '导入商品失败，请稍后重试'))
    } finally {
      setPending(false)
    }
  }

  const failedPids = result?.failed_pids?.slice(0, 20) ?? []
  const fallbackGroups = result?.map_fallback_group_ids ?? []

  return (
    <Dialog
      open
      onOpenChange={(next) => {
        if (!next && !pending) {
          onClose()
        }
      }}
    >
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>从上游导入商品</DialogTitle>
          <DialogDescription>
            只读调用上游目录与商品详情，耗时可能数十秒。按上游 PID 幂等：已存在则更新、无变化则跳过，
            并自动创建缺失的分组。勾选要导入的上游分组（默认全选）；全选即等同全量导入。
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          {groupsLoading ? (
            <p className="text-sm text-muted-foreground">正在读取上游分组…</p>
          ) : null}

          {groupsError ? (
            <Alert variant="destructive">
              <AlertCircleIcon aria-hidden />
              <AlertDescription>分组读取失败：{groupsError}</AlertDescription>
            </Alert>
          ) : null}

          {!groupsLoading && !groupsError && groups.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              还没有上游分组：首次导入会按上游目录自动创建分组，此处将执行全量导入。
            </p>
          ) : null}

          {groups.length > 0 ? (
            <div className="space-y-2">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <span className="text-xs text-muted-foreground">
                  已选 {selectedIds.length} / {groups.length} 个分组
                </span>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  onClick={() => setDeselected(allSelected ? new Set(groups.map((g) => g.upstream_group_id)) : new Set())}
                >
                  {allSelected ? '全不选' : '全选'}
                </Button>
              </div>
              <div className="max-h-64 space-y-1 overflow-y-auto rounded-lg border border-border p-2">
                {groups.map((group) => (
                  <label
                    key={group.id}
                    className="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 hover:bg-muted/60"
                  >
                    <Checkbox
                      checked={!deselected.has(group.upstream_group_id)}
                      onCheckedChange={(checked) =>
                        toggle(group.upstream_group_id, checked === true)
                      }
                    />
                    <span className="min-w-0 flex-1 truncate text-sm text-foreground">
                      {group.name}
                    </span>
                    <span className="text-xs text-muted-foreground">
                      {group.products.total} 个商品
                    </span>
                  </label>
                ))}
              </div>
            </div>
          ) : null}

          {noneSelected ? (
            <p className="text-xs text-destructive">
              请至少勾选一个上游分组；一个都不选无法确定导入范围（若要全量导入请点「全选」）。
            </p>
          ) : null}

          {error ? (
            <Alert variant="destructive">
              <AlertCircleIcon aria-hidden />
              <AlertTitle>导入失败</AlertTitle>
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}

          {result ? (
            <Alert
              className={
                result.failed > 0 || fallbackGroups.length > 0
                  ? 'border-warning/40 bg-warning/5'
                  : 'border-success/40 bg-success/5'
              }
            >
              <CheckCircle2Icon className="text-success" aria-hidden />
              <AlertTitle>上游导入完成</AlertTitle>
              <AlertDescription>
                新建 {result.created} 个、更新 {result.updated} 个、无变化 {result.unchanged} 个、分组{' '}
                {result.groups} 个。
                {result.selected_groups?.length
                  ? ` 本次只导入了选中的 ${result.selected_groups.length} 个分组。`
                  : ''}
                {result.failed > 0
                  ? ` 另有 ${result.failed} 个商品详情抓取失败，已跳过（不影响其余商品）。`
                  : ''}
              </AlertDescription>
              {fallbackGroups.length > 0 ? (
                <p className="mt-1 text-xs text-muted-foreground">
                  有 {fallbackGroups.length} 个分组的映射目标已失效（分类被删除或不再是子分类），
                  本次已回落到按规则自动归类。请到「分类」页签检查映射。
                </p>
              ) : null}
              {failedPids.length > 0 ? (
                <p className="mt-1 text-xs text-muted-foreground">
                  跳过的商品 ID（最多展示 20 个）：{failedPids.join('、')}
                </p>
              ) : null}
            </Alert>
          ) : null}
        </div>

        <DialogFooter>
          <Button variant="outline" disabled={pending} onClick={onClose}>
            {result ? '关闭' : '取消'}
          </Button>
          <Button disabled={pending || noneSelected} onClick={handleImport}>
            {pending ? (
              <>
                <LoaderCircleIcon className="animate-spin" data-icon="inline-start" aria-hidden />
                正在导入…
              </>
            ) : (
              '开始导入'
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
