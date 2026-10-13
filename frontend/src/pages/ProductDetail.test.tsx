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
  it('渲染商品信息与配置项；不再展示配置速览与价格一览', async () => {
    mockDetail()
    renderApp(['/products/1'])

    expect(await screen.findByRole('heading', { level: 1, name: '香港二区 CN2 A型' })).toBeInTheDocument()
    // R9-C：「商品说明」区块已移除，接口 description 不再渲染
    expect(screen.queryByRole('heading', { name: '商品说明' })).not.toBeInTheDocument()
    expect(screen.queryByText('该商品暂无描述。')).not.toBeInTheDocument()
    // R11：「配置速览」（description_lines 清单）不再进详情页——那是给外面的人看的。
    // 商品卡（列表页）的配置简介不动，接口 description_lines 仍下发。
    expect(screen.queryByTestId('product-config-overview')).not.toBeInTheDocument()
    expect(screen.queryByText('CPU:2核心')).not.toBeInTheDocument()
    // R11：「价格一览」删除（选择计费周期处已有价格）
    expect(screen.queryByRole('heading', { name: '价格一览' })).not.toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    // 配置项（上游文案去前缀）仍在
    expect(screen.getByRole('heading', { name: '配置项' })).toBeInTheDocument()
    expect(screen.getByText('区域')).toBeInTheDocument()
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

  it('不可售周期（两年付/三年付）直接不渲染', async () => {
    mockDetail()
    renderApp(['/products/1'])

    await screen.findByRole('heading', { level: 1, name: '香港二区 CN2 A型' })
    // R11：不可售周期不显示（此前是置灰），只渲染 4 个可售周期。
    expect(screen.queryByRole('radio', { name: /两年付/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('radio', { name: /三年付/ })).not.toBeInTheDocument()
    expect(screen.queryByText('不可售')).not.toBeInTheDocument()
    // 配置项横条也是 radio，这里只数周期选择器内的档位。
    const cycles = within(screen.getByRole('radiogroup', { name: '计费周期' }))
    expect(cycles.getAllByRole('radio')).toHaveLength(4)
    // 档数文案与新行为一致
    expect(screen.getByText('仅展示可售周期，共 4 档')).toBeInTheDocument()
  })

  it('全部周期不可售时出空态并禁用下单（极端数据降级）', async () => {
    installFetchMock((url) => {
      if (url.pathname === '/api/v1/products/1') {
        return ok(
          makeProductDetail({
            prices: {
              monthly: null,
              quarterly: null,
              semiannual: null,
              annual: null,
              biennial: null,
              triennial: null,
            },
          }),
        )
      }
      return undefined
    })
    renderApp(['/products/1'])

    expect(await screen.findByTestId('cycle-selector-empty')).toHaveTextContent('暂无可售的计费周期')
    // 空态取代整个周期控件，不留空的 radiogroup。
    expect(screen.queryByRole('radiogroup', { name: '计费周期' })).not.toBeInTheDocument()
    const buy = screen.getByRole('button', { name: /当前周期不可售/ })
    expect(buy).toBeDisabled()
    expect(screen.getByText('该商品暂无可售周期')).toBeInTheDocument()
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
