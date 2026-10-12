import { useState } from 'react'
import { AlertCircleIcon, LoaderCircleIcon } from 'lucide-react'

import { updateAdminProduct } from '@/api/adminProducts'
import { errorMessage } from '@/api/client'
import type { AdminCategory, AdminProduct } from '@/api/types'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

// 商品改挂分类弹窗（契约 10.4 的 category_id 扩展）：目标只能是**子分类**或「未归类」(0)，
// 选项数据来自 GET /admin/categories。提交 PUT /admin/products/:id 的 { category_id }。
// 仅 admin / finance（products.write）可打开——由页面按角色矩阵控制挂载，这里不做重复校验。

/** 「未归类」选项值（Radix Select 不接受空串，用 0 的字符串承载并还原成数字）。 */
const UNCATEGORIZED = '0'

export default function ProductCategoryDialog({
  product,
  categories,
  onClose,
  onUpdated,
}: {
  product: AdminProduct
  /** 分类树（主分类 → 子分类）；商品只挂子分类。 */
  categories: AdminCategory[]
  onClose: () => void
  /** 提交成功后回调（页面负责刷新列表并提示）。 */
  onUpdated: () => void
}) {
  const [value, setValue] = useState(String(product.category_id))
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  const handleSubmit = async () => {
    const categoryId = Number(value)
    if (categoryId === product.category_id) {
      setError('分类未变化，无需保存')
      return
    }
    setPending(true)
    setError('')
    try {
      await updateAdminProduct(product.id, { category_id: categoryId })
      onUpdated()
    } catch (err) {
      setError(errorMessage(err, '改挂分类失败，请稍后重试'))
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog
      open
      onOpenChange={(next) => {
        if (!next && !pending) {
          onClose()
        }
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>改分类</DialogTitle>
          <DialogDescription>
            把「{product.name}」改挂到某个子分类；设置后 category_source 置为 manual，
            后续从上游导入不再自动覆盖该商品的分类。
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <Field>
            <FieldLabel htmlFor="product_category">目标子分类</FieldLabel>
            <Select
              value={value}
              onValueChange={(next) => {
                setValue(next)
                setError('')
              }}
            >
              <SelectTrigger id="product_category" aria-label="目标子分类" className="w-full">
                <SelectValue placeholder="选择子分类" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={UNCATEGORIZED}>未归类</SelectItem>
                {categories.map((main) =>
                  main.subcategories.map((sub) => (
                    <SelectItem key={sub.id} value={String(sub.id)}>
                      {main.name} / {sub.name}
                    </SelectItem>
                  )),
                )}
              </SelectContent>
            </Select>
            <FieldDescription>
              只能挂到子分类（主分类不可直接挂商品）；当前：
              {product.category_id ? product.category_name : '未归类'}。
            </FieldDescription>
          </Field>

          {error ? (
            <Alert variant="destructive">
              <AlertCircleIcon aria-hidden />
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}
        </div>

        <DialogFooter>
          <Button variant="outline" disabled={pending} onClick={onClose}>
            取消
          </Button>
          <Button disabled={pending} onClick={handleSubmit}>
            {pending ? (
              <>
                <LoaderCircleIcon className="animate-spin" data-icon="inline-start" aria-hidden />
                正在保存…
              </>
            ) : (
              '保存'
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
