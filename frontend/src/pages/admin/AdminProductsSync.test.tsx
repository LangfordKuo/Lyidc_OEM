import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import type { AdminProduct, ProductSyncResult, SyncProductsInput } from '@/api/types'
import { makeAdminCategory, makeAdminProduct, makeAdminProductGroup } from '@/test/fixtures'
import {
  fail,
  installFetchMock,
  ok,
  renderApp,
  requestBody,
  seedAdminProfile,
  seedAdminToken,
  type FetchHandler,
} from '@/test/harness'

// R20 商品「同步」（契约 10.7）：商品列表的行多选 + 「同步」按钮 + 二次确认弹窗 +
// 逐条结果与汇总（成功 N / 失败 M + 失败原因）。
// 与「导入」是两个入口：同步不塞进导入弹窗，按钮文案也不是导入那一套。

const PRODUCTS: AdminProduct[] = [
  makeAdminProduct({ id: 1, name: '香港二区 CN2 A型' }),
  makeAdminProduct({ id: 2, name: '香港二区 CN2 B型', upstream_pid: 502 }),
  makeAdminProduct({ id: 3, name: '美国一区 特价', upstream_pid: 503, category_id: 0, category_name: '', category_parent: { id: 0, name: '' } }),
]

/** 同步结果夹具（契约 10.7：total = succeeded + failed）。 */
function makeSyncResult(overrides: Partial<ProductSyncResult> = {}): ProductSyncResult {
  return {
    total: 2,
    succeeded: 1,
    failed: 1,
    updated: 1,
    unchanged: 0,
    items: [
      { product_id: 1, upstream_pid: 501, name: '香港二区 CN2 A型', status: 'updated' },
      { product_id: 2, upstream_pid: 502, name: '香港二区 CN2 B型', status: 'failed', reason: '上游已不存在该商品（可能已下架或删除）' },
    ],
    ...overrides,
  }
}

function mockProducts(handler?: FetchHandler, role: 'admin' | 'finance' | 'support' = 'admin') {
  return installFetchMock((url, init) => {
    const path = url.pathname
    if (path === '/api/v1/admin/profile') {
      return ok({
        id: 1,
        username: role,
        nickname: '管理员',
        role,
        status: 'active',
        created_at: '2026-10-08T06:00:00Z',
        updated_at: '2026-10-08T06:00:00Z',
        last_login_at: '2026-10-08T06:20:00Z',
      })
    }
    if (path === '/api/v1/admin/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    if (path === '/api/v1/admin/products') {
      return ok({ items: PRODUCTS, page: 1, page_size: 20, total: PRODUCTS.length })
    }
    if (path === '/api/v1/admin/product-groups') {
      return ok({ items: [makeAdminProductGroup()] })
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

async function renderProducts(role: 'admin' | 'finance' | 'support' = 'admin') {
  seedAdminToken()
  seedAdminProfile({ role, username: role })
  renderApp(['/admin/products'])
  await screen.findByRole('heading', { name: '商品与分类' })
  await screen.findByText('香港二区 CN2 A型')
}

describe('管理后台 · 商品同步（R20）', () => {
  it('未勾选时「同步」按钮禁用且不发请求；勾选后可点开确认弹窗并提交选中的商品 ID', async () => {
    const fetchMock = mockProducts((url, init) => {
      if (url.pathname === '/api/v1/admin/products/sync' && init.method === 'POST') {
        return ok(makeSyncResult())
      }
      return undefined
    })
    const user = userEvent.setup()
    await renderProducts()

    const syncButton = screen.getByRole('button', { name: '同步' })
    expect(syncButton).toBeDisabled()
    expect(
      fetchMock.mock.calls.some(([input, init]) =>
        String(input).includes('/admin/products/sync') && init?.method === 'POST',
      ),
    ).toBe(false)

    // 勾选两行（A 与 B）→ 按钮启用、显示已选数。
    await user.click(screen.getByRole('checkbox', { name: '选择商品 香港二区 CN2 A型' }))
    await user.click(screen.getByRole('checkbox', { name: '选择商品 香港二区 CN2 B型' }))
    expect(screen.getByText('已选 2 个商品')).toBeInTheDocument()
    expect(syncButton).toBeEnabled()

    await user.click(syncButton)
    // 二次确认：写明语义边界（只更新已存在的商品，不新增、不删除）。
    expect(await screen.findByRole('heading', { name: '同步所选商品' })).toBeInTheDocument()
    expect(screen.getByText(/只更新已存在的商品，不新增、不删除/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '开始同步' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/api/v1/admin/products/sync') && init?.method === 'POST',
      )
      expect(call).toBeTruthy()
      expect(requestBody<SyncProductsInput>(call?.[1] as RequestInit)).toEqual({
        product_ids: [1, 2],
      })
    })
  })

  it('结果区展示逐条结果与汇总：成功/更新/未变计数 + 失败商品与可读原因；成功后刷新列表', async () => {
    const fetchMock = mockProducts((url, init) => {
      if (url.pathname === '/api/v1/admin/products/sync' && init.method === 'POST') {
        return ok(
          makeSyncResult({
            total: 3,
            succeeded: 2,
            failed: 1,
            updated: 1,
            unchanged: 1,
            items: [
              { product_id: 1, upstream_pid: 501, name: '香港二区 CN2 A型', status: 'updated' },
              { product_id: 2, upstream_pid: 502, name: '香港二区 CN2 B型', status: 'unchanged' },
              {
                product_id: 3,
                upstream_pid: 503,
                name: '美国一区 特价',
                status: 'failed',
                reason: '上游商品详情拉取失败：上游请求超时',
              },
            ],
          }),
        )
      }
      return undefined
    })
    const user = userEvent.setup()
    await renderProducts()

    const listCallsBefore = fetchMock.mock.calls.filter(([input]) =>
      String(input).includes('/api/v1/admin/products?'),
    ).length

    await user.click(screen.getByRole('checkbox', { name: '全选本页商品' }))
    expect(screen.getByText('已选 3 个商品')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '同步' }))
    await screen.findByRole('heading', { name: '同步所选商品' })
    await user.click(screen.getByRole('button', { name: '开始同步' }))

    const result = within(await screen.findByTestId('product-sync-result'))
    expect(result.getByText(/成功 2 条（更新 1 条、无变化 1 条）/)).toBeInTheDocument()
    expect(result.getByText(/失败 1 条/)).toBeInTheDocument()
    // 失败项要能看清是哪个商品 + 原因。
    expect(result.getByText('美国一区 特价')).toBeInTheDocument()
    expect(result.getByText('#3')).toBeInTheDocument()
    expect(result.getByText('上游商品详情拉取失败：上游请求超时')).toBeInTheDocument()

    // 列表重新拉取（后端已失效缓存，前端照常重拉即可）。
    await waitFor(() => {
      const listCallsAfter = fetchMock.mock.calls.filter(([input]) =>
        String(input).includes('/api/v1/admin/products?'),
      ).length
      expect(listCallsAfter).toBeGreaterThan(listCallsBefore)
    })
  })

  it('失败项全部失败时不谎报成功，也不再刷新列表', async () => {
    const fetchMock = mockProducts((url, init) => {
      if (url.pathname === '/api/v1/admin/products/sync' && init.method === 'POST') {
        return ok(
          makeSyncResult({
            total: 1,
            succeeded: 0,
            failed: 1,
            updated: 0,
            unchanged: 0,
            items: [
              { product_id: 1, upstream_pid: 0, name: '香港二区 CN2 A型', status: 'failed', reason: '商品不存在' },
            ],
          }),
        )
      }
      return undefined
    })
    const user = userEvent.setup()
    await renderProducts()

    const listCallsBefore = fetchMock.mock.calls.filter(([input]) =>
      String(input).includes('/api/v1/admin/products?'),
    ).length
    await user.click(screen.getByRole('checkbox', { name: '选择商品 香港二区 CN2 A型' }))
    await user.click(screen.getByRole('button', { name: '同步' }))
    await screen.findByRole('heading', { name: '同步所选商品' })
    await user.click(screen.getByRole('button', { name: '开始同步' }))

    const result = within(await screen.findByTestId('product-sync-result'))
    expect(result.getByText(/成功 0 条/)).toBeInTheDocument()
    expect(result.getByText('商品不存在')).toBeInTheDocument()

    // 全部失败 = 数据没有变化：不触发列表重拉。
    const listCallsAfter = fetchMock.mock.calls.filter(([input]) =>
      String(input).includes('/api/v1/admin/products?'),
    ).length
    expect(listCallsAfter).toBe(listCallsBefore)
  })

  it('整次失败（后端 500，如上游未配置）展示后端 message，不显示结果汇总', async () => {
    mockProducts((url, init) => {
      if (url.pathname === '/api/v1/admin/products/sync' && init.method === 'POST') {
        return fail(500, '上游未配置，无法同步商品（请在后台设置中填写上游参数）', 500)
      }
      return undefined
    })
    const user = userEvent.setup()
    await renderProducts()

    await user.click(screen.getByRole('checkbox', { name: '选择商品 香港二区 CN2 A型' }))
    await user.click(screen.getByRole('button', { name: '同步' }))
    await screen.findByRole('heading', { name: '同步所选商品' })
    await user.click(screen.getByRole('button', { name: '开始同步' }))

    expect(await screen.findByText('同步失败')).toBeInTheDocument()
    expect(
      screen.getByText('上游未配置，无法同步商品（请在后台设置中填写上游参数）'),
    ).toBeInTheDocument()
    expect(screen.queryByTestId('product-sync-result')).not.toBeInTheDocument()
  })

  it('翻页 / 改筛选后清空勾选，避免「已选计数」与提交条数不一致', async () => {
    mockProducts()
    const user = userEvent.setup()
    await renderProducts()

    await user.click(screen.getByRole('checkbox', { name: '选择商品 香港二区 CN2 A型' }))
    expect(screen.getByText('已选 1 个商品')).toBeInTheDocument()

    // 改筛选（按状态）：勾选清空、按钮回到禁用。
    await user.click(screen.getByRole('button', { name: '已上架' }))
    await waitFor(() => {
      expect(screen.queryByText('已选 1 个商品')).not.toBeInTheDocument()
    })
    expect(screen.getByRole('button', { name: '同步' })).toBeDisabled()
  })

  it.each(['finance', 'support'] as const)(
    '角色矩阵：%s 的「同步」按钮禁用并给出原因，不发同步请求',
    async (role) => {
      const fetchMock = mockProducts(undefined, role)
      await renderProducts(role)

      const button = screen.getByRole('button', { name: '同步' })
      expect(button).toBeDisabled()
      expect(button).toHaveAttribute('title', '仅超级管理员可执行')
      expect(
        fetchMock.mock.calls.some(
          ([input, init]) =>
            String(input).includes('/admin/products/sync') && init?.method === 'POST',
        ),
      ).toBe(false)
    },
  )
})
