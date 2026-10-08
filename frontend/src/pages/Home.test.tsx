import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { makeCatalog } from '@/test/fixtures'
import { fail, installFetchMock, ok, renderApp } from '@/test/harness'

function mockCatalog() {
  return installFetchMock((url) =>
    url.pathname === '/api/v1/products' ? ok(makeCatalog()) : undefined,
  )
}

describe('首页', () => {
  it('渲染 hero、卖点与购买流程', async () => {
    mockCatalog()
    renderApp(['/'])

    expect(screen.getByRole('heading', { level: 1, name: '岭云互联' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '为什么选择我们' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '购买流程' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '常见问题' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /浏览商品/ })).toBeInTheDocument()
    // 支付方式与开通承诺在流程区展示
    expect(screen.getByText('支付宝 / 微信 / 余额支付')).toBeInTheDocument()
  })

  it('热门商品展示接口返回的真实数据（名称与最低价）', async () => {
    mockCatalog()
    renderApp(['/'])

    expect(await screen.findByText('香港二区 CN2 A型')).toBeInTheDocument()
    expect(screen.getByText('襄阳云服务器-A型')).toBeInTheDocument()

    // 最低价周期展示：香港二区 CN2 A型 的月付 22.00
    const card = screen.getByText('香港二区 CN2 A型').closest('[data-slot="card"]')
    expect(card).not.toBeNull()
    expect(within(card as HTMLElement).getByText('¥22.00')).toBeInTheDocument()
    expect(within(card as HTMLElement).getByText('/ 月付起')).toBeInTheDocument()
  })

  it('点击「浏览商品」跳转商品列表页', async () => {
    mockCatalog()
    const user = userEvent.setup()
    const { router } = renderApp(['/'])

    await user.click(screen.getByRole('button', { name: /浏览商品/ }))

    await waitFor(() => expect(router.state.location.pathname).toBe('/products'))
  })

  it('商品接口失败时展示错误态与重试按钮', async () => {
    let attempts = 0
    installFetchMock((url) => {
      if (url.pathname !== '/api/v1/products') {
        return undefined
      }
      attempts += 1
      return attempts === 1 ? fail(500, '服务器内部错误', 500) : ok(makeCatalog())
    })

    renderApp(['/'])

    expect(await screen.findByText('加载失败')).toBeInTheDocument()
    expect(screen.getByText('服务器内部错误')).toBeInTheDocument()

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: '重新加载' }))

    expect(await screen.findByText('香港二区 CN2 A型')).toBeInTheDocument()
  })

  it('无已上架商品时展示空态', async () => {
    installFetchMock((url) =>
      url.pathname === '/api/v1/products' ? ok({ groups: [], total: 0 }) : undefined,
    )
    renderApp(['/'])

    expect(await screen.findByText('暂无已上架商品')).toBeInTheDocument()
  })
})
