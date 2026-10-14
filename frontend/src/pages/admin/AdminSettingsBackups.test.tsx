import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'

import {
  makeAdminAccount,
  makeDbBackup,
  makeDbBackupList,
  makeDbBackupSettings,
  makeUpdateStatus,
} from '@/test/fixtures'
import type { DbBackupSettings } from '@/api/types'
import {
  fail,
  installFetchMock,
  ok,
  renderApp,
  requestBody,
  seedAdminProfile,
  seedAdminToken,
  type MockResponse,
} from '@/test/harness'

// 管理后台「设置」页的数据库备份分区（R14 / 契约 21.3 / 21.5 / 21.7）：
// 分区渲染、目录与占用展示、备份请求、恢复弹窗（密码必填 / 确认 / 不可撤销文案）、
// 删除确认、权限表现，以及升级弹窗新增的「先备份」提示。

const DB_LIST = makeDbBackupList()
const DB_BACKUP = makeDbBackup()

/** 组装设置页所需的全部接口；db 节点控制备份相关响应。 */
function mockSettingsPage(nodes: {
  role?: 'admin' | 'finance'
  list?: MockResponse | (() => MockResponse)
  create?: () => MockResponse
  restore?: () => MockResponse
  remove?: () => MockResponse
  updates?: MockResponse
  dirSettings?: MockResponse | (() => MockResponse)
  saveDir?: () => MockResponse
} = {}) {
  const role = nodes.role ?? 'admin'
  // 备份目录设置的「服务端状态」：保存成功后 GET 回读保存结果（与真实后端一致）。
  let savedDir: DbBackupSettings | null = null
  return installFetchMock((url, init) => {
    const path = url.pathname
    const method = init.method ?? 'GET'
    if (path === '/api/v1/admin/profile') {
      return ok(makeAdminAccount({ role, username: role }))
    }
    if (path === '/api/v1/admin/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    if (path === '/api/v1/admin/updates') {
      return nodes.updates ?? ok(makeUpdateStatus())
    }
    if (path === '/api/v1/admin/updates/job') {
      return ok(null)
    }
    if (path === '/api/v1/admin/settings/updates') {
      return ok({
        repo: 'LangfordKuo/Lyidc_OEM',
        github_token_configured: false,
        github_token_masked: '',
        api_base: '',
        download_base: '',
        updated_by: null,
        updated_at: null,
      })
    }
    if (path === '/api/v1/admin/db-backups' && method === 'GET') {
      const list = nodes.list ?? ok(DB_LIST)
      return typeof list === 'function' ? list() : list
    }
    if (path === '/api/v1/admin/db-backups' && method === 'POST') {
      return nodes.create ? nodes.create() : ok(makeDbBackup())
    }
    // 备份目录设置（R15，契约 21.10）：GET 读当前值；PUT 默认按请求体回显保存结果。
    if (path === '/api/v1/admin/settings/db_backup' && method === 'GET') {
      if (savedDir) {
        return ok(savedDir)
      }
      const settings = nodes.dirSettings ?? ok(makeDbBackupSettings())
      return typeof settings === 'function' ? settings() : settings
    }
    if (path === '/api/v1/admin/settings/db_backup' && method === 'PUT') {
      if (nodes.saveDir) {
        return nodes.saveDir()
      }
      const dir = (requestBody<{ dir?: string }>(init)?.dir ?? '').trim()
      savedDir =
        dir === ''
          ? makeDbBackupSettings()
          : makeDbBackupSettings({
              dir,
              default: false,
              resolved_dir: dir.startsWith('/') ? dir : `/opt/lyidc/${dir}`,
            })
      return ok(savedDir)
    }
    if (path.endsWith('/restore') && method === 'POST') {
      return nodes.restore
        ? nodes.restore()
        : ok({
            backup_id: DB_BACKUP.id,
            file_name: DB_BACKUP.file_name,
            database: 'lyidc',
            from_schema_version: 16,
            to_schema_version: 17,
            migrated: true,
            table_count: 17,
            dropped_tables: ['r14_extra'],
            restored_at: '2026-10-14T08:00:00Z',
            warning: '',
          })
    }
    if (path.startsWith('/api/v1/admin/db-backups/') && method === 'DELETE') {
      return nodes.remove
        ? nodes.remove()
        : ok({ deleted: true, id: DB_BACKUP.id, file_name: DB_BACKUP.file_name })
    }
    if (path === '/api/v1/admin/settings/payment/epay') {
      return ok({
        enabled: false,
        gateway: '',
        pid: '',
        key_configured: false,
        key_masked: '',
        notify_url: '',
        notify_url_recommended: '',
        return_url: '',
        updated_by: null,
        updated_at: null,
      })
    }
    if (path === '/api/v1/admin/settings/upstream') {
      return ok({
        base_url: '',
        base_url_fixed: true,
        username: '',
        api_key_configured: false,
        api_key_masked: '',
        timeout_seconds: 5,
        updated_by: null,
        updated_at: null,
      })
    }
    if (path === '/api/v1/admin/settings/email/smtp') {
      return ok({
        enabled: false,
        host: '',
        port: 0,
        port_effective: 25,
        username: '',
        password_configured: false,
        password_masked: '',
        from: '',
        from_name: '',
        encryption: 'none',
        updated_by: null,
        updated_at: null,
      })
    }
    if (path === '/api/v1/admin/settings/notifications') {
      return ok({
        inapp_enabled: true,
        email_enabled: false,
        expiry_reminder_enabled: true,
        expiry_reminder_days: 7,
        updated_by: null,
        updated_at: null,
      })
    }
    if (path === '/api/v1/admin/settings/referral') {
      return ok({
        enabled: false,
        default_percent: '0.00',
        max_coupon_percent: '100.00',
        updated_by: null,
        updated_at: null,
      })
    }
    return undefined
  })
}

async function renderSettings(
  mock: Parameters<typeof mockSettingsPage>[0] = {},
  role: 'admin' | 'finance' = 'admin',
) {
  const fetchMock = mockSettingsPage({ ...mock, role })
  seedAdminToken()
  seedAdminProfile({ role, username: role })
  renderApp(['/admin/settings'])
  if (role === 'admin') {
    await screen.findByRole('heading', { name: '设置' })
    await screen.findByTestId('db-backups-panel')
  } else {
    await screen.findByText('无权访问后台设置')
  }
  return fetchMock
}

/** 按方法 + 路径后缀统计接口调用。 */
function callsTo(fetchMock: ReturnType<typeof installFetchMock>, path: string, method?: string) {
  return fetchMock.mock.calls.filter(
    ([input, init]) =>
      String(input).endsWith(path) && (method === undefined || (init?.method ?? 'GET') === method),
  )
}

describe('管理后台 · 数据库备份（R14，契约 21）', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('分区渲染：库名、目录（在 Web 根外）、总占用与「不自动清理」说明', async () => {
    await renderSettings()

    const panel = screen.getByTestId('db-backups-panel')
    expect(panel).toHaveAttribute('id', 'db-backups')
    expect(within(panel).getByText('数据库备份')).toBeInTheDocument()
    expect(within(panel).getByText('lyidc')).toBeInTheDocument()
    expect(within(panel).getByText('/opt/lyidc/backups')).toBeInTheDocument()
    expect(within(panel).getByText('不在 Web 根内')).toBeInTheDocument()
    // 备份总占用（1 MiB → 1.0 MB）。
    expect(screen.getByTestId('db-backup-total-size')).toHaveTextContent('1.0 MB')
    expect(within(panel).getByText(/不会自动清理/)).toBeInTheDocument()
    // 列表行：文件名、时间、大小、schema 版本、操作人与版本。
    expect(within(panel).getByText(DB_BACKUP.file_name)).toBeInTheDocument()
    expect(within(panel).getByText('17 表 · 1234 行')).toBeInTheDocument()
    expect(within(panel).getByText('admin')).toBeInTheDocument()
    // 不提供任何下载 / 查看内容的交互入口（契约 21.4：绝不可从网站下载）。
    expect(within(panel).queryByRole('button', { name: /下载|查看/ })).not.toBeInTheDocument()
    expect(within(panel).queryByRole('link', { name: /下载|查看/ })).not.toBeInTheDocument()
  })

  it('目录落在 Web 根内：展示原因并禁用「立即备份」，不发备份请求', async () => {
    const fetchMock = await renderSettings({
      list: ok(
        makeDbBackupList({
          backup_dir: '/opt/lyidc/dist/backups',
          backup_dir_default: false,
          backup_dir_outside_web_root: false,
          backup_dir_message: '备份目录不能位于 Web 根（/opt/lyidc/dist）内',
          backups: [],
          total_size_bytes: 0,
        }),
      ),
    })
    const user = userEvent.setup()

    expect(await screen.findByText('备份目录不可用')).toBeInTheDocument()
    expect(screen.getByText(/不能位于 Web 根/)).toBeInTheDocument()
    const button = screen.getByTestId('db-backup-create')
    expect(button).toBeDisabled()
    await user.click(button)
    expect(callsTo(fetchMock, '/admin/db-backups', 'POST')).toHaveLength(0)
  })

  it('备份目录设置（R15）：展示解析路径与可写状态，并写明两条必读提示', async () => {
    await renderSettings()

    const form = await screen.findByTestId('db-backup-dir-form')
    expect(within(form).getByText('备份目录')).toBeInTheDocument()
    // 留空即缺省目录：显示缺省目录的实际路径。
    expect(within(form).getByText(/留空即使用缺省目录（\/opt\/lyidc\/backups）/)).toBeInTheDocument()
    // 改目录不搬走已有备份。
    expect(within(form).getByText(/改目录不会搬走已有备份/)).toBeInTheDocument()
    // 校验结果：解析后的绝对路径 + 不在 Web 根内 + 可写。
    expect(await screen.findByTestId('db-backup-dir-status')).toHaveTextContent(
      '目录可用：解析为 /opt/lyidc/backups，不在 Web 根内且可写（当前使用缺省目录）。',
    )
    // 配置留空时输入框为空（placeholder 展示缺省路径）。
    expect(within(form).getByLabelText('备份目录')).toHaveValue('')
  })

  it('保存备份目录：提交 {dir} 请求体，成功后重读列表（目录展示随之更新）', async () => {
    const fetchMock = await renderSettings()
    const user = userEvent.setup()

    await user.type(await screen.findByLabelText('备份目录'), '/srv/lyidc-bk')
    await user.click(screen.getByTestId('db-backup-dir-save'))

    const calls = await waitFor(() => {
      const found = callsTo(fetchMock, '/admin/settings/db_backup', 'PUT')
      expect(found).toHaveLength(1)
      return found
    })
    expect(requestBody(calls[0][1] as RequestInit)).toEqual({ dir: '/srv/lyidc-bk' })
    expect(await screen.findByText(/备份目录已保存：\/srv\/lyidc-bk/)).toBeInTheDocument()
    await waitFor(() =>
      expect(callsTo(fetchMock, '/admin/db-backups', 'GET').length).toBeGreaterThanOrEqual(2),
    )
  })

  it('留空保存：dir 为空串 = 恢复缺省目录', async () => {
    const fetchMock = await renderSettings({
      dirSettings: ok(
        makeDbBackupSettings({
          dir: '/srv/lyidc-bk',
          default: false,
          resolved_dir: '/srv/lyidc-bk',
        }),
      ),
    })
    const user = userEvent.setup()

    const input = await screen.findByLabelText('备份目录')
    expect(input).toHaveValue('/srv/lyidc-bk')
    await user.clear(input)
    await user.click(screen.getByTestId('db-backup-dir-save'))

    const calls = await waitFor(() => {
      const found = callsTo(fetchMock, '/admin/settings/db_backup', 'PUT')
      expect(found).toHaveLength(1)
      return found
    })
    expect(requestBody(calls[0][1] as RequestInit)).toEqual({ dir: '' })
    expect(await screen.findByText(/备份目录已恢复为缺省目录/)).toBeInTheDocument()
  })

  it('保存被拒（Web 根内 40002）：展示后端原因、输入保留、不谎报成功', async () => {
    const fetchMock = await renderSettings({
      saveDir: () =>
        fail(
          40002,
          '备份目录不可用: 备份目录不能位于 Web 根（/opt/lyidc/dist）内——备份含管理员密码哈希等敏感数据，绝不能被 Web 服务器读取',
          400,
        ),
    })
    const user = userEvent.setup()

    await user.type(await screen.findByLabelText('备份目录'), '/opt/lyidc/dist/bk')
    await user.click(screen.getByTestId('db-backup-dir-save'))

    expect(await screen.findByText(/不能位于 Web 根/)).toBeInTheDocument()
    expect(callsTo(fetchMock, '/admin/settings/db_backup', 'PUT')).toHaveLength(1)
    // 被拒绝的这次保存绝不出现成功提示（只断言该值，避免同文件里其他用例的 toast 残留干扰）。
    expect(screen.queryByText(/备份目录已保存：\/opt\/lyidc\/dist/)).not.toBeInTheDocument()
    // 输入值保留，管理员可据此改正。
    expect(screen.getByLabelText('备份目录')).toHaveValue('/opt/lyidc/dist/bk')
  })

  it('「立即备份」：进行中禁用并提示结果，成功后重新拉取列表', async () => {
    const fetchMock = await renderSettings()
    const user = userEvent.setup()

    await user.click(screen.getByTestId('db-backup-create'))
    await waitFor(() => expect(callsTo(fetchMock, '/admin/db-backups', 'POST')).toHaveLength(1))
    expect(await screen.findByText(/备份完成：lyidc-backup-/)).toBeInTheDocument()
    // 备份成功后重新读取列表（列表请求 ≥ 2 次：初始 + 刷新）。
    await waitFor(() =>
      expect(callsTo(fetchMock, '/admin/db-backups', 'GET').length).toBeGreaterThanOrEqual(2),
    )
  })

  it('备份失败：给出可读原因（目录不可用等），不谎报成功', async () => {
    await renderSettings({
      create: () => fail(40002, '备份目录不可用：/opt/lyidc/dist/backups 已存在且不是目录', 400),
    })
    const user = userEvent.setup()

    await user.click(screen.getByTestId('db-backup-create'))
    expect(await screen.findByText(/备份目录不可用/)).toBeInTheDocument()
  })

  it('恢复弹窗：不可撤销警告 + 密码必填（未填密码不发请求）', async () => {
    const fetchMock = await renderSettings()
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: '恢复' }))
    const dialog = await screen.findByRole('alertdialog')
    expect(within(dialog).getByText('恢复数据库（覆盖式）')).toBeInTheDocument()
    expect(within(dialog).getByText(DB_BACKUP.file_name)).toBeInTheDocument()
    expect(within(dialog).getByText('不可撤销', { exact: false })).toBeInTheDocument()
    expect(within(dialog).getByText(/失败可能留下不完整的库/)).toBeInTheDocument()
    expect(within(dialog).getByText(/强烈建议先点一次「立即备份」/)).toBeInTheDocument()
    expect(within(dialog).getByLabelText('当前管理员密码（必填）')).toBeInTheDocument()

    // 未填密码：只提示，不发请求，弹窗保持打开。
    await user.click(within(dialog).getByRole('button', { name: '确认恢复并覆盖' }))
    expect(await screen.findByText('请输入当前管理员密码以确认恢复')).toBeInTheDocument()
    expect(callsTo(fetchMock, '/restore', 'POST')).toHaveLength(0)
    expect(screen.getByRole('alertdialog')).toBeInTheDocument()
  })

  it('恢复确认：提交 confirm 与密码，成功后展示覆盖结果（含删除的表与迁移）', async () => {
    const fetchMock = await renderSettings()
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: '恢复' }))
    const dialog = await screen.findByRole('alertdialog')
    await user.type(within(dialog).getByLabelText('当前管理员密码（必填）'), 'admin123456')
    await user.click(within(dialog).getByRole('button', { name: '确认恢复并覆盖' }))

    const calls = await waitFor(() => {
      const found = callsTo(fetchMock, '/restore', 'POST')
      expect(found).toHaveLength(1)
      return found
    })
    expect(requestBody(calls[0][1] as RequestInit)).toEqual({
      confirm: true,
      password: 'admin123456',
    })
    const result = await screen.findByTestId('db-backup-restore-result')
    expect(result).toHaveTextContent('已用 lyidc-backup-')
    expect(result).toHaveTextContent('删除备份中不存在的表：r14_extra')
    expect(result).toHaveTextContent('已执行数据库迁移：16 → 17')
  })

  it('恢复被拒（密码错误 40002）：展示原因且弹窗保持打开，不误报成功', async () => {
    await renderSettings({
      restore: () => fail(40002, '管理员密码不正确，恢复已被拒绝', 400),
    })
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: '恢复' }))
    const dialog = await screen.findByRole('alertdialog')
    await user.type(within(dialog).getByLabelText('当前管理员密码（必填）'), 'wrong')
    await user.click(within(dialog).getByRole('button', { name: '确认恢复并覆盖' }))

    expect(await screen.findByText('管理员密码不正确，恢复已被拒绝')).toBeInTheDocument()
    expect(screen.queryByTestId('db-backup-restore-result')).not.toBeInTheDocument()
    expect(screen.getByRole('alertdialog')).toBeInTheDocument()
  })

  it('删除需二次确认：确认后按 ID 删除并刷新列表', async () => {
    const fetchMock = await renderSettings()
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: '删除' }))
    const dialog = await screen.findByRole('alertdialog')
    expect(within(dialog).getByText(/删除后无法恢复该备份文件/)).toBeInTheDocument()

    // 取消：不发请求。
    await user.click(within(dialog).getByRole('button', { name: '取消' }))
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument())
    expect(callsTo(fetchMock, `/admin/db-backups/${DB_BACKUP.id}`, 'DELETE')).toHaveLength(0)

    await user.click(screen.getByRole('button', { name: '删除' }))
    await user.click(
      within(await screen.findByRole('alertdialog')).getByRole('button', { name: '确认删除' }),
    )
    await waitFor(() =>
      expect(callsTo(fetchMock, `/admin/db-backups/${DB_BACKUP.id}`, 'DELETE')).toHaveLength(1),
    )
    expect(await screen.findByText(/已删除备份 lyidc-backup-/)).toBeInTheDocument()
  })

  it('文件已被手工删除：行内提示且「恢复」不可用', async () => {
    await renderSettings({
      list: ok(
        makeDbBackupList({ backups: [makeDbBackup({ file_exists: false, meta_missing: true })] }),
      ),
    })
    expect(await screen.findByText(/文件已不存在（被手工删除）/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '恢复' })).toBeDisabled()
  })

  it('非 admin 角色：整页无权，分区不渲染且不发任何备份请求', async () => {
    const fetchMock = await renderSettings({}, 'finance')
    await waitFor(() => expect(callsTo(fetchMock, '/admin/db-backups')).toHaveLength(0))
    expect(screen.queryByTestId('db-backups-panel')).not.toBeInTheDocument()
  })

  it('升级二次确认弹窗含「建议先备份数据库」提示与锚点链接（R14，契约 20.10 第 6 条）', async () => {
    await renderSettings({
      updates: ok(
        makeUpdateStatus({
          current_version: 'v0.9.0',
          latest_version: 'v0.10.0',
          release_name: 'v0.10.0',
          has_update: true,
          compare: 'ok',
        }),
      ),
    })
    const user = userEvent.setup()

    await user.click(await screen.findByTestId('upgrade-apply-button'))
    const dialog = await screen.findByRole('alertdialog')
    expect(within(dialog).getByText(/建议先备份数据库/)).toBeInTheDocument()
    const link = within(dialog).getByRole('link', { name: '「数据库备份」' })
    expect(link).toHaveAttribute('href', '#db-backups')
    // 只提示不阻断：确认按钮仍可用。
    expect(within(dialog).getByRole('button', { name: '开始升级' })).toBeEnabled()
  })
})
