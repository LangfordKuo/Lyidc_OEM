import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { makeAdminAccount, makeLedgerEntry, makeMember, makeRecharge } from '@/test/fixtures'
import {
  installFetchMock,
  ok,
  renderApp,
  requestBody,
  seedAdminProfile,
  seedAdminToken,
  type FetchHandler,
} from '@/test/harness'

// 管理后台「会员」页：搜索参数、禁用/启用二次确认（含请求体）、详情弹窗的对账数据与角色矩阵。

const MEMBER = makeMember({
  id: 1,
  username: 'demo7a',
  email: 'demo7a@example.com',
  balance: '250.00',
  rebate_percent_override: '10.00',
})
const DISABLED = makeMember({
  id: 2,
  username: 'banned',
  email: 'banned@example.com',
  status: 'disabled',
})

function mockMembers(role: 'admin' | 'finance' | 'support' = 'admin', handler?: FetchHandler) {
  return installFetchMock((url, init) => {
    const path = url.pathname
    if (path === '/api/v1/admin/profile') {
      return ok(makeAdminAccount({ role, username: role }))
    }
    if (path === '/api/v1/admin/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    if (path === '/api/v1/admin/members') {
      const status = url.searchParams.get('status')
      const username = url.searchParams.get('username')
      let items = [MEMBER, DISABLED]
      if (status) {
        items = items.filter((member) => member.status === status)
      }
      if (username) {
        items = items.filter((member) => member.username.includes(username))
      }
      return ok({ items, page: 1, page_size: 20, total: items.length })
    }
    if (path === '/api/v1/admin/ledger') {
      return ok({ items: [makeLedgerEntry()], page: 1, page_size: 10, total: 1 })
    }
    if (path === '/api/v1/admin/recharges') {
      return ok({ items: [makeRecharge()], page: 1, page_size: 10, total: 1 })
    }
    if (path === '/api/v1/admin/members/1/status' && init.method === 'PUT') {
      return ok({ ...MEMBER, status: 'disabled' })
    }
    if (path === '/api/v1/admin/members/1' && init.method === 'PUT') {
      return ok(MEMBER)
    }
    return handler?.(url, init)
  })
}

async function renderMembers(role: 'admin' | 'finance' | 'support' = 'admin') {
  const fetchMock = mockMembers(role)
  seedAdminToken()
  seedAdminProfile({ role, username: role })
  renderApp(['/admin/members'])
  await screen.findByRole('heading', { name: '会员' })
  // 用户名与昵称同为 demo7a
  await screen.findAllByText('demo7a')
  return fetchMock
}

describe('管理后台 · 会员管理', () => {
  it('渲染列表并支持按用户名与状态筛选（请求参数）', async () => {
    const fetchMock = await renderMembers()
    const user = userEvent.setup()

    const table = within(screen.getByTestId('admin-table'))
    expect(table.getByText('demo7a@example.com')).toBeInTheDocument()
    expect(table.getByText('正常')).toBeInTheDocument()
    expect(table.getByText('已禁用')).toBeInTheDocument()

    await user.type(screen.getByLabelText('用户名'), 'banned')
    await user.click(screen.getByRole('button', { name: '查询' }))

    await waitFor(() => {
      expect(fetchMock.mock.calls.some(([input]) => String(input).includes('username=banned'))).toBe(
        true,
      )
    })
    await waitFor(() => {
      expect(screen.queryByText('demo7a@example.com')).not.toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '已禁用' }))
    await waitFor(() => {
      expect(fetchMock.mock.calls.some(([input]) => String(input).includes('status=disabled'))).toBe(
        true,
      )
    })
  })

  it('禁用会员：二次确认后 PUT status=disabled 并刷新', async () => {
    const fetchMock = await renderMembers()
    const user = userEvent.setup()

    const row = within(screen.getByTestId('admin-table'))
    await user.click(row.getAllByRole('button', { name: '禁用' })[0])

    expect(await screen.findByText('禁用会员')).toBeInTheDocument()
    expect(screen.getByText(/禁用后该会员无法登录/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '确认禁用' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/api/v1/admin/members/1/status') && init?.method === 'PUT',
      )
      expect(call).toBeTruthy()
      expect(requestBody(call?.[1] as RequestInit)).toEqual({ status: 'disabled' })
    })
  })

  it('详情弹窗：展示余额、余额流水与充值单（admin）', async () => {
    await renderMembers()
    const user = userEvent.setup()

    await user.click(screen.getAllByRole('button', { name: '详情' })[0])

    expect(await screen.findByText('会员详情 · demo7a')).toBeInTheDocument()
    expect(screen.getByText('账户余额')).toBeInTheDocument()
    // 列表行与详情弹窗各展示一次余额
    expect(screen.getAllByText('¥250.00').length).toBeGreaterThan(0)
    expect(await screen.findByText('余额流水')).toBeInTheDocument()
    expect(screen.getByText('充值入账')).toBeInTheDocument()
    expect(screen.getByText('充值单')).toBeInTheDocument()
    expect(screen.getByText('R20261008143500M3P8QT')).toBeInTheDocument()
  })

  it('客服角色：禁用按钮禁用、详情不请求对账数据并给出权限说明', async () => {
    const fetchMock = await renderMembers('support')
    const user = userEvent.setup()

    expect(screen.getAllByRole('button', { name: '禁用' })[0]).toBeDisabled()
    expect(screen.getByText(/无权禁用或启用会员/)).toBeInTheDocument()

    await user.click(screen.getAllByRole('button', { name: '详情' })[0])
    expect(await screen.findByText('财务对账数据不可见')).toBeInTheDocument()

    // 不发起流水/充值单请求（避免无谓的 403）
    expect(
      fetchMock.mock.calls.some(
        ([input]) =>
          String(input).includes('/admin/ledger') || String(input).includes('/admin/recharges'),
      ),
    ).toBe(false)
  })

  it('返佣特批比例：列表展示已特批值，设值提交字符串三态', async () => {
    const fetchMock = await renderMembers()
    const user = userEvent.setup()

    // 列表列展示「已特批 10.00%」
    expect(screen.getByText('已特批 10.00%')).toBeInTheDocument()

    await user.click(screen.getAllByRole('button', { name: '详情' })[0])
    await screen.findByText('会员详情 · demo7a')

    const input = screen.getByLabelText('比例（%）· 留空并点保存 = 清空')
    await user.clear(input)
    await user.type(input, '25.50')
    await user.click(screen.getByRole('button', { name: '保存特批比例' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/api/v1/admin/members/1') &&
          !String(input).includes('/status') &&
          init?.method === 'PUT',
      )
      expect(call).toBeTruthy()
      expect(requestBody(call?.[1] as RequestInit)).toEqual({ rebate_percent_override: '25.50' })
    })
  })

  it('返佣特批比例：清空提交 null，不点保存则不改（不发请求）', async () => {
    const fetchMock = await renderMembers()
    const user = userEvent.setup()

    await user.click(screen.getAllByRole('button', { name: '详情' })[0])
    await screen.findByText('会员详情 · demo7a')

    // 不改：只打开详情不保存 → 不产生 PUT /admin/members/1
    expect(
      fetchMock.mock.calls.some(
        ([input, init]) =>
          String(input).includes('/api/v1/admin/members/1') &&
          !String(input).includes('/status') &&
          init?.method === 'PUT',
      ),
    ).toBe(false)

    await user.click(screen.getByRole('button', { name: '清空' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/api/v1/admin/members/1') &&
          !String(input).includes('/status') &&
          init?.method === 'PUT',
      )
      expect(call).toBeTruthy()
      expect(requestBody(call?.[1] as RequestInit)).toEqual({ rebate_percent_override: null })
    })
  })

  it('会员行的「日志」入口跳到日志页并预置 channel=member 与 actor_id（契约 23.11）', async () => {
    const fetchMock = await renderMembers()
    const user = userEvent.setup()

    await user.click(screen.getAllByRole('link', { name: '日志' })[0])

    expect(await screen.findByRole('heading', { name: '操作日志' })).toBeInTheDocument()
    await waitFor(() => {
      const call = fetchMock.mock.calls.find(([input]) =>
        String(input).includes('/api/v1/admin/operation-logs'),
      )
      expect(call).toBeTruthy()
      const url = new URL(String(call?.[0]), 'http://localhost')
      expect(url.searchParams.get('channel')).toBe('member')
      expect(url.searchParams.get('actor_id')).toBe('1')
    })
    expect(screen.getAllByText(/当前通道：用户日志/).length).toBeGreaterThan(0)
    expect(screen.getByLabelText('操作人 ID')).toHaveValue('1')
  })

  it('返佣特批比例：客服只读，保存与清空按钮禁用', async () => {
    const fetchMock = await renderMembers('support')
    const user = userEvent.setup()

    await user.click(screen.getAllByRole('button', { name: '详情' })[0])
    await screen.findByText('会员详情 · demo7a')

    expect(screen.getByRole('button', { name: '保存特批比例' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '清空' })).toBeDisabled()

    await user.click(screen.getByRole('button', { name: '保存特批比例' }))
    expect(
      fetchMock.mock.calls.some(
        ([input, init]) =>
          String(input).includes('/api/v1/admin/members/1') &&
          !String(input).includes('/status') &&
          init?.method === 'PUT',
      ),
    ).toBe(false)
  })
})
