import { useState } from 'react'
import { AlertCircleIcon, LoaderCircleIcon } from 'lucide-react'
import { toast } from 'sonner'

import { updateAdminCategory } from '@/api/adminCategories'
import { errorMessage } from '@/api/client'
import type { AdminCategory, AdminSubcategory, UpdateCategoryInput } from '@/api/types'
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
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { validateCatalogName, validateCatalogSort } from '@/lib/validate'

// 分类编辑弹窗（原「分类」页的编辑能力，R9-B 并入商品与分类页的分类页签）：
// 改名（1-128 字符）与改排序（-999999 ~ 999999）；
// 子分类额外提供「所属主分类」下拉（不能选自身——自身是子分类，目标只列主分类）。
// 只提交有变化的字段（契约 10.4：至少提供一个）。写操作仅 admin，由页面控制挂载。

/** 编辑目标：主分类（无改挂项）或子分类（带当前所属主分类）。 */
export type CategoryEditTarget =
  | { kind: 'main'; item: AdminCategory }
  | { kind: 'sub'; item: AdminSubcategory; parentId: number }

/** 目标主分类下拉的哨兵值（Radix Select 不接受空串 value）。 */
const PARENT_REQUIRED = '__parent_required__'

export default function CategoryEditDialog({
  target,
  mainCategories,
  onClose,
  onSaved,
}: {
  target: CategoryEditTarget
  mainCategories: AdminCategory[]
  onClose: () => void
  onSaved: () => void
}) {
  const isSub = target.kind === 'sub'
  const item = target.item

  const [name, setName] = useState(item.name)
  const [sort, setSort] = useState(String(item.sort))
  const [parentId, setParentId] = useState(
    isSub ? String(target.parentId) : PARENT_REQUIRED,
  )
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  const nameError = name ? validateCatalogName(name, '分类名称') : ''
  const sortError = sort ? validateCatalogSort(sort) : ''

  const handleSubmit = async () => {
    const trimmed = name.trim()
    const nameMessage = validateCatalogName(name, '分类名称')
    const sortMessage = validateCatalogSort(sort)
    if (nameMessage || sortMessage) {
      setError(nameMessage || sortMessage)
      return
    }

    const body: UpdateCategoryInput = {}
    if (trimmed !== item.name) {
      body.name = trimmed
    }
    const nextSort = Number(sort.trim())
    if (nextSort !== item.sort) {
      body.sort = nextSort
    }
    if (isSub && parentId !== String(target.parentId)) {
      body.parent_id = Number(parentId)
    }
    if (Object.keys(body).length === 0) {
      setError('名称、排序与所属主分类均未变化，无需保存')
      return
    }

    setPending(true)
    setError('')
    try {
      await updateAdminCategory(item.id, body)
      toast.success(`分类「${trimmed}」已更新`)
      onSaved()
    } catch (err) {
      setError(errorMessage(err, '保存分类失败，请稍后重试'))
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
          <DialogTitle>{isSub ? '编辑子分类' : '编辑主分类'}</DialogTitle>
          <DialogDescription>
            分类名与排序是本地字段，上游导入不会覆盖（改名后 key 不变、不重建）。
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <Field data-invalid={name && nameError ? true : undefined}>
            <FieldLabel htmlFor="category_name">分类名称</FieldLabel>
            <Input
              id="category_name"
              name="category_name"
              value={name}
              aria-invalid={name && nameError ? true : undefined}
              placeholder="1-128 个字符，保存时自动去首尾空格"
              onChange={(event) => {
                setName(event.target.value)
                setError('')
              }}
            />
            {name && nameError ? (
              <FieldDescription className="text-destructive">{nameError}</FieldDescription>
            ) : null}
          </Field>

          <Field data-invalid={sort && sortError ? true : undefined}>
            <FieldLabel htmlFor="category_sort">排序</FieldLabel>
            <Input
              id="category_sort"
              name="category_sort"
              inputMode="numeric"
              value={sort}
              aria-invalid={sort && sortError ? true : undefined}
              placeholder="-999999 ~ 999999，越小越靠前"
              onChange={(event) => {
                setSort(event.target.value)
                setError('')
              }}
            />
            {sort && sortError ? (
              <FieldDescription className="text-destructive">{sortError}</FieldDescription>
            ) : null}
          </Field>

          {isSub ? (
            <Field>
              <FieldLabel htmlFor="category_parent">所属主分类</FieldLabel>
              <Select
                value={parentId}
                onValueChange={(next) => {
                  setParentId(next)
                  setError('')
                }}
              >
                <SelectTrigger id="category_parent" aria-label="所属主分类" className="w-full">
                  <SelectValue placeholder="选择主分类" />
                </SelectTrigger>
                <SelectContent>
                  {mainCategories.map((main) => (
                    <SelectItem key={main.id} value={String(main.id)}>
                      {main.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FieldDescription>改挂后商品仍留在该子分类下，导入也不会覆盖。</FieldDescription>
            </Field>
          ) : (
            <p className="text-xs text-muted-foreground">
              主分类不能改挂（只有子分类可以换所属主分类）；商品计数由系统实时统计，不在本弹窗编辑。
            </p>
          )}

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
