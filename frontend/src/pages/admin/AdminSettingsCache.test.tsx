import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'

import type { CacheSettings, CacheStatus, UpdateCacheSettingsInput } from '@/api/types'
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

// 管理后台「设置」页的「缓存」分区（R19 批 1，契约第 24 节）：
// 默认文件缓存、降级标红与原因、Redis 连接信息、驱动切换与参数提交、口令三态、
// 就地校验不发请求、后端 40002 展示原因、重置统计与清空缓存。

const FILE_DIR = 'C:\\Users\\Administrator\\AppData\\Local\\Temp\\lyidc-cache'

const CACHE_SETTINGS: CacheSettings = {
  driver: 'file',
  redis_enabled: false,
  effective_driver: 'file',
  degraded: false,
  degraded_reason: '',
  file_dir: FILE_DIR,
  redis: {
    host: '',
    port: 6379,
    password_configured: false,
    password_masked: '',
    db: 0,
    key_prefix: 'lyidc:',
    timeout_seconds: 1,
  },
  updated_by: null,
  updated_at: null,
}

const CACHE_STATUS: CacheStatus = {
  driver: 'file',
  effective_driver: 'file',
  degraded: false,
  degraded_reason: '',
  keys: 12,
  hits: 30,
  misses: 5,
  hit_rate: 85.71,
  stats_since: '2026-10-14T07:10:00Z',
  file: { dir: FILE_DIR },
  redis: {
    host: '127.0.0.1',
    port: 6379,
    db: 0,
    key_prefix: 'lyidc:',
    timeout_seconds: 1,
    version: '',
    latency_ms: 0,
  },
}

/** 按请求体把设置合并进「服务端」副本（与后端语义一致：省略的字段保持原值）。 */
function applyInput(current: CacheSettings, input: UpdateCacheSettingsInput): CacheSettings {
  const next: CacheSettings = { ...current, redis: { ...current.redis } }
  if (input.driver) {
    next.driver = input.driver
    next.redis_enabled = input.driver === 'redis'
    next.effective_driver = input.driver
  }
  const redis = input.redis
  if (redis) {
    if (redis.host !== undefined) next.redis.host = redis.host
    if (redis.port !== undefined) next.redis.port = redis.port
    if (redis.db !== undefined) next.redis.db = redis.db
    if (redis.key_prefix !== undefined) next.redis.key_prefix = redis.key_prefix
    if (redis.timeout_seconds !== undefined) next.redis.timeout_seconds = redis.timeout_seconds
    if (redis.password !== undefined) {
      next.redis.password_configured = redis.password !== ''
      next.redis.password_masked = redis.password === '' ? '' : 'supe****'
    }
  }
  return next
}

/** 组装设置页所需的全部接口；cache / status 节点控制缓存分区的响应。 */
function mockCachePage(
  nodes: {
    cache?: () => MockResponse
    status?: () => MockResponse
    saveCache?: (init: RequestInit) => MockResponse
  } = {},
) {
  let settings: CacheSettings = { ...CACHE_SETTINGS }
  let status: CacheStatus = { ...CACHE_STATUS }
  return installFetchMock((url, init) => {
    const path = url.pathname
    const method = init.method ?? 'GET'
    if (path === '/api/v1/admin/profile') {
      return ok(makeAdminAccount({ role: 'admin', username: 'admin' }))
    }
    if (path === '/api/v1/admin/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    if (path === '/api/v1/admin/updates') {
      return ok(makeUpdateStatus())
    }
    if (path === '/api/v1/admin/updates/job') {
      return ok(null)
    }
    if (path === '/api/v1/admin/settings/cache') {
      if (method === 'PUT') {
        if (nodes.saveCache) {
          return nodes.saveCache(init)
        }
        const body = requestBody<UpdateCacheSettingsInput>(init) ?? {}
        settings = applyInput(settings, body)
        return ok(settings)
      }
      return nodes.cache ? nodes.cache() : ok(settings)
    }
    if (path === '/api/v1/admin/cache/status') {
      if (nodes.status) {
        return nodes.status()
      }
      return ok({ ...status, driver: settings.driver, effective_driver: settings.effective_driver })
    }
    if (path === '/api/v1/admin/cache/reset-stats') {
      status = { ...status, hits: 0, misses: 0, hit_rate: 0 }
      return ok({ hits: 0, misses: 0, hit_rate: 0, stats_since: '2026-10-14T08:00:00Z' })
    }
    if (path === '/api/v1/admin/cache/flush') {
      status = { ...status, keys: 0 }
      return ok({ driver: settings.effective_driver, flushed: true, keys: 0 })
    }
    return undefined
  })
}

async function renderSettings(nodes: Parameters<typeof mockCachePage>[0] = {}) {
  const fetchMock = mockCachePage(nodes)
  seedAdminToken()
  seedAdminProfile({ role: 'admin', username: 'admin' })
  renderApp(['/admin/settings'])
  await screen.findByRole('heading', { name: '设置' })
  return fetchMock
}

/** 缓存分区卡片（卡根节点，用于把断言限定在本分区内）。 */
function cacheCard(): HTMLElement {
  const heading = screen.getByText('缓存（cache）')
  const card = heading.closest('[data-slot="card"]')
  if (!card) {
    throw new Error('找不到缓存分区卡片')
  }
  return card as HTMLElement
}

/** 缓存状态行（键数量 / 命中 / 未命中 / 命中率）。 */
async function cacheStatusText(): Promise<string> {
  const node = await screen.findByTestId('cache-status')
  return node.textContent ?? ''
}

describe('管理后台设置页：缓存（R19 批 1）', () => {
  beforeEach(() => {
    window.localStorage.clear()
  })

  it('默认展示文件缓存：驱动、文件目录、键数量与命中率', async () => {
    await renderSettings()

    const card = cacheCard()
    expect(within(card).getByText('配置驱动：文件缓存')).toBeTruthy()
    expect(within(card).getByText('当前生效：文件缓存')).toBeTruthy()
    expect(within(card).queryByTestId('cache-degraded')).toBeNull()

    const text = await cacheStatusText()
    expect(text).toContain('键数量 12')
    expect(text).toContain('命中 30')
    expect(text).toContain('未命中 5')
    expect(text).toContain('命中率 85.71%')
    expect(within(card).getByText(new RegExp('文件缓存目录：'))).toBeTruthy()
  })

  it('启用 Redis 且不可用时标红并显示可读原因', async () => {
    await renderSettings({
      cache: () =>
        ok({
          ...CACHE_SETTINGS,
          driver: 'redis',
          redis_enabled: true,
          effective_driver: 'file',
          degraded: true,
          degraded_reason: '连接被拒绝（127.0.0.1:6399）',
        }),
    })

    const alert = await screen.findByTestId('cache-degraded')
    expect(alert.textContent).toContain('已降级到文件缓存')
    expect(alert.textContent).toContain('连接被拒绝（127.0.0.1:6399）')
    const card = cacheCard()
    expect(within(card).getByText('已降级')).toBeTruthy()
    expect(within(card).getByText('配置驱动：Redis')).toBeTruthy()
    expect(within(card).getByText('当前生效：文件缓存')).toBeTruthy()
  })

  it('Redis 正常时展示连接信息（不含口令）且不标红', async () => {
    await renderSettings({
      cache: () =>
        ok({
          ...CACHE_SETTINGS,
          driver: 'redis',
          redis_enabled: true,
          effective_driver: 'redis',
          redis: {
            host: '127.0.0.1',
            port: 6380,
            password_configured: true,
            password_masked: 'supe****',
            db: 2,
            key_prefix: 'oem:',
            timeout_seconds: 3,
          },
        }),
      status: () =>
        ok({
          ...CACHE_STATUS,
          driver: 'redis',
          effective_driver: 'redis',
          redis: {
            host: '127.0.0.1',
            port: 6380,
            db: 2,
            key_prefix: 'oem:',
            timeout_seconds: 3,
            version: '7.2.5',
            latency_ms: 1,
          },
        }),
    })

    const card = cacheCard()
    await waitFor(() => {
      expect(within(card).getByText(/Redis 连接：127.0.0.1:6380/)).toBeTruthy()
    })
    const info = within(card).getByText(/Redis 连接：127.0.0.1:6380/)
    expect(info.textContent).toContain('db 2')
    expect(info.textContent).toContain('前缀 oem:')
    expect(info.textContent).toContain('版本 7.2.5')
    expect(within(card).queryByTestId('cache-degraded')).toBeNull()
    // 口令只以掩码展示，页面上不出现明文输入。
    expect(screen.getByTestId('cache_password-status').textContent).toContain('supe****')
  })

  it('切到 Redis 并保存：请求体带上驱动与参数，口令保持时不提交 password', async () => {
    const user = userEvent.setup()
    const fetchMock = await renderSettings()
    const card = cacheCard()

    await user.click(within(card).getByRole('radio', { name: 'Redis' }))
    await user.type(within(card).getByLabelText('Redis 主机（host）'), '127.0.0.1')
    await user.clear(within(card).getByLabelText('端口（port）'))
    await user.type(within(card).getByLabelText('端口（port）'), '6380')
    await user.clear(within(card).getByLabelText('数据库序号（db）'))
    await user.type(within(card).getByLabelText('数据库序号（db）'), '2')
    await user.clear(within(card).getByLabelText('键前缀（key_prefix）'))
    await user.type(within(card).getByLabelText('键前缀（key_prefix）'), 'oem:')
    await user.clear(within(card).getByLabelText('超时（timeout_seconds）'))
    await user.type(within(card).getByLabelText('超时（timeout_seconds）'), '3')
    await user.click(within(card).getByRole('button', { name: '保存缓存设置' }))

    await waitFor(() => {
      const put = fetchMock.mock.calls.find(([, init]) => init?.method === 'PUT')
      expect(put).toBeTruthy()
      const body = requestBody<UpdateCacheSettingsInput>(put![1] as RequestInit)
      expect(body).toEqual({
        driver: 'redis',
        redis: {
          host: '127.0.0.1',
          port: 6380,
          db: 2,
          key_prefix: 'oem:',
          timeout_seconds: 3,
        },
      })
      expect(body?.redis && 'password' in body.redis).toBe(false)
    })

    // 保存成功后回读服务端值（驱动与连接信息同步刷新）。
    await waitFor(() => {
      expect(within(cacheCard()).getByText('配置驱动：Redis')).toBeTruthy()
    })
  })

  it('口令三态：输入新口令与清空口令都按契约提交', async () => {
    const user = userEvent.setup()
    const fetchMock = await renderSettings({
      cache: () =>
        ok({
          ...CACHE_SETTINGS,
          driver: 'redis',
          redis_enabled: true,
          effective_driver: 'redis',
          redis: { ...CACHE_SETTINGS.redis, host: '127.0.0.1', password_configured: true, password_masked: 'supe****' },
        }),
    })
    const card = cacheCard()

    await user.type(within(card).getByLabelText('Redis 口令（password）'), 'new-secret')
    await user.click(within(card).getByRole('button', { name: '保存缓存设置' }))
    await waitFor(() => {
      const put = fetchMock.mock.calls.find(([, init]) => init?.method === 'PUT')
      expect(requestBody<UpdateCacheSettingsInput>(put![1] as RequestInit)?.redis?.password).toBe('new-secret')
    })

    // 清空口令：先点「清空 Redis 口令（password）」再保存 → password 提交空串。
    await user.click(within(cacheCard()).getByRole('button', { name: '清空Redis 口令（password）' }))
    await user.click(within(cacheCard()).getByRole('button', { name: '保存缓存设置' }))
    await waitFor(() => {
      const puts = fetchMock.mock.calls.filter(([, init]) => init?.method === 'PUT')
      const last = puts[puts.length - 1]
      expect(requestBody<UpdateCacheSettingsInput>(last![1] as RequestInit)?.redis?.password).toBe('')
    })
  })

  it('就地校验：启用 Redis 但主机为空时不发保存请求', async () => {
    const user = userEvent.setup()
    const fetchMock = await renderSettings()
    const card = cacheCard()

    await user.click(within(card).getByRole('radio', { name: 'Redis' }))
    await user.click(within(card).getByRole('button', { name: '保存缓存设置' }))

    expect(await within(cacheCard()).findByText(/必须填写主机/)).toBeTruthy()
    expect(fetchMock.mock.calls.some(([, init]) => init?.method === 'PUT')).toBe(false)
  })

  it('就地校验：端口越界时不发保存请求', async () => {
    const user = userEvent.setup()
    const fetchMock = await renderSettings()
    const card = cacheCard()

    await user.clear(within(card).getByLabelText('端口（port）'))
    await user.type(within(card).getByLabelText('端口（port）'), '70000')
    await user.click(within(card).getByRole('button', { name: '保存缓存设置' }))

    expect(await within(cacheCard()).findByText(/端口需为 1-65535/)).toBeTruthy()
    expect(fetchMock.mock.calls.some(([, init]) => init?.method === 'PUT')).toBe(false)
  })

  it('后端拒绝（40002）时展示原因且不谎报成功', async () => {
    const user = userEvent.setup()
    await renderSettings({
      saveCache: () => fail(40002, '启用 Redis 时必须填写 redis.host'),
    })
    const card = cacheCard()

    await user.click(within(card).getByRole('radio', { name: 'Redis' }))
    await user.type(within(card).getByLabelText('Redis 主机（host）'), '127.0.0.1')
    await user.click(within(card).getByRole('button', { name: '保存缓存设置' }))

    expect(await within(cacheCard()).findByText('启用 Redis 时必须填写 redis.host')).toBeTruthy()
    // 仍在文件缓存（服务端没变）。
    expect(within(cacheCard()).getByText('配置驱动：文件缓存')).toBeTruthy()
  })

  it('重置统计：调用接口并刷新计数（缓存内容不动）', async () => {
    const user = userEvent.setup()
    const fetchMock = await renderSettings()
    expect(await cacheStatusText()).toContain('命中 30')

    await user.click(within(cacheCard()).getByRole('button', { name: '重置统计' }))

    await waitFor(() => {
      expect(fetchMock.mock.calls.some(([input]) => String(input).includes('/admin/cache/reset-stats'))).toBe(true)
    })
    await waitFor(async () => {
      const text = await cacheStatusText()
      expect(text).toContain('命中 0')
      expect(text).toContain('键数量 12')
    })
  })

  it('清空缓存：二次确认后调用接口并把键数量刷成 0', async () => {
    const user = userEvent.setup()
    const fetchMock = await renderSettings()
    expect(await cacheStatusText()).toContain('键数量 12')

    await user.click(within(cacheCard()).getByTestId('cache-flush-open'))
    const dialog = await screen.findByRole('alertdialog')
    expect(dialog.textContent).toContain('只清缓存键，不触碰任何业务数据')
    await user.click(within(dialog).getByRole('button', { name: '确认清空' }))

    await waitFor(() => {
      expect(fetchMock.mock.calls.some(([input]) => String(input).includes('/admin/cache/flush'))).toBe(true)
    })
    await waitFor(async () => {
      expect(await cacheStatusText()).toContain('键数量 0')
    })
    // 弹窗已关闭。
    expect(screen.queryByRole('alertdialog')).toBeNull()
  })
})
