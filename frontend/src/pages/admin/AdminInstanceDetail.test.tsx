import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { makeAdminAccount, makeAdminInstanceDetail, makeInstanceLog } from '@/test/fixtures'
import {
  installFetchMock,
  ok,
  renderApp,
  requestBody,
  seedAdminProfile,
  seedAdminToken,
} from '@/test/harness'

// 管理后台「实例详情」：主机凭据遮蔽、暂停/恢复/终止申请的操作确认与请求体、角色矩阵。

const INSTANCE = makeAdminInstanceDetail({
  id: 101,
  name: 'oem-o20261008105520t0j03j',
  status: 'active',
  password: 'Abcd1234Efgh5678',
})

function mockInstance(role: 'admin' | 'finance' | 'support' = 'admin', status = 'active') {
  const instance = { ...INSTANCE, status: status as typeof INSTANCE.status }
  return installFetchMock((url, init) => {
    const path = url.pathname
    if (path === '/api/v1/admin/profile') {
      return ok(makeAdminAccount({ role, username: role }))
    }
    if (path === '/api/v1/admin/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    if (path === '/api/v1/admin/instances/101') {
      return ok(instance)
    }
    if (path === '/api/v1/admin/instances/101/logs') {
      return ok({ items: [makeInstanceLog()], page: 1, page_size: 20, total: 1 })
    }
    if (path === '/api/v1/admin/instances/101/sync' && init.method === 'POST') {
      return ok({
        instance_id: 101,
        action: 'sync',
        message: '已回读上游状态',
        status: 'active',
        power_state: 'on',
        power_desc: '运行中',
        status_changed: false,
        terminated: false,
        instance: INSTANCE,
        next_due_date: INSTANCE.next_due_date,
        upstream_status: 'Active',
      })
    }
    if (path === '/api/v1/admin/instances/101/suspend' && init.method === 'POST') {
      return ok({ instance_id: 101, action: 'suspend', message: '实例已暂停', status: 'suspended' })
    }
    if (path === '/api/v1/admin/instances/101/unsuspend' && init.method === 'POST') {
      return ok({ instance_id: 101, action: 'unsuspend', message: '实例已恢复', status: 'active' })
    }
    if (path === '/api/v1/admin/instances/101/cancel' && init.method === 'POST') {
      return ok({
        instance_id: 101,
        action: 'cancel',
        message: '终止申请已提交',
        status: 'active',
        cancel_request_id: 7,
        cancel_type: 'end_of_billing',
        cancel_status: 'pending',
        cancel_requested_at: '2026-10-08T16:00:00Z',
        duplicate: false,
      })
    }
    return undefined
  })
}

async function renderInstance(role: 'admin' | 'finance' | 'support' = 'admin', status = 'active') {
  const fetchMock = mockInstance(role, status)
  seedAdminToken()
  seedAdminProfile({ role, username: role })
  renderApp(['/admin/instances/101'])
  await screen.findByRole('heading', { name: INSTANCE.name })
  return fetchMock
}

describe('管理后台 · 实例详情与操作', () => {
  it('渲染实例信息与操作记录；主机密码默认遮蔽，可显式显示', async () => {
    await renderInstance()
    const user = userEvent.setup()

    expect(screen.getByText('主机凭据')).toBeInTheDocument()
    // 默认遮蔽：明文不出现在 DOM 中
    expect(screen.queryByText('Abcd1234Efgh5678')).not.toBeInTheDocument()
    expect(screen.getByText('••••••••••')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '显示密码' }))
    expect(screen.getByText('Abcd1234Efgh5678')).toBeInTheDocument()

    // 操作记录
    expect(screen.getByText('开通实例')).toBeInTheDocument()
    expect(screen.getByText('开通完成，主机 ID 10922')).toBeInTheDocument()
  })

  it('暂停实例：原因必填，确认后提交 reason 给上游接口', async () => {
    const fetchMock = await renderInstance()
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: '暂停实例' }))
    expect(await screen.findByText('确认暂停实例？')).toBeInTheDocument()

    // 未填原因 → 拦截提交
    await user.click(screen.getByRole('button', { name: '暂停实例', hidden: false }))
    expect(await screen.findByText('请填写暂停原因（会同步提交给上游）')).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => String(input).includes('/suspend'))).toBe(false)

    await user.type(screen.getByLabelText(/暂停原因/), '欠费催缴')
    await user.click(screen.getByRole('button', { name: '暂停实例' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(([input]) => String(input).includes('/suspend'))
      expect(call).toBeTruthy()
      expect(requestBody(call?.[1] as RequestInit)).toEqual({ reason: '欠费催缴' })
    })
  })

  it('终止申请：选择到期终止并填写原因后提交 type + reason', async () => {
    const fetchMock = await renderInstance()
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: '提交终止申请' }))
    expect(await screen.findByText('确认提交终止申请？')).toBeInTheDocument()

    await user.click(screen.getByRole('radio', { name: '到期终止' }))
    await user.type(screen.getByLabelText(/终止原因/), '会员申请退款终止')
    await user.click(screen.getByRole('button', { name: '提交终止申请' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(([input]) => String(input).includes('/cancel'))
      expect(call).toBeTruthy()
      expect(requestBody(call?.[1] as RequestInit)).toEqual({
        type: 'end_of_billing',
        reason: '会员申请退款终止',
      })
    })
  })

  it('财务角色：暂停/恢复/终止按钮禁用（仅超级管理员），同步仍可用', async () => {
    const fetchMock = await renderInstance('finance')

    expect(screen.getByRole('button', { name: '暂停实例' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '恢复实例' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '提交终止申请' })).toBeDisabled()
    expect(screen.getByText(/仅超级管理员可执行/)).toBeInTheDocument()

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: '同步状态' }))
    await waitFor(() => {
      expect(fetchMock.mock.calls.some(([input]) => String(input).includes('/sync'))).toBe(true)
    })
  })
})
