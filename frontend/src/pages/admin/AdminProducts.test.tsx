import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AdminRole } from '../../api/types'
import { renderAdmin } from '../../test/adminHarness'
import { apiGet, apiPost, fail, ok, requestBody, type ApiHandler } from '../../test/consoleHarness'

// 管理后台「商品」页（契约 10.4 / 10.5）：列表 + 上游导入 + 定价设置 + 上下架 + 分组。
// 权限矩阵是本页验收重点：support 只读（按钮在位但禁用），admin / finance 可用。

const GROUPS = [
  {
    id: 1,
    upstream_group_id: 1,
    name: '香港二区',
    sort: 0,
    products: { total: 2, on: 1, off: 1 },
    created_at: '2026-10-08T09:00:00Z',
    updated_at: '2026-10-08T09:12:03Z',
  },
  {
    id: 2,
    upstream_group_id: 2,
    name: '美国洛杉矶',
    sort: 1,
    products: { total: 0, on: 0, off: 0 },
    created_at: '2026-10-08T09:00:00Z',
    updated_at: '2026-10-08T09:12:03Z',
  },
]

/** 已上架商品：月付可售，库存受控（70）。 */
const ON_PRODUCT = {
  id: 1,
  upstream_pid: 1,
  upstream_group_id: 1,
  group_id: 1,
  group_name: '香港二区',
  name: '香港二区 CN2 A型',
  type: 'dcimcloud',
  module: 'idcsmart_common',
  status: 'on',
  sort: 0,
  stock_qty: 70,
  stock_control: 1,
  ontrial_max: 0,
  pricing: { mode: 'markup', markup_percent: 10 },
  prices: {
    monthly: '22.00',
    quarterly: '66.00',
    semiannual: '132.00',
    annual: '220.00',
    biennial: null,
    triennial: null,
  },
  created_at: '2026-10-08T09:00:00Z',
  updated_at: '2026-10-08T09:12:03Z',
}

/** 已下架商品：月付不可售（展示首个可售周期），库存不限（stock_control=0）。 */
const OFF_PRODUCT = {
  ...ON_PRODUCT,
  id: 2,
  upstream_pid: 2,
  upstream_group_id: 2,
  group_id: 2,
  group_name: '美国洛杉矶',
  name: '美国洛杉矶 B型',
  status: 'off',
  sort: 1,
  stock_qty: 0,
  stock_control: 0,
  pricing: { mode: 'upstream' },
  prices: {
    monthly: null,
    quarterly: '99.00',
    semiannual: null,
    annual: null,
    biennial: null,
    triennial: null,
  },
}

/** 导入结果：含 2 个抓取失败的商品（failed_pids 最多 20 个）。 */
const IMPORT_RESULT = {
  created: 3,
  updated: 1,
  unchanged: 155,
  groups: 29,
  failed: 2,
  failed_pids: [1024, 2048],
}

/** consoleHarness 只有 apiGet / apiPost，这里补一个 PUT 桩（按方法 + 前缀匹配）。 */
function apiPut(prefix: string, data: unknown, status = 200): ApiHandler {
  return (url, init) =>
    (init.method ?? 'GET').toUpperCase() === 'PUT' && url.startsWith(prefix)
      ? status >= 400
        ? fail(status, String(data), status)
        : ok(data)
      : undefined
}

function renderProducts(
  role: AdminRole = 'admin',
  extra: ApiHandler[] = [],
  items: unknown[] = [ON_PRODUCT, OFF_PRODUCT],
) {
  return renderAdmin('/admin/products', role, [
    ...extra,
    apiGet('/api/v1/admin/products?', { items, page: 1, page_size: 20, total: items.length }),
    apiGet('/api/v1/admin/product-groups', { items: GROUPS }),
    apiPost('/api/v1/admin/products/import', IMPORT_RESULT),
  ])
}

/** 列表接口的请求 URL（用于断言查询参数）。 */
function listUrls(): string[] {
  return (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls
    .map(([url]) => String(url))
    .filter((url) => url.startsWith('/api/v1/admin/products?'))
}

/** 是否已发出某个方法 + 前缀的请求。 */
function hasRequest(method: string, prefix: string): boolean {
  return (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls.some(
    ([url, init]) =>
      String(url).startsWith(prefix) && ((init as RequestInit)?.method ?? 'GET') === method,
  )
}

/** 等待商品列表渲染完成（第一行商品名出现）。 */
async function waitForList() {
  return screen.findByText('香港二区 CN2 A型')
}

describe('管理后台 · 商品', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('support 角色：导入 / 定价设置 / 上下架按钮均在位但禁用，并给出原因', async () => {
    renderProducts('support')
    await waitForList()

    // 顶部导入入口：禁用而非隐藏
    expect(screen.getByRole('button', { name: '从上游导入商品' })).toBeDisabled()

    // 每行的定价设置 / 上下架按钮：禁用
    const pricingButtons = screen.getAllByRole('button', { name: '定价设置' })
    expect(pricingButtons).toHaveLength(2)
    pricingButtons.forEach((button) => expect(button).toBeDisabled())
    expect(screen.getByRole('button', { name: '下架' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '上架' })).toBeDisabled()

    // 列表本身仍然可见（只读视角不隐藏整页）
    expect(screen.getByLabelText('商品列表')).toBeInTheDocument()
    // 页面与分组面板各有一处禁用原因提示（permissionHint 文案）
    expect(screen.getAllByText(/需要超级管理员或财务角色/).length).toBeGreaterThan(0)

    // 分组编辑入口同样禁用
    await userEvent.setup().click(screen.getByRole('button', { name: /展开分组/ }))
    screen.getAllByRole('button', { name: '编辑' }).forEach((button) => {
      expect(button).toBeDisabled()
    })
  })

  // 契约 10.5：admin 与 finance 同权（商品域的写操作两者都可执行）
  it.each<AdminRole>(['admin', 'finance'])('%s 角色：写操作按钮全部可用', async (role) => {
    renderProducts(role)
    await waitForList()

    expect(screen.getByRole('button', { name: '从上游导入商品' })).toBeEnabled()
    screen.getAllByRole('button', { name: '定价设置' }).forEach((button) => {
      expect(button).toBeEnabled()
    })
    expect(screen.getByRole('button', { name: '下架' })).toBeEnabled()
    expect(screen.getByRole('button', { name: '上架' })).toBeEnabled()

    await userEvent.setup().click(screen.getByRole('button', { name: /展开分组/ }))
    screen.getAllByRole('button', { name: '编辑' }).forEach((button) => {
      expect(button).toBeEnabled()
    })
  })

  it('列表展示分组、状态、本地售价（月付优先）与库存', async () => {
    renderProducts('admin')
    await waitForList()

    const table = within(screen.getByLabelText('商品列表'))
    expect(table.getByText('#1')).toBeInTheDocument()
    expect(table.getByText('香港二区')).toBeInTheDocument()
    expect(table.getByText('美国洛杉矶')).toBeInTheDocument()
    expect(table.getByText('已上架')).toBeInTheDocument()
    expect(table.getByText('已下架')).toBeInTheDocument()
    // 月付可售时展示月付价与可售周期数
    expect(table.getByText('¥22.00 / 月付')).toBeInTheDocument()
    expect(table.getByText('可售 4 个周期')).toBeInTheDocument()
    // 月付不可售时回退到首个可售周期
    expect(table.getByText('¥99.00 / 季付')).toBeInTheDocument()
    expect(table.getByText('可售 1 个周期')).toBeInTheDocument()
    // 库存：stock_control=0 → 不限
    expect(table.getByText('不限')).toBeInTheDocument()
    expect(table.getByText('70')).toBeInTheDocument()
  })

  it('没有商品时显示空态', async () => {
    renderProducts('admin', [], [])

    expect(await screen.findByText('没有符合条件的商品')).toBeInTheDocument()
    expect(screen.queryByLabelText('商品列表')).not.toBeInTheDocument()
  })

  it('状态筛选与关键词搜索会带 status / keyword 查询参数', async () => {
    const user = userEvent.setup()
    renderProducts('admin')
    await waitForList()

    await user.click(screen.getByRole('button', { name: '已上架' }))
    await waitFor(() => {
      expect(listUrls().some((url) => url.includes('status=on'))).toBe(true)
    })

    await user.type(screen.getByLabelText('关键词'), '香港{enter}')
    await waitFor(() => {
      expect(listUrls().some((url) => decodeURIComponent(url).includes('keyword=香港'))).toBe(true)
    })
  })

  it('从上游导入：二次确认后 POST /admin/products/import 并展示统计结果', async () => {
    const user = userEvent.setup()
    renderProducts('admin')
    await waitForList()

    await user.click(screen.getByRole('button', { name: '从上游导入商品' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/只读调用/)).toBeInTheDocument()
    expect(within(dialog).getByText(/耗时可能数十秒/)).toBeInTheDocument()

    // 未确认前不发请求
    expect(hasRequest('POST', '/api/v1/admin/products/import')).toBe(false)

    await user.click(within(dialog).getByRole('button', { name: '开始导入' }))
    await waitFor(() => {
      expect(hasRequest('POST', '/api/v1/admin/products/import')).toBe(true)
    })

    // 结果 Alert：created / updated / unchanged / groups 四个数字
    expect(await screen.findByText(/新建 3 个/)).toBeInTheDocument()
    expect(screen.getByText(/更新 1 个/)).toBeInTheDocument()
    expect(screen.getByText(/无变化 155 个/)).toBeInTheDocument()
    expect(screen.getByText(/分组 29 个/)).toBeInTheDocument()
    // 失败商品被跳过并列出 PID
    expect(screen.getByText(/2 个商品详情抓取失败/)).toBeInTheDocument()
    expect(screen.getByText(/1024、2048/)).toBeInTheDocument()
  })

  it('导入失败时展示后端错误信息', async () => {
    const user = userEvent.setup()
    renderProducts('admin', [
      (url, init) =>
        String(url).startsWith('/api/v1/admin/products/import') &&
        (init.method ?? 'GET') === 'POST'
          ? fail(500, '上游未配置，无法导入商品', 500)
          : undefined,
    ])
    await waitForList()

    await user.click(screen.getByRole('button', { name: '从上游导入商品' }))
    await user.click(within(await screen.findByRole('dialog')).getByRole('button', { name: '开始导入' }))

    expect(await screen.findByText('上游未配置，无法导入商品')).toBeInTheDocument()
  })

  it('定价设置：当前值回显，切到加价模式填 10 后提交 pricing_json', async () => {
    const user = userEvent.setup()
    renderProducts('admin', [
      apiPut('/api/v1/admin/products/2', { ...OFF_PRODUCT, pricing: { mode: 'markup', markup_percent: 10 } }),
    ])
    await waitForList()

    // 打开第二行（上游价模式）的定价弹窗
    await user.click(screen.getAllByRole('button', { name: '定价设置' })[1])
    const dialog = await screen.findByRole('dialog')

    // 当前定价回显：规则摘要 + 六周期售价（不可售周期显示「不可售」）
    expect(within(dialog).getByText(/当前定价：直接使用上游价格/)).toBeInTheDocument()
    expect(within(dialog).getByText('¥99.00')).toBeInTheDocument()
    expect(within(dialog).getAllByText('不可售').length).toBeGreaterThan(0)

    // 切到加价模式并填写加价率
    await user.click(within(dialog).getByRole('radio', { name: '按上游价加价' }))
    await user.type(within(dialog).getByLabelText('加价率（%）'), '10')
    await user.click(within(dialog).getByRole('button', { name: '保存定价' }))

    await waitFor(() => {
      expect(requestBody('PUT', '/api/v1/admin/products/2')).toEqual({
        pricing_json: { mode: 'markup', markup_percent: 10 },
      })
    })
  })

  it('定价保存失败（409）时把后端提示原样展示在弹窗里', async () => {
    const user = userEvent.setup()
    const conflict = '商品没有任何可用周期的价格，无法上架（请先配置固定价或确认上游价格可用）'
    renderProducts('admin', [apiPut('/api/v1/admin/products/2', conflict, 409)])
    await waitForList()

    await user.click(screen.getAllByRole('button', { name: '定价设置' })[1])
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('radio', { name: '固定价覆盖' }))
    await user.type(within(dialog).getByLabelText('月付固定价（元）'), '29.90')
    await user.click(within(dialog).getByRole('button', { name: '保存定价' }))

    expect(await screen.findByText(/商品没有任何可用周期的价格/)).toBeInTheDocument()
    // 弹窗保持打开，便于对照修改
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(requestBody('PUT', '/api/v1/admin/products/2')).toEqual({
      pricing_json: { mode: 'fixed', fixed: { monthly: '29.90' } },
    })
  })

  it('下架需二次确认，确认后 PUT status=off', async () => {
    const user = userEvent.setup()
    renderProducts('admin', [apiPut('/api/v1/admin/products/1', { ...ON_PRODUCT, status: 'off' })])
    await waitForList()

    await user.click(screen.getByRole('button', { name: '下架' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/下架后会员端不可见、不可下单/)).toBeInTheDocument()

    // 确认前不发出写请求
    expect(hasRequest('PUT', '/api/v1/admin/products/1')).toBe(false)

    await user.click(within(dialog).getByRole('button', { name: '确认下架' }))
    await waitFor(() => {
      expect(requestBody('PUT', '/api/v1/admin/products/1')).toEqual({ status: 'off' })
    })
  })

  it('加价模式未填加价率时不提交，并给出字段校验提示', async () => {
    const user = userEvent.setup()
    renderProducts('admin', [apiPut('/api/v1/admin/products/2', OFF_PRODUCT)])
    await waitForList()

    await user.click(screen.getAllByRole('button', { name: '定价设置' })[1])
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('radio', { name: '按上游价加价' }))
    await user.click(within(dialog).getByRole('button', { name: '保存定价' }))

    expect(await screen.findByText('请输入加价率')).toBeInTheDocument()
    expect(hasRequest('PUT', '/api/v1/admin/products/2')).toBe(false)
  })

  it('分组管理：改名后 PUT 分组接口（name + sort）', async () => {
    const user = userEvent.setup()
    renderProducts('admin', [apiPut('/api/v1/admin/product-groups/1', { ...GROUPS[0], name: '香港二区（自营）' })])
    await waitForList()

    await user.click(screen.getByRole('button', { name: /展开分组/ }))
    await user.click(screen.getAllByRole('button', { name: '编辑' })[0])

    const dialog = await screen.findByRole('dialog')
    const nameInput = within(dialog).getByLabelText('分组名称')
    await user.clear(nameInput)
    await user.type(nameInput, '香港二区（自营）')
    await user.click(within(dialog).getByRole('button', { name: '保存' }))

    await waitFor(() => {
      expect(requestBody('PUT', '/api/v1/admin/product-groups/1')).toEqual({
        name: '香港二区（自营）',
        sort: 0,
      })
    })
  })

  it('分组面板：展示计数，点击分组行按该分组筛选列表', async () => {
    const user = userEvent.setup()
    renderProducts('admin')
    await waitForList()

    await user.click(screen.getByRole('button', { name: /展开分组/ }))
    expect(screen.getByText(/排序 0 · 商品 2 个（已上架 1 \/ 已下架 1）/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /香港二区/ }))
    await waitFor(() => {
      expect(listUrls().some((url) => url.includes('group_id=1'))).toBe(true)
    })
  })
})
