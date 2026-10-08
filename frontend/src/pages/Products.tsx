import { Button } from '@heroui/react'
import { useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'

import { fetchProductCatalog } from '../api/products'
import { CardSkeletonGrid, EmptyBlock, ErrorState } from '../components/common/PageState'
import ProductCard from '../components/product/ProductCard'
import { useAsync } from '../hooks/useAsync'

// Products 是商品列表页：按分组筛选 + 商品卡片（最低价与周期提示）。
// 分组筛选走 URL query（?group=<id>），便于分享与刷新保持。
export default function Products() {
  const [searchParams, setSearchParams] = useSearchParams()
  const { data, loading, error, reload } = useAsync(fetchProductCatalog, [])

  const groupParam = searchParams.get('group')
  const activeGroupId = groupParam && /^\d+$/.test(groupParam) ? Number(groupParam) : null

  const groups = useMemo(() => data?.groups ?? [], [data])
  const visibleGroups = useMemo(
    () => (activeGroupId === null ? groups : groups.filter((group) => group.id === activeGroupId)),
    [groups, activeGroupId],
  )
  const productCount = visibleGroups.reduce((sum, group) => sum + group.products.length, 0)

  const selectGroup = (id: number | null) => {
    if (id === null) {
      setSearchParams({})
      return
    }
    setSearchParams({ group: String(id) })
  }

  return (
    <div className="mx-auto max-w-6xl px-4 py-10 sm:px-6">
      <header className="mb-6">
        <h1 className="text-2xl font-semibold text-foreground">商品列表</h1>
        <p className="mt-1 text-sm text-muted">
          共 {data?.total ?? 0} 个已上架商品，价格与库存实时同步，登录后可直接下单。
        </p>
      </header>

      {loading ? <CardSkeletonGrid /> : null}
      {!loading && error ? <ErrorState message={error} onRetry={reload} /> : null}

      {!loading && !error ? (
        <>
          {groups.length > 0 ? (
            <div className="mb-6 flex flex-wrap gap-2">
              <Button
                size="sm"
                variant={activeGroupId === null ? 'primary' : 'outline'}
                onPress={() => selectGroup(null)}
              >
                全部分组
              </Button>
              {groups.map((group) => (
                <Button
                  key={group.id}
                  size="sm"
                  variant={activeGroupId === group.id ? 'primary' : 'outline'}
                  onPress={() => selectGroup(group.id)}
                >
                  {group.name}
                  <span className="ml-1 text-xs opacity-70">{group.products.length}</span>
                </Button>
              ))}
            </div>
          ) : null}

          {productCount === 0 ? (
            <EmptyBlock
              title="没有可展示的商品"
              description={
                activeGroupId === null
                  ? '管理员导入并上架商品后，这里会自动展示。'
                  : '该分组下暂无已上架商品，试试其他分组。'
              }
            />
          ) : (
            <div className="space-y-10">
              {visibleGroups.map((group) => (
                <section key={group.id}>
                  <h2 className="mb-3 text-sm font-medium text-muted">
                    {group.name}
                    <span className="ml-2 text-xs opacity-70">{group.products.length} 个商品</span>
                  </h2>
                  <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
                    {group.products.map((product) => (
                      <ProductCard key={product.id} product={product} groupName={group.name} />
                    ))}
                  </div>
                </section>
              ))}
            </div>
          )}
        </>
      ) : null}
    </div>
  )
}
