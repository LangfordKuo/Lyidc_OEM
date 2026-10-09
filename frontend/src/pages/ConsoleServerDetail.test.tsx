import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { makeInstanceDetail, makeInstanceLog, makeMember } from '@/test/fixtures'
import { installFetchMock, ok, renderApp, requestBody, seedMemberToken } from '@/test/harness'

// 会员区「实例详情」：密码默认遮蔽可显示、危险操作二次确认。

const PASSWORD = 'Abcd1234Efgh5678'

function mockDetail() {
  return installFetchMock((url, init) => {
    if (url.pathname === '/api/v1/members/me') {
      return ok(makeMember())
    }
    if (url.pathname === '/api/v1/notifications/unread-count') {
      return ok({ unread: 0 })
    }
    if (url.pathname === '/api/v1/instances/101') {
      return ok(makeInstanceDetail({ password: PASSWORD }))
    }
    if (url.pathname === '/api/v1/instances/101/logs') {
      return ok({ items: [makeInstanceLog()], page: 1, page_size: 20, total: 1 })
    }
    if (url.pathname === '/api/v1/instances/101/power' && init.method === 'POST') {
      return ok({
        instance_id: 101,
        action: 'hard_off',
        message: '强制关机指令已提交',
        status: 'active',
      })
    }
    return undefined
  })
}

async function renderDetail() {
  seedMemberToken()
  renderApp(['/console/servers/101'])
  // R4：主标题改为商品名（含区域），实例名与 ID 降为次要行。
  await screen.findByRole('heading', { name: '香港二区 CN2 A型' })
}

describe('会员区 · 实例详情', () => {
  it('密码默认遮蔽，点击后可显示明文并可再次隐藏', async () => {
    mockDetail()
    const user = userEvent.setup()
    await renderDetail()

    // 默认只显示遮蔽点，明文不出现在文档里
    expect(screen.getByText('••••••••••')).toBeInTheDocument()
    expect(screen.queryByText(PASSWORD)).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '显示密码' }))
    expect(screen.getByText(PASSWORD)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '隐藏密码' }))
    expect(screen.queryByText(PASSWORD)).not.toBeInTheDocument()
  })

  it('强制关机需二次确认，确认后按 op 提交电源请求', async () => {
    const fetchMock = mockDetail()
    const user = userEvent.setup()
    await renderDetail()

    await user.click(screen.getByRole('button', { name: '强制关机' }))

    const dialog = await screen.findByRole('alertdialog')
    expect(within(dialog).getByText('确认强制关机？')).toBeInTheDocument()
    expect(within(dialog).getByText(/可能造成数据损坏/)).toBeInTheDocument()

    await user.click(within(dialog).getByRole('button', { name: '强制关机' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(([input]) =>
        String(input).includes('/api/v1/instances/101/power'),
      )
      expect(call).toBeTruthy()
      expect(requestBody(call![1] ?? {})).toEqual({ op: 'hard_off' })
    })
  })

  it('取消二次确认不会发起电源请求', async () => {
    const fetchMock = mockDetail()
    const user = userEvent.setup()
    await renderDetail()

    await user.click(screen.getByRole('button', { name: '重启' }))
    const dialog = await screen.findByRole('alertdialog')
    await user.click(within(dialog).getByRole('button', { name: '取消' }))

    await waitFor(() => {
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    })
    expect(
      fetchMock.mock.calls.some(([input]) => String(input).includes('/power')),
    ).toBe(false)
  })

  it('实例已暂停时电源操作与重装/改密按钮禁用', async () => {
    installFetchMock((url) => {
      if (url.pathname === '/api/v1/members/me') {
        return ok(makeMember())
      }
      if (url.pathname === '/api/v1/notifications/unread-count') {
        return ok({ unread: 0 })
      }
      if (url.pathname === '/api/v1/instances/101') {
        return ok(makeInstanceDetail({ status: 'suspended' }))
      }
      if (url.pathname === '/api/v1/instances/101/logs') {
        return ok({ items: [], page: 1, page_size: 20, total: 0 })
      }
      return undefined
    })
    await renderDetail()

    expect(screen.getByRole('button', { name: '开机' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '重装系统' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '重置密码' })).toBeDisabled()
    // 已暂停仍可续费与申请终止（契约 15.1 操作矩阵）
    expect(screen.getByRole('button', { name: '续费' })).toBeEnabled()
    expect(screen.getByRole('button', { name: '申请终止' })).toBeEnabled()
  })
})
