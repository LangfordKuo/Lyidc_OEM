import { render, screen, waitFor } from '@testing-library/react'
import { createMemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import App from './App'
import { routes } from './app/routes'

// 路由级冒烟测试：用 Memory Router 指定入口路径，验证公开页面能渲染、会员区守卫能跳登录。
function envelopeResponse(payload: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(payload),
  } as unknown as Response
}

const catalogEnvelope = {
  code: 0,
  message: 'ok',
  data: {
    groups: [
      {
        id: 1,
        name: '香港二区',
        sort: 0,
        products: [
          {
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
          },
        ],
      },
    ],
    total: 1,
  },
}

function stubCatalog() {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation((url: string) => {
      if (url.startsWith('/api/v1/products')) {
        return Promise.resolve(envelopeResponse(catalogEnvelope))
      }
      return Promise.resolve(envelopeResponse({ code: 404, message: '资源不存在' }, 404))
    }),
  )
}

function renderApp(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] })
  return render(<App router={router} />)
}

describe('官网路由冒烟', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('首页渲染站点名、CTA 与精选商品', async () => {
    stubCatalog()

    renderApp('/')

    expect(screen.getByRole('heading', { level: 1, name: '岭云互联' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '浏览商品' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '进入控制台' })).toBeInTheDocument()

    await waitFor(() => {
      expect(screen.getAllByText('香港二区 CN2 A型').length).toBeGreaterThan(0)
    })
  })

  it('商品列表页展示分组、商品与最低价', async () => {
    stubCatalog()

    renderApp('/products')

    expect(await screen.findByRole('heading', { level: 1, name: '商品列表' })).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.getByText('¥22.00')).toBeInTheDocument()
    })
    expect(screen.getAllByText('香港二区').length).toBeGreaterThan(0)
  })

  it('未登录访问会员区会跳转登录页并带上 redirect', async () => {
    stubCatalog()

    renderApp('/console/orders')

    expect(await screen.findByRole('heading', { level: 1, name: '登录会员账号' })).toBeInTheDocument()
  })

  it('未登录访问下单页会跳转登录页', async () => {
    stubCatalog()

    renderApp('/checkout/7')

    expect(await screen.findByRole('heading', { level: 1, name: '登录会员账号' })).toBeInTheDocument()
  })

  it('未知路由展示 404 页面', async () => {
    stubCatalog()

    renderApp('/not-exist')

    expect(await screen.findByText('404 · 页面不存在')).toBeInTheDocument()
  })
})
