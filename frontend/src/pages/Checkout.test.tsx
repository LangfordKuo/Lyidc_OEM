import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import AuthProvider from '../auth/AuthProvider'
import { routes } from '../app/routes'

// 下单确认页集成测试：覆盖「校验优惠码 → 提交订单 → 取消订单」主链路与提交给后端的请求体。
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

const product = {
  id: 7,
  name: '香港二区 CN2 A型',
  type: 'dcimcloud',
  sort: 0,
  prices: {
    monthly: '22.00',
    quarterly: '66.00',
    semiannual: null,
    annual: '220.00',
    biennial: null,
    triennial: null,
  },
  stock_qty: 70,
  stock_control: 1,
  ontrial_max: 0,
  description: '&lt;li&gt;CPU:2核心&lt;/li&gt;',
  group: { id: 1, name: '香港二区' },
  config_groups: [
    {
      id: 1,
      name: '区域',
      options: [
        {
          id: 11,
          name: 'area|区域',
          type: 12,
          upstream_id: 0,
          values: [{ id: 111, name: '1|HK^香港', upstream_id: 0 }],
        },
      ],
    },
  ],
  custom_fields: [],
  updated_at: '2026-10-08T09:12:03Z',
}

// 订单对象字段与契约 12.4 一致（待支付态）。
const pendingOrder = {
  id: 12,
  trade_no: 'O20261008143015K7Q2ZP',
  member_id: 1,
  product_id: 7,
  product_name: '香港二区 CN2 A型',
  cycle: 'monthly',
  qty: 1,
  config: { '11': '111' },
  amount: '22.00',
  discount_amount: '22.00',
  final_amount: '0.00',
  coupon_code: 'WELCOME10',
  status: 'pending',
  type: 'new',
  pay_channel: '',
  channel_trade_no: '',
  pay_time: null,
  host_id: null,
  instance_id: null,
  provision_error: '',
  delivered_at: null,
  created_at: '2026-10-08T14:30:15Z',
  updated_at: '2026-10-08T14:30:15Z',
}

interface StubOptions {
  onOrder?: (body: unknown) => void
  onCancel?: () => void
  /** 优惠码校验结果：默认 WELCOME10 可用 */
  couponInvalid?: boolean
}

function stubApi(options: StubOptions = {}) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      if (url.startsWith('/api/v1/members/me')) {
        return Promise.resolve(ok(member))
      }
      if (url.startsWith('/api/v1/finance/balance')) {
        return Promise.resolve(ok({ member_id: 1, balance: '50.00' }))
      }
      if (url.startsWith('/api/v1/products/7')) {
        return Promise.resolve(ok(product))
      }
      if (url.includes('/coupons/WELCOME10/validate')) {
        if (options.couponInvalid) {
          return Promise.resolve(ok({ valid: false, reason: 'expired' }))
        }
        return Promise.resolve(
          ok({
            valid: true,
            code: 'WELCOME10',
            type: 'percent',
            value: '10.00',
            price: '22.00',
            discount_amount: '22.00',
            final_amount: '0.00',
          }),
        )
      }
      if (url === '/api/v1/orders' && init?.method === 'POST') {
        options.onOrder?.(JSON.parse(String(init.body)))
        return Promise.resolve(ok(pendingOrder))
      }
      if (url === '/api/v1/orders/12/cancel') {
        options.onCancel?.()
        return Promise.resolve(ok({ ...pendingOrder, status: 'cancelled' }))
      }
      return Promise.resolve(envelopeResponse({ code: 404, message: '资源不存在' }, 404))
    }),
  )
}

function renderCheckout() {
  localStorage.setItem('lyidc.member.token', 'member-token')
  localStorage.setItem('lyidc.member.profile', JSON.stringify(member))
  const router = createMemoryRouter(routes, { initialEntries: ['/checkout/7'] })
  return render(
    <AuthProvider>
      <RouterProvider router={router} />
    </AuthProvider>,
  )
}

describe('下单确认页', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('校验优惠码后提交订单：请求体带上周期、配置项与优惠码', async () => {
    const onOrder = vi.fn()
    stubApi({ onOrder })
    const user = userEvent.setup()

    renderCheckout()

    expect(await screen.findByRole('heading', { level: 1, name: '确认订单' })).toBeInTheDocument()
    expect(screen.getAllByText('香港二区 CN2 A型').length).toBeGreaterThan(0)

    // 默认周期为最低价周期（月付 22.00）
    expect(screen.getAllByText('¥22.00').length).toBeGreaterThan(0)

    await user.type(screen.getByLabelText('优惠码'), 'WELCOME10')
    await user.click(screen.getByRole('button', { name: '校验' }))

    expect(await screen.findByText(/WELCOME10 可用：减免 ¥22\.00/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '提交订单' }))

    await waitFor(() => {
      expect(onOrder).toHaveBeenCalledTimes(1)
    })
    expect(onOrder).toHaveBeenCalledWith({
      product_id: 7,
      cycle: 'monthly',
      config: { '11': '111' },
      coupon_code: 'WELCOME10',
    })

    // 订单创建后进入支付环节
    expect(await screen.findByText(/O20261008143015K7Q2ZP/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '前往支付' })).toBeInTheDocument()
  })

  it('提交订单后可以取消订单', async () => {
    const onCancel = vi.fn()
    stubApi({ onCancel })
    const user = userEvent.setup()

    renderCheckout()

    await user.click(await screen.findByRole('button', { name: '提交订单' }))
    await screen.findByRole('button', { name: '取消订单' })

    await user.click(screen.getByRole('button', { name: '取消订单' }))

    await waitFor(() => {
      expect(onCancel).toHaveBeenCalledTimes(1)
    })
    expect(await screen.findByText('该订单已取消。')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重新下单' })).toBeInTheDocument()
  })

  it('优惠码校验不通过时不提交订单', async () => {
    const onOrder = vi.fn()
    stubApi({ onOrder, couponInvalid: true })
    const user = userEvent.setup()

    renderCheckout()

    await user.type(await screen.findByLabelText('优惠码'), 'WELCOME10')
    await user.click(screen.getByRole('button', { name: '校验' }))

    expect(await screen.findByText('优惠码已过期')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '提交订单' }))

    expect(await screen.findByText('优惠码不可用，请清除或更换后再提交订单')).toBeInTheDocument()
    expect(onOrder).not.toHaveBeenCalled()
  })
})
