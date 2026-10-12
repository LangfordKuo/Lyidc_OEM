import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import type { AdminProduct } from '@/api/types'
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

// 管理后台「商品」页：定价弹窗（三模式参数）、上下架二次确认、分类列与改分类、角色矩阵（support 只读）。

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
      return ok({ items: [makeAdminProductGroup()] })
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
  await screen.findByRole('heading', { name: '商品' })
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
})
