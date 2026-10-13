import { useState } from 'react'
import { AlertCircleIcon, LoaderCircleIcon } from 'lucide-react'
import { toast } from 'sonner'

import {
  deleteAdminProductGroupMap,
  listAdminProductGroupMaps,
  upsertAdminProductGroupMap,
} from '@/api/adminProducts'
import { errorMessage } from '@/api/client'
import type { AdminCategory, AdminProductGroup, ProductGroupCategoryMap } from '@/api/types'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useAsync } from '@/hooks/useAsync'
import { permissionHint } from '@/lib/adminRoles'

// 「上游分组 → 本地子分类」映射管理（R12，契约 10.1 / 10.4）：与分类管理同页（分类页签），
// 属同一心智。表格列出每个上游分组，目标只能从**已有子分类**里选（两级展示「香港 / 香港二区」），
// 支持新增/修改（同一个下拉 + 保存）与删除；未映射的分组显示「未映射（按规则自动归类）」。
// 不做弹窗内联新建分类——要新建分类请先去分类树创建。
// 写操作沿用 categories.write（**仅 admin**），由页面按角色矩阵传入 canWrite。

/** 子分类下拉选项（两级展示标签，值为本地子分类 ID）。 */
interface SubOption {
  id: number
  label: string
}

export default function ProductGroupMapsPanel({
  groups,
  categories,
  canWrite,
}: {
  groups: AdminProductGroup[]
  categories: AdminCategory[]
  /** categories.write（仅 admin）：无权限时只读，写入口禁用并给出原因。 */
  canWrite: boolean
}) {
  const [reloadKey, setReloadKey] = useState(0)
  const mapsState = useAsync(() => listAdminProductGroupMaps(), [reloadKey])
  const reload = () => setReloadKey((value) => value + 1)

  const maps = mapsState.data?.items ?? []
  const byGroup = new Map<number, ProductGroupCategoryMap>(
    maps.map((item) => [item.upstream_group_id, item]),
  )
  const subOptions: SubOption[] = categories.flatMap((main) =>
    main.subcategories.map((sub) => ({ id: sub.id, label: `${main.name} / ${sub.name}` })),
  )

  return (
    <Card>
      <CardContent className="space-y-3 px-4 py-3">
        <div className="min-w-0">
          <p className="text-sm font-medium text-foreground">上游分组映射</p>
          <p className="mt-0.5 text-xs text-muted-foreground">
            把某个上游分组固定映射到一个已有子分类：之后每次导入该分组的商品都自动套用该归类，
            不再依赖商品名 / 组名派生。未映射的分组按规则自动归类；删除映射后重新导入即回落到自动归类。
            {canWrite ? '' : `（${permissionHint('categories.write')}，当前只读）`}
          </p>
        </div>

        {mapsState.loading ? (
          <p className="text-xs text-muted-foreground">正在读取映射…</p>
        ) : null}

        {mapsState.error ? (
          <Alert variant="destructive">
            <AlertCircleIcon aria-hidden />
            <AlertDescription>映射读取失败：{mapsState.error}</AlertDescription>
            <Button size="sm" variant="outline" onClick={mapsState.reload}>
              重试
            </Button>
          </Alert>
        ) : null}

        {!mapsState.loading && !mapsState.error && groups.length === 0 ? (
          <p className="text-xs text-muted-foreground">
            还没有上游分组：从上游导入商品后即可在这里配置映射。
          </p>
        ) : null}

        {!mapsState.loading && !mapsState.error && groups.length > 0 ? (
          <div className="space-y-2">
            {groups.map((group) => {
              const mapping = byGroup.get(group.upstream_group_id)
              return (
                <GroupMapRow
                  // 映射变化后重挂载该行，让下拉状态与最新映射同步。
                  key={`${group.upstream_group_id}:${mapping?.category_id ?? 'none'}`}
                  group={group}
                  mapping={mapping}
                  subOptions={subOptions}
                  canWrite={canWrite}
                  onChanged={reload}
                />
              )
            })}
          </div>
        ) : null}
      </CardContent>
    </Card>
  )
}

/** 单个上游分组的映射行：下拉选子分类 + 保存 / 删除。 */
function GroupMapRow({
  group,
  mapping,
  subOptions,
  canWrite,
  onChanged,
}: {
  group: AdminProductGroup
  mapping: ProductGroupCategoryMap | undefined
  subOptions: SubOption[]
  canWrite: boolean
  onChanged: () => void
}) {
  const currentId = mapping ? String(mapping.category_id) : ''
  const [value, setValue] = useState<string | undefined>(mapping ? String(mapping.category_id) : undefined)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  const changed = (value ?? '') !== currentId
  // 目标分类已被删除（接口对悬空映射返回空分类名）——提示并让用户重选。
  const targetMissing = Boolean(mapping && !mapping.category_name)

  const handleSave = async () => {
    if (!value) {
      return
    }
    setPending(true)
    setError('')
    try {
      await upsertAdminProductGroupMap(group.upstream_group_id, Number(value))
      toast.success(`分组「${group.name}」的映射已保存，重新导入后生效`)
      onChanged()
    } catch (err) {
      setError(errorMessage(err, '保存映射失败，请稍后重试'))
    } finally {
      setPending(false)
    }
  }

  const handleDelete = async () => {
    setPending(true)
    setError('')
    try {
      await deleteAdminProductGroupMap(group.upstream_group_id)
      toast.success(`分组「${group.name}」的映射已删除，重新导入后回落自动归类`)
      onChanged()
    } catch (err) {
      setError(errorMessage(err, '删除映射失败，请稍后重试'))
    } finally {
      setPending(false)
    }
  }

  return (
    <div
      data-testid={`group-map-${group.upstream_group_id}`}
      className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border px-3 py-2"
    >
      <div className="min-w-0">
        <p className="truncate text-sm text-foreground">{group.name}</p>
        <p className="mt-0.5 text-xs text-muted-foreground">
          {mapping ? (
            targetMissing ? (
              <span className="text-destructive">目标分类已失效（分类被删除），请重新选择</span>
            ) : (
              <>
                → {mapping.category_parent_name ? `${mapping.category_parent_name} / ` : ''}
                {mapping.category_name}
              </>
            )
          ) : (
            '未映射（按规则自动归类）'
          )}
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <Select
          value={value}
          onValueChange={(next) => {
            setValue(next)
            setError('')
          }}
        >
          <SelectTrigger
            aria-label={`映射目标 ${group.name}`}
            className="w-56"
            disabled={!canWrite}
          >
            <SelectValue placeholder="选择子分类" />
          </SelectTrigger>
          <SelectContent>
            {subOptions.map((option) => (
              <SelectItem key={option.id} value={String(option.id)}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button
          size="sm"
          disabled={!canWrite || pending || !value || !changed}
          title={canWrite ? undefined : permissionHint('categories.write')}
          onClick={handleSave}
        >
          {pending ? (
            <>
              <LoaderCircleIcon className="animate-spin" data-icon="inline-start" aria-hidden />
              保存中…
            </>
          ) : (
            '保存'
          )}
        </Button>
        <Button
          size="sm"
          variant="outline"
          disabled={!canWrite || pending || !mapping}
          title={canWrite ? undefined : permissionHint('categories.write')}
          onClick={handleDelete}
        >
          删除
        </Button>
      </div>

      {error ? (
        <p className="w-full text-xs text-destructive">{error}</p>
      ) : null}
    </div>
  )
}
