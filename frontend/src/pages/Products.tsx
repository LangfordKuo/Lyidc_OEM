import { useMemo, useState, type FormEvent } from 'react'
import { useSearchParams } from 'react-router-dom'
import { SearchIcon } from 'lucide-react'

import { fetchProductCatalog } from '@/api/products'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Pagination,
  PaginationContent,
  PaginationEllipsis,
  PaginationItem,
  PaginationLink,
  PaginationNext,
  PaginationPrevious,
} from '@/components/ui/pagination'
import { CardSkeletonGrid, EmptyBlock, ErrorState } from '@/components/common/PageState'
import ProductCard from '@/components/product/ProductCard'
import { useAsync } from '@/hooks/useAsync'
import { PRODUCT_PAGE_SIZE, pageWindow, totalPages } from '@/lib/pagination'
import { cn } from '@/lib/utils'

/**
 * 商品列表页：分组筛选 + 关键词搜索 + 分页。
 * 数据来源是 GET /api/v1/products（一次返回全量上架商品，契约 10.3 无查询参数），
 * 筛选与分页在前端完成；筛选条件走 URL query（?group=&q=&page=），便于分享与刷新保持。
 */
export default function Products() {
  const [searchParams, setSearchParams] = useSearchParams()
  const { data, loading, error, reload } = useAsync(fetchProductCatalog, [])

  const groupParam = searchParams.get('group')
  const activeGroupId = groupParam && /^\d+$/.test(groupParam) ? Number(groupParam) : null
  const queryParam = searchParams.get('q') ?? ''
  const pageParam = searchParams.get('page')
  const requestedPage = pageParam && /^\d+$/.test(pageParam) ? Number(pageParam) : 1

  const [keyword, setKeyword] = useState(queryParam)

  const groups = useMemo(() => data?.groups ?? [], [data])

  const flatProducts = useMemo(
    () =>
      groups.flatMap((group) =>
        group.products.map((product) => ({ product, groupName: group.name, groupId: group.id })),
      ),
    [groups],
  )

  const filtered = useMemo(() => {
    const q = queryParam.trim().toLowerCase()
    return flatProducts.filter((item) => {
      if (activeGroupId !== null && item.groupId !== activeGroupId) {
        return false
      }
      if (q && !item.product.name.toLowerCase().includes(q)) {
        return false
      }
      return true
    })
  }, [flatProducts, activeGroupId, queryParam])

  const lastPage = totalPages(filtered.length, PRODUCT_PAGE_SIZE)
  const page = Math.min(Math.max(1, requestedPage), lastPage)
  const paged = filtered.slice((page - 1) * PRODUCT_PAGE_SIZE, page * PRODUCT_PAGE_SIZE)

  const updateParams = (next: { group?: number | null; q?: string; page?: number }) => {
    const params = new URLSearchParams()
    const group = next.group === undefined ? activeGroupId : next.group
    const q = next.q === undefined ? queryParam : next.q
    const nextPage = next.page ?? 1
    if (group !== null && group !== undefined) {
      params.set('group', String(group))
    }
    if (q.trim()) {
      params.set('q', q.trim())
    }
    if (nextPage > 1) {
      params.set('page', String(nextPage))
    }
    setSearchParams(params)
  }

  const handleSearchSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    updateParams({ q: keyword, page: 1 })
  }

  const activeGroup = groups.find((group) => group.id === activeGroupId) ?? null

  return (
    <div className="mx-auto max-w-6xl px-4 py-10 sm:px-6">
      <header className="mb-6">
        <h1 className="text-2xl font-semibold tracking-tight text-foreground">商品列表</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          共 {data?.total ?? 0} 个已上架商品，价格与库存实时同步，登录后可直接下单。
        </p>
      </header>

      {loading ? <CardSkeletonGrid /> : null}
      {!loading && error ? <ErrorState message={error} onRetry={reload} /> : null}

      {!loading && !error ? (
        <>
          <div className="mb-6 space-y-4">
            <form onSubmit={handleSearchSubmit} className="flex max-w-md gap-2" role="search">
              <div className="relative flex-1">
                <SearchIcon
                  className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground"
                  aria-hidden
                />
                <Input
                  value={keyword}
                  onChange={(event) => setKeyword(event.target.value)}
                  placeholder="搜索商品名称"
                  aria-label="搜索商品名称"
                  className="h-9 pl-9"
                />
              </div>
              <Button type="submit" variant="outline" className="h-9">
                搜索
              </Button>
              {queryParam ? (
                <Button
                  type="button"
                  variant="ghost"
                  className="h-9"
                  onClick={() => {
                    setKeyword('')
                    updateParams({ q: '', page: 1 })
                  }}
                >
                  清除
                </Button>
              ) : null}
            </form>

            {groups.length > 0 ? (
              <div className="flex flex-wrap gap-2">
                <Button
                  size="sm"
                  variant={activeGroupId === null ? 'default' : 'outline'}
                  onClick={() => updateParams({ group: null, page: 1 })}
                >
                  全部分组
                </Button>
                {groups.map((group) => (
                  <Button
                    key={group.id}
                    size="sm"
                    variant={activeGroupId === group.id ? 'default' : 'outline'}
                    onClick={() => updateParams({ group: group.id, page: 1 })}
                  >
                    {group.name}
                    <span className="ml-1 text-xs opacity-70">{group.products.length}</span>
                  </Button>
                ))}
              </div>
            ) : null}
          </div>

          <p className="mb-4 text-sm text-muted-foreground" aria-live="polite">
            {activeGroup ? `${activeGroup.name} · ` : ''}
            {queryParam ? `关键词「${queryParam}」 · ` : ''}
            共 {filtered.length} 个商品
          </p>

          {filtered.length === 0 ? (
            <EmptyBlock
              title="没有找到匹配的商品"
              description={
                queryParam || activeGroupId !== null
                  ? '试试更换关键词或切换分组。'
                  : '管理员导入并上架商品后，这里会自动展示。'
              }
              action={
                queryParam || activeGroupId !== null ? (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      setKeyword('')
                      updateParams({ group: null, q: '', page: 1 })
                    }}
                  >
                    重置筛选
                  </Button>
                ) : undefined
              }
            />
          ) : (
            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {paged.map(({ product, groupName }) => (
                <ProductCard key={product.id} product={product} groupName={groupName} />
              ))}
            </div>
          )}

          {lastPage > 1 ? (
            <Pagination className="mt-8">
              <PaginationContent>
                <PaginationItem>
                  <PaginationPrevious
                    href="#"
                    text="上一页"
                    aria-disabled={page <= 1}
                    className={cn(page <= 1 && 'pointer-events-none opacity-50')}
                    onClick={(event) => {
                      event.preventDefault()
                      if (page > 1) {
                        updateParams({ page: page - 1 })
                        window.scrollTo({ top: 0 })
                      }
                    }}
                  />
                </PaginationItem>
                {pageWindow(page, lastPage).map((item, index) =>
                  item === 'gap' ? (
                    <PaginationItem key={`gap-${index}`}>
                      <PaginationEllipsis />
                    </PaginationItem>
                  ) : (
                    <PaginationItem key={item}>
                      <PaginationLink
                        href="#"
                        isActive={item === page}
                        aria-label={`第 ${item} 页`}
                        onClick={(event) => {
                          event.preventDefault()
                          updateParams({ page: item })
                          window.scrollTo({ top: 0 })
                        }}
                      >
                        {item}
                      </PaginationLink>
                    </PaginationItem>
                  ),
                )}
                <PaginationItem>
                  <PaginationNext
                    href="#"
                    text="下一页"
                    aria-disabled={page >= lastPage}
                    className={cn(page >= lastPage && 'pointer-events-none opacity-50')}
                    onClick={(event) => {
                      event.preventDefault()
                      if (page < lastPage) {
                        updateParams({ page: page + 1 })
                        window.scrollTo({ top: 0 })
                      }
                    }}
                  />
                </PaginationItem>
              </PaginationContent>
            </Pagination>
          ) : null}
        </>
      ) : null}
    </div>
  )
}
