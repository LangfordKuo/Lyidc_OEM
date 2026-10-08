import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { makeAdminAccount, makeAdminInstance } from '@/test/fixtures'
import { installFetchMock, ok, renderApp, seedAdminProfile, seedAdminToken } from '@/test/harness'

// 管理后台「实例」列表：会员/状态筛选（请求参数）、敏感字段不在列表出现。

const ACTIVE = makeAdminInstance({ id: 101, name: 'oem-active-101', member_id: 1 })
const SUSPENDED = makeAdminInstance({
  id: 102,
  name: 'oem-suspended-102',
  member_id: 9,
  status: 'suspended',
})

function mockInstances() {
  return installFetchMock((url) => {
    const path = url.pathname
    if (path === '/api/v1/admin/profile') {
      return ok(makeAdminAccount())
    }
    if (path === '/api/v1/admin/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    if (path === '/api/v1/admin/instances') {
      const status = url.searchParams.get('status')
      const memberID = url.searchParams.get('member_id')
      let items = [ACTIVE, SUSPENDED]
      if (status) {
        items = items.filter((item) => item.status === status)
      }
      if (memberID) {
        items = items.filter((item) => String(item.member_id) === memberID)
      }
      return ok({ items, page: 1, page_size: 20, total: items.length })
    }
    return undefined
  })
}

async function renderInstances() {
  const fetchMock = mockInstances()
  seedAdminToken()
  seedAdminProfile()
  renderApp(['/admin/instances'])
  await screen.findByRole('heading', { name: '实例' })
  await screen.findByText('oem-active-101')
  return fetchMock
}

describe('管理后台 · 实例列表', () => {
  it('渲染实例行（不含主机凭据等敏感字段）', async () => {
    await renderInstances()

    const table = within(screen.getByTestId('admin-table'))
    expect(table.getByText('oem-active-101')).toBeInTheDocument()
    expect(table.getByText('oem-suspended-102')).toBeInTheDocument()
    expect(table.getAllByText('203.0.113.9').length).toBeGreaterThan(0)
    // 列表不下发密码：DOM 中不出现主机密码
    expect(screen.queryByText('Abcd1234Efgh5678')).not.toBeInTheDocument()
    expect(screen.queryByText('root')).not.toBeInTheDocument()
  })

  it('状态与会员 ID 筛选按查询参数重新请求', async () => {
    const fetchMock = await renderInstances()
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: '已暂停' }))
    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(([input]) => String(input).includes('status=suspended')),
      ).toBe(true)
    })
    await waitFor(() => {
      expect(screen.queryByText('oem-active-101')).not.toBeInTheDocument()
    })

    await user.type(screen.getByLabelText('会员 ID'), 'abc')
    await user.click(screen.getByRole('button', { name: '查询' }))
    expect(await screen.findByText('会员 ID 必须为正整数')).toBeInTheDocument()

    await user.clear(screen.getByLabelText('会员 ID'))
    await user.type(screen.getByLabelText('会员 ID'), '1')
    await user.click(screen.getByRole('button', { name: '查询' }))
    await waitFor(() => {
      expect(fetchMock.mock.calls.some(([input]) => String(input).includes('member_id=1'))).toBe(
        true,
      )
    })
  })
})
