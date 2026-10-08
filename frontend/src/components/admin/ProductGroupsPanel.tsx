import { useState } from 'react'
import { AlertCircleIcon, ChevronDownIcon, ChevronUpIcon, LoaderCircleIcon } from 'lucide-react'
import { toast } from 'sonner'

import { updateAdminProductGroup } from '@/api/adminProducts'
import { errorMessage } from '@/api/client'
import type { AdminProductGroup } from '@/api/types'
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
import { permissionHint } from '@/lib/adminRoles'
import { cn } from '@/lib/utils'

// 商品分组面板（契约 10.4）：可折叠区块展示全部分组（含空分组）与商品计数，
// 支持「点击分组行 → 列表按该分组筛选」与「重命名 / 改排序」（仅 admin / finance，groups.write）。
// 分组名与排序是本地字段，导入不会覆盖，因此可放心按自己的品牌命名。

const MAX_SORT = 999999

/** 分组名校验：去首尾空格后 1-128 个字符。 */
function validateGroupName(value: string): string {
  const trimmed = value.trim()
  if (!trimmed) {
    return '请输入分组名称'
  }
  if (trimmed.length > 128) {
    return '名称最多 128 个字符'
  }
  return ''
}

/** 排序校验：整数，-999999 ~ 999999。 */
function validateGroupSort(value: string): string {
  const trimmed = value.trim()
  if (!trimmed) {
    return '请输入排序值'
  }
  if (!/^-?\d+$/.test(trimmed)) {
    return '排序需为整数'
  }
  const sort = Number(trimmed)
  if (sort < -MAX_SORT || sort > MAX_SORT) {
    return `排序需为 ${-MAX_SORT} 到 ${MAX_SORT} 之间的整数`
  }
  return ''
}

export default function ProductGroupsPanel({
  groups,
  loading,
  error,
  onReload,
  canWrite,
  selectedGroupId,
  onSelectGroup,
}: {
  groups: AdminProductGroup[]
  loading: boolean
  error: string
  onReload: () => void
  /** groups.write：无权限时仅可查看与筛选，编辑入口禁用。 */
  canWrite: boolean
  /** 当前列表使用中的分组筛选值（'' = 全部分组）。 */
  selectedGroupId: string
  onSelectGroup: (groupId: string) => void
}) {
  const [expanded, setExpanded] = useState(false)
  const [editTarget, setEditTarget] = useState<AdminProductGroup | null>(null)

  return (
    <Card>
      <CardContent className="space-y-0 px-4 py-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="min-w-0">
            <p className="text-sm font-medium text-foreground">商品分组</p>
            <p className="mt-0.5 text-xs text-muted-foreground">
              共 {groups.length} 个分组；点击分组行可把该分组作为列表筛选条件。
              {canWrite ? '' : `（${permissionHint('groups.write')}，暂不可重命名或改排序）`}
            </p>
          </div>
          <Button
            size="sm"
            variant="outline"
            aria-expanded={expanded}
            onClick={() => setExpanded((value) => !value)}
          >
            {expanded ? <ChevronUpIcon aria-hidden /> : <ChevronDownIcon aria-hidden />}
            {expanded ? '收起分组' : `展开分组（${groups.length}）`}
          </Button>
        </div>

        {expanded ? (
          <div className="mt-3 space-y-2 border-t border-border pt-3">
            {loading ? (
              <p className="text-xs text-muted-foreground">正在读取分组…</p>
            ) : null}

            {error ? (
              <Alert variant="destructive">
                <AlertCircleIcon aria-hidden />
                <AlertDescription>分组读取失败：{error}</AlertDescription>
                <Button size="sm" variant="outline" onClick={onReload}>
                  重试
                </Button>
              </Alert>
            ) : null}

            {!loading && !error && groups.length === 0 ? (
              <p className="text-xs text-muted-foreground">
                还没有分组：从上游导入商品时会按上游目录自动创建分组。
              </p>
            ) : null}

            {groups.map((group) => {
              const active = selectedGroupId === String(group.id)
              return (
                <div
                  key={group.id}
                  className={cn(
                    'flex flex-wrap items-center justify-between gap-3 rounded-lg border px-3 py-2',
                    active ? 'border-primary bg-primary/5' : 'border-border',
                  )}
                >
                  <button
                    type="button"
                    className="min-w-0 text-left"
                    aria-pressed={active}
                    onClick={() => onSelectGroup(active ? '' : String(group.id))}
                  >
                    <span className="block truncate text-sm text-foreground hover:text-primary">
                      {group.name}
                      {active ? <span className="ml-2 text-xs text-primary">筛选中</span> : null}
                    </span>
                    <span className="mt-0.5 block text-xs text-muted-foreground">
                      排序 {group.sort} · 商品 {group.products.total} 个（已上架 {group.products.on} /
                      已下架 {group.products.off}）
                    </span>
                  </button>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={!canWrite}
                    title={canWrite ? undefined : permissionHint('groups.write')}
                    onClick={() => setEditTarget(group)}
                  >
                    编辑
                  </Button>
                </div>
              )
            })}
          </div>
        ) : null}

        {editTarget ? (
          <GroupEditDialog
            group={editTarget}
            onClose={() => setEditTarget(null)}
            onSaved={() => {
              setEditTarget(null)
              onReload()
            }}
          />
        ) : null}
      </CardContent>
    </Card>
  )
}

/** 分组编辑弹窗：重命名（1-128 字符）与改排序（-999999 ~ 999999），至少提交一次修改。 */
function GroupEditDialog({
  group,
  onClose,
  onSaved,
}: {
  group: AdminProductGroup
  onClose: () => void
  onSaved: () => void
}) {
  const [name, setName] = useState(group.name)
  const [sort, setSort] = useState(String(group.sort))
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  const nameError = name ? validateGroupName(name) : ''
  const sortError = sort ? validateGroupSort(sort) : ''

  const handleSubmit = async () => {
    const trimmed = name.trim()
    const nameMessage = validateGroupName(name)
    const sortMessage = validateGroupSort(sort)
    if (nameMessage || sortMessage) {
      setError(nameMessage || sortMessage)
      return
    }
    if (trimmed === group.name && Number(sort) === group.sort) {
      setError('名称与排序均未变化，无需保存')
      return
    }

    setPending(true)
    setError('')
    try {
      await updateAdminProductGroup(group.id, { name: trimmed, sort: Number(sort.trim()) })
      toast.success(`分组「${trimmed}」已更新`)
      onSaved()
    } catch (err) {
      setError(errorMessage(err, '保存分组失败，请稍后重试'))
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
          <DialogTitle>编辑分组</DialogTitle>
          <DialogDescription>分组名与排序是本地字段，上游导入不会覆盖。</DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <Field data-invalid={name && nameError ? true : undefined}>
            <FieldLabel htmlFor="group_name">分组名称</FieldLabel>
            <Input
              id="group_name"
              name="group_name"
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
            <FieldLabel htmlFor="group_sort">排序</FieldLabel>
            <Input
              id="group_sort"
              name="group_sort"
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

          <p className="text-xs text-muted-foreground">商品计数由系统实时统计，不在本弹窗编辑。</p>

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
