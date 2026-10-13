import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { makeAdminAccount, makeLedgerEntry, makeReferralPayout } from '@/test/fixtures'
import {
  installFetchMock,
  ok,
  renderApp,
  requestBody,
  seedAdminProfile,
  seedAdminToken,
  type FetchHandler,
} from '@/test/harness'

// 管理后台「返佣管理」：提现/转余额审核（approve/reject 请求体）、标记打款、
// 返佣流水对账（ledger?type=referral_rebate）与角色矩阵（审核仅 admin、support 无入口）。

const WITHDRAW = makeReferralPayout({
  id: 21,
  kind: 'withdraw',
  member_id: 1,
  amount: '15.00',
  channel: 'wechat',
  account_name: '张**',
  account_no: '12****34',
  status: 'pending',
  member: { id: 1, username: 'demo7a', nickname: 'demo7a' },
})

const APPROVED = makeReferralPayout({
  id: 22,
  kind: 'withdraw',
  member_id: 1,
  amount: '20.00',
  status: 'approved',
  member: { id: 1, username: 'demo7a', nickname: 'demo7a' },
})

const CONVERT = makeReferralPayout({
  id: 31,
  kind: 'convert',
  member_id: 2,
  amount: '8.00',
  channel: '',
  account_name: '',
  account_no: '',
  status: 'pending',
  member: { id: 2, username: 'friend1', nickname: '好友' },
})

function mockReferralAdmin(role: 'admin' | 'finance' | 'support' = 'admin', handler?: FetchHandler) {
  return installFetchMock((url, init) => {
    const overridden = handler?.(url, init)
    if (overridden) {
      return overridden
    }
    const path = url.pathname
    if (path === '/api/v1/admin/profile') {
      return ok(makeAdminAccount({ role, username: role }))
    }
    if (path === '/api/v1/admin/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    if (path === '/api/v1/admin/referral/withdrawals') {
      return ok({ items: [WITHDRAW, APPROVED], page: 1, page_size: 20, total: 2 })
    }
    if (path === '/api/v1/admin/referral/conversions') {
      return ok({ items: [CONVERT], page: 1, page_size: 20, total: 1 })
    }
    if (path === '/api/v1/admin/referral/withdrawals/21/review' && init.method === 'POST') {
      return ok({ payout: { ...WITHDRAW, status: 'approved' }, already: false })
    }
    if (path === '/api/v1/admin/referral/withdrawals/22/mark-paid' && init.method === 'POST') {
      return ok({ payout: { ...APPROVED, status: 'paid' }, already: false })
    }
    if (path === '/api/v1/admin/referral/conversions/31/review' && init.method === 'POST') {
      return ok({ payout: { ...CONVERT, status: 'approved' }, already: false })
    }
    if (path === '/api/v1/admin/ledger') {
      return ok({ items: [makeLedgerEntry({ type: 'referral_rebate' })], page: 1, page_size: 20, total: 1 })
    }
    return undefined
  })
}

async function renderReferralAdmin(role: 'admin' | 'finance' | 'support' = 'admin') {
  const fetchMock = mockReferralAdmin(role)
  seedAdminToken()
  seedAdminProfile({ role, username: role })
  renderApp(['/admin/referral'])
  if (role === 'support') {
    await screen.findByText('无权访问返佣管理')
  } else {
    await screen.findByRole('heading', { name: '返佣管理' })
    await screen.findByTestId('admin-table')
  }
  return fetchMock
}

function postCalls(fetchMock: ReturnType<typeof installFetchMock>, path: string) {
  return fetchMock.mock.calls.filter(
    ([input, init]) => String(input).includes(path) && (init as RequestInit)?.method === 'POST',
  )
}

/** 取同名按钮里可点的那个（列表里「待审核」与「已通过」两行都会渲染操作按钮）。 */
function enabledButton(name: string): HTMLButtonElement {
  const button = screen
    .getAllByRole('button', { name })
    .find((item) => !(item as HTMLButtonElement).disabled)
  if (!button) {
    throw new Error(`没有可点击的「${name}」按钮`)
  }
  return button as HTMLButtonElement
}

describe('管理后台 · 返佣管理', () => {
  it('提现审核通过：POST review 的 action=approve', async () => {
    const fetchMock = await renderReferralAdmin()
    const user = userEvent.setup()

    const table = within(screen.getByTestId('admin-table'))
    // 账号字段后端已脱敏，原样展示（姓名与账号同格）
    expect(table.getAllByText('张** · 12****34').length).toBeGreaterThan(0)

    // 两行都有「审核」按钮，只有「待审核」那行可点（id=21）
    await user.click(enabledButton('审核'))
    await screen.findByRole('dialog')
    await user.click(screen.getByRole('button', { name: '确认通过' }))

    await waitFor(() => {
      const calls = postCalls(fetchMock, '/admin/referral/withdrawals/21/review')
      expect(calls.length).toBe(1)
      expect(requestBody(calls[0][1] as RequestInit)).toEqual({ action: 'approve' })
    })
  })

  it('提现审核驳回：请求体带 action=reject 与 review_note', async () => {
    const fetchMock = await renderReferralAdmin()
    const user = userEvent.setup()

    await user.click(enabledButton('审核'))
    await screen.findByRole('dialog')

    await user.click(screen.getByRole('radio', { name: '驳回' }))
    await user.type(screen.getByLabelText('审核备注（可选，≤ 255 字符）'), '账号信息不符')
    await user.click(screen.getByRole('button', { name: '确认驳回' }))

    await waitFor(() => {
      const calls = postCalls(fetchMock, '/admin/referral/withdrawals/21/review')
      expect(calls.length).toBe(1)
      expect(requestBody(calls[0][1] as RequestInit)).toEqual({
        action: 'reject',
        review_note: '账号信息不符',
      })
    })
  })

  it('标记打款：POST mark-paid（可带追加备注）', async () => {
    const fetchMock = await renderReferralAdmin()
    const user = userEvent.setup()

    // 已通过的那条（id=22）才有可点的「标记打款」
    await user.click(enabledButton('标记打款'))
    await screen.findByText('标记线下已打款')
    await user.type(screen.getByLabelText('打款备注（可选，追加留档）'), '线下转账截图已存档')
    await user.click(screen.getByRole('button', { name: '确认已打款' }))

    await waitFor(() => {
      const calls = postCalls(fetchMock, '/admin/referral/withdrawals/22/mark-paid')
      expect(calls.length).toBe(1)
      expect(requestBody(calls[0][1] as RequestInit)).toEqual({
        review_note: '线下转账截图已存档',
      })
    })
  })

  it('转余额审核：POST conversions/:id/review 的 action', async () => {
    const fetchMock = await renderReferralAdmin()
    const user = userEvent.setup()

    await user.click(screen.getByRole('tab', { name: '转余额审核' }))
    await screen.findByText('friend1')

    await user.click(screen.getByRole('button', { name: '审核' }))
    await screen.findByRole('dialog')
    await user.click(screen.getByRole('button', { name: '确认通过' }))

    await waitFor(() => {
      const calls = postCalls(fetchMock, '/admin/referral/conversions/31/review')
      expect(calls.length).toBe(1)
      expect(requestBody(calls[0][1] as RequestInit)).toEqual({ action: 'approve' })
    })
  })

  it('返佣流水：按 ledger?type=referral_rebate 对账', async () => {
    const fetchMock = await renderReferralAdmin()
    const user = userEvent.setup()

    await user.click(screen.getByRole('tab', { name: '返佣流水' }))

    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(([input]) =>
          String(input).includes('type=referral_rebate'),
        ),
      ).toBe(true)
    })
  })

  it('财务角色：可查看但审核/打款按钮禁用，且不发 POST', async () => {
    const fetchMock = await renderReferralAdmin('finance')
    const user = userEvent.setup()

    expect(screen.getByRole('heading', { name: '返佣管理' })).toBeInTheDocument()
    expect(
      screen.getAllByRole('button', { name: '审核' }).every((b) => (b as HTMLButtonElement).disabled),
    ).toBe(true)
    expect(
      screen
        .getAllByRole('button', { name: '标记打款' })
        .every((b) => (b as HTMLButtonElement).disabled),
    ).toBe(true)

    await user.click(screen.getAllByRole('button', { name: '审核' })[0])
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(postCalls(fetchMock, '/admin/referral/')).toHaveLength(0)
  })

  it('客服角色：整页无权占位且不发任何返佣请求', async () => {
    const fetchMock = await renderReferralAdmin('support')

    expect(screen.getByText('无权访问返佣管理')).toBeInTheDocument()
    expect(
      fetchMock.mock.calls.some(([input]) => String(input).includes('/admin/referral')),
    ).toBe(false)
  })
})
