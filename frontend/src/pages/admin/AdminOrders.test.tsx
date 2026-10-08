import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { makeAdminAccount, makeAdminOrder } from '@/test/fixtures'
import {
  installFetchMock,
  ok,
  renderApp,
  seedAdminProfile,
  seedAdminToken,
  type FetchHandler,
} from '@/test/harness'

// 管理后台「订单」列表：状态/类型/会员/单号筛选（请求参数）与分页。

const ORDER_A = makeAdminOrder({ id: 1, trade_no: 'O20261008143015K7Q2ZP', status: 'active' })
const ORDER_B = makeAdminOrder({
  id: 2,
  trade_no: 'O20261008150000AAAAAA',
  status: 'failed',
  type: 'renew',
  member: null,
  member_id: 9,
})

function mockOrders(handler?: FetchHandler) {
  return installFetchMock((url, init) => {
    const path = url.pathname
    if (path === '/api/v1/admin/profile') {
      return ok(makeAdminAccount())
    }
    if (path === '/api/v1/admin/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    if (path === '/api/v1/admin/orders') {
      const status = url.searchParams.get('status')
      const type = url.searchParams.get('type')
      const memberID = url.searchParams.get('member_id')
      const tradeNo = url.searchParams.get('trade_no')
      let items = [ORDER_A, ORDER_B]
      if (status) {
        items = items.filter((order) => order.status === status)
      }
      if (type) {
        items = items.filter((order) => order.type === type)
      }
      if (memberID) {
        items = items.filter((order) => String(order.member_id) === memberID)
      }
      if (tradeNo) {
        items = items.filter((order) => order.trade_no.includes(tradeNo))
      }
      return ok({ items, page: 1, page_size: 20, total: items.length })
    }
    return handler?.(url, init)
  })
}

async function renderOrders() {
  seedAdminToken()
  seedAdminProfile()
  renderApp(['/admin/orders'])
  await screen.findByRole('heading', { name: '订单' })
  await screen.findByText('O20261008143015K7Q2ZP')
}

describe('管理后台 · 订单列表', () => {
  it('渲染订单行：单号、会员、商品、金额与状态；会员资料缺失时退化展示', async () => {
    mockOrders()
    await renderOrders()

    const table = within(screen.getByTestId('admin-table'))
    expect(table.getByText('demo7a')).toBeInTheDocument()
    expect(table.getByText('#9（资料缺失）')).toBeInTheDocument()
    expect(table.getByText('已开通')).toBeInTheDocument()
    expect(table.getByText('交付失败')).toBeInTheDocument()
    expect(table.getAllByText('¥220.00')).toHaveLength(2)
  })

  it('状态筛选按 status 参数重新请求', async () => {
    const fetchMock = mockOrders()
    const user = userEvent.setup()
    await renderOrders()

    await user.click(screen.getByRole('button', { name: '交付失败' }))

    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(([input]) => String(input).includes('status=failed')),
      ).toBe(true)
    })
    await waitFor(() => {
      expect(screen.queryByText('O20261008143015K7Q2ZP')).not.toBeInTheDocument()
    })
    expect(screen.getByText('O20261008150000AAAAAA')).toBeInTheDocument()
  })

  it('类型下拉筛选（续费）按 type 参数重新请求', async () => {
    const fetchMock = mockOrders()
    const user = userEvent.setup()
    await renderOrders()

    await user.click(screen.getByRole('combobox', { name: '类型' }))
    await user.click(await screen.findByRole('option', { name: '续费' }))

    await waitFor(() => {
      expect(fetchMock.mock.calls.some(([input]) => String(input).includes('type=renew'))).toBe(true)
    })
    await waitFor(() => {
      expect(screen.queryByText('O20261008143015K7Q2ZP')).not.toBeInTheDocument()
    })
  })

  it('会员 ID 与单号筛选用查询按钮提交；非法会员 ID 被拦截', async () => {
    const fetchMock = mockOrders()
    const user = userEvent.setup()
    await renderOrders()

    // 非法会员 ID：提示且不发起筛选请求
    await user.type(screen.getByLabelText('会员 ID'), 'abc')
    await user.click(screen.getByRole('button', { name: '查询' }))
    expect(await screen.findByText('会员 ID 必须为正整数')).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => String(input).includes('member_id='))).toBe(false)

    // 合法 ID + 单号模糊匹配
    await user.clear(screen.getByLabelText('会员 ID'))
    await user.type(screen.getByLabelText('会员 ID'), '9')
    await user.type(screen.getByLabelText('订单号'), 'O2026100815')
    await user.click(screen.getByRole('button', { name: '查询' }))

    await waitFor(() => {
      const filtered = fetchMock.mock.calls.filter(([input]) => {
        const raw = String(input)
        return raw.includes('member_id=9') && raw.includes('trade_no=O2026100815')
      })
      expect(filtered.length).toBeGreaterThan(0)
    })
    await waitFor(() => {
      expect(screen.queryByText('O20261008143015K7Q2ZP')).not.toBeInTheDocument()
    })
    expect(screen.getByText('O20261008150000AAAAAA')).toBeInTheDocument()
  })
})
