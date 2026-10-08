import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { apiFail, renderAdmin } from '../../test/adminHarness'
import { apiGet, apiPut, fetchCalls, requestBody, type ApiHandler } from '../../test/consoleHarness'

// 管理后台「会员」页：角色矩阵（禁用按钮）、搜索、详情弹窗（流水/充值单权限）、禁用二次确认。

const alice = {
  id: 1,
  username: 'alice',
  email: 'alice@example.com',
  nickname: '爱丽丝',
  phone: '13800000000',
  status: 'active',
  balance: '128.50',
  created_at: '2026-10-01T02:30:00Z',
  updated_at: '2026-10-08T06:18:31Z',
  last_login_at: '2026-10-08T06:18:31Z',
}

const bob = {
  ...alice,
  id: 2,
  username: 'bob',
  email: 'bob@example.com',
  nickname: '鲍勃',
  phone: null,
  status: 'disabled',
  balance: '0.00',
  last_login_at: null,
}

const memberList = { items: [alice, bob], page: 1, page_size: 20, total: 2 }
const singleMemberList = { items: [alice], page: 1, page_size: 20, total: 1 }

// 余额流水（契约 12.3）：入账为正、出账为负，带 ref_type / ref_id 与备注。
const ledgerPage = {
  items: [
    {
      id: 9001,
      member_id: 1,
      type: 'recharge',
      amount: '100.00',
      balance_before: '28.50',
      balance_after: '128.50',
      ref_type: 'recharge',
      ref_id: 501,
      note: '充值 R20261001023000ABCD',
      created_at: '2026-10-01T02:31:00Z',
    },
    {
      id: 9002,
      member_id: 1,
      type: 'order_pay',
      amount: '-72.00',
      balance_before: '128.50',
      balance_after: '56.50',
      ref_type: 'order',
      ref_id: 12,
      note: '订单支付 O20261002120000WXYZ',
      created_at: '2026-10-02T12:00:00Z',
    },
  ],
  page: 1,
  page_size: 10,
  total: 2,
}

const rechargePage = {
  items: [
    {
      id: 501,
      trade_no: 'R20261001023000ABCD',
      member_id: 1,
      amount: '100.00',
      channel: 'epay',
      status: 'paid',
      channel_trade_no: '4200001',
      created_at: '2026-10-01T02:30:00Z',
      paid_at: '2026-10-01T02:31:00Z',
      expires_at: null,
    },
  ],
  page: 1,
  page_size: 10,
  total: 1,
}

/** 单会员场景的完整桩：列表 + 流水 + 充值单 + 改状态。 */
const singleMemberHandlers: ApiHandler[] = [
  apiGet('/api/v1/admin/members?', singleMemberList),
  apiGet('/api/v1/admin/ledger?', ledgerPage),
  apiGet('/api/v1/admin/recharges?', rechargePage),
  apiPut('/api/v1/admin/members/1/status', { ...alice, status: 'disabled' }),
]

/** 财务对账接口（流水 / 充值单）的调用记录。 */
function financeCalls(): [string, RequestInit][] {
  return fetchCalls().filter(
    ([url]) => url.includes('/api/v1/admin/ledger') || url.includes('/api/v1/admin/recharges'),
  )
}

describe('管理后台 · 会员', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('admin 角色下「禁用」按钮可用', async () => {
    renderAdmin('/admin/members', 'admin', singleMemberHandlers)

    expect(await screen.findByRole('button', { name: '禁用' })).toBeEnabled()
  })

  it('support 角色下「禁用」按钮为禁用态并给出权限提示', async () => {
    renderAdmin('/admin/members', 'support', singleMemberHandlers)

    const button = await screen.findByRole('button', { name: '禁用' })
    expect(button).toBeDisabled()
    // 页头给出 permissionHint('members.status') 的文案
    expect(screen.getByText(/无权禁用或启用会员/)).toBeInTheDocument()
    expect(screen.getByText(/需要超级管理员或财务角色/)).toBeInTheDocument()
  })

  it('列表展示用户名/昵称/余额/状态，查询把 username 拼进请求', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/members', 'admin', [apiGet('/api/v1/admin/members?', memberList)])

    expect(await screen.findByText('alice')).toBeInTheDocument()
    expect(screen.getByText('爱丽丝')).toBeInTheDocument()
    expect(screen.getByText('bob')).toBeInTheDocument()
    expect(screen.getByText('¥128.50')).toBeInTheDocument()
    // 「正常」「已禁用」既是状态筛选按钮也是状态徽章，断言至少各出现一次
    expect(screen.getAllByText('正常').length).toBeGreaterThan(0)
    expect(screen.getAllByText('已禁用').length).toBeGreaterThan(0)

    await user.type(screen.getByPlaceholderText('用户名（模糊匹配）'), 'ali')
    await user.click(screen.getByRole('button', { name: '查询' }))

    await waitFor(() => {
      expect(
        fetchCalls().some(
          ([url]) => url.includes('/api/v1/admin/members?') && url.includes('username=ali'),
        ),
      ).toBe(true)
    })
  })

  it('邮箱输入框回车即提交（email 拼进请求）', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/members', 'admin', [apiGet('/api/v1/admin/members?', memberList)])

    await screen.findByText('alice')
    await user.type(screen.getByPlaceholderText('邮箱（模糊匹配）'), 'example.com{enter}')

    await waitFor(() => {
      expect(
        fetchCalls().some(
          ([url]) =>
            url.includes('/api/v1/admin/members?') && url.includes('email=example.com'),
        ),
      ).toBe(true)
    })
  })

  it('详情弹窗：admin 请求该会员的流水与充值单并渲染', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/members', 'admin', singleMemberHandlers)

    await user.click(await screen.findByRole('button', { name: '详情' }))

    // 会员资料 + 余额
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('爱丽丝')).toBeInTheDocument()
    expect(within(dialog).getByText('13800000000')).toBeInTheDocument()

    // 流水：类型文案（表格与类型筛选下拉里都有「充值入账」）、有符号金额、余额变化、备注与关联单号
    expect((await screen.findAllByText('充值入账')).length).toBeGreaterThan(0)
    expect(screen.getByText('+¥100.00')).toBeInTheDocument()
    expect(screen.getByText('¥-72.00')).toBeInTheDocument()
    expect(screen.getByText(/¥28\.50 → ¥128\.50/)).toBeInTheDocument()
    expect(screen.getByText('充值 R20261001023000ABCD')).toBeInTheDocument()
    expect(screen.getByText(/关联 充值单 #501/)).toBeInTheDocument()

    // 充值单：单号 + 状态
    expect(screen.getByText('R20261001023000ABCD')).toBeInTheDocument()
    expect(screen.getAllByText('已到账').length).toBeGreaterThan(0)

    // 两个接口都带 member_id
    const urls = financeCalls().map(([url]) => url)
    expect(urls.some((url) => url.includes('/api/v1/admin/ledger?') && url.includes('member_id=1'))).toBe(
      true,
    )
    expect(
      urls.some((url) => url.includes('/api/v1/admin/recharges?') && url.includes('member_id=1')),
    ).toBe(true)
  })

  it('详情的流水可按类型筛选（FilterSelect 复用）', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/members', 'admin', singleMemberHandlers)

    await user.click(await screen.findByRole('button', { name: '详情' }))
    await screen.findByText('充值 R20261001023000ABCD')

    await user.selectOptions(screen.getByLabelText('流水类型'), 'order_pay')

    await waitFor(() => {
      expect(financeCalls().some(([url]) => url.includes('type=order_pay'))).toBe(true)
    })
  })

  it('support 打开详情时不请求流水与充值单，改为展示权限说明', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/members', 'support', singleMemberHandlers)

    await user.click(await screen.findByRole('button', { name: '详情' }))

    expect(
      await screen.findByText(/财务对账数据仅超级管理员与财务可见/),
    ).toBeInTheDocument()
    expect(screen.queryByText('充值入账')).not.toBeInTheDocument()
    // 关键验收点：财务对账接口一次都不能被调用
    expect(financeCalls()).toHaveLength(0)
  })

  it('禁用为危险操作：二次确认后才提交 PUT 状态', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/members', 'admin', singleMemberHandlers)

    await user.click(await screen.findByRole('button', { name: '禁用' }))

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/禁用后该会员无法登录，已签发的 token 会立即失效/)).toBeInTheDocument()

    await user.click(within(dialog).getByRole('button', { name: '确认禁用' }))

    await waitFor(() => {
      expect(requestBody('PUT', '/api/v1/admin/members/1/status')).toEqual({ status: 'disabled' })
    })
    // 关闭确认弹窗并刷新列表（改状态成功后重新拉列表）
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: '确认禁用' })).not.toBeInTheDocument()
    })
  })

  it('改状态失败时把后端 message 展示在确认弹窗里', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/members', 'admin', [
      apiGet('/api/v1/admin/members?', singleMemberList),
      apiFail('PUT', '/api/v1/admin/members/1/status', 403, '当前角色无权执行该操作', 403),
    ])

    await user.click(await screen.findByRole('button', { name: '禁用' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '确认禁用' }))

    expect(await screen.findByText('当前角色无权执行该操作')).toBeInTheDocument()
  })
})
