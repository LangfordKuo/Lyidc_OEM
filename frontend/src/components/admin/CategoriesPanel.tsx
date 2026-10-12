import { useState } from 'react'

import type { AdminCategory } from '@/api/types'
import CategoryEditDialog, { type CategoryEditTarget } from '@/components/admin/CategoryEditDialog'
import CategoryRow from '@/components/admin/CategoryRow'
import { EmptyBlock, ErrorState, LoadingBlock } from '@/components/common/PageState'
import { Card, CardContent } from '@/components/ui/card'

// 分类管理面板（原「分类」页的展示能力，R9-B 并入商品与分类页的分类页签）：
// 两级分类树（主分类 → 子分类）+ 商品计数，支持改名 / 改排序 / 子分类改挂主分类。
// 数据由父级页面统一拉取（与商品筛选、改分类弹窗同一份 GET /admin/categories），
// 写操作成功后回调 onChanged 让父级刷新，避免各处数据不一致。
// 角色只读提示（含角色名）由页面渲染，这里只负责把写入口按 canWrite 禁用。

export default function CategoriesPanel({
  categories,
  loading,
  error,
  onReload,
  canWrite,
  onChanged,
}: {
  categories: AdminCategory[]
  loading: boolean
  error: string
  onReload: () => void
  /** categories.write（仅 admin）：无权限时只读视图，编辑入口禁用。 */
  canWrite: boolean
  /** 分类写操作成功后的回调（父级刷新分类树与商品列表）。 */
  onChanged: () => void
}) {
  const [editTarget, setEditTarget] = useState<CategoryEditTarget | null>(null)

  return (
    <div className="space-y-5">
      {loading ? <LoadingBlock label="正在读取分类…" /> : null}
      {error ? <ErrorState message={error} onRetry={onReload} /> : null}

      {!loading && !error ? (
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
            onChanged()
          }}
        />
      ) : null}
    </div>
  )
}
