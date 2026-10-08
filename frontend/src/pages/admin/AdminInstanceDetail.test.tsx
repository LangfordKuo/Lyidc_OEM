import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AdminInstanceDetail } from '../../api/types'
import { renderAdmin } from '../../test/adminHarness'
import { apiGet, apiPost, requestBody } from '../../test/consoleHarness'

// 实例详情（阶段 8 新接口）：字段对齐会员端 + member_id；操作按契约 15.3 的角色矩阵控制。
function detailResponse(): AdminInstanceDetail {
  return {
    id: 5,
    order_id: 12,
    host_id: 10922,
    product_id: 3,
    product_name: '香港二区 CN2 A型',
    name: 'oem-o20261008105520t0j03j',
    billing_cycle: 'monthly',
    next_due_date: '2026-11-08T10:55:25Z',
    status: 'active',
    upstream_status: 'Active',
    dedicated_ip: '203.0.113.9',
    cancel_status: 'none',
    cancel_type: '',
    cancel_request_id: 0,
    cancel_requested_at: null,
    created_at: '2026-10-08T10:55:40Z',
    member_id: 9,
    assigned_ips: ['203.0.113.10'],
    port: 22,
    username: 'root',
    password: 'Abcd1234Efgh5678',
    updated_at: '2026-10-08T11:00:00Z',
  }
}

function handlers() {
  return [
    apiGet('/api/v1/admin/instances/5/logs', { items: [], page: 1, page_size: 20, total: 0 }),
    apiGet('/api/v1/admin/instances/5', detailResponse()),
    apiPost('/api/v1/admin/instances/5/sync', {
      instance_id: 5,
      action: 'sync',
      message: '同步完成',
      status: 'active',
      power_state: 'on',
      power_desc: '运行中',
      status_changed: false,
      terminated: false,
      instance: detailResponse(),
      next_due_date: '2026-11-08T10:55:25Z',
      upstream_status: 'Active',
    }),
    apiPost('/api/v1/admin/instances/5/suspend', {
      instance_id: 5,
      action: 'suspend',
      message: '已暂停',
      status: 'suspended',
    }),
  ]
}

describe('管理后台 · 实例详情', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('渲染详情字段：会员归属、主机凭据（密码默认遮蔽）', async () => {
    renderAdmin('/admin/instances/5', 'admin', handlers())

    expect(await screen.findByText('oem-o20261008105520t0j03j')).toBeInTheDocument()
    expect(screen.getAllByText('#9').length).toBeGreaterThan(0)
    expect(screen.getByText('10922')).toBeInTheDocument()
    expect(screen.getByText('root')).toBeInTheDocument()
    // 密码默认遮蔽，明文不出现在 DOM 中
    expect(screen.getByText('••••••••')).toBeInTheDocument()
    expect(screen.queryByText('Abcd1234Efgh5678')).not.toBeInTheDocument()
  })

  it('admin：同步状态直接调用接口', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/instances/5', 'admin', handlers())

    await screen.findByText('oem-o20261008105520t0j03j')
    await user.click(screen.getByRole('button', { name: '同步状态' }))

    await waitFor(() => {
      expect(
        fetchCallsHas('POST', '/api/v1/admin/instances/5/sync'),
      ).toBe(true)
    })
    // 结果同时出现在页面提示与 toast 中，这里只要求至少渲染一处
    expect((await screen.findAllByText(/同步完成/)).length).toBeGreaterThan(0)
  })

  it('admin：暂停实例需填写原因（必填校验 + 请求体）', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/instances/5', 'admin', handlers())

    await screen.findByText('oem-o20261008105520t0j03j')
    await user.click(screen.getByRole('button', { name: '暂停实例' }))

    const dialog = await screen.findByRole('dialog')
    const confirm = within(dialog).getByRole('button', { name: '暂停实例' })
    expect(confirm).toBeDisabled()

    await user.type(within(dialog).getByLabelText(/暂停原因/), '涉嫌滥用资源')
    await user.click(confirm)

    await waitFor(() => {
      expect(requestBody('POST', '/api/v1/admin/instances/5/suspend')).toEqual({
        reason: '涉嫌滥用资源',
      })
    })
  })

  it('finance：暂停/恢复/终止禁用，同步仍可用', async () => {
    renderAdmin('/admin/instances/5', 'finance', handlers())

    await screen.findByText('oem-o20261008105520t0j03j')
    expect(screen.getByRole('button', { name: '暂停实例' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '恢复实例' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '提交终止申请' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '同步状态' })).toBeEnabled()
    expect(screen.getByText(/暂停 \/ 恢复 \/ 终止申请/)).toBeInTheDocument()
  })
})

function fetchCallsHas(method: string, path: string): boolean {
  const calls = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls as [string, RequestInit][]
  return calls.some(
    ([url, init]) => url.startsWith(path) && (init.method ?? 'GET').toUpperCase() === method,
  )
}
