import { render, screen } from '@testing-library/react'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { routes } from '../app/routes'
import AuthProvider from '../auth/AuthProvider'
import { rememberOrder } from '../lib/checkout'

// 支付结果页：按 order 参数查询，或按渠道回跳的 out_trade_no 匹配本地记录的最近订单。
function envelopeResponse(payload: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(payload),
  } as unknown as Response
}

const ok = (data: unknown) => envelopeResponse({ code: 0, message: 'ok', data })

const member = {
  id: 1,
  username: 'alice',
  email: 'alice@example.com',
  nickname: '爱丽丝',
  phone: null,
  status: 'active',
  balance: '50.00',
  created_at: '2026-10-08T06:18:31Z',
  updated_at: '2026-10-08T06:18:31Z',
  last_login_at: null,
}

function orderWith(status: string) {
  return {
    id: 12,
    trade_no: 'O20261008143015K7Q2ZP',
    member_id: 1,
    product_id: 7,
    product_name: '香港二区 CN2 A型',
    cycle: 'annual',
    qty: 1,
    config: { '11': '111' },
    amount: '220.00',
    discount_amount: '22.00',
    final_amount: '198.00',
    coupon_code: 'CASH20',
    status,
    type: 'new',
    pay_channel: status === 'pending' ? '' : 'epay',
    channel_trade_no: status === 'pending' ? '' : '2026100822001',
    pay_time: status === 'pending' ? null : '2026-10-08T14:31:00Z',
    host_id: null,
    instance_id: null,
    provision_error: '',
    delivered_at: null,
    created_at: '2026-10-08T14:30:15Z',
    updated_at: '2026-10-08T14:31:00Z',
  }
}

function stubOrder(status: string, onRequest?: (url: string) => void) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation((url: string) => {
      if (url.startsWith('/api/v1/members/me')) {
        return Promise.resolve(ok(member))
      }
      if (url.startsWith('/api/v1/orders/12')) {
        onRequest?.(url)
        return Promise.resolve(ok(orderWith(status)))
      }
      return Promise.resolve(envelopeResponse({ code: 404, message: '订单不存在' }, 404))
    }),
  )
}

function renderPayResult(path: string) {
  localStorage.setItem('lyidc.member.token', 'member-token')
  localStorage.setItem('lyidc.member.profile', JSON.stringify(member))
  const router = createMemoryRouter(routes, { initialEntries: [path] })
  return render(
    <AuthProvider>
      <RouterProvider router={router} />
    </AuthProvider>,
  )
}

describe('支付结果页', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('pending：提示处理中并提供重新查询', async () => {
    stubOrder('pending')

    renderPayResult('/pay/result?order=12')

    expect(await screen.findByText('支付结果确认中')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重新查询' })).toBeInTheDocument()
    expect(screen.getByText(/O20261008143015K7Q2ZP/)).toBeInTheDocument()
    expect(screen.getByText('待支付')).toBeInTheDocument()
  })

  it('active：展示支付成功与开通结果', async () => {
    stubOrder('active')

    renderPayResult('/pay/result?order=12')

    expect(await screen.findByText('支付成功')).toBeInTheDocument()
    expect(screen.getByText('实例已开通，可在会员区「我的服务器」查看并使用。')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '查看订单' })).toBeInTheDocument()
  })

  it('无 order 参数时按 out_trade_no 匹配本地记录的最近订单', async () => {
    const onRequest = vi.fn()
    stubOrder('paid', onRequest)
    rememberOrder({ id: 12, trade_no: 'O20261008143015K7Q2ZP', productId: 7 })

    renderPayResult('/pay/result?out_trade_no=O20261008143015K7Q2ZP&trade_no=2026100822001')

    expect(await screen.findByText(/O20261008143015K7Q2ZP/)).toBeInTheDocument()
    expect(onRequest).toHaveBeenCalledWith('/api/v1/orders/12')
  })

  it('既无 order 参数也无本地记录时给出引导，不猜测支付状态', async () => {
    stubOrder('paid')

    renderPayResult('/pay/result')

    expect(await screen.findByText('未找到对应的订单信息')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '查看我的订单' })).toBeInTheDocument()
  })
})
