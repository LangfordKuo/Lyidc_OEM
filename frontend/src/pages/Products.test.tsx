import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import type { ProductCatalog } from '@/api/types'
import { makeCatalog, makeProductSummary } from '@/test/fixtures'
import { installFetchMock, ok, renderApp } from '@/test/harness'

function catalogOf(count: number): ProductCatalog {
  const products = Array.from({ length: count }, (_, index) =>
    makeProductSummary({
      id: index + 1,
      name: `测试商品${String(index + 1).padStart(2, '0')}`,
    }),
  )
  // R7：目录已是两级分类结构——groups[] 语义为「子分类」（带 parent），categories[] 是主分类树。
  return {
    categories: [
      {
        id: 90,
        name: '默认',
        sort: 0,
        subcategories: [{ id: 1, name: '默认分组', sort: 0, product_count: count }],
      },
    ],
    groups: [{ id: 1, name: '默认分组', sort: 0, parent: { id: 90, name: '默认' }, products }],
    total: count,
  }
}

function mockCatalog(catalog: ProductCatalog = makeCatalog()) {
  return installFetchMock((url) =>
    url.pathname === '/api/v1/products' ? ok(catalog) : undefined,
  )
}

describe('商品列表页', () => {
  it('渲染两级分类筛选与全部商品', async () => {
    mockCatalog()
    renderApp(['/products'])

    expect(await screen.findByText('香港二区 CN2 A型')).toBeInTheDocument()
    expect(screen.getByText('襄阳云服务器-A型')).toBeInTheDocument()
    // 一级：主分类按钮（含「全部分类」）
    expect(screen.getByRole('button', { name: /全部分类/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /香港/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /国内/ })).toBeInTheDocument()
    // 未选主分类时不展示二级子分类区
    expect(screen.queryByTestId('subcategory-filter')).not.toBeInTheDocument()
    expect(screen.getByText(/共 3 个商品/)).toBeInTheDocument()
  })

  it('两级分类筛选：点主分类露出子分类，再点子分类只剩该子类商品', async () => {
    mockCatalog()
    const user = userEvent.setup()
    renderApp(['/products'])

    await screen.findByText('香港二区 CN2 A型')

    // 一级：选「国内」主分类 → 二级子分类区出现
    await user.click(screen.getByRole('button', { name: /国内/ }))
    const subFilter = await screen.findByTestId('subcategory-filter')

    // 二级：点子分类「襄阳云服务器」
    await user.click(within(subFilter).getByRole('button', { name: /襄阳云服务器/ }))

    await waitFor(() => {
      expect(screen.queryByText('香港二区 CN2 A型')).not.toBeInTheDocument()
    })
    expect(screen.getByText('襄阳云服务器-A型')).toBeInTheDocument()
    expect(screen.getByText(/共 1 个商品/)).toBeInTheDocument()
  })

  it('?cat= 直达主分类：高亮该主分类并只展示其下商品', async () => {
    mockCatalog()
    renderApp(['/products?cat=101'])

    const categoryFilter = await screen.findByTestId('category-filter')
    const hongkong = within(categoryFilter).getByRole('button', { name: /香港/ })
    // 选中的主分类用 primary 底色（variant=default）
    expect(hongkong).toHaveClass('bg-primary')
    // 二级子分类直接展示
    const subFilter = await screen.findByTestId('subcategory-filter')
    expect(within(subFilter).getByRole('button', { name: /香港二区/ })).toBeInTheDocument()
    // 只剩香港分类下的商品
    expect(screen.getByText('香港二区 CN2 A型')).toBeInTheDocument()
    expect(screen.queryByText('襄阳云服务器-A型')).not.toBeInTheDocument()
  })

  it('主分类下无子分类时展示空态提示', async () => {
    mockCatalog({
      categories: [{ id: 101, name: '香港', sort: 0, subcategories: [] }],
      groups: [],
      total: 0,
    })
    const user = userEvent.setup()
    renderApp(['/products'])

    await user.click(await screen.findByRole('button', { name: /香港/ }))
    const subFilter = await screen.findByTestId('subcategory-filter')
    expect(within(subFilter).getByText(/暂无子分类/)).toBeInTheDocument()
  })

  it('按关键词搜索过滤商品', async () => {
    mockCatalog()
    const user = userEvent.setup()
    renderApp(['/products'])

    await screen.findByText('香港二区 CN2 A型')
    await user.type(screen.getByLabelText('搜索商品名称'), '襄阳')
    await user.click(screen.getByRole('button', { name: '搜索' }))

    await waitFor(() => expect(screen.queryByText('香港二区 CN2 A型')).not.toBeInTheDocument())
    expect(screen.getByText('襄阳云服务器-A型')).toBeInTheDocument()

    // 清除后恢复全部
    await user.click(screen.getByRole('button', { name: '清除' }))
    expect(await screen.findByText('香港二区 CN2 A型')).toBeInTheDocument()
  })

  it('超过每页条数时分页，翻页切换商品', async () => {
    mockCatalog(catalogOf(20))
    const user = userEvent.setup()
    renderApp(['/products'])

    expect(await screen.findByText('测试商品01')).toBeInTheDocument()
    expect(screen.getByText('测试商品12')).toBeInTheDocument()
    expect(screen.queryByText('测试商品13')).not.toBeInTheDocument()

    await user.click(screen.getByRole('link', { name: '第 2 页' }))

    expect(await screen.findByText('测试商品13')).toBeInTheDocument()
    expect(screen.getByText('测试商品20')).toBeInTheDocument()
    expect(screen.queryByText('测试商品01')).not.toBeInTheDocument()
  })

  it('搜索无结果时展示空态并可重置', async () => {
    mockCatalog()
    const user = userEvent.setup()
    renderApp(['/products'])

    await screen.findByText('香港二区 CN2 A型')
    await user.type(screen.getByLabelText('搜索商品名称'), '不存在的商品')
    await user.click(screen.getByRole('button', { name: '搜索' }))

    expect(await screen.findByText('没有找到匹配的商品')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '重置筛选' }))
    expect(await screen.findByText('香港二区 CN2 A型')).toBeInTheDocument()
  })

  it('点「立即购买」进入商品详情', async () => {
    mockCatalog()
    const user = userEvent.setup()
    const { router } = renderApp(['/products'])

    const card = (await screen.findByText('香港二区 CN2 A型')).closest('[data-slot="card"]')
    await user.click(within(card as HTMLElement).getByRole('button', { name: '立即购买' }))

    await waitFor(() => expect(router.state.location.pathname).toBe('/products/1'))
  })
})
