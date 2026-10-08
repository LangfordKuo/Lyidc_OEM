import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { makeInstance, makeMember } from '@/test/fixtures'
import { installFetchMock, ok, renderApp, seedMemberToken } from '@/test/harness'

// 会员区「我的服务器」列表：渲染、状态筛选（请求参数）与空态。

function dueIn(days: number): string {
  return new Date(Date.now() + days * 86_400_000).toISOString()
}

const ACTIVE = makeInstance({ id: 101, name: 'oem-active-101', next_due_date: dueIn(3) })
const SUSPENDED = makeInstance({
  id: 102,
  name: 'oem-suspended-102',
  status: 'suspended',
  next_due_date: dueIn(30),
})
const TERMINATED = makeInstance({
  id: 103,
  name: 'oem-terminated-103',
  status: 'terminated',
  next_due_date: null,
})

/** 按 status 查询参数返回不同集合，模拟真实后端筛选。 */
function mockInstances() {
  return installFetchMock((url) => {
    if (url.pathname === '/api/v1/members/me') {
      return ok(makeMember())
    }
    if (url.pathname === '/api/v1/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    if (url.pathname === '/api/v1/instances') {
      const status = url.searchParams.get('status')
      const all = [ACTIVE, SUSPENDED, TERMINATED]
      const items = status ? all.filter((item) => item.status === status) : all
      return ok({ items, page: 1, page_size: 20, total: items.length })
    }
    return undefined
  })
}

async function renderServers() {
  seedMemberToken()
  renderApp(['/console/servers'])
  await screen.findByRole('heading', { name: '我的服务器' })
}

describe('会员区 · 我的服务器列表', () => {
  it('渲染实例卡片：名称、商品、状态与临期到期文案', async () => {
    mockInstances()
    await renderServers()

    expect(await screen.findByText('oem-active-101')).toBeInTheDocument()
    expect(screen.getByText('oem-suspended-102')).toBeInTheDocument()
    expect(screen.getByText('oem-terminated-103')).toBeInTheDocument()

    // 状态徽标（在卡片内查询，避免与上方筛选按钮的同名文案冲突）
    expect(within(screen.getByTestId('instance-card-101')).getByText('运行中')).toBeInTheDocument()
    expect(within(screen.getByTestId('instance-card-102')).getByText('已暂停')).toBeInTheDocument()
    expect(within(screen.getByTestId('instance-card-103')).getByText('已终止')).toBeInTheDocument()

    // 临期高亮：3 天后到期属于 7 天窗口，显示「3 天后到期」
    expect(screen.getByText(/3 天后到期/)).toBeInTheDocument()
    // 已终止实例无到期时间，显示占位
    expect(within(screen.getByTestId('instance-card-103')).getAllByText('—').length).toBeGreaterThan(0)
  })

  it('点击状态筛选后按 status 重新请求并只展示对应实例', async () => {
    const fetchMock = mockInstances()
    const user = userEvent.setup()
    await renderServers()
    await screen.findByText('oem-active-101')

    await user.click(screen.getByRole('button', { name: '已暂停' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(([input]) =>
        String(input).includes('/api/v1/instances?'),
      )
      expect(call).toBeTruthy()
    })
    const filtered = fetchMock.mock.calls.filter(([input]) =>
      String(input).includes('status=suspended'),
    )
    expect(filtered.length).toBeGreaterThan(0)

    await waitFor(() => {
      expect(screen.queryByText('oem-active-101')).not.toBeInTheDocument()
    })
    expect(screen.getByText('oem-suspended-102')).toBeInTheDocument()
  })

  it('筛选结果为空时展示空态并提示换筛选', async () => {
    installFetchMock((url) => {
      if (url.pathname === '/api/v1/members/me') {
        return ok(makeMember())
      }
      if (url.pathname === '/api/v1/notifications/unread-count') {
        return ok({ unread: 0 })
      }
      if (url.pathname === '/api/v1/instances') {
        return ok({ items: [], page: 1, page_size: 20, total: 0 })
      }
      return undefined
    })
    const user = userEvent.setup()
    await renderServers()

    expect(await screen.findByText('还没有服务器')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '已终止' }))
    expect(await screen.findByText('该状态下没有实例')).toBeInTheDocument()
  })

  it('未登录访问会员区跳登录并带 redirect', async () => {
    mockInstances()
    const { router } = renderApp(['/console/servers'])

    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
    expect(router.state.location.search).toBe('?redirect=%2Fconsole%2Fservers')
  })
})
