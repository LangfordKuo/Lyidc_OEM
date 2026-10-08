import { screen } from '@testing-library/react'
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
