import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import type { AdminProduct, ImportProductsInput } from '@/api/types'
import {
  makeAdminAccount,
  makeAdminCategory,
  makeAdminProduct,
  makeAdminProductGroup,
} from '@/test/fixtures'
import {
  installFetchMock,
  ok,
  renderApp,
  requestBody,
  seedAdminProfile,
  seedAdminToken,
  type FetchHandler,
} from '@/test/harness'

// 管理后台「商品与分类」页的**商品页签**：定价弹窗（三模式参数）、上下架二次确认、分类列与改分类、
// 上游导入弹窗（R12：分组多选 + 请求体口径）、角色矩阵（support 只读）；
// 另有页签 URL 与旧地址 /admin/categories 重定向。
// 分类页签自身的用例见 AdminProductsCategories.test.tsx。

/** 两个上游分组（R12 导入弹窗多选用）。 */
const IMPORT_GROUPS = [
  makeAdminProductGroup({ id: 1, upstream_group_id: 9, name: '香港二区' }),
  makeAdminProductGroup({ id: 2, upstream_group_id: 12, name: '美国一区', sort: 1 }),
]

function mockProducts(
  handler?: FetchHandler,
  role: 'admin' | 'support' = 'admin',
  products: AdminProduct[] = [makeAdminProduct({ id: 1, name: '香港二区 CN2 A型', status: 'on' })],
) {
  return installFetchMock((url, init) => {
    const path = url.pathname
    if (path === '/api/v1/admin/profile') {
      return ok(makeAdminAccount({ role, username: role }))
    }
    if (path === '/api/v1/admin/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    if (path === '/api/v1/admin/products') {
      return ok({ items: products, page: 1, page_size: 20, total: products.length })
    }
    if (path === '/api/v1/admin/product-groups') {
      return ok({ items: IMPORT_GROUPS })
    }
    if (path === '/api/v1/admin/product-group-maps') {
      return ok({ items: [] })
    }
    if (path === '/api/v1/admin/categories') {
      return ok({ items: [makeAdminCategory()] })
    }
    return handler?.(url, init)
  })
}

async function renderProducts() {
  seedAdminToken()
  seedAdminProfile()
  renderApp(['/admin/products'])
  await screen.findByRole('heading', { name: '商品与分类' })
  await screen.findByText('香港二区 CN2 A型')
}

describe('管理后台 · 商品管理', () => {
  it('定价弹窗展示六周期价格预览，按加价率保存时提交 pricing_json（markup）', async () => {
    const fetchMock = mockProducts((url, init) => {
      if (url.pathname === '/api/v1/admin/products/1' && init.method === 'PUT') {
        return ok(makeAdminProduct())
      }
      return undefined
    })
    const user = userEvent.setup()
    await renderProducts()

    await user.click(screen.getByRole('button', { name: '定价设置' }))

    // 六周期预览（月付 / 季付 / 半年付 / 年付 / 两年付 / 三年付）
    const preview = within(await screen.findByTestId('cycle-price-preview'))
    expect(preview.getByText('月付')).toBeInTheDocument()
    expect(preview.getByText('季付')).toBeInTheDocument()
    expect(preview.getByText('半年付')).toBeInTheDocument()
    expect(preview.getByText('年付')).toBeInTheDocument()
    expect(preview.getByText('两年付')).toBeInTheDocument()
    expect(preview.getByText('三年付')).toBeInTheDocument()
    expect(preview.getByText('¥22.00')).toBeInTheDocument()
    // costs 22.00/66.00/132.00/220.00 + 两个「不可售」
    expect(preview.getAllByText('不可售')).toHaveLength(2)

    // 加价率模式：不填直接保存 → 校验错误
    await user.click(screen.getByRole('radio', { name: '按上游价加价' }))
    await user.click(screen.getByRole('button', { name: '保存定价' }))
    expect(await screen.findByText('请输入加价率')).toBeInTheDocument()
    expect(
      fetchMock.mock.calls.some(
        ([input, init]) => String(input).includes('/admin/products/1') && init?.method === 'PUT',
      ),
    ).toBe(false)

    // 填写 10 → 保存 → 请求体为 markup 规则
    await user.type(screen.getByLabelText('加价率（%）'), '10')
    await user.click(screen.getByRole('button', { name: '保存定价' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/api/v1/admin/products/1') && init?.method === 'PUT',
      )
      expect(call).toBeTruthy()
      expect(requestBody(call?.[1] as RequestInit)).toEqual({
        pricing_json: { mode: 'markup', markup_percent: 10 },
      })
    })
  })

  it('固定价模式：六个周期全留空时拦截提交', async () => {
    const fetchMock = mockProducts()
    const user = userEvent.setup()
    await renderProducts()

    await user.click(screen.getByRole('button', { name: '定价设置' }))
    await user.click(screen.getByRole('radio', { name: '固定价覆盖' }))
    await user.click(screen.getByRole('button', { name: '保存定价' }))

    expect(await screen.findByText('固定价模式至少填写一个周期的金额')).toBeInTheDocument()
    expect(
      fetchMock.mock.calls.some(
        ([, init]) => init?.method === 'PUT',
      ),
    ).toBe(false)
  })

  it('上下架：二次确认后提交 status 参数并刷新列表', async () => {
    const fetchMock = mockProducts((url, init) => {
      if (url.pathname === '/api/v1/admin/products/1' && init.method === 'PUT') {
        return ok(makeAdminProduct({ status: 'off' }))
      }
      return undefined
    })
    const user = userEvent.setup()
    await renderProducts()

    await user.click(screen.getByRole('button', { name: '下架' }))

    // 二次确认弹窗：文案说明影响范围
    expect(await screen.findByText('确认下架商品')).toBeInTheDocument()
    expect(screen.getByText(/下架后会员端不可见、不可下单/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '确认下架' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/api/v1/admin/products/1') && init?.method === 'PUT',
      )
      expect(call).toBeTruthy()
      expect(requestBody(call?.[1] as RequestInit)).toEqual({ status: 'off' })
    })
  })

  it('分类列：已归类展示子分类与主分类，未归类显示占位', async () => {
    mockProducts(
      undefined,
      'admin',
      [
        makeAdminProduct({ id: 1, name: '香港二区 CN2 A型', status: 'on' }),
        makeAdminProduct({
          id: 2,
          name: '美国一区 特价',
          category_id: 0,
          category_name: '',
          category_parent: { id: 0, name: '' },
        }),
      ],
    )
    await renderProducts()
    await screen.findByText('美国一区 特价')

    const table = within(screen.getByRole('table', { name: '商品列表' }))
    // 已归类：主分类 / 子分类（子分类名可能与分组名同名，合并展示避免歧义）
    expect(table.getByText('香港 / 香港二区')).toBeInTheDocument()
    // 未归类：占位
    expect(table.getByText('未归类')).toBeInTheDocument()
  })

  it('改分类：弹窗提交 category_id 并提示导入不再覆盖', async () => {
    const fetchMock = mockProducts((url, init) => {
      if (url.pathname === '/api/v1/admin/products/1' && init.method === 'PUT') {
        return ok(
          makeAdminProduct({
            category_id: 0,
            category_name: '',
            category_parent: { id: 0, name: '' },
          }),
        )
      }
      return undefined
    })
    const user = userEvent.setup()
    await renderProducts()

    await user.click(screen.getByRole('button', { name: '改分类' }))
    expect(await screen.findByRole('heading', { name: '改分类' })).toBeInTheDocument()
    await user.click(screen.getByRole('combobox', { name: '目标子分类' }))
    await user.click(await screen.findByRole('option', { name: '未归类' }))
    await user.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/api/v1/admin/products/1') && init?.method === 'PUT',
      )
      expect(call).toBeTruthy()
      expect(requestBody(call?.[1] as RequestInit)).toEqual({ category_id: 0 })
    })
    expect(await screen.findByText('已改挂，后续导入不再覆盖该商品分类')).toBeInTheDocument()
  })

  it('分类筛选：按 category_id 重新请求（0 = 只看未归类）', async () => {
    const fetchMock = mockProducts()
    const user = userEvent.setup()
    await renderProducts()

    await user.click(screen.getByRole('combobox', { name: '分类' }))
    await user.click(await screen.findByRole('option', { name: '香港 / 香港二区' }))
    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(([input]) => String(input).includes('category_id=201')),
      ).toBe(true)
    })

    await user.click(screen.getByRole('combobox', { name: '分类' }))
    await user.click(await screen.findByRole('option', { name: '未归类' }))
    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(([input]) => String(input).includes('category_id=0')),
      ).toBe(true)
    })
  })

  it('客服角色只读：定价与上下架按钮禁用且不发写请求', async () => {
    const fetchMock = mockProducts(undefined, 'support')
    seedAdminToken()
    seedAdminProfile({ role: 'support', username: 'cs6a', nickname: '客服' })
    renderApp(['/admin/products'])
    await screen.findByText('香港二区 CN2 A型')

    const pricing = screen.getByRole('button', { name: '定价设置' })
    const offline = screen.getByRole('button', { name: '下架' })
    expect(pricing).toBeDisabled()
    expect(offline).toBeDisabled()
    expect(screen.getByRole('button', { name: '从上游导入商品' })).toBeDisabled()
    expect(
      fetchMock.mock.calls.some(
        ([, init]) => init?.method === 'PUT' || init?.method === 'POST',
      ),
    ).toBe(false)
  })

  it('页签切换：分类页签写入 ?tab=categories，切回商品恢复干净 URL', async () => {
    mockProducts()
    seedAdminToken()
    seedAdminProfile()
    const { router } = renderApp(['/admin/products'])
    await screen.findByRole('heading', { name: '商品与分类' })
    await screen.findByText('香港二区 CN2 A型')
    const user = userEvent.setup()

    await user.click(screen.getByRole('tab', { name: '分类' }))
    expect(router.state.location.pathname).toBe('/admin/products')
    expect(router.state.location.search).toBe('?tab=categories')
    // 分类分区内容挂载，商品表格卸载
    expect(await screen.findByText('排序 0 · 子分类 1 个')).toBeInTheDocument()
    expect(screen.queryByRole('table', { name: '商品列表' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('tab', { name: '商品' }))
    expect(router.state.location.search).toBe('')
    expect(await screen.findByText('香港二区 CN2 A型')).toBeInTheDocument()
    expect(screen.queryByText('排序 0 · 子分类 1 个')).not.toBeInTheDocument()
  })

  it('旧地址 /admin/categories 重定向到商品页的分类分区', async () => {
    mockProducts()
    seedAdminToken()
    seedAdminProfile()
    const { router } = renderApp(['/admin/categories'])
    await screen.findByRole('heading', { name: '商品与分类' })

    expect(router.state.location.pathname).toBe('/admin/products')
    expect(router.state.location.search).toBe('?tab=categories')
    // 落在分类分区：分类树在、商品表格不在
    expect(await screen.findByText('排序 0 · 子分类 1 个')).toBeInTheDocument()
    expect(screen.queryByRole('table', { name: '商品列表' })).not.toBeInTheDocument()
  })

  // —— 上游导入弹窗（R12） ——

  it('导入弹窗：默认全选，提交时省略 upstream_group_ids（缺省=全量导入）', async () => {
    const fetchMock = mockProducts((url, init) => {
      if (url.pathname === '/api/v1/admin/products/import' && init.method === 'POST') {
        return ok({ created: 3, updated: 0, unchanged: 0, groups: 2, failed: 0 })
      }
      return undefined
    })
    const user = userEvent.setup()
    await renderProducts()

    await user.click(screen.getByRole('button', { name: '从上游导入商品' }))
    expect(await screen.findByRole('heading', { name: '从上游导入商品' })).toBeInTheDocument()

    // 两个分组默认全选。
    const boxes = screen.getAllByRole('checkbox')
    expect(boxes).toHaveLength(2)
    for (const box of boxes) {
      expect(box).toBeChecked()
    }

    await user.click(screen.getByRole('button', { name: '开始导入' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/api/v1/admin/products/import') && init?.method === 'POST',
      )
      expect(call).toBeTruthy()
      // 全选 = 全量导入：请求体省略（不传 upstream_group_ids）。
      expect(requestBody(call?.[1] as RequestInit)).toBeNull()
    })
    expect(await screen.findByText('上游导入完成')).toBeInTheDocument()
  })

  it('导入弹窗：部分选择时提交选中的 upstream_group_ids（升序）', async () => {
    const fetchMock = mockProducts((url, init) => {
      if (url.pathname === '/api/v1/admin/products/import' && init.method === 'POST') {
        return ok({
          created: 2,
          updated: 0,
          unchanged: 0,
          groups: 1,
          failed: 0,
          selected_groups: [12],
        })
      }
      return undefined
    })
    const user = userEvent.setup()
    await renderProducts()

    await user.click(screen.getByRole('button', { name: '从上游导入商品' }))
    await screen.findByRole('heading', { name: '从上游导入商品' })

    // 取消「香港二区」，只保留「美国一区」（upstream_group_id=12）。
    await user.click(screen.getByRole('checkbox', { name: /香港二区/ }))
    expect(screen.getByRole('checkbox', { name: /香港二区/ })).not.toBeChecked()
    await user.click(screen.getByRole('button', { name: '开始导入' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/api/v1/admin/products/import') && init?.method === 'POST',
      )
      expect(call).toBeTruthy()
      expect(requestBody<ImportProductsInput>(call?.[1] as RequestInit)).toEqual({
        upstream_group_ids: [12],
      })
    })
    expect(await screen.findByText(/本次只导入了选中的 1 个分组/)).toBeInTheDocument()
  })

  it('导入弹窗：一个都不选时禁用确认并提示原因，不发请求', async () => {
    const fetchMock = mockProducts()
    const user = userEvent.setup()
    await renderProducts()

    await user.click(screen.getByRole('button', { name: '从上游导入商品' }))
    await screen.findByRole('heading', { name: '从上游导入商品' })

    await user.click(screen.getByRole('button', { name: '全不选' }))
    expect(screen.getByRole('button', { name: '开始导入' })).toBeDisabled()
    expect(
      screen.getByText(/请至少勾选一个上游分组/),
    ).toBeInTheDocument()

    expect(
      fetchMock.mock.calls.some(
        ([input, init]) =>
          String(input).includes('/admin/products/import') && init?.method === 'POST',
      ),
    ).toBe(false)
  })

  it('导入弹窗：映射目标失效时在结果里给出回落提示', async () => {
    mockProducts((url, init) => {
      if (url.pathname === '/api/v1/admin/products/import' && init.method === 'POST') {
        return ok({
          created: 0,
          updated: 2,
          unchanged: 0,
          groups: 1,
          failed: 0,
          selected_groups: [9],
          map_fallback_group_ids: [9],
        })
      }
      return undefined
    })
    const user = userEvent.setup()
    await renderProducts()

    await user.click(screen.getByRole('button', { name: '从上游导入商品' }))
    await screen.findByRole('heading', { name: '从上游导入商品' })
    await user.click(screen.getByRole('button', { name: '开始导入' }))

    expect(await screen.findByText(/映射目标已失效/)).toBeInTheDocument()
  })
})
