import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import type { ReferralOverview } from '@/api/types'
import {
  makeMember,
  makeMemberCoupon,
  makeReferralInvitee,
  makeReferralOverview,
  makeReferralPayout,
  makeReferralRebate,
} from '@/test/fixtures'
import {
  fail,
  installFetchMock,
  ok,
  renderApp,
  requestBody,
  seedMemberToken,
  type FetchHandler,
} from '@/test/harness'

// 会员区「邀请有礼」：概览（含负数余额）、开通切换、建券表单提交体、建券超限 40002 展示、
// 只允许改 status/expires_at、提现/转换提交体、负数余额禁用入口。

function mockReferral(overview: ReferralOverview, handler?: FetchHandler) {
  return installFetchMock((url, init) => {
    // 传入的 handler 优先：便于单测覆盖某一条路径为失败响应。
    const overridden = handler?.(url, init)
    if (overridden) {
      return overridden
    }
    const path = url.pathname
    if (path === '/api/v1/members/me') {
      return ok(makeMember())
    }
    if (path === '/api/v1/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    if (path === '/api/v1/referral') {
      return ok(overview)
    }
    if (path === '/api/v1/referral/coupons' && init.method === 'POST') {
      return ok(makeMemberCoupon())
    }
    if (path === '/api/v1/referral/coupons') {
      return ok({ items: [makeMemberCoupon()], page: 1, page_size: 20, total: 1 })
    }
    if (path === '/api/v1/referral/coupons/11' && init.method === 'PUT') {
      return ok(makeMemberCoupon())
    }
    if (path === '/api/v1/referral/withdrawals' && init.method === 'POST') {
      return ok(makeReferralPayout())
    }
    if (path === '/api/v1/referral/withdrawals') {
      return ok({ items: [makeReferralPayout()], page: 1, page_size: 20, total: 1 })
    }
    if (path === '/api/v1/referral/conversions' && init.method === 'POST') {
      return ok(makeReferralPayout({ kind: 'convert', channel: '', account_name: '', account_no: '' }))
    }
    if (path === '/api/v1/referral/conversions') {
      return ok({ items: [], page: 1, page_size: 20, total: 0 })
    }
    if (path === '/api/v1/referral/rebates') {
      return ok({ items: [makeReferralRebate()], page: 1, page_size: 20, total: 1 })
    }
    if (path === '/api/v1/referral/invitees') {
      return ok({ items: [makeReferralInvitee()], page: 1, page_size: 20, total: 1 })
    }
    return undefined
  })
}

async function renderReferral(overview: ReferralOverview, handler?: FetchHandler) {
  const fetchMock = mockReferral(overview, handler)
  seedMemberToken()
  renderApp(['/console/referral'])
  await screen.findByRole('heading', { name: '邀请有礼' })
  await screen.findByText('邀请返现余额')
  return fetchMock
}

function putCalls(fetchMock: ReturnType<typeof installFetchMock>, path: string) {
  return fetchMock.mock.calls.filter(
    ([input, init]) => String(input).includes(path) && (init as RequestInit)?.method === 'PUT',
  )
}

function postCalls(fetchMock: ReturnType<typeof installFetchMock>, path: string) {
  return fetchMock.mock.calls.filter(
    ([input, init]) => String(input).includes(path) && (init as RequestInit)?.method === 'POST',
  )
}

describe('会员区 · 邀请有礼', () => {
  it('概览渲染：负数返现余额显眼提示，并给出禁用原因', async () => {
    await renderReferral(makeReferralOverview({ rebate_balance: '-5.00', enabled: true }))

    expect(screen.getByText('¥-5.00')).toBeInTheDocument()
    expect(
      screen.getByText('余额为负，暂不能提现或转余额（其余功能不受影响）。'),
    ).toBeInTheDocument()
    expect(screen.getByText('¥20.00')).toBeInTheDocument() // 累计返佣
    expect(screen.getByText('ABCD2345')).toBeInTheDocument()
  })

  it('开通切换：PUT /referral 的 enabled 为 true', async () => {
    const fetchMock = await renderReferral(
      makeReferralOverview({ enabled: false, invite_code: '', invite_link: '' }),
    )
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: '开通邀请返现' }))

    await waitFor(() => {
      const calls = putCalls(fetchMock, '/api/v1/referral')
      expect(calls.length).toBe(1)
      expect(requestBody(calls[0][1] as RequestInit)).toEqual({ enabled: true })
    })
  })

  it('建券表单：提交体为 percent 折扣 + 周期/过期/次数/备注', async () => {
    const fetchMock = await renderReferral(makeReferralOverview())
    const user = userEvent.setup()

    await user.type(screen.getByLabelText('优惠码'), 'FRIEND20')
    await user.type(screen.getByLabelText('折扣百分比（%）'), '15.5')
    await user.click(screen.getByLabelText('年付'))
    await user.type(screen.getByLabelText('使用次数上限（0 = 不限）'), '50')
    await user.type(screen.getByLabelText('备注（可选，≤ 255 字符）'), '老带新活动')
    await user.click(screen.getByRole('button', { name: '创建优惠码' }))

    await waitFor(() => {
      const calls = postCalls(fetchMock, '/api/v1/referral/coupons')
      expect(calls.length).toBe(1)
      const body = requestBody<{
        code: string
        type: string
        value: string
        cycles: string[]
        max_uses: number
        comment: string
        expires_at?: string
      }>(calls[0][1] as RequestInit)
      expect(body?.code).toBe('FRIEND20')
      expect(body?.type).toBe('percent')
      expect(body?.value).toBe('15.5')
      expect(body?.cycles).toEqual(['annual'])
      expect(body?.max_uses).toBe(50)
      expect(body?.comment).toBe('老带新活动')
      expect(body).not.toHaveProperty('owner_member_id')
      expect(body).not.toHaveProperty('expires_at')
    })
  })

  it('建券超限：后端 40002 的 message 原样展示（含折扣上限口径）', async () => {
    const capMessage = '折扣不能超过 20.00（上限 = min(设置 50.00, 生效比例 20.00)）'
    await renderReferral(makeReferralOverview(), (url, init) => {
      if (url.pathname === '/api/v1/referral/coupons' && init.method === 'POST') {
        return fail(40002, capMessage)
      }
      return undefined
    })
    const user = userEvent.setup()

    await user.type(screen.getByLabelText('优惠码'), 'TOOBIG')
    await user.type(screen.getByLabelText('折扣百分比（%）'), '30')
    await user.click(screen.getByRole('button', { name: '创建优惠码' }))

    expect(await screen.findByText(capMessage)).toBeInTheDocument()
  })

  it('改券只允许 status 与 expires_at：启停只提交 status，过期清空提交 null', async () => {
    const fetchMock = await renderReferral(makeReferralOverview())
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: '停用' }))
    await waitFor(() => {
      const calls = putCalls(fetchMock, '/api/v1/referral/coupons/11')
      expect(calls.length).toBe(1)
      expect(requestBody(calls[0][1] as RequestInit)).toEqual({ status: 'off' })
    })

    await user.click(screen.getByRole('button', { name: '保存过期时间' }))
    await waitFor(() => {
      const calls = putCalls(fetchMock, '/api/v1/referral/coupons/11')
      expect(calls.length).toBe(2)
      expect(requestBody(calls[1][1] as RequestInit)).toEqual({ expires_at: null })
    })
  })

  it('提现与转余额提交体：渠道/收款账号/备注分别成体', async () => {
    const fetchMock = await renderReferral(makeReferralOverview())
    const user = userEvent.setup()

    await user.type(screen.getByLabelText('提现金额（元）'), '12.50')
    await user.click(screen.getByRole('radio', { name: '支付宝' }))
    await user.type(screen.getByLabelText('收款人姓名'), '张三')
    await user.type(screen.getByLabelText('收款账号'), '13800138000')
    await user.type(screen.getByLabelText('提现备注（可选，≤ 255 字符）'), '急用')
    await user.click(screen.getByRole('button', { name: '提交提现申请' }))

    await waitFor(() => {
      const calls = postCalls(fetchMock, '/api/v1/referral/withdrawals')
      expect(calls.length).toBe(1)
      expect(requestBody(calls[0][1] as RequestInit)).toEqual({
        amount: '12.50',
        channel: 'alipay',
        account_name: '张三',
        account_no: '13800138000',
        remark: '急用',
      })
    })

    await user.type(screen.getByLabelText('转余额金额（元）'), '30')
    await user.click(screen.getByRole('button', { name: '提交转余额申请' }))

    await waitFor(() => {
      const calls = postCalls(fetchMock, '/api/v1/referral/conversions')
      expect(calls.length).toBe(1)
      expect(requestBody(calls[0][1] as RequestInit)).toEqual({ amount: '30' })
    })
  })

  it('负数余额：提现与转换提交入口禁用并给出原因（其余照常）', async () => {
    const fetchMock = await renderReferral(makeReferralOverview({ rebate_balance: '-1.00' }))
    const user = userEvent.setup()

    expect(screen.getByRole('button', { name: '提交提现申请' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '提交转余额申请' })).toBeDisabled()
    expect(
      screen.getAllByText('邀请返现余额为负，暂不能提现或转余额').length,
    ).toBeGreaterThan(0)

    // 其余功能照常：建券仍可提交
    await user.type(screen.getByLabelText('优惠码'), 'STILLOK')
    await user.type(screen.getByLabelText('折扣百分比（%）'), '5')
    await user.click(screen.getByRole('button', { name: '创建优惠码' }))
    await waitFor(() => {
      expect(postCalls(fetchMock, '/api/v1/referral/coupons').length).toBe(1)
    })
  })

  it('提现失败时原样展示后端 message（余额不足）', async () => {
    await renderReferral(makeReferralOverview(), (url, init) => {
      if (url.pathname === '/api/v1/referral/withdrawals' && init.method === 'POST') {
        return fail(40002, '余额不足（可用余额 1.00）')
      }
      return undefined
    })
    const user = userEvent.setup()

    await user.type(screen.getByLabelText('提现金额（元）'), '99')
    await user.type(screen.getByLabelText('收款人姓名'), '张三')
    await user.type(screen.getByLabelText('收款账号'), '13800138000')
    await user.click(screen.getByRole('button', { name: '提交提现申请' }))

    expect(await screen.findByText('余额不足（可用余额 1.00）')).toBeInTheDocument()
  })
})
