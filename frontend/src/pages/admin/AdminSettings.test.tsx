import { act, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'

import { resetAdminUpdateStatus } from '@/hooks/useAdminUpdateStatus'
import { makeAdminAccount, makeUpdateStatus } from '@/test/fixtures'
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

// 管理后台「设置」页：密钥脱敏展示与三态提交、上游探活、角色矩阵（仅 admin）。

const EPAY = {
  enabled: true,
  gateway: 'https://pay.example.com/',
  pid: '1001',
  key_configured: true,
  key_masked: '1sXR****ZG5',
  notify_url: '',
  notify_url_recommended: 'https://lyidc.example.com/api/v1/payments/epay/notify',
  return_url: '',
  updated_by: 1,
  updated_at: '2026-10-08T06:20:00Z',
}

const UPSTREAM = {
  // 构建期注入的地址：只读展示，保存时绝不提交（契约 8.6 / 12.1，R17）。
  base_url: 'https://idc.example.com/',
  base_url_fixed: true,
  username: 'oem',
  api_key_configured: true,
  api_key_masked: 'abcd****wxyz',
  timeout_seconds: 5,
  updated_by: 1,
  updated_at: '2026-10-08T06:22:00Z',
}

const SMTP = {
  enabled: false,
  host: 'smtp.example.com',
  port: 0,
  port_effective: 587,
  username: 'noreply@example.com',
  password_configured: true,
  password_masked: 'pass****word',
  from: 'noreply@example.com',
  from_name: '岭云互联',
  encryption: 'starttls' as const,
  updated_by: 1,
  updated_at: '2026-10-08T06:24:00Z',
}

const NOTIFY = {
  inapp_enabled: true,
  email_enabled: false,
  expiry_reminder_enabled: true,
  expiry_reminder_days: 7,
  updated_by: 1,
  updated_at: '2026-10-08T06:26:00Z',
}

const REFERRAL = {
  enabled: true,
  default_percent: '10.00',
  max_coupon_percent: '50.00',
  updated_by: 1,
  updated_at: '2026-10-08T06:28:00Z',
}

const UPDATES_SETTINGS = {
  repo: 'LangfordKuo/Lyidc_OEM',
  github_token_configured: true,
  github_token_masked: 'ghp_****abcd',
  // 两个地址是构建期固定项的生效值（P2，契约 19.5），只读展示、永不提交。
  api_base: 'https://api.github.example/v3',
  download_base: 'https://mirror.example.com/dl',
  fixed: true,
  updated_by: 1,
  updated_at: '2026-10-08T06:30:00Z',
}

const RELEASE_ASSETS = [
  {
    name: 'lyidc-oem-v0.10.0-linux-amd64.tar.gz',
    size: 12345678,
    download_url: 'https://github.com/LangfordKuo/Lyidc_OEM/releases/download/v0.10.0/lyidc-oem-v0.10.0-linux-amd64.tar.gz',
    platform_match: true,
  },
  {
    name: 'lyidc-oem-v0.10.0-linux-amd64.tar.gz.sha256',
    size: 96,
    download_url: 'https://github.com/LangfordKuo/Lyidc_OEM/releases/download/v0.10.0/lyidc-oem-v0.10.0-linux-amd64.tar.gz.sha256',
    platform_match: false,
  },
  {
    name: 'lyidc-oem-v0.10.0-windows-amd64.zip',
    size: 13000000,
    download_url: 'https://github.com/LangfordKuo/Lyidc_OEM/releases/download/v0.10.0/lyidc-oem-v0.10.0-windows-amd64.zip',
    platform_match: false,
  },
]

/** 有新版本的检查结果（服务器平台 linux-amd64，与 platform_match 口径一致）。 */
const NEWER_RELEASE = {
  has_update: true,
  latest_version: 'v0.10.0',
  release_name: 'v0.10.0',
  published_at: '2026-10-10T12:00:00Z',
  notes: '修复导入分组的映射问题\n新增上游分组选择',
  notes_truncated: false,
  html_url: 'https://github.com/LangfordKuo/Lyidc_OEM/releases/tag/v0.10.0',
  assets: RELEASE_ASSETS,
}

const RATE_LIMIT_MESSAGE =
  'GitHub 访问次数已达上限，请稍后重试；可在更新源设置中配置 GitHub Token 提高额度'

/** 系统更新接口的可替换响应（缺省：缓存为「已是最新」、实时检查成功）。 */
interface UpdatesMock {
  cache?: () => MockResponse
  check?: () => MockResponse | Promise<MockResponse>
  /** 更新源设置响应的覆盖（缺省用 UPDATES_SETTINGS；用于覆盖「构建时未注入」等场景）。 */
  settings?: Partial<typeof UPDATES_SETTINGS>
}

function mockSettings(role: 'admin' | 'finance' | 'support' = 'admin', updates: UpdatesMock = {}) {
  // 服务端保存的更新源仓库：PUT 生效，之后的 GET 回读新值（与后端一致）。
  let repoOnServer = updates.settings?.repo ?? UPDATES_SETTINGS.repo
  return installFetchMock((url, init) => {
    const path = url.pathname
    if (path === '/api/v1/admin/profile') {
      return ok(makeAdminAccount({ role, username: role }))
    }
    if (path === '/api/v1/admin/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    if (path === '/api/v1/admin/settings/payment/epay') {
      return init.method === 'PUT' ? ok(EPAY) : ok(EPAY)
    }
    if (path === '/api/v1/admin/settings/upstream') {
      return init.method === 'PUT' ? ok(UPSTREAM) : ok(UPSTREAM)
    }
    if (path === '/api/v1/admin/upstream/health') {
      return ok({
        connected: true,
        base_url: UPSTREAM.base_url,
        latency_ms: 21,
        api_key_masked: UPSTREAM.api_key_masked,
        checked_at: '2026-10-08T06:30:00Z',
      })
    }
    if (path === '/api/v1/admin/settings/email/smtp') {
      return init.method === 'PUT' ? ok(SMTP) : ok(SMTP)
    }
    if (path === '/api/v1/admin/settings/email/test') {
      return ok({ sent: true, to: 'ops@example.com' })
    }
    if (path === '/api/v1/admin/settings/notifications') {
      return init.method === 'PUT' ? ok(NOTIFY) : ok(NOTIFY)
    }
    if (path === '/api/v1/admin/settings/referral') {
      return init.method === 'PUT' ? ok(REFERRAL) : ok(REFERRAL)
    }
    if (path === '/api/v1/admin/updates') {
      return updates.cache ? updates.cache() : ok(makeUpdateStatus())
    }
    if (path === '/api/v1/admin/updates/check') {
      return updates.check ? updates.check() : ok(makeUpdateStatus())
    }
    if (path === '/api/v1/admin/settings/updates') {
      if (init.method === 'PUT') {
        repoOnServer = requestBody<{ repo?: string }>(init)?.repo ?? repoOnServer
      }
      return ok({ ...UPDATES_SETTINGS, ...updates.settings, repo: repoOnServer })
    }
    return undefined
  })
}

async function renderSettings(
  role: 'admin' | 'finance' | 'support' = 'admin',
  updates: UpdatesMock = {},
) {
  const fetchMock = mockSettings(role, updates)
  seedAdminToken()
  seedAdminProfile({ role, username: role })
  renderApp(['/admin/settings'])
  if (role === 'admin') {
    await screen.findByRole('heading', { name: '设置' })
    await screen.findByText('易支付（payment.epay）')
    await screen.findByLabelText('GitHub 仓库（owner/name）')
  } else {
    await screen.findByText('无权访问后台设置')
  }
  return fetchMock
}

describe('管理后台 · 全站设置', () => {
  it('密钥只回显掩码：输入框不回显明文，状态文案给出掩码', async () => {
    await renderSettings()

    // 易支付密钥：已配置 + 掩码展示
    expect(screen.getByTestId('epay_key-status')).toHaveTextContent('已配置（1sXR****ZG5）')
    const keyInput = screen.getByLabelText('商户密钥')
    expect(keyInput).toHaveValue('')
    expect(keyInput).toHaveAttribute('type', 'password')
    // 页面任何地方都不出现明文（掩码只出现前缀 + ****）
    expect(document.body.textContent).not.toContain('1sXRZG5')

    // 上游密钥与 SMTP 口令同理
    expect(screen.getByTestId('upstream_api_key-status')).toHaveTextContent(
      '已配置（abcd****wxyz）',
    )
    expect(screen.getByTestId('smtp_password-status')).toHaveTextContent('已配置（pass****word）')
  })

  it('密钥三态：留空 = 不改、输入 = 替换、清空 = 提交空串', async () => {
    const fetchMock = await renderSettings()
    const user = userEvent.setup()

    // 只切换启用开关 → 请求体只含 enabled
    await user.click(screen.getByRole('switch', { name: '启用易支付' }))
    await user.click(screen.getByRole('button', { name: '保存易支付设置' }))
    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/admin/settings/payment/epay') && init?.method === 'PUT',
      )
      expect(call).toBeTruthy()
      expect(requestBody(call?.[1] as RequestInit)).toEqual({ enabled: false })
    })

    // 输入新密钥 → 请求体含 key 的新值
    await user.type(screen.getByLabelText('商户密钥'), 'brand-new-secret')
    await user.click(screen.getByRole('button', { name: '保存易支付设置' }))
    await waitFor(() => {
      const call = fetchMock.mock.calls
        .filter(
          ([input, init]) =>
            String(input).includes('/admin/settings/payment/epay') && init?.method === 'PUT',
        )
        .at(-1)
      expect(requestBody(call?.[1] as RequestInit)).toEqual({ key: 'brand-new-secret' })
    })

    // 点「清空」→ 请求体 key 为空串（显式清空语义）
    await user.click(screen.getByRole('button', { name: '清空商户密钥' }))
    await user.click(screen.getByRole('button', { name: '保存易支付设置' }))
    await waitFor(() => {
      const call = fetchMock.mock.calls
        .filter(
          ([input, init]) =>
            String(input).includes('/admin/settings/payment/epay') && init?.method === 'PUT',
        )
        .at(-1)
      expect(requestBody(call?.[1] as RequestInit)).toEqual({ key: '' })
    })
  })

  it('上游设置：测试连通性展示探活结果；超时越界被拦截', async () => {
    await renderSettings()
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: '测试连通性' }))
    expect(await screen.findByTestId('upstream-probe-result')).toHaveTextContent('上游连通正常')
    expect(screen.getByText(/耗时 21 ms/)).toBeInTheDocument()

    // 超时越界（>120）→ 本地拦截，不发请求
    await user.type(screen.getByLabelText(/单次请求超时/), '999')
    await user.click(screen.getByRole('button', { name: '保存上游设置' }))
    expect(await screen.findByText(/超时需为 1-120 之间的整数/)).toBeInTheDocument()
  })

  it('上游地址只读展示、不可修改，保存也不提交 base_url', async () => {
    const fetchMock = await renderSettings()
    const user = userEvent.setup()

    // 只读：禁用 + 不可编辑，并给出「由系统固定」的说明。
    const baseURLInput = screen.getByLabelText(/上游地址/) as HTMLInputElement
    expect(baseURLInput).toBeDisabled()
    expect(baseURLInput).toHaveValue(UPSTREAM.base_url)
    expect(screen.getByText(/由系统固定，不可修改/)).toBeInTheDocument()

    // 改用户名并保存 → 请求体只含 username，绝不含 base_url。
    const usernameInput = screen.getByLabelText('上游用户名')
    await user.clear(usernameInput)
    await user.type(usernameInput, 'oem2')
    await user.click(screen.getByRole('button', { name: '保存上游设置' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls
        .filter(
          ([input, init]) =>
            String(input).includes('/admin/settings/upstream') && init?.method === 'PUT',
        )
        .at(-1)
      expect(requestBody(call?.[1] as RequestInit)).toEqual({ username: 'oem2' })
    })
  })

  it('SMTP：发送测试邮件前校验收件地址格式', async () => {
    const fetchMock = await renderSettings()
    const user = userEvent.setup()

    await user.type(screen.getByLabelText('收件地址（可空）'), 'not-an-email')
    await user.click(screen.getByRole('button', { name: '发送测试邮件' }))

    expect(await screen.findByText(/收件地址不正确/)).toBeInTheDocument()
    expect(
      fetchMock.mock.calls.some(([input]) => String(input).includes('/settings/email/test')),
    ).toBe(false)
  })

  it('通知开关：修改后保存只提交变化字段', async () => {
    const fetchMock = await renderSettings()
    const user = userEvent.setup()

    await user.click(screen.getByRole('switch', { name: '站内通知' }))
    await user.type(screen.getByLabelText('提前提醒天数（1-30）'), '3')
    await user.click(screen.getByRole('button', { name: '保存通知开关' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/admin/settings/notifications') && init?.method === 'PUT',
      )
      expect(call).toBeTruthy()
      expect(requestBody(call?.[1] as RequestInit)).toEqual({
        inapp_enabled: false,
        expiry_reminder_days: 3,
      })
    })
  })

  it('返佣设置：总开关与比例只提交变化字段', async () => {
    const fetchMock = await renderSettings()
    const user = userEvent.setup()

    expect(screen.getByText('返佣设置（referral）')).toBeInTheDocument()

    await user.click(screen.getByRole('switch', { name: '返佣总开关' }))
    await user.type(screen.getByLabelText('全局默认返佣比例（%，留空保持不变）'), '25.00')
    await user.click(screen.getByRole('button', { name: '保存返佣设置' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/admin/settings/referral') && init?.method === 'PUT',
      )
      expect(call).toBeTruthy()
      expect(requestBody(call?.[1] as RequestInit)).toEqual({
        enabled: false,
        default_percent: '25.00',
      })
    })
  })

  it('返佣设置：自建券折扣上限越界被前端拦截（不发请求）', async () => {
    const fetchMock = await renderSettings()
    const user = userEvent.setup()

    await user.type(screen.getByLabelText('自建券折扣上限（%，留空保持不变）'), '101')
    await user.click(screen.getByRole('button', { name: '保存返佣设置' }))

    expect(await screen.findByText('比例不能超过 100.00')).toBeInTheDocument()
    expect(
      fetchMock.mock.calls.some(
        ([input, init]) =>
          String(input).includes('/admin/settings/referral') && init?.method === 'PUT',
      ),
    ).toBe(false)
  })

  it('财务与客服角色：整页无权占位且不请求设置接口', async () => {
    for (const role of ['finance', 'support'] as const) {
      const fetchMock = mockSettings(role)
      seedAdminToken()
      seedAdminProfile({ role, username: role })
      const { unmount } = renderApp(['/admin/settings'])
      await screen.findByText('无权访问后台设置')

      expect(
        fetchMock.mock.calls.some(([input]) => String(input).includes('/admin/settings')),
      ).toBe(false)
      unmount()
    }
  })
})

// ---------------------------------------------------------------------------
// 系统更新（R13-A，契约 19）：版本展示、检查与失败态、红点、纯文本说明、产物高亮、权限与更新源表单。
// ---------------------------------------------------------------------------

/** 让出一个宏任务，等待布局的缓存读取等异步请求落定（用于断言「不出现」类结论）。 */
function settle() {
  return act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

/** 按路径后缀统计接口调用（方法不限）。 */
function callsTo(fetchMock: ReturnType<typeof installFetchMock>, path: string) {
  return fetchMock.mock.calls.filter(([input]) => String(input).endsWith(path))
}

/** 最近一次 PUT /admin/settings/updates 的请求体（用于断言三态提交）。 */
function lastUpdatesPut(fetchMock: ReturnType<typeof installFetchMock>) {
  const call = fetchMock.mock.calls
    .filter(
      ([input, init]) => String(input).includes('/admin/settings/updates') && init?.method === 'PUT',
    )
    .at(-1)
  return requestBody(call?.[1] as RequestInit)
}

/** 红点位置（契约 19.7）：侧栏「设置」项与「系统更新」卡片标题，两处同一判定。 */
function expectSettingsDot(visible: boolean) {
  const sidebar = screen.getByRole('navigation', { name: '管理后台导航' })
  const settingsLink = within(sidebar).getByRole('link', { name: /设置/ })
  const cardTitleRow = screen.getByText('系统更新').parentElement as HTMLElement
  if (visible) {
    expect(within(settingsLink).getByTestId('update-dot')).toBeInTheDocument()
    expect(within(cardTitleRow).getByTestId('update-dot')).toBeInTheDocument()
  } else {
    expect(within(settingsLink).queryByTestId('update-dot')).not.toBeInTheDocument()
    expect(within(cardTitleRow).queryByTestId('update-dot')).not.toBeInTheDocument()
  }
}

describe('管理后台 · 系统更新（R13-A，契约 19）', () => {
  beforeEach(() => {
    // 红点数据源是模块级 store：每个用例开始前清空，避免上一个用例的更新状态串入。
    resetAdminUpdateStatus()
  })

  it('最新版本态：显示当前版本与构建信息，结论为已是最新，无红点', async () => {
    await renderSettings()
    await waitFor(() =>
      expect(screen.getByTestId('update-current-version')).toHaveTextContent('v0.9.0'),
    )
    expect(screen.getByText(/构建 commit a1b2c3d/)).toBeInTheDocument()
    expect(await screen.findByText(/已是最新版本 v0\.9\.0（上次检查：/)).toBeInTheDocument()
    expectSettingsDot(false)
  })

  it('开发构建：显示「开发构建，无法比较版本」与说明，不报有新版本', async () => {
    await renderSettings('admin', {
      cache: () =>
        ok(
          makeUpdateStatus({
            current_version: 'dev',
            commit: 'unknown',
            built_at: 'unknown',
            latest_version: '',
            checked_at: '',
            compare: 'dev_build',
            message: '开发构建不与最新版本比较',
          }),
        ),
    })
    expect(await screen.findByText('开发构建，无法比较版本')).toBeInTheDocument()
    expect(screen.getByText('开发构建不与最新版本比较')).toBeInTheDocument()
    expect(screen.getByTestId('update-current-version')).toHaveTextContent('dev')
    expect(screen.queryByText(/发现新版本/)).not.toBeInTheDocument()
    expect(screen.queryByText(/已是最新版本/)).not.toBeInTheDocument()
    expectSettingsDot(false)
  })

  it('有新版本：醒目展示版本、发布时间、说明与产物，仅本服务器平台的产物高亮标注', async () => {
    await renderSettings('admin', { cache: () => ok(makeUpdateStatus(NEWER_RELEASE)) })
    expect(await screen.findByText('发现新版本 v0.10.0')).toBeInTheDocument()
    expect(screen.getByText(/发布时间：/)).toBeInTheDocument()
    expect(screen.getByTestId('update-notes')).toHaveTextContent('修复导入分组的映射问题')
    expectSettingsDot(true)

    const rows = screen.getAllByTestId('update-asset')
    expect(rows).toHaveLength(3)
    expect(rows[0]).toHaveTextContent('本服务器平台')
    expect(rows[0]).toHaveTextContent('11.8 MB')
    expect(rows[1]).not.toHaveTextContent('本服务器平台')
    expect(rows[2]).not.toHaveTextContent('本服务器平台')
    expect(screen.getAllByText('本服务器平台')).toHaveLength(1)
    expect(within(rows[0]).getByRole('link', { name: /下载/ })).toHaveAttribute(
      'href',
      RELEASE_ASSETS[0].download_url,
    )
    expect(screen.getByRole('link', { name: /查看 Release 页/ })).toHaveAttribute(
      'href',
      NEWER_RELEASE.html_url,
    )
  })

  it('Release 说明按纯文本展示：HTML 与 Markdown 不渲染成元素，截断时给出提示', async () => {
    const notes =
      '<img src=x onerror="window.__pwned=1">\n<a href="https://evil.example">链接</a>\n**粗体** 与 [标题](#x)'
    await renderSettings('admin', {
      cache: () => ok(makeUpdateStatus({ ...NEWER_RELEASE, notes, notes_truncated: true })),
    })
    const body = await screen.findByTestId('update-notes')
    expect(body).toHaveTextContent('<img src=x onerror="window.__pwned=1">')
    expect(body).toHaveTextContent('**粗体** 与 [标题](#x)')
    expect(body.querySelector('img')).toBeNull()
    expect(body.querySelector('a')).toBeNull()
    expect(body.querySelector('strong')).toBeNull()
    expect('__pwned' in window).toBe(false)
    expect(screen.getByText(/说明已截断/)).toBeInTheDocument()
  })

  it('尚未检查：提示点击「检查更新」，不显示红点', async () => {
    await renderSettings('admin', {
      cache: () =>
        ok(
          makeUpdateStatus({
            status: 'not_checked',
            compare: 'unknown',
            latest_version: '',
            checked_at: '',
          }),
        ),
    })
    expect(
      await screen.findByText('尚未检查更新，点击「检查更新」获取最新版本'),
    ).toBeInTheDocument()
    expectSettingsDot(false)
  })

  it('失败态（限流）：展示 message 与「重试」；没有上一次成功结果时不显示「上次成功结果」', async () => {
    await renderSettings('admin', {
      cache: () =>
        ok(
          makeUpdateStatus({
            status: 'rate_limited',
            compare: 'unknown',
            latest_version: '',
            checked_at: '',
            message: RATE_LIMIT_MESSAGE,
          }),
        ),
    })
    expect(await screen.findByText(RATE_LIMIT_MESSAGE)).toBeInTheDocument()
    expect(screen.getByTestId('update-failure')).toHaveTextContent('访问受限')
    expect(screen.getByRole('button', { name: '重试' })).toBeEnabled()
    expect(screen.queryByText(/上次成功结果/)).not.toBeInTheDocument()
    expect(screen.queryByText(/发现新版本|已是最新版本/)).not.toBeInTheDocument()
    expectSettingsDot(false)
  })

  it('失败态（不可达）：保留上一次成功结果，页面不崩溃且检查按钮仍可用', async () => {
    const message = '无法连接 GitHub 或其服务暂不可用，请稍后重试'
    await renderSettings('admin', {
      cache: () =>
        ok(makeUpdateStatus({ status: 'unreachable', message, checked_at: '2026-10-14T10:00:00Z' })),
    })
    expect(await screen.findByText(message)).toBeInTheDocument()
    expect(screen.getByText(/上次成功结果：v0\.9\.0（/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重试' })).toBeEnabled()
    expect(screen.queryByText(/已是最新版本/)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '检查更新' })).toBeEnabled()
  })

  it('失败态的「重试」再次调用实时检查接口，成功后失败提示消失', async () => {
    const fetchMock = await renderSettings('admin', {
      cache: () =>
        ok(
          makeUpdateStatus({
            status: 'timeout',
            compare: 'unknown',
            message: '连接 GitHub 超时，请稍后重试',
          }),
        ),
    })
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: '重试' }))
    expect(await screen.findByText(/已是最新版本/)).toBeInTheDocument()
    expect(screen.queryByTestId('update-failure')).not.toBeInTheDocument()
    expect(callsTo(fetchMock, '/admin/updates/check')).toHaveLength(1)
  })

  it('检查更新：请求中按钮 loading 且禁用，返回后按结果渲染并同步红点', async () => {
    let finishCheck!: (value: MockResponse) => void
    const pendingCheck = new Promise<MockResponse>((resolve) => {
      finishCheck = resolve
    })
    const fetchMock = await renderSettings('admin', { check: () => pendingCheck })
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: '检查更新' }))
    expect(await screen.findByRole('button', { name: '检查中…' })).toBeDisabled()
    const checkCalls = callsTo(fetchMock, '/admin/updates/check')
    expect(checkCalls).toHaveLength(1)
    expect(checkCalls[0][1]?.method).toBe('GET')

    finishCheck(ok(makeUpdateStatus(NEWER_RELEASE)))
    expect(await screen.findByText('发现新版本 v0.10.0')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '检查更新' })).toBeEnabled()
    expectSettingsDot(true)
  })

  it('红点：仅 has_update=true 时在侧栏「设置」与卡片标题出现', async () => {
    await renderSettings('admin', { cache: () => ok(makeUpdateStatus(NEWER_RELEASE)) })
    await screen.findByText('发现新版本 v0.10.0')
    expectSettingsDot(true)
  })

  it('红点：已是最新（has_update=false）时不出现', async () => {
    await renderSettings('admin')
    expect(await screen.findByText(/已是最新版本/)).toBeInTheDocument()
    expectSettingsDot(false)
  })

  it('红点：从未检查时不出现', async () => {
    await renderSettings('admin', {
      cache: () =>
        ok(makeUpdateStatus({ status: 'not_checked', compare: 'unknown', latest_version: '' })),
    })
    expect(await screen.findByText(/尚未检查更新/)).toBeInTheDocument()
    expectSettingsDot(false)
  })

  it('红点：读取失败（缓存接口 500）时不出现，且不误报', async () => {
    await renderSettings('admin', { cache: () => fail(50001, '库操作失败', 500) })
    expect(
      await screen.findByText('暂无版本信息，点击「检查更新」获取最新版本'),
    ).toBeInTheDocument()
    await settle()
    expectSettingsDot(false)
  })

  it('修改仓库：后端清空检查缓存，红点与旧结果同步消失', async () => {
    let cached = makeUpdateStatus(NEWER_RELEASE)
    const fetchMock = await renderSettings('admin', { cache: () => ok(cached) })
    await screen.findByText('发现新版本 v0.10.0')
    expectSettingsDot(true)

    // 后端在 PUT 同一请求内清空 updates.check 缓存：之后读取到的是 not_checked。
    cached = makeUpdateStatus({
      status: 'not_checked',
      compare: 'unknown',
      has_update: false,
      latest_version: '',
      checked_at: '',
    })
    const user = userEvent.setup()
    const repoInput = await screen.findByLabelText('GitHub 仓库（owner/name）')
    await user.clear(repoInput)
    await user.type(repoInput, 'octo-org/Lyidc_OEM')
    await user.click(screen.getByRole('button', { name: '保存更新源设置' }))

    expect(
      await screen.findByText('尚未检查更新，点击「检查更新」获取最新版本'),
    ).toBeInTheDocument()
    expectSettingsDot(false)
    expect(lastUpdatesPut(fetchMock)).toEqual({ repo: 'octo-org/Lyidc_OEM' })
  })

  it('更新源：只改仓库时只提交 repo，Token 保持不变时不带 github_token', async () => {
    const fetchMock = await renderSettings()
    const user = userEvent.setup()
    const repoInput = await screen.findByLabelText('GitHub 仓库（owner/name）')
    await user.clear(repoInput)
    await user.type(repoInput, 'octo-org/Lyidc_OEM')
    await user.click(screen.getByRole('button', { name: '保存更新源设置' }))
    await waitFor(() => expect(lastUpdatesPut(fetchMock)).toEqual({ repo: 'octo-org/Lyidc_OEM' }))
  })

  it('更新源：Token 只显示掩码、不回填明文；清空提交空串，替换提交新值', async () => {
    const fetchMock = await renderSettings()
    const user = userEvent.setup()
    expect(screen.getByTestId('updates_github_token-status')).toHaveTextContent(
      '已配置（ghp_****abcd）',
    )
    expect(screen.getByLabelText('GitHub Token')).toHaveValue('')

    await user.click(screen.getByRole('button', { name: '清空GitHub Token' }))
    await user.click(screen.getByRole('button', { name: '保存更新源设置' }))
    await waitFor(() => expect(lastUpdatesPut(fetchMock)).toEqual({ github_token: '' }))

    await user.type(screen.getByLabelText('GitHub Token'), 'ghp_brandNewToken1')
    await user.click(screen.getByRole('button', { name: '保存更新源设置' }))
    await waitFor(() =>
      expect(lastUpdatesPut(fetchMock)).toEqual({ github_token: 'ghp_brandNewToken1' }),
    )
  })

  it('更新源：仓库与 Token 都未变化时保存禁用；仓库格式不合法时禁用并给出提示', async () => {
    await renderSettings()
    const user = userEvent.setup()
    expect(screen.getByRole('button', { name: '保存更新源设置' })).toBeDisabled()

    const repoInput = screen.getByLabelText('GitHub 仓库（owner/name）')
    await user.clear(repoInput)
    await user.type(repoInput, 'not a repo')
    expect(screen.getByText(/仓库格式应为 owner\/name/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '保存更新源设置' })).toBeDisabled()
  })

  it('更新源地址：构建期固定，只读展示生效值与说明，保存请求体绝不带这两个字段', async () => {
    const fetchMock = await renderSettings()
    const user = userEvent.setup()

    // 两个地址按 fixed 渲染成禁用只读输入框，值是后端回显的生效值。
    const apiBaseInput = await screen.findByLabelText('更新源 API 地址（api_base）')
    expect(apiBaseInput).toHaveValue('https://api.github.example/v3')
    expect(apiBaseInput).toBeDisabled()
    const downloadBaseInput = screen.getByLabelText('资产下载地址（download_base）')
    expect(downloadBaseInput).toHaveValue('https://mirror.example.com/dl')
    expect(downloadBaseInput).toBeDisabled()
    expect(screen.getByTestId('updates-source-fixed-hint')).toHaveTextContent(
      '更新源由构建期固定，不可修改',
    )

    // 保存仓库时请求体只有 repo：地址字段一个都不发（带了后端必然 40002）。
    const repoInput = screen.getByLabelText('GitHub 仓库（owner/name）')
    await user.clear(repoInput)
    await user.type(repoInput, 'octo-org/Lyidc_OEM')
    await user.click(screen.getByRole('button', { name: '保存更新源设置' }))
    await waitFor(() => expect(lastUpdatesPut(fetchMock)).toEqual({ repo: 'octo-org/Lyidc_OEM' }))
    expect(lastUpdatesPut(fetchMock)).not.toHaveProperty('api_base')
    expect(lastUpdatesPut(fetchMock)).not.toHaveProperty('download_base')
  })

  it('更新源地址未注入时：api_base 回退官方缺省，download_base 显示按 Release 下载', async () => {
    await renderSettings('admin', {
      settings: { api_base: 'https://api.github.com', download_base: '' },
    })

    expect(await screen.findByLabelText('更新源 API 地址（api_base）')).toHaveValue(
      'https://api.github.com',
    )
    // 空串 → 给一句占位说明，而不是留一个用途不明的空白框。
    expect(screen.getByLabelText('资产下载地址（download_base）')).toHaveValue(
      '（按 Release 自带的下载地址）',
    )
  })

  it('只有 admin 读取更新缓存：finance / support 在后台任何页面都不发起更新相关请求', async () => {
    // 对照组：admin 进入后台布局时读取一次缓存（红点数据源）。
    const adminMock = mockSettings('admin')
    seedAdminToken()
    seedAdminProfile({ role: 'admin', username: 'admin' })
    const adminView = renderApp(['/admin'])
    await screen.findByRole('navigation', { name: '管理后台导航' })
    await waitFor(() => expect(callsTo(adminMock, '/admin/updates').length).toBeGreaterThanOrEqual(1))
    adminView.unmount()

    for (const role of ['finance', 'support'] as const) {
      for (const path of ['/admin', '/admin/settings']) {
        const fetchMock = mockSettings(role)
        seedAdminToken()
        seedAdminProfile({ role, username: role })
        const view = renderApp([path])
        await screen.findByRole('navigation', { name: '管理后台导航' })
        await settle()
        const updateCalls = fetchMock.mock.calls.filter(
          ([input]) =>
            String(input).includes('/admin/updates') ||
            String(input).includes('/admin/settings/updates'),
        )
        expect(updateCalls).toHaveLength(0)
        view.unmount()
      }
    }
  })
})
