import { useState } from 'react'
import { AlertCircleIcon, LoaderCircleIcon } from 'lucide-react'
import { toast } from 'sonner'

import { listAdminCategories, updateAdminCategory } from '@/api/adminCategories'
import { errorMessage } from '@/api/client'
import type { AdminCategory, AdminSubcategory, UpdateCategoryInput } from '@/api/types'
import { useAdminAuth } from '@/auth/adminAuthContext'
import { EmptyBlock, ErrorState, LoadingBlock } from '@/components/common/PageState'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
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
import { useAsync } from '@/hooks/useAsync'
import { ADMIN_ROLE_LABELS, hasPermission, permissionHint } from '@/lib/adminRoles'
import { validateCatalogName, validateCatalogSort } from '@/lib/validate'

// 管理后台「分类」页（契约 10.4）：两级分类树（主分类 → 子分类）+ 商品计数，
// 支持改名 / 改排序 / 子分类改挂主分类。
// 角色矩阵（契约 10.5 / 12.7）：三类角色都能进（GET /admin/categories 只读）；
// 写操作**仅 admin**（categories.write），其余角色进来是只读视图，写入口禁用且不发无权限请求。

/** 编辑目标：主分类（无改挂项）或子分类（带当前所属主分类）。 */
type EditTarget =
  | { kind: 'main'; item: AdminCategory }
  | { kind: 'sub'; item: AdminSubcategory; parentId: number }

/** 目标主分类下拉的哨兵值（Radix Select 不接受空串 value）。 */
const PARENT_REQUIRED = '__parent_required__'

export default function AdminCategories() {
  const { role } = useAdminAuth()
  const canWrite = hasPermission(role, 'categories.write')

  const [refreshKey, setRefreshKey] = useState(0)
  const [editTarget, setEditTarget] = useState<EditTarget | null>(null)

  const state = useAsync(() => listAdminCategories(), [refreshKey])
  const categories = state.data?.items ?? []

  const refresh = () => setRefreshKey((value) => value + 1)

  return (
    <div className="space-y-5">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-foreground">分类</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            商品两级分类（主分类 → 子分类）：改名、改排序、把子分类改挂到别的主分类。
          </p>
          {!canWrite ? (
            <p className="mt-1 text-xs text-muted-foreground">
              当前角色（{role ? ADMIN_ROLE_LABELS[role] : '未登录'}）为只读视角：改名、改排序与改挂均已禁用（
              {permissionHint('categories.write')}）。
            </p>
          ) : null}
        </div>
        <Button variant="outline" size="sm" onClick={refresh}>
          刷新
        </Button>
      </header>

      {state.loading ? <LoadingBlock label="正在读取分类…" /> : null}
      {state.error ? <ErrorState message={state.error} onRetry={state.reload} /> : null}

      {!state.loading && !state.error ? (
        categories.length === 0 ? (
          <EmptyBlock
            title="还没有分类"
            description="从上游导入商品时会按商品名与上游组名自动派生两级分类，导入后可在这里改名与改挂。"
          />
        ) : (
          <div className="space-y-3">
            {categories.map((category) => (
              <Card key={category.id} data-testid={`category-${category.id}`}>
                <CardContent className="space-y-2 px-4 py-3">
                  <CategoryRow
                    title={category.name}
                    meta={`排序 ${category.sort} · 子分类 ${category.subcategories.length} 个`}
                    badge="主分类"
                    canWrite={canWrite}
                    onEdit={() => setEditTarget({ kind: 'main', item: category })}
                  />
                  {category.subcategories.length === 0 ? (
                    <p className="pl-4 text-xs text-muted-foreground">
                      该主分类下还没有子分类（商品都挂在子分类上）。
                    </p>
                  ) : (
                    <div className="space-y-1.5 border-l border-border pl-4">
                      {category.subcategories.map((sub) => (
                        <CategoryRow
                          key={sub.id}
                          title={sub.name}
                          meta={`排序 ${sub.sort} · 商品 ${sub.products.total} 个（已上架 ${sub.products.on} / 已下架 ${sub.products.off}）`}
                          canWrite={canWrite}
                          onEdit={() =>
                            setEditTarget({ kind: 'sub', item: sub, parentId: category.id })
                          }
                        />
                      ))}
                    </div>
                  )}
                </CardContent>
              </Card>
            ))}
          </div>
        )
      ) : null}

      {editTarget ? (
        <CategoryEditDialog
          target={editTarget}
          mainCategories={categories}
          onClose={() => setEditTarget(null)}
          onSaved={() => {
            setEditTarget(null)
            refresh()
          }}
        />
      ) : null}
    </div>
  )
}

/** 分类行：名称 + 计数摘要 + 编辑入口（无权限时禁用并给出原因）。 */
function CategoryRow({
  title,
  meta,
  badge,
  canWrite,
  onEdit,
}: {
  title: string
  meta: string
  badge?: string
  canWrite: boolean
  onEdit: () => void
}) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border px-3 py-2">
      <div className="min-w-0">
        {/* 名称与徽标分成两个元素：名称独立成节点，便于按文本精确定位。 */}
        <p className="flex items-center gap-2">
          <span className="truncate text-sm font-medium text-foreground">{title}</span>
          {badge ? (
            <span className="rounded-full bg-muted px-1.5 py-0.5 text-[10px] leading-4 text-muted-foreground">
              {badge}
            </span>
          ) : null}
        </p>
        <p className="mt-0.5 text-xs text-muted-foreground">{meta}</p>
      </div>
      <Button
        size="sm"
        variant="outline"
        disabled={!canWrite}
        title={canWrite ? undefined : permissionHint('categories.write')}
        onClick={onEdit}
      >
        编辑
      </Button>
    </div>
  )
}

/**
 * 分类编辑弹窗：改名（1-128 字符）与改排序（-999999 ~ 999999）；
 * 子分类额外提供「所属主分类」下拉（不能选自身——自身是子分类，目标只列主分类）。
 * 只提交有变化的字段（契约 10.4：至少提供一个）。
 */
function CategoryEditDialog({
  target,
  mainCategories,
  onClose,
  onSaved,
}: {
  target: EditTarget
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
