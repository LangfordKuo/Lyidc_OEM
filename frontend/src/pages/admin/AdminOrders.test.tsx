import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Order } from '../../api/types'
import { apiFail, renderAdmin } from '../../test/adminHarness'
import { apiPost, fetchCalls } from '../../test/consoleHarness'

// 订单页：管理端只有「重试交付」这一个订单接口（无列表/详情），页面按角色矩阵控制操作权限。
const retriedOrder: Order = {
  id: 12,
  trade_no: 'O20261008105520T0J03J',
  member_id: 9,
  product_id: 3,
  product_name: '香港二区 CN2 A型',
  cycle: 'monthly',
  qty: 1,
  config: {},
  amount: '20.00',
  discount_amount: '0.00',
  final_amount: '20.00',
  coupon_code: '',
  status: 'active',
  type: 'new',
  pay_channel: 'epay',
  channel_trade_no: 'CH20261008001',
  pay_time: '2026-10-08T10:55:25Z',
  host_id: 10922,
  instance_id: 5,
  provision_error: '',
  delivered_at: '2026-10-08T10:55:40Z',
  created_at: '2026-10-08T10:55:20Z',
  updated_at: '2026-10-08T10:55:40Z',
}

function handlers() {
  return [apiPost('/api/v1/admin/orders/12/retry-delivery', retriedOrder)]
}

describe('管理后台 · 订单（重试交付）', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('页面明示管理端订单列表接口缺失，不做订单检索', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/orders', 'admin', handlers())

    expect(await screen.findByText('管理端订单列表接口暂缺')).toBeInTheDocument()
    // 未填订单 ID 时按钮禁用（防误触发上游调用）
    expect(screen.getByRole('button', { name: '重试交付' })).toBeDisabled()

    await user.type(screen.getByLabelText('订单 ID'), '12')
    expect(screen.getByRole('button', { name: '重试交付' })).toBeEnabled()
  })

  it('admin：输入订单 ID → 二次确认 → 调用 retry-delivery 并展示最新订单', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/orders', 'admin', handlers())

    await user.type(await screen.findByLabelText('订单 ID'), '12')
    await user.click(screen.getByRole('button', { name: '重试交付' }))

    // 二次确认弹窗
    expect(await screen.findByText('确认重试交付？')).toBeInTheDocument()
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: '开始重试交付' }))

    await waitFor(() => {
      const call = fetchCalls().find(([url]) => url === '/api/v1/admin/orders/12/retry-delivery')
      expect(call?.[1].method).toBe('POST')
    })

    expect(await screen.findByText('最近一次重试结果')).toBeInTheDocument()
    expect(screen.getByText('O20261008105520T0J03J')).toBeInTheDocument()
    expect(screen.getByText('已开通')).toBeInTheDocument()
    expect(screen.getByText('10922')).toBeInTheDocument()
  })

  it('非 admin 角色（finance / support）：按钮禁用且不发起请求', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/orders', 'support', handlers())

    const button = await screen.findByRole('button', { name: '重试交付' })
    expect(button).toBeDisabled()
    expect(screen.getByText(/仅超级管理员可执行/)).toBeInTheDocument()
    // 输入框同样禁用
    expect(screen.getByLabelText('订单 ID')).toBeDisabled()

    await user.click(button)
    expect(fetchCalls().some(([url]) => url.includes('/retry-delivery'))).toBe(false)
  })

  it('订单 ID 非正整数时给出前端提示', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/orders', 'admin', handlers())

    await user.type(await screen.findByLabelText('订单 ID'), 'abc')
    await user.click(screen.getByRole('button', { name: '重试交付' }))

    expect(await screen.findByText('订单 ID 必须为正整数')).toBeInTheDocument()
    expect(screen.queryByText('确认重试交付？')).not.toBeInTheDocument()
  })

  it('后端 40002（订单不可重试）直接展示 message', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/orders', 'admin', [
      apiFail(
        'POST',
        '/api/v1/admin/orders/12/retry-delivery',
        40002,
        '订单正在交付中，无法重试交付',
      ),
    ])

    await user.type(await screen.findByLabelText('订单 ID'), '12')
    await user.click(screen.getByRole('button', { name: '重试交付' }))
    await user.click(within(await screen.findByRole('dialog')).getByRole('button', { name: '开始重试交付' }))

    expect(await screen.findByText('订单正在交付中，无法重试交付')).toBeInTheDocument()
  })
})
