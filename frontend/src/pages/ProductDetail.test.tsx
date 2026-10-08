import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { makeProductDetail } from '@/test/fixtures'
import { fail, installFetchMock, ok, renderApp, seedMemberToken } from '@/test/harness'
import { makeMember } from '@/test/fixtures'

function mockDetail() {
  return installFetchMock((url) => {
    if (url.pathname === '/api/v1/products/1') {
      return ok(makeProductDetail())
    }
    if (url.pathname === '/api/v1/members/me') {
      return ok(makeMember())
    }
    return undefined
  })
}

describe('商品详情页', () => {
  it('渲染商品信息、说明、配置项与六周期价格表', async () => {
    mockDetail()
    renderApp(['/products/1'])

    expect(await screen.findByRole('heading', { level: 1, name: '香港二区 CN2 A型' })).toBeInTheDocument()
    // 说明里的 HTML 实体被反转义后以纯文本展示
    expect(screen.getByText(/CPU:2核心/)).toBeInTheDocument()
    // 配置项（上游文案去前缀）
    expect(screen.getByText('区域')).toBeInTheDocument()
    // 价格表六个周期都在（两年付/三年付不可售）
    expect(screen.getByRole('heading', { name: '价格一览' })).toBeInTheDocument()
    const priceTable = screen.getByRole('table')
    expect(within(priceTable).getAllByText('两年付').length).toBeGreaterThan(0)
    expect(within(priceTable).getAllByText('不可售')).toHaveLength(2)
  })

  it('默认选中最低价周期（月付），切换周期后价格与月均联动', async () => {
    mockDetail()
    const user = userEvent.setup()
    renderApp(['/products/1'])

    await screen.findByRole('heading', { level: 1, name: '香港二区 CN2 A型' })
    const summary = screen.getByTestId('product-summary')
    expect(within(summary).getByTestId('current-cycle-price')).toHaveTextContent('¥22.00')

    await user.click(screen.getByRole('radio', { name: /^年付/ }))

    expect(within(summary).getByTestId('current-cycle-price')).toHaveTextContent('¥220.00')
    expect(within(summary).getByText(/折合约 ¥18.33 \/ 月/)).toBeInTheDocument()
  })

  it('不可售周期（两年付）禁用不可选', async () => {
    mockDetail()
    renderApp(['/products/1'])

    const biennial = await screen.findByRole('radio', { name: /两年付/ })
    expect(biennial).toBeDisabled()
  })

  it('未登录点「立即购买」先跳登录并带 redirect 回跳', async () => {
    mockDetail()
    const user = userEvent.setup()
    const { router } = renderApp(['/products/1'])

    await user.click(await screen.findByRole('button', { name: /立即购买/ }))

    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
    expect(router.state.location.search).toBe('?redirect=%2Fcheckout%2F1')
  })

  it('已登录点「立即购买」直达结算页（携带周期与配置草稿）', async () => {
    seedMemberToken()
    mockDetail()
    const user = userEvent.setup()
    const { router } = renderApp(['/products/1'])

    await user.click(await screen.findByRole('radio', { name: /^季付/ }))
    await user.click(screen.getByRole('button', { name: /立即购买/ }))

    await waitFor(() => expect(router.state.location.pathname).toBe('/checkout/1'))
  })

  it('商品不存在（404）时展示下架提示', async () => {
    installFetchMock((url) =>
      url.pathname === '/api/v1/products/999' ? fail(404, '商品不存在', 404) : undefined,
    )
    renderApp(['/products/999'])

    expect(await screen.findByText('商品不存在或已下架')).toBeInTheDocument()
  })
})
