import { useState } from 'react'
import { AlertCircleIcon, CheckCircle2Icon, LoaderCircleIcon } from 'lucide-react'

import { syncAdminProducts } from '@/api/adminProducts'
import { errorMessage } from '@/api/client'
import type { AdminProduct, ProductSyncResult } from '@/api/types'
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

// 商品同步弹窗（R20，契约 10.7）：勾选若干本地商品 → 二次确认 → 把它们的**上游来源字段**
// 刷成上游最新。语义边界（弹窗必须写明）：**只更新已存在的商品，不新增、不删除**；
// 本地字段（定价/售价/上下架/排序/返佣/手工改挂的分类）一律不动。
// 结果区展示逐条结果 + 汇总计数：失败项写明是哪个商品与可读原因。
// 写权限由页面按 products.sync 闸门控制挂载（**仅 admin**），这里不做重复校验。

/** 确认区最多逐条列出的选中商品（其余只给计数，避免弹窗被撑爆）。 */
const MAX_LISTED_SELECTION = 10
/** 结果区最多逐条列出的失败项。 */
const MAX_LISTED_FAILURES = 20

export default function ProductSyncDialog({
  products,
  onClose,
  onSynced,
}: {
  /** 本次勾选的商品（列表当前页的子集）。 */
  products: AdminProduct[]
  onClose: () => void
  /** 至少一条成功后的回调（页面刷新商品列表，立即反映新值）。 */
  onSynced: () => void
}) {
  // 挂载时快照一次勾选商品：同步成功后父页会刷新列表并清空勾选，
  // 弹窗的确认区与提交口径不应随之变化（避免「提交 3 个、界面显示 0 个」）。
  const [targets] = useState(() => products)
  const [pending, setPending] = useState(false)
  const [result, setResult] = useState<ProductSyncResult | null>(null)
  const [error, setError] = useState('')

  const handleSync = async () => {
    setPending(true)
    setError('')
    setResult(null)
    try {
      const view = await syncAdminProducts({
        product_ids: targets.map((product) => product.id),
      })
      setResult(view)
      if (view.succeeded > 0) {
        onSynced()
      }
    } catch (err) {
      // 上游未配置 / 目录拉取失败（500）等原样展示后端 message。
      setError(errorMessage(err, '同步商品失败，请稍后重试'))
    } finally {
      setPending(false)
    }
  }

  const listed = targets.slice(0, MAX_LISTED_SELECTION)
  const failures = (result?.items ?? []).filter((item) => item.status === 'failed')
  const listedFailures = failures.slice(0, MAX_LISTED_FAILURES)

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
          <DialogTitle>同步所选商品</DialogTitle>
          <DialogDescription>
            把选中的 {targets.length} 个商品刷成上游最新信息（只读调用上游目录与商品详情，可能耗时数十秒）。
            同步只更新已存在的商品，不新增、不删除；本地定价、售价、上架状态、排序、返佣与手工改挂的
            分类不会被改动。
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          {!result ? (
            <div className="rounded-lg border border-border p-2">
              <p className="px-1 text-xs text-muted-foreground">待同步商品</p>
              <ul className="mt-1 space-y-0.5">
                {listed.map((product) => (
                  <li key={product.id} className="truncate px-1 text-sm text-foreground">
                    {product.name}
                    <span className="ml-1 font-mono text-xs text-muted-foreground">#{product.id}</span>
                  </li>
                ))}
              </ul>
              {targets.length > listed.length ? (
                <p className="mt-1 px-1 text-xs text-muted-foreground">
                  等 {targets.length} 个商品
                </p>
              ) : null}
            </div>
          ) : null}

          {error ? (
            <Alert variant="destructive">
              <AlertCircleIcon aria-hidden />
              <AlertTitle>同步失败</AlertTitle>
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}

          {result ? (
            <div data-testid="product-sync-result" className="space-y-2">
              <Alert
                className={
                  result.failed > 0 ? 'border-warning/40 bg-warning/5' : 'border-success/40 bg-success/5'
                }
              >
                <CheckCircle2Icon className="text-success" aria-hidden />
                <AlertTitle>同步完成</AlertTitle>
                <AlertDescription>
                  成功 {result.succeeded} 条（更新 {result.updated} 条、无变化 {result.unchanged} 条）
                  {result.failed > 0 ? `；失败 ${result.failed} 条：` : '。'}
                </AlertDescription>
              </Alert>
              {failures.length > 0 ? (
                <div className="rounded-lg border border-border p-2">
                  <p className="px-1 text-xs text-muted-foreground">
                    失败条目（共 {failures.length} 条，未影响其它商品）
                  </p>
                  <ul className="mt-1 space-y-1">
                    {listedFailures.map((item) => (
                      <li key={item.product_id} className="px-1 text-sm">
                        <span className="text-foreground">
                          {item.name || '（商品已不存在）'}
                        </span>
                        <span className="ml-1 font-mono text-xs text-muted-foreground">
                          #{item.product_id}
                        </span>
                        <span className="ml-1 text-destructive">{item.reason}</span>
                      </li>
                    ))}
                  </ul>
                  {failures.length > listedFailures.length ? (
                    <p className="mt-1 px-1 text-xs text-muted-foreground">
                      另有 {failures.length - listedFailures.length} 条未逐条列出。
                    </p>
                  ) : null}
                </div>
              ) : null}
            </div>
          ) : null}
        </div>

        <DialogFooter>
          <Button variant="outline" disabled={pending} onClick={onClose}>
            {result ? '关闭' : '取消'}
          </Button>
          <Button disabled={pending || targets.length === 0} onClick={handleSync}>
            {pending ? (
              <>
                <LoaderCircleIcon className="animate-spin" data-icon="inline-start" aria-hidden />
                正在同步…
              </>
            ) : (
              '开始同步'
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
