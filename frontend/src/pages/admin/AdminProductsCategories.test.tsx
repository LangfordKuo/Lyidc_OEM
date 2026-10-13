import { cleanup as cleanupRender, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import type {
  AdminCategory,
  AdminRole,
  ProductGroupCategoryMap,
  UpdateCategoryInput,
} from '@/api/types'
import {
  makeAdminAccount,
  makeAdminCategory,
  makeAdminProductGroup,
  makeAdminSubcategory,
  makeProductGroupCategoryMap,
} from '@/test/fixtures'
import {
  installFetchMock,
  ok,
  renderApp,
  requestBody,
  seedAdminProfile,
  seedAdminToken,
} from '@/test/harness'

// 「商品与分类」页的**分类页签**（原「分类」页，R9-B 并入）：两级树渲染、改名、改排序、
// 子分类改挂主分类，以及角色矩阵（finance / support 只读：写入口禁用 + 原因文案 + 不发无权限请求）。
// 入口 URL 为 /admin/products?tab=categories（旧地址 /admin/categories 的重定向见 AdminProducts.test.tsx）。

/** 可变分类树：PUT 之后就地改写，再由 GET 回读，用于断言「刷新后的展示」。 */
function seedCategoryTree(): AdminCategory[] {
  return [
    makeAdminCategory({
      id: 101,
      name: '香港',
      sort: 0,
      subcategories: [
        makeAdminSubcategory({
          id: 201,
          name: '香港二区',
          sort: 0,
          products: { total: 6, on: 3, off: 3 },
        }),
      ],
    }),
    makeAdminCategory({
      id: 102,
      name: '美国',
      sort: 1,
      subcategories: [
        makeAdminSubcategory({
          id: 202,
          name: '美国一区',
          sort: 0,
          products: { total: 2, on: 1, off: 1 },
        }),
      ],
    }),
  ]
}

/** 上游分组（R12「上游分组映射」区块用）：名字刻意与分类名不同，避免文本查询歧义。 */
const MOCK_GROUPS = [
  makeAdminProductGroup({ id: 1, upstream_group_id: 9, name: '二区 CN2标准性能' }),
  makeAdminProductGroup({ id: 2, upstream_group_id: 12, name: '一区 Kurun', sort: 1 }),
]

function mockCategories(role: AdminRole = 'admin') {
  const tree = seedCategoryTree()
  // 初始映射：分组 12 → 子分类「美国一区」(202)；分组 9 未映射。
  const maps: ProductGroupCategoryMap[] = [
    makeProductGroupCategoryMap({
      upstream_group_id: 12,
      upstream_group_name: '一区 Kurun',
      category_id: 202,
      category_name: '美国一区',
      category_parent_name: '美国',
    }),
  ]
  const fetchMock = installFetchMock((url, init) => {
    const path = url.pathname
    if (path === '/api/v1/admin/profile') {
      return ok(makeAdminAccount({ role, username: role }))
    }
    if (path === '/api/v1/admin/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    if (path === '/api/v1/admin/categories' && init.method !== 'PUT') {
      return ok({ items: tree })
    }
    if (path === '/api/v1/admin/product-groups') {
      return ok({ items: MOCK_GROUPS })
    }
    if (path === '/api/v1/admin/product-group-maps' && init.method === 'GET') {
      return ok({ items: maps })
    }
    const mapPut = path.match(/^\/api\/v1\/admin\/product-group-maps\/(\d+)$/)
    if (mapPut && init.method === 'PUT') {
      const upstreamGroupID = Number(mapPut[1])
      const body = requestBody<{ category_id: number }>(init) ?? { category_id: 0 }
      const view = makeProductGroupCategoryMap({
        upstream_group_id: upstreamGroupID,
        category_id: body.category_id,
        category_name: body.category_id === 201 ? '香港二区' : '美国一区',
        category_parent_name: body.category_id === 201 ? '香港' : '美国',
      })
      const index = maps.findIndex((item) => item.upstream_group_id === upstreamGroupID)
      if (index >= 0) {
        maps[index] = view
      } else {
        maps.push(view)
      }
      return ok(view)
    }
    if (mapPut && init.method === 'DELETE') {
      const upstreamGroupID = Number(mapPut[1])
      const index = maps.findIndex((item) => item.upstream_group_id === upstreamGroupID)
      if (index < 0) {
        return { status: 404, body: { code: 404, message: '该分组没有映射', data: null } }
      }
      maps.splice(index, 1)
      return ok(null)
    }
    const putMatch = path.match(/^\/api\/v1\/admin\/categories\/(\d+)$/)
    if (putMatch && init.method === 'PUT') {
      const id = Number(putMatch[1])
      const body = requestBody<UpdateCategoryInput>(init) ?? {}
      for (const main of tree) {
        if (main.id === id) {
          if (body.name !== undefined) main.name = body.name
          if (body.sort !== undefined) main.sort = body.sort
          return ok(main)
        }
        for (const sub of main.subcategories) {
          if (sub.id !== id) {
            continue
          }
          if (body.name !== undefined) sub.name = body.name
          if (body.sort !== undefined) sub.sort = body.sort
          if (body.parent_id !== undefined && body.parent_id !== main.id) {
            // 改挂：从原主分类摘下，挂到目标主分类。
            main.subcategories = main.subcategories.filter((item) => item.id !== id)
            const target = tree.find((item) => item.id === body.parent_id)
            target?.subcategories.push(sub)
          }
          return ok(sub)
        }
      }
      return undefined
    }
    return undefined
  })
  return { fetchMock, tree, maps }
}

async function renderCategoriesTab(role: AdminRole = 'admin') {
  const mocks = mockCategories(role)
  seedAdminToken()
  seedAdminProfile({ role, username: role })
  renderApp(['/admin/products?tab=categories'])
  await screen.findByRole('heading', { name: '商品与分类' })
  await screen.findByText('香港二区')
  return mocks
}

describe('管理后台 · 商品与分类 · 分类页签', () => {
  it('分类树渲染两级结构、排序与商品计数', async () => {
    await renderCategoriesTab()

    // 主分类：名称 + 排序 + 子分类数
    expect(screen.getByText('香港')).toBeInTheDocument()
    expect(screen.getByText('排序 0 · 子分类 1 个')).toBeInTheDocument()
    expect(screen.getByText('美国')).toBeInTheDocument()
    expect(screen.getByText('排序 1 · 子分类 1 个')).toBeInTheDocument()
    // 子分类：名称 + 排序 + 商品计数（total / on / off）
    expect(screen.getByText('香港二区')).toBeInTheDocument()
    expect(screen.getByText('排序 0 · 商品 6 个（已上架 3 / 已下架 3）')).toBeInTheDocument()
    expect(screen.getByText('排序 0 · 商品 2 个（已上架 1 / 已下架 1）')).toBeInTheDocument()
    // 主分类行带「主分类」标记（两个主分类各一个）
    expect(screen.getAllByText('主分类')).toHaveLength(2)
  })

  it('改名：提交 name 字段并按刷新后的分类树展示', async () => {
    const { fetchMock } = await renderCategoriesTab()
    const user = userEvent.setup()

    // 第一个「编辑」是主分类「香港」
    await user.click(screen.getAllByRole('button', { name: '编辑' })[0])
    await user.clear(screen.getByLabelText('分类名称'))
    await user.type(screen.getByLabelText('分类名称'), '香港云')
    await user.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/api/v1/admin/categories/101') && init?.method === 'PUT',
      )
      expect(call).toBeTruthy()
      expect(requestBody(call?.[1] as RequestInit)).toEqual({ name: '香港云' })
    })
    // 刷新后的展示（PUT 就地改写了分类树，页面重新拉取）
    expect(await screen.findByText('香港云')).toBeInTheDocument()
    expect(screen.queryByText('香港')).not.toBeInTheDocument()
  })

  it('改排序：提交 sort 字段并刷新展示', async () => {
    const { fetchMock } = await renderCategoriesTab()
    const user = userEvent.setup()

    // 第二个「编辑」是子分类「香港二区」
    await user.click(screen.getAllByRole('button', { name: '编辑' })[1])
    await user.clear(screen.getByLabelText('排序'))
    await user.type(screen.getByLabelText('排序'), '5')
    await user.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/api/v1/admin/categories/201') && init?.method === 'PUT',
      )
      expect(call).toBeTruthy()
      expect(requestBody(call?.[1] as RequestInit)).toEqual({ sort: 5 })
    })
    expect(await screen.findByText('排序 5 · 商品 6 个（已上架 3 / 已下架 3）')).toBeInTheDocument()
  })

  it('子分类改挂主分类：提交 parent_id 并把子分类挪到目标主分类下', async () => {
    const { fetchMock } = await renderCategoriesTab()
    const user = userEvent.setup()

    await user.click(screen.getAllByRole('button', { name: '编辑' })[1])
    await user.click(screen.getByRole('combobox', { name: '所属主分类' }))
    await user.click(await screen.findByRole('option', { name: '美国' }))
    await user.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/api/v1/admin/categories/201') && init?.method === 'PUT',
      )
      expect(call).toBeTruthy()
      expect(requestBody(call?.[1] as RequestInit)).toEqual({ parent_id: 102 })
    })
    // 刷新后：香港下子分类清空，美国下 2 个（含改挂过来的「香港二区」）
    expect(await screen.findByText('排序 0 · 子分类 0 个')).toBeInTheDocument()
    expect(screen.getByText('排序 1 · 子分类 2 个')).toBeInTheDocument()
    expect(
      within(screen.getByTestId('category-102')).getByText('香港二区'),
    ).toBeInTheDocument()
  })

  it('无变化时不发请求；名称为空给出校验错误', async () => {
    const { fetchMock } = await renderCategoriesTab()
    const user = userEvent.setup()

    await user.click(screen.getAllByRole('button', { name: '编辑' })[0])
    await user.click(screen.getByRole('button', { name: '保存' }))
    expect(
      await screen.findByText('名称、排序与所属主分类均未变化，无需保存'),
    ).toBeInTheDocument()

    await user.clear(screen.getByLabelText('分类名称'))
    await user.type(screen.getByLabelText('分类名称'), '   ')
    await user.click(screen.getByRole('button', { name: '保存' }))
    // 行内校验与提交错误都会给出同一句提示
    expect((await screen.findAllByText('请输入分类名称')).length).toBeGreaterThan(0)

    expect(
      fetchMock.mock.calls.some(
        ([, init]) => init?.method === 'PUT',
      ),
    ).toBe(false)
  })

  it('finance 只读：编辑按钮禁用 + 原因文案，且不发写请求', async () => {
    const { fetchMock } = await renderCategoriesTab('finance')

    expect(screen.getByText(/当前角色（财务）为只读视角/)).toBeInTheDocument()
    const editButtons = screen.getAllByRole('button', { name: '编辑' })
    expect(editButtons.length).toBeGreaterThan(0)
    for (const button of editButtons) {
      expect(button).toBeDisabled()
      expect(button).toHaveAttribute('title', '仅超级管理员可执行')
    }
    expect(
      fetchMock.mock.calls.some(([, init]) => init?.method === 'PUT'),
    ).toBe(false)
  })

  it('support 只读：编辑按钮禁用且不发写请求', async () => {
    const { fetchMock } = await renderCategoriesTab('support')

    const editButtons = screen.getAllByRole('button', { name: '编辑' })
    for (const button of editButtons) {
      expect(button).toBeDisabled()
    }
    expect(
      fetchMock.mock.calls.some(([, init]) => init?.method === 'PUT'),
    ).toBe(false)
  })

  it('admin 可写：编辑按钮可用', async () => {
    await renderCategoriesTab('admin')

    const editButtons = screen.getAllByRole('button', { name: '编辑' })
    for (const button of editButtons) {
      expect(button).toBeEnabled()
    }
    expect(screen.queryByText(/为只读视角/)).not.toBeInTheDocument()
  })

  // —— 上游分组映射（R12） ——

  it('上游分组映射：已映射显示目标子分类，未映射显示「未映射（按规则自动归类）」', async () => {
    await renderCategoriesTab()

    expect(screen.getByTestId('group-map-9')).toHaveTextContent('二区 CN2标准性能')
    expect(screen.getByTestId('group-map-9')).toHaveTextContent('未映射（按规则自动归类）')
    expect(screen.getByTestId('group-map-12')).toHaveTextContent('美国 / 美国一区')
  })

  it('上游分组映射：下拉只列已有子分类（两级展示，不含主分类）', async () => {
    await renderCategoriesTab()
    const user = userEvent.setup()

    await user.click(screen.getByRole('combobox', { name: '映射目标 二区 CN2标准性能' }))
    expect(await screen.findByRole('option', { name: '香港 / 香港二区' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: '美国 / 美国一区' })).toBeInTheDocument()
    // 只有两个子分类；主分类「香港」「美国」不在下拉里。
    expect(screen.getAllByRole('option')).toHaveLength(2)
  })

  it('上游分组映射：选择子分类后保存，提交 category_id 并刷新展示', async () => {
    const { fetchMock } = await renderCategoriesTab()
    const user = userEvent.setup()

    const row = screen.getByTestId('group-map-9')
    await user.click(within(row).getByRole('combobox'))
    await user.click(await screen.findByRole('option', { name: '香港 / 香港二区' }))
    await user.click(within(row).getByRole('button', { name: '保存' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/api/v1/admin/product-group-maps/9') && init?.method === 'PUT',
      )
      expect(call).toBeTruthy()
      expect(requestBody(call?.[1] as RequestInit)).toEqual({ category_id: 201 })
    })
    await waitFor(() => {
      expect(screen.getByTestId('group-map-9')).toHaveTextContent('香港 / 香港二区')
    })
  })

  it('上游分组映射：删除已映射分组，调用 DELETE 并回落为未映射', async () => {
    const { fetchMock } = await renderCategoriesTab()
    const user = userEvent.setup()

    await user.click(within(screen.getByTestId('group-map-12')).getByRole('button', { name: '删除' }))

    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(
          ([input, init]) =>
            String(input).includes('/api/v1/admin/product-group-maps/12') &&
            init?.method === 'DELETE',
        ),
      ).toBe(true)
    })
    await waitFor(() => {
      expect(screen.getByTestId('group-map-12')).toHaveTextContent('未映射（按规则自动归类）')
    })
  })

  it('上游分组映射：finance / support 只读（写入口禁用 + 原因，且不发写请求）', async () => {
    for (const role of ['finance', 'support'] as const) {
      const { fetchMock } = await renderCategoriesTab(role)
      const row = screen.getByTestId('group-map-12')
      const save = within(row).getByRole('button', { name: '保存' })
      const remove = within(row).getByRole('button', { name: '删除' })
      expect(save).toBeDisabled()
      expect(remove).toBeDisabled()
      expect(save).toHaveAttribute('title', '仅超级管理员可执行')
      expect(within(row).getByRole('combobox')).toBeDisabled()
      expect(
        fetchMock.mock.calls.some(
          ([, init]) => init?.method === 'PUT' || init?.method === 'DELETE',
        ),
      ).toBe(false)
      cleanupRender()
    }
  })
})
