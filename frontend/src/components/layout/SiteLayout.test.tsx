import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { makeAdminAccount, makeCatalog, makeMember } from '@/test/fixtures'
import {
  installFetchMock,
  ok,
  renderApp,
  seedAdminToken,
  seedMemberToken,
} from '@/test/harness'

// 官网页脚只保留给官网公开页：会员区（/console）与管理后台（/admin）不展示页脚。

const FOOTER_TEXT = /Lyidc_OEM/

function mockPublic() {
  return installFetchMock((url) => {
    if (url.pathname === '/api/v1/products') {
      return ok(makeCatalog())
    }
    if (url.pathname === '/api/v1/members/me') {
      return ok(makeMember())
    }
    if (url.pathname === '/api/v1/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    if (url.pathname === '/api/v1/instances') {
      return ok({ items: [], page: 1, page_size: 20, total: 0 })
    }
    if (url.pathname === '/api/v1/admin/profile') {
      return ok(makeAdminAccount())
    }
    if (url.pathname === '/api/v1/admin/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    return ok({ items: [], page: 1, page_size: 20, total: 0 })
  })
}

describe('布局 · 页脚可见性', () => {
  it('官网公开页展示页脚', async () => {
    mockPublic()
    renderApp(['/products'])
    await screen.findByRole('heading', { name: '商品列表' })

    expect(screen.getByText(FOOTER_TEXT)).toBeInTheDocument()
  })

  it('会员区不展示官网页脚（顶栏仍在）', async () => {
    mockPublic()
    seedMemberToken()
    renderApp(['/console/servers'])
    await screen.findByRole('heading', { name: '我的服务器' })

    expect(screen.queryByText(FOOTER_TEXT)).not.toBeInTheDocument()
    // 官网顶栏仍在（会员区只是隐藏页脚）
    expect(screen.getAllByRole('banner').length).toBeGreaterThan(0)
  })

  it('管理后台使用独立布局：无官网页脚与官网顶栏', async () => {
    mockPublic()
    seedAdminToken()
    renderApp(['/admin'])
    await screen.findByRole('heading', { name: '仪表盘' })

    expect(screen.queryByText(FOOTER_TEXT)).not.toBeInTheDocument()
    // 后台顶栏是自带的（含「管理后台」标识与账号菜单），没有官网导航
    expect(screen.getByText('管理后台')).toBeInTheDocument()
    expect(screen.queryByRole('navigation', { name: '主导航' })).not.toBeInTheDocument()
  })
})

// 顶栏「控制台」入口：登录后才渲染，且位于右侧操作区（DOM 顺序在主导航之后），主导航只剩公开项。
describe('布局 · 顶栏控制台入口', () => {
  it('未登录：顶栏不出现「控制台」，主导航仅首页/商品', async () => {
    mockPublic()
    renderApp(['/products'])
    await screen.findByRole('heading', { name: '商品列表' })

    const nav = screen.getByRole('navigation', { name: '主导航' })
    expect(within(nav).getAllByRole('link').map((link) => link.textContent)).toEqual(['首页', '商品'])
    expect(within(nav).queryByText(/控制台|会员区/)).not.toBeInTheDocument()

    // 未登录时整个顶栏既无「控制台」链接，也不再出现旧文案「会员区」（不再走登录 redirect）
    const header = screen.getByTestId('header-actions').closest('header')
    expect(header).not.toBeNull()
    expect(within(header as HTMLElement).queryByRole('link', { name: '控制台' })).not.toBeInTheDocument()
    expect(within(header as HTMLElement).queryByText(/控制台|会员区/)).not.toBeInTheDocument()
  })

  it('已登录：右侧操作区出现「控制台」（href=/console）且在主导航之后', async () => {
    mockPublic()
    seedMemberToken()
    renderApp(['/products'])

    const actions = screen.getByTestId('header-actions')
    const consoleLink = await within(actions).findByRole('link', { name: '控制台' })
    expect(consoleLink).toHaveAttribute('href', '/console')

    const nav = screen.getByRole('navigation', { name: '主导航' })
    expect(within(nav).queryByRole('link', { name: '控制台' })).not.toBeInTheDocument()
    expect(within(nav).getAllByRole('link').map((link) => link.textContent)).toEqual(['首页', '商品'])
    // 锁「放右边」：右侧操作区在 DOM 顺序上位于主导航之后
    expect(nav.compareDocumentPosition(actions) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('已登录：移动端面板在公开项之后展示「控制台」', async () => {
    mockPublic()
    seedMemberToken()
    renderApp(['/products'])
    // 等登录态生效（token → /members/me 回填）后再开面板
    await within(screen.getByTestId('header-actions')).findByRole('link', { name: '控制台' })

    await userEvent.setup().click(screen.getByRole('button', { name: '打开菜单' }))

    const mobileNav = screen.getByRole('navigation', { name: '移动端导航' })
    expect(within(mobileNav).getAllByRole('link').map((link) => link.textContent)).toEqual([
      '首页',
      '商品',
      '控制台',
    ])
  })

  it('未登录：移动端面板只保留公开项，无「控制台」', async () => {
    mockPublic()
    renderApp(['/products'])
    await screen.findByRole('heading', { name: '商品列表' })

    await userEvent.setup().click(screen.getByRole('button', { name: '打开菜单' }))

    const mobileNav = screen.getByRole('navigation', { name: '移动端导航' })
    expect(within(mobileNav).getAllByRole('link').map((link) => link.textContent)).toEqual(['首页', '商品'])
    expect(within(mobileNav).queryByText(/控制台|会员区/)).not.toBeInTheDocument()
  })
})
