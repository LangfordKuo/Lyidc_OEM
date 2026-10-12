import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import type { AdminCategory, AdminRole, UpdateCategoryInput } from '@/api/types'
import {
  makeAdminAccount,
  makeAdminCategory,
  makeAdminSubcategory,
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

function mockCategories(role: AdminRole = 'admin') {
  const tree = seedCategoryTree()
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
  return { fetchMock, tree }
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
})
