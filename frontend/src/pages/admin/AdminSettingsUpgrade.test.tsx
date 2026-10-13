import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'

import { resetAdminUpdateStatus } from '@/hooks/useAdminUpdateStatus'
import { makeAdminAccount, makeUpdateJob, makeUpdateStatus } from '@/test/fixtures'
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

// 管理后台「设置」页的一键升级（R13-B / 契约 20.3 / 20.4 / 20.10）：
// 二次确认弹窗、目标版本回传、进行中互斥、阶段进度渲染、失败与回滚展示、角色权限。

const PLATFORM_ASSET = {
  name: 'lyidc-oem-v0.10.0-linux-amd64.tar.gz',
  size: 12000000,
  download_url:
    'https://github.com/LangfordKuo/Lyidc_OEM/releases/download/v0.10.0/lyidc-oem-v0.10.0-linux-amd64.tar.gz',
  platform_match: true,
}

/** 有新版可升级的检查结果（linux-amd64 服务器平台）。 */
const NEWER_RELEASE = {
  has_update: true,
  latest_version: 'v0.10.0',
  release_name: 'v0.10.0',
  published_at: '2026-10-10T12:00:00Z',
  notes: '修复导入分组的映射问题',
  html_url: 'https://github.com/LangfordKuo/Lyidc_OEM/releases/tag/v0.10.0',
  assets: [PLATFORM_ASSET],
}

const UPDATES_SETTINGS = {
  repo: 'LangfordKuo/Lyidc_OEM',
  github_token_configured: false,
  github_token_masked: '',
  api_base: '',
  download_base: '',
  updated_by: null,
  updated_at: null,
}

interface UpgradeMock {
  /** GET /admin/updates/job 的响应序列（依次返回，用尽后重复最后一次）。 */
  jobs?: MockResponse[]
  /** POST /admin/updates/apply 的响应（缺省返回进行中的任务）。 */
  apply?: () => MockResponse
}

/** 组装设置页所需的全部接口；upgrade.jobs 用于模拟「刷新后仍能看到进行中/已完成」。 */
function mockUpgrade(nodes: {
  role?: 'admin' | 'finance'
  check?: MockResponse | Promise<MockResponse>
  cache?: MockResponse
}) {
  const role = nodes.role ?? 'admin'
  let jobIndex = 0
  return installFetchMock((url) => {
    const path = url.pathname
    if (path === '/api/v1/admin/profile') {
      return ok(makeAdminAccount({ role, username: role }))
    }
    if (path === '/api/v1/admin/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    if (path === '/api/v1/admin/updates') {
      return nodes.cache ?? ok(makeUpdateStatus(NEWER_RELEASE))
    }
    if (path === '/api/v1/admin/updates/check') {
      return nodes.check === undefined ? ok(makeUpdateStatus(NEWER_RELEASE)) : nodes.check
    }
    if (path === '/api/v1/admin/updates/job') {
      const queue = upgrade.jobs ?? []
      if (queue.length === 0) {
        return ok(null)
      }
      const response = queue[Math.min(jobIndex, queue.length - 1)]
      jobIndex += 1
      return response
    }
    if (path === '/api/v1/admin/updates/apply') {
      return upgrade.apply ? upgrade.apply() : ok(makeUpdateJob({ progress: 20 }))
    }
    if (path === '/api/v1/admin/settings/updates') {
      return ok(UPDATES_SETTINGS)
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

let upgrade: UpgradeMock = {}

async function renderUpgradePage(
  role: 'admin' | 'finance' = 'admin',
  mock: UpgradeMock = {},
  cache?: MockResponse,
) {
  upgrade = mock
  const fetchMock = mockUpgrade({ role, cache })
  seedAdminToken()
  seedAdminProfile({ role, username: role })
  renderApp(['/admin/settings'])
  if (role === 'admin') {
    await screen.findByRole('heading', { name: '设置' })
    if (!cache) {
      await screen.findByText('发现新版本 v0.10.0')
    }
  } else {
    await screen.findByText('无权访问后台设置')
  }
  return fetchMock
}

/** 按路径后缀统计接口调用（方法不限）。 */
function callsTo(fetchMock: ReturnType<typeof installFetchMock>, path: string, method?: string) {
  return fetchMock.mock.calls.filter(
    ([input, init]) =>
      String(input).endsWith(path) && (method === undefined || (init?.method ?? 'GET') === method),
  )
}

describe('管理后台 · 一键升级（R13-B，契约 20）', () => {
  beforeEach(() => {
    resetAdminUpdateStatus()
  })

  it('有新版本时出现「立即升级」，二次确认弹窗写清目标版本、替换内容与中断提示', async () => {
    const fetchMock = await renderUpgradePage()
    const user = userEvent.setup()

    await user.click(await screen.findByTestId('upgrade-apply-button'))
    const dialog = await screen.findByRole('alertdialog')
    expect(within(dialog).getByText('升级到 v0.10.0')).toBeInTheDocument()
    expect(within(dialog).getByText(/lyidc-server（服务二进制）与 lyidc-migrate（迁移命令）/)).toBeInTheDocument()
    expect(within(dialog).getByText(/dist\/（前端静态产物）/)).toBeInTheDocument()
    expect(within(dialog).getByText(/服务会短暂中断/)).toBeInTheDocument()
    expect(within(dialog).getByText(/config.yaml 与数据库配置不会被修改/)).toBeInTheDocument()

    // 取消不应发起任何升级请求。
    await user.click(within(dialog).getByRole('button', { name: '取消' }))
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument())
    expect(callsTo(fetchMock, '/admin/updates/apply', 'POST')).toHaveLength(0)
  })

  it('确认后回传目标版本，并把返回的任务渲染成阶段进度', async () => {
    const fetchMock = await renderUpgradePage()
    const user = userEvent.setup()

    await user.click(await screen.findByTestId('upgrade-apply-button'))
    const dialog = await screen.findByRole('alertdialog')
    await user.click(within(dialog).getByRole('button', { name: '开始升级' }))

    const applyCalls = await waitFor(() => {
      const calls = callsTo(fetchMock, '/admin/updates/apply', 'POST')
      expect(calls).toHaveLength(1)
      return calls
    })
    expect(requestBody(applyCalls[0][1] as RequestInit)).toEqual({
      confirm: true,
      target_version: 'v0.10.0',
    })

    // 任务面板：状态、版本、阶段名与进度条。
    const panel = await screen.findByTestId('upgrade-job')
    expect(within(panel).getAllByText('升级进行中').length).toBeGreaterThan(0)
    expect(within(panel).getByText('v0.9.0 → v0.10.0')).toBeInTheDocument()
    expect(screen.getByTestId('upgrade-phase')).toHaveTextContent('下载发布包')
    expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '20')
    expect(screen.getByTestId('upgrade-progress')).toHaveStyle({ width: '20%' })
  })

  it('刷新页面后仍能看到进行中的任务：读持久化状态并禁用重复提交', async () => {
    const fetchMock = await renderUpgradePage('admin', {
      jobs: [
        ok(
          makeUpdateJob({
            phase: 'migrating',
            phase_label: '执行数据库迁移',
            progress: 78,
            log_tail: ['2026-10-14T10:15:40Z migrating 执行数据库迁移：lyidc-migrate up'],
          }),
        ),
      ],
    })

    const panel = await screen.findByTestId('upgrade-job')
    expect(screen.getByTestId('upgrade-phase')).toHaveTextContent('执行数据库迁移')
    expect(within(panel).getByTestId('upgrade-log')).toHaveTextContent('lyidc-migrate up')

    // 进行中：按钮禁用且不发重复请求。
    const button = screen.getByTestId('upgrade-apply-button')
    expect(button).toBeDisabled()
    expect(button).toHaveAttribute('title', '已有升级任务进行中，请等待其结束')
    expect(callsTo(fetchMock, '/admin/updates/apply', 'POST')).toHaveLength(0)
  })

  it('阶段推进：轮询读到终态后展示升级成功并停止轮询', async () => {
    await renderUpgradePage('admin', {
      jobs: [
        ok(makeUpdateJob({ phase: 'restarting', phase_label: '启动新版本', progress: 85 })),
        ok(
          makeUpdateJob({
            status: 'done',
            phase: 'done',
            phase_label: '完成',
            progress: 100,
            finished_at: '2026-10-14T10:16:30Z',
          }),
        ),
      ],
    })

    await screen.findByTestId('upgrade-job')
    await waitFor(
      () => {
        // 状态徽标与进度文案都会写「升级完成」，这里只断言它出现。
        expect(screen.getAllByText('升级完成').length).toBeGreaterThan(0)
      },
      { timeout: 8000 },
    )
    expect(screen.getByTestId('upgrade-message')).toHaveTextContent('已升级到 v0.10.0')
    expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '100')
  }, 15000)

  it('失败与回滚展示：已回滚给出原因、旧版本与「迁移已前进」的提示', async () => {
    await renderUpgradePage('admin', {
      jobs: [
        ok(
          makeUpdateJob({
            status: 'rolled_back',
            phase: 'rolled_back',
            phase_label: '已回滚',
            progress: 92,
            message: '新版本健康检查未通过：健康检查超时（1m0s）：服务尚未响应',
            rolled_back: true,
            db_migrated: true,
            log_tail: [
              '2026-10-14T10:16:00Z health_check 健康检查未通过：健康检查超时（1m0s）',
              '2026-10-14T10:16:10Z rolled_back 已回滚到 v0.9.0 并确认服务可用',
            ],
          }),
        ),
      ],
    })

    const panel = await screen.findByTestId('upgrade-job')
    expect(within(panel).getAllByText('已回滚').length).toBeGreaterThan(0)
    expect(screen.getByTestId('upgrade-message')).toHaveTextContent(
      '升级失败，已自动回滚到 v0.9.0，服务已恢复可用',
    )
    expect(panel).toHaveTextContent('新版本健康检查未通过')
    expect(panel).toHaveTextContent('数据库迁移已执行且不可逆')
    expect(screen.getByTestId('upgrade-log')).toHaveTextContent('已回滚到 v0.9.0')

    // 任务已结束：可以再次发起升级。
    expect(screen.getByTestId('upgrade-apply-button')).toBeEnabled()
  })

  it('失败任务：展示失败原因与日志，不谎报成功', async () => {
    await renderUpgradePage('admin', {
      jobs: [
        ok(
          makeUpdateJob({
            status: 'failed',
            phase: 'failed',
            phase_label: '失败',
            progress: 95,
            message: 'sha256 校验失败：发布包与 .sha256 不一致，已中止且未替换任何文件',
            log_tail: ['2026-10-14T10:15:50Z verifying sha256 校验失败'],
          }),
        ),
      ],
    })

    const panel = await screen.findByTestId('upgrade-job')
    expect(within(panel).getAllByText('升级失败').length).toBeGreaterThan(0)
    expect(screen.getByTestId('upgrade-message')).toHaveTextContent('升级失败')
    expect(panel).toHaveTextContent('sha256 校验失败')
    expect(screen.getByTestId('upgrade-log')).toHaveTextContent('校验失败')
  })

  it('前置检查被拒（40002）：toast 给出可读原因，不进入任务面板', async () => {
    await renderUpgradePage('admin', {
      apply: () => fail(40002, '当前为开发构建（版本 dev），没有可靠版本基线，无法判断升级目标', 400),
    })
    const user = userEvent.setup()

    await user.click(await screen.findByTestId('upgrade-apply-button'))
    await user.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: '开始升级' }))

    expect(
      await screen.findByText(/当前为开发构建（版本 dev），没有可靠版本基线/),
    ).toBeInTheDocument()
    expect(screen.queryByTestId('upgrade-job')).not.toBeInTheDocument()
  })

  it('互斥拒绝（409）：toast 提示已有任务进行中', async () => {
    await renderUpgradePage('admin', {
      apply: () => fail(409, '已有升级任务进行中（20261014T101500Z-1a2b，阶段 downloading）', 409),
    })
    const user = userEvent.setup()

    await user.click(await screen.findByTestId('upgrade-apply-button'))
    await user.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: '开始升级' }))

    expect(await screen.findByText(/已有升级任务进行中/)).toBeInTheDocument()
  })

  it('开发构建没有可升级目标：不出现「立即升级」按钮', async () => {
    await renderUpgradePage('admin', {}, ok(
      makeUpdateStatus({
        current_version: 'dev',
        commit: 'unknown',
        built_at: 'unknown',
        latest_version: '',
        has_update: false,
        compare: 'dev_build',
        message: '开发构建不与最新版本比较',
      }),
    ))
    expect(await screen.findByText('开发构建，无法比较版本')).toBeInTheDocument()
    expect(screen.queryByTestId('upgrade-apply-button')).not.toBeInTheDocument()
  })

  it('非 admin 角色：无权访问设置页，且不发任何升级相关请求', async () => {
    const fetchMock = await renderUpgradePage('finance')
    await waitFor(() => expect(callsTo(fetchMock, '/admin/updates/job')).toHaveLength(0))
    await waitFor(() => expect(callsTo(fetchMock, '/admin/updates')).toHaveLength(0))
    expect(callsTo(fetchMock, '/admin/updates/apply', 'POST')).toHaveLength(0)
    expect(screen.queryByTestId('upgrade-apply-button')).not.toBeInTheDocument()
  })
})
