import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import type { AdminRole } from '@/api/types'
import { makeAdminAccount, makeOperationLog } from '@/test/fixtures'
import {
  installFetchMock,
  ok,
  renderApp,
  fail,
  seedAdminProfile,
  seedAdminToken,
  type MockResponse,
} from '@/test/harness'

// 管理后台「日志」页（契约 23.4 / 23.8 / 23.11，R18 批 2）：
// 通道与 actor_id 的 URL 预置、高级搜索（关键字 / 时间范围 / 结果 / 操作人）的请求参数与前端校验、
// 命中较多的提示、分页、清空的二次确认（按通道 / 全部）与「没有按条删改控件」、非 admin 的无权占位。

const ADMIN_LOG = makeOperationLog()

const MEMBER_LOG = makeOperationLog({
  id: 2,
  channel: 'member',
  actor_id: 7,
  actor_name: 'demo7a',
  action: '会员登录',
  method: 'POST',
  path: '/api/v1/auth/login',
  ip: '198.51.100.7',
  duration_ms: 34,
  summary: '会员登录成功',
  created_at: '2026-10-14T08:00:00Z',
})

// 失败留痕：无名操作人（被鉴权拒绝的写请求）——结果要标红、操作人显示占位。
const FAILED_LOG = makeOperationLog({
  id: 3,
  channel: 'member',
  actor_id: 0,
  actor_name: '',
  action: 'POST /api/v1/members/me',
  method: 'POST',
  path: '/api/v1/members/me',
  http_status: 401,
  status: 'failed',
  ip: '203.0.113.66',
  duration_ms: 1,
  summary: '未认证或凭证无效',
})

const SCHEDULER_LOG = makeOperationLog({
  id: 4,
  channel: 'scheduler',
  actor_id: 0,
  actor_name: '',
  action: '定时任务：到期扫描',
  method: '',
  path: '',
  http_status: 0,
  duration_ms: 52,
  summary: '扫描完成：检查 3 台，暂停 1 台',
})

const ALL_LOGS = [ADMIN_LOG, MEMBER_LOG, FAILED_LOG, SCHEDULER_LOG]

/** 日志列表接口的返回视图（覆盖用）。 */
interface LogsView {
  items: typeof ALL_LOGS
  page: number
  page_size: number
  total: number
}

function mockBackend(
  options: { role?: AdminRole; logs?: (url: URL) => LogsView; clear?: () => MockResponse } = {},
) {
  const { role = 'admin', logs, clear } = options
  return installFetchMock((url, init) => {
    const path = url.pathname
    if (path === '/api/v1/admin/profile') {
      return ok(makeAdminAccount({ role, username: role }))
    }
    if (path === '/api/v1/admin/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    if (path === '/api/v1/admin/operation-logs' && (init.method ?? 'GET') === 'GET') {
      if (logs) {
        return ok(logs(url))
      }
      const channel = url.searchParams.get('channel')
      const status = url.searchParams.get('status')
      const actorID = url.searchParams.get('actor_id')
      let items = [...ALL_LOGS]
      if (channel) {
        items = items.filter((log) => log.channel === channel)
      }
      if (status) {
        items = items.filter((log) => log.status === status)
      }
      if (actorID) {
        items = items.filter((log) => String(log.actor_id) === actorID)
      }
      return ok({
        items,
        page: Number(url.searchParams.get('page') ?? '1'),
        page_size: 20,
        total: items.length,
      })
    }
    if (path === '/api/v1/admin/operation-logs' && init.method === 'DELETE') {
      if (clear) {
        return clear()
      }
      const channel = url.searchParams.get('channel') ?? ''
      return ok({
        channel,
        deleted: 3,
        record_channel: channel === 'admin' || channel === 'all' ? 'scheduler' : 'admin',
      })
    }
    return undefined
  })
}

/** 记录所有日志列表请求的查询串（断言请求参数用）。 */
function logQueries(fetchMock: ReturnType<typeof installFetchMock>): string[] {
  return fetchMock.mock.calls
    .filter(([input, init]) => String(input).includes('/admin/operation-logs') && (init?.method ?? 'GET') === 'GET')
    .map(([input]) => new URL(String(input), 'http://localhost').search)
}

async function renderLogs(
  path = '/admin/logs?channel=admin',
  options: { role?: AdminRole; logs?: (url: URL) => LogsView; clear?: () => MockResponse } = {},
) {
  const fetchMock = mockBackend(options)
  seedAdminToken()
  seedAdminProfile({ role: options.role ?? 'admin', username: options.role ?? 'admin' })
  renderApp([path])
  await screen.findByRole('heading', { name: '操作日志' })
  return fetchMock
}

describe('管理后台 · 操作日志', () => {
  it('渲染日志行：时间、操作人、动作、方法+路径、结果、IP、耗时与摘要；失败标红', async () => {
    mockBackend()
    seedAdminToken()
    seedAdminProfile()
    renderApp(['/admin/logs'])

    const table = within(await screen.findByTestId('admin-table'))
    // 管理员留痕
    expect(table.getByText('admin')).toBeInTheDocument()
    expect(table.getByText('修改站点地址设置')).toBeInTheDocument()
    expect(table.getByText('/api/v1/admin/settings/site')).toBeInTheDocument()
    expect(table.getAllByText('12 ms').length).toBeGreaterThan(0)
    expect(table.getByText(/已保存站点对外地址设置/)).toBeInTheDocument()
    // 会员留痕：用户名快照 + 会员 ID
    expect(table.getByText('demo7a')).toBeInTheDocument()
    expect(table.getByText('#7')).toBeInTheDocument()
    // 定时任务：无方法/路径、无名操作人 → 占位
    expect(table.getByText('定时任务：到期扫描')).toBeInTheDocument()
    expect(table.getByText('扫描完成：检查 3 台，暂停 1 台')).toBeInTheDocument()
    expect(table.getAllByText('—').length).toBeGreaterThan(0)

    // 结果：成功 / 失败（失败标红 = 走 statusTone 的 error 色）
    expect(table.getAllByText('成功').length).toBe(3)
    const failed = table.getByText('失败')
    expect(failed).toHaveClass('text-destructive')
    expect(table.getByText('HTTP 401')).toBeInTheDocument()
  })

  it('侧栏「日志」可展开三个通道入口，点击切换到对应 channel', async () => {
    const fetchMock = await renderLogs('/admin/logs')
    const user = userEvent.setup()

    const nav = within(screen.getByRole('navigation', { name: '管理后台导航' }))
    const logsEntry = nav.getByRole('button', { name: '日志' })
    // 在日志页时默认展开（否则侧栏看不到自己在哪一项）
    expect(logsEntry).toHaveAttribute('aria-expanded', 'true')
    const links = nav.getAllByRole('link')
    const labels = links.map((link) => link.textContent?.trim())
    expect(labels).toContain('用户日志')
    expect(labels).toContain('定时任务日志')
    expect(labels).toContain('管理员日志')

    await user.click(nav.getByRole('link', { name: '用户日志' }))
    await waitFor(() => {
      expect(logQueries(fetchMock).some((query) => query.includes('channel=member'))).toBe(true)
    })
    expect(screen.getAllByText(/当前通道：用户日志/).length).toBeGreaterThan(0)
  })

  it('URL 预置 channel 与 actor_id：进页即按这两个条件请求，输入框预填', async () => {
    const fetchMock = mockBackend()
    seedAdminToken()
    seedAdminProfile()
    renderApp(['/admin/logs?channel=member&actor_id=7'])

    expect(await screen.findByRole('heading', { name: '操作日志' })).toBeInTheDocument()
    expect(screen.getAllByText(/当前通道：用户日志/).length).toBeGreaterThan(0)
    expect(screen.getByLabelText('操作人 ID')).toHaveValue('7')

    await waitFor(() => {
      expect(logQueries(fetchMock)[0]).toContain('channel=member')
    })
    expect(logQueries(fetchMock)[0]).toContain('actor_id=7')

    // 预置条件下只剩该会员的留痕
    const table = within(await screen.findByTestId('admin-table'))
    expect(table.getByText('会员登录')).toBeInTheDocument()
    expect(table.queryByText('修改站点地址设置')).not.toBeInTheDocument()
  })

  it('关键字：至少 2 个字符（单字符前端就拦下，不发请求）；合法关键字按参数查询', async () => {
    const fetchMock = await renderLogs('/admin/logs?channel=member')
    const user = userEvent.setup()

    await user.type(screen.getByLabelText('关键字'), '上')
    await user.click(screen.getByRole('button', { name: '查询' }))

    expect(
      await screen.findByText('关键字至少 2 个字符（中文按字、英文按字母），且不能包含单字符的词'),
    ).toBeInTheDocument()
    expect(logQueries(fetchMock).some((query) => query.includes('keyword='))).toBe(false)

    await user.clear(screen.getByLabelText('关键字'))
    await user.type(screen.getByLabelText('关键字'), '会员登录')
    await user.click(screen.getByRole('button', { name: '查询' }))

    await waitFor(() => {
      expect(logQueries(fetchMock).some((query) => query.includes('keyword=%E4%BC%9A%E5%91%98%E7%99%BB%E5%BD%95'))).toBe(true)
    })
  })

  it('时间范围与结果筛选：起止日期（可留空）与 status 一并提交', async () => {
    const fetchMock = await renderLogs('/admin/logs?channel=member')
    const user = userEvent.setup()

    fireEvent.change(screen.getByLabelText('开始日期'), { target: { value: '2026-10-01' } })
    fireEvent.change(screen.getByLabelText('结束日期'), { target: { value: '2026-10-14' } })
    await user.click(screen.getByRole('button', { name: '查询' }))

    await waitFor(() => {
      const latest = logQueries(fetchMock).at(-1) ?? ''
      expect(latest).toContain('start=2026-10-01')
      expect(latest).toContain('end=2026-10-14')
    })

    await user.click(screen.getByRole('button', { name: '失败' }))
    await waitFor(() => {
      expect(logQueries(fetchMock).at(-1) ?? '').toContain('status=failed')
    })

    // 起晚于止：前端拦截（与后端 40002 同口径），不发请求
    const before = logQueries(fetchMock).length
    fireEvent.change(screen.getByLabelText('开始日期'), { target: { value: '2026-10-20' } })
    fireEvent.change(screen.getByLabelText('结束日期'), { target: { value: '2026-10-01' } })
    await user.click(screen.getByRole('button', { name: '查询' }))
    expect(await screen.findByText('开始日期不能晚于结束日期')).toBeInTheDocument()
    expect(logQueries(fetchMock).length).toBe(before)
  })

  it('清空筛选：清掉关键字/时间/结果/操作人并重新请求', async () => {
    const fetchMock = await renderLogs('/admin/logs?channel=member&actor_id=7')
    const user = userEvent.setup()

    await user.type(screen.getByLabelText('关键字'), '会员登录')
    await user.click(screen.getByRole('button', { name: '查询' }))
    await waitFor(() => {
      expect(logQueries(fetchMock).at(-1) ?? '').toContain('keyword=')
    })

    await user.click(screen.getByRole('button', { name: '清空筛选' }))

    await waitFor(() => {
      const latest = logQueries(fetchMock).at(-1) ?? ''
      expect(latest).not.toContain('keyword=')
      expect(latest).not.toContain('actor_id=')
      // 通道是页面上下文，不被「清空筛选」清掉
      expect(latest).toContain('channel=member')
    })
    expect(screen.getByLabelText('关键字')).toHaveValue('')
    expect(screen.getByLabelText('操作人 ID')).toHaveValue('')
  })

  it('命中较多时给出提示（建议加时间窗或通道过滤）', async () => {
    const big = makeOperationLog({ id: 9, channel: 'member', summary: '命中很多' })
    mockBackend({ logs: () => ({ items: [big], page: 1, page_size: 20, total: 5000 }) })
    seedAdminToken()
    seedAdminProfile()
    renderApp(['/admin/logs?channel=member'])
    const user = userEvent.setup()

    await screen.findByRole('heading', { name: '操作日志' })
    // 没有关键字时不提示（毫秒级路径，没有收窄的必要）
    expect(screen.queryByText('命中较多')).not.toBeInTheDocument()

    await user.type(screen.getByLabelText('关键字'), '会员')
    await user.click(screen.getByRole('button', { name: '查询' }))

    expect(await screen.findByText('命中较多')).toBeInTheDocument()
    expect(
      screen.getByText('命中 5000 条，建议加时间窗或通道过滤以加快检索，或换更精确的关键字'),
    ).toBeInTheDocument()
  })

  it('分页：翻页按 page 参数重新请求', async () => {
    const rows = Array.from({ length: 20 }, (_, index) =>
      makeOperationLog({ id: 100 + index, channel: 'member', summary: `第 ${index + 1} 条` }),
    )
    const fetchMock = mockBackend({
      logs: (url) => ({
        items: rows,
        page: Number(url.searchParams.get('page') ?? '1'),
        page_size: 20,
        total: 45,
      }),
    })
    seedAdminToken()
    seedAdminProfile()
    renderApp(['/admin/logs?channel=member'])
    const user = userEvent.setup()

    await screen.findByTestId('admin-table')
    await user.click(await screen.findByRole('button', { name: '第 2 页' }))

    await waitFor(() => {
      expect(logQueries(fetchMock).at(-1) ?? '').toContain('page=2')
    })
  })

  it('清空当前通道：二次确认后 DELETE 带该通道，成功后重新拉取列表', async () => {
    const fetchMock = await renderLogs('/admin/logs?channel=member')
    const user = userEvent.setup()
    const before = logQueries(fetchMock).length

    await user.click(screen.getByRole('button', { name: '清空当前通道' }))
    const dialog = within(await screen.findByRole('alertdialog'))
    expect(dialog.getByText('清空当前通道的操作日志')).toBeInTheDocument()
    expect(dialog.getByText(/全部日志/)).toBeInTheDocument()
    expect(dialog.getByText(/清空后不可恢复/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '确认清空' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) => String(input).includes('/admin/operation-logs') && init?.method === 'DELETE',
      )
      expect(call).toBeTruthy()
      expect(new URL(String(call?.[0]), 'http://localhost').searchParams.get('channel')).toBe('member')
    })
    // 清空后刷新列表（成功提示里带上删除条数与留痕通道）
    expect(await screen.findByText(/已清空用户日志，共删除 3 条日志/)).toBeInTheDocument()
    await waitFor(() => {
      expect(logQueries(fetchMock).length).toBeGreaterThan(before)
    })
  })

  it('清空全部：更强确认（写明不可恢复与自增归零），DELETE 带 channel=all', async () => {
    const fetchMock = await renderLogs('/admin/logs?channel=admin')
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: '清空全部' }))
    const dialog = within(await screen.findByRole('alertdialog'))
    expect(dialog.getByText('清空全部通道的操作日志')).toBeInTheDocument()
    expect(dialog.getByText(/全部三个通道/)).toBeInTheDocument()
    expect(dialog.getByText(/清空后不可恢复/)).toBeInTheDocument()
    expect(dialog.getByText(/自增 ID 会从 1 重新开始/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '确认清空全部' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) => String(input).includes('/admin/operation-logs') && init?.method === 'DELETE',
      )
      expect(call).toBeTruthy()
      expect(new URL(String(call?.[0]), 'http://localhost').searchParams.get('channel')).toBe('all')
    })
  })

  it('未指定通道时不能按通道清空（按钮禁用），清空全部仍可用', async () => {
    await renderLogs('/admin/logs')
    expect(screen.getByText(/当前通道：全部通道/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '清空当前通道' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '清空全部' })).toBeEnabled()
  })

  it('清空失败：保留弹窗并展示后端原因，不谎报成功', async () => {
    await renderLogs('/admin/logs?channel=member', { clear: () => fail(50001, '数据库错误', 500) })
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: '清空当前通道' }))
    await screen.findByRole('alertdialog')

    await user.click(screen.getByRole('button', { name: '确认清空' }))

    expect(await screen.findByText('数据库错误')).toBeInTheDocument()
    // 失败时弹窗保持打开（成功才关闭）
    expect(screen.getByRole('alertdialog')).toBeInTheDocument()
  })

  it('没有按条删除或编辑的控件（契约 23.4.3 的前端侧）', async () => {
    await renderLogs('/admin/logs')
    const table = within(await screen.findByTestId('admin-table'))

    // 行内一个按钮都没有：日志只允许整体清空，不存在按条删除 / 编辑 / 批量删选
    expect(table.queryAllByRole('button')).toHaveLength(0)
    expect(table.queryAllByRole('checkbox')).toHaveLength(0)
    expect(table.queryByText(/删除/)).not.toBeInTheDocument()
    expect(table.queryByText(/编辑/)).not.toBeInTheDocument()
  })

  it('finance 直连日志页：无权占位且不发任何日志请求', async () => {
    const fetchMock = mockBackend({ role: 'finance' })
    seedAdminToken()
    seedAdminProfile({ role: 'finance', username: 'finance' })
    renderApp(['/admin/logs?channel=admin'])

    expect(await screen.findByText('无权访问操作日志')).toBeInTheDocument()
    expect(screen.getByText(/无权查看与清空操作日志/)).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => String(input).includes('/admin/operation-logs'))).toBe(false)
    // 侧栏也没有「日志」入口
    const nav = within(screen.getByRole('navigation', { name: '管理后台导航' }))
    expect(nav.queryByRole('button', { name: '日志' })).not.toBeInTheDocument()
  })

  it('空态：没有留痕时给出可读提示', async () => {
    mockBackend({ logs: () => ({ items: [], page: 1, page_size: 20, total: 0 }) })
    seedAdminToken()
    seedAdminProfile()
    renderApp(['/admin/logs?channel=scheduler'])

    expect(await screen.findByText('暂无日志')).toBeInTheDocument()
  })
})
