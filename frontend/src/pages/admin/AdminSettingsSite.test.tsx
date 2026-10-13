import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'

import type { SiteSettings } from '@/api/types'
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

// 管理后台「设置」页的「站点对外地址」分区（R16 / 契约 22.4）：
// 未配置提示、保存请求体（去尾斜杠）、保存后回读、非法地址就地拒绝（不发请求）。

const SITE_UNSET: SiteSettings = {
  url: '',
  configured: false,
  url_recommended: 'http://localhost',
  updated_by: null,
  updated_at: null,
}

/** 组装设置页所需的全部接口；site 节点控制「站点对外地址」的响应。 */
function mockSettingsPage(
  nodes: { site?: MockResponse | (() => MockResponse); saveSite?: () => MockResponse } = {},
) {
  let saved: SiteSettings | null = null
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
    if (path === '/api/v1/admin/settings/site' && method === 'GET') {
      if (saved) {
        return ok(saved)
      }
      const site = nodes.site ?? ok(SITE_UNSET)
      return typeof site === 'function' ? site() : site
    }
    if (path === '/api/v1/admin/settings/site' && method === 'PUT') {
      if (nodes.saveSite) {
        return nodes.saveSite()
      }
      const url = (requestBody<{ url?: string }>(init)?.url ?? '').trim()
      saved = { ...SITE_UNSET, url, configured: url !== '' }
      return ok(saved)
    }
    return undefined
  })
}

async function renderSettings(nodes: Parameters<typeof mockSettingsPage>[0] = {}) {
  const fetchMock = mockSettingsPage(nodes)
  seedAdminToken()
  seedAdminProfile({ role: 'admin', username: 'admin' })
  renderApp(['/admin/settings'])
  await screen.findByRole('heading', { name: '设置' })
  return fetchMock
}

/** 站点分区卡片（卡根节点，用于把断言限定在本分区内）。 */
function siteCard(): HTMLElement {
  const heading = screen.getByText('站点对外地址（site）')
  const card = heading.closest('[data-slot="card"]')
  if (!card) {
    throw new Error('找不到站点分区卡片')
  }
  return card as HTMLElement
}

describe('管理后台设置页：站点对外地址（R16）', () => {
  beforeEach(() => {
    window.localStorage.clear()
  })

  it('未配置时展示「按请求推断」提示与推荐地址', async () => {
    const fetchMock = await renderSettings({ site: ok(SITE_UNSET) })

    const state = await screen.findByTestId('site-url-state')
    expect(state.textContent).toContain('当前未配置')
    expect(state.textContent).toContain(SITE_UNSET.url_recommended)

    // 输入框是空的（原值为空串）。
    const input = screen.getByLabelText('站点对外地址') as HTMLInputElement
    expect(input.value).toBe('')

    const calls = fetchMock.mock.calls.map(([input]) => String(input))
    expect(calls.some((raw) => raw.includes('/api/v1/admin/settings/site'))).toBe(true)
  })

  it('保存时带上去尾斜杠的地址并在成功后回读', async () => {
    const user = userEvent.setup()
    const fetchMock = await renderSettings({ site: ok(SITE_UNSET) })

    const input = await screen.findByLabelText('站点对外地址')
    await user.type(input, 'https://oem.example.com/')
    await user.click(screen.getByRole('button', { name: '保存站点对外地址' }))

    await waitFor(() => {
      const put = fetchMock.mock.calls.find(([, init]) => init?.method === 'PUT')
      expect(put).toBeTruthy()
      expect(requestBody<{ url: string }>(put![1] as RequestInit)).toEqual({
        url: 'https://oem.example.com',
      })
    })

    await waitFor(() => {
      expect(screen.getByTestId('site-url-state').textContent).toContain('当前已配置')
    })
    expect(screen.getByTestId('site-url-state').textContent).toContain('https://oem.example.com')
  })

  it('非法地址就地拒绝，且不发保存请求', async () => {
    const user = userEvent.setup()
    const fetchMock = await renderSettings({ site: ok(SITE_UNSET) })

    const input = await screen.findByLabelText('站点对外地址')
    await user.type(input, 'oem.example.com')
    await user.click(screen.getByRole('button', { name: '保存站点对外地址' }))

    const card = siteCard()
    expect(await within(card).findByText(/必须是带主机名的 http/)).toBeTruthy()
    expect(fetchMock.mock.calls.some(([, init]) => init?.method === 'PUT')).toBe(false)
  })

  it('后端拒绝（40002）时就地展示原因且不改变展示值', async () => {
    const user = userEvent.setup()
    await renderSettings({
      site: ok(SITE_UNSET),
      saveSite: () => fail(40002, '站点对外地址必须是带主机名的 http:// 或 https:// 地址，收到 "https://"'),
    })

    const input = await screen.findByLabelText('站点对外地址')
    await user.clear(input)
    await user.type(input, 'https://oem.example.com')
    await user.click(screen.getByRole('button', { name: '保存站点对外地址' }))

    expect(await screen.findByText(/必须是带主机名的 http/)).toBeTruthy()
    expect(screen.getByTestId('site-url-state').textContent).toContain('当前未配置')
  })

  it('已配置时回显原值', async () => {
    await renderSettings({
      site: ok({
        url: 'https://oem.example.com',
        configured: true,
        url_recommended: 'http://localhost',
        updated_by: 1,
        updated_at: '2026-10-14T08:00:00Z',
      }),
    })

    const input = (await screen.findByLabelText('站点对外地址')) as HTMLInputElement
    expect(input.value).toBe('https://oem.example.com')
    expect(screen.getByTestId('site-url-state').textContent).toContain('当前已配置')
  })
})
