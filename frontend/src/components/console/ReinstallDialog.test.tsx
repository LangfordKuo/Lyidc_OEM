import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { ReinstallOptions } from '@/api/types'
import { makeInstance } from '@/test/fixtures'
import { installFetchMock, ok, requestBody, seedMemberToken } from '@/test/harness'
import ReinstallDialog from './ReinstallDialog'

// 重装可选系统样本取自 2026-10-09 上游实测（/host/cloudos?productid=1）：
// 13 个系统分 3 个大类（Debian / Ubuntu / CentOS），每项带 group 字段。

function makeOptions(): ReinstallOptions {
  return {
    instance_id: 101,
    os: [
      { id: 3, name: 'Debian-10.3.3-x64', group: 'Debian' },
      { id: 8, name: 'Debian-11.1-x64', group: 'Debian' },
      { id: 9, name: 'CentOS-7.9.2111-x64', group: 'CentOS' },
      { id: 11, name: 'CentOS-9-Stream-x64', group: 'CentOS' },
      { id: 10, name: 'Ubuntu-22.04-x64', group: 'Ubuntu' },
      { id: 5884, name: 'Ubuntu-24.04.1-x64', group: 'Ubuntu' },
    ],
    groups: [
      { id: 'Debian', name: 'Debian' },
      { id: 'Ubuntu', name: 'Ubuntu' },
      { id: 'CentOS', name: 'CentOS' },
    ],
  }
}

function mockOptions(options: ReinstallOptions = makeOptions()) {
  seedMemberToken()
  return installFetchMock((url, init) => {
    if (url.pathname === '/api/v1/instances/101/reinstall-options') {
      return ok(options)
    }
    if (url.pathname === '/api/v1/instances/101/reinstall' && init.method === 'POST') {
      return ok({ status: 'success', message: '重装指令已提交', created_at: '2026-10-09T12:00:00Z' })
    }
    return undefined
  })
}

async function renderDialog() {
  const onOpenChange = vi.fn()
  const onDone = vi.fn()
  render(<ReinstallDialog instance={makeInstance()} onOpenChange={onOpenChange} onDone={onDone} />)
  await screen.findByRole('radiogroup', { name: '系统大类' })
  return { onOpenChange, onDone }
}

describe('重装系统两级选择（R6）', () => {
  it('第一级为系统大类，未选大类时第二级提示且不可提交', async () => {
    mockOptions()
    await renderDialog()

    const groupBox = screen.getByRole('radiogroup', { name: '系统大类' })
    expect(within(groupBox).getAllByRole('radio').map((item) => item.getAttribute('aria-label'))).toEqual([
      'Debian',
      'CentOS',
      'Ubuntu',
    ])
    expect(screen.getByText('请先选择系统大类')).toBeInTheDocument()
    expect(screen.queryByRole('radiogroup', { name: '系统版本' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '确认重装' })).toBeDisabled()
  })

  it('选大类后版本列表按大类过滤，选版本后可提交', async () => {
    const fetchMock = mockOptions()
    const { onOpenChange, onDone } = await renderDialog()
    const user = userEvent.setup()

    await user.click(screen.getByRole('radio', { name: 'CentOS' }))
    const versionBox = screen.getByRole('radiogroup', { name: '系统版本' })
    expect(within(versionBox).getAllByRole('radio').map((item) => item.getAttribute('aria-label'))).toEqual([
      'CentOS-7.9.2111-x64',
      'CentOS-9-Stream-x64',
    ])
    // 只选了大类还没有版本：仍不可提交
    expect(screen.getByRole('button', { name: '确认重装' })).toBeDisabled()

    await user.click(screen.getByRole('radio', { name: 'CentOS-9-Stream-x64' }))
    await user.click(screen.getByRole('button', { name: '确认重装' }))

    const call = fetchMock.mock.calls.find(
      ([input, init]) =>
        String(input).includes('/api/v1/instances/101/reinstall') &&
        (init as RequestInit)?.method === 'POST',
    )
    expect(call).toBeTruthy()
    // 提交口径不变：os_id 为该版本的上游系统 ID
    expect(requestBody((call as [RequestInfo | URL, RequestInit])[1])).toEqual({ os_id: 11 })
    expect(onDone).toHaveBeenCalled()
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('切换大类时丢弃不属于新大类的已选版本', async () => {
    mockOptions()
    await renderDialog()
    const user = userEvent.setup()

    await user.click(screen.getByRole('radio', { name: 'Debian' }))
    await user.click(screen.getByRole('radio', { name: 'Debian-11.1-x64' }))
    expect(screen.getByRole('button', { name: '确认重装' })).toBeEnabled()

    await user.click(screen.getByRole('radio', { name: 'Ubuntu' }))
    // 已选版本不属于 Ubuntu → 清空，需重新选版本
    expect(screen.getByRole('button', { name: '确认重装' })).toBeDisabled()
    expect(screen.queryByRole('radio', { name: 'Debian-11.1-x64' })).not.toBeInTheDocument()
  })

  it('大类间来回切换时保留同大类内的已选版本', async () => {
    mockOptions()
    await renderDialog()
    const user = userEvent.setup()

    await user.click(screen.getByRole('radio', { name: 'Debian' }))
    await user.click(screen.getByRole('radio', { name: 'Debian-11.1-x64' }))
    await user.click(screen.getByRole('radio', { name: 'Ubuntu' }))
    await user.click(screen.getByRole('radio', { name: 'Debian' }))

    expect(screen.getByRole('radio', { name: 'Debian-11.1-x64' })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByRole('button', { name: '确认重装' })).toBeEnabled()
  })

  it('上游未返回分组信息时退化为单级列表（历史兼容）', async () => {
    mockOptions({
      instance_id: 101,
      os: [
        { id: 3, name: 'Debian-10.3.3-x64', group: '' },
        { id: 9, name: 'CentOS-7.9.2111-x64', group: '' },
      ],
      groups: [],
    })
    const onOpenChange = vi.fn()
    render(<ReinstallDialog instance={makeInstance()} onOpenChange={onOpenChange} onDone={vi.fn()} />)
    await screen.findByRole('radiogroup', { name: '选择操作系统' })

    expect(screen.queryByRole('radiogroup', { name: '系统大类' })).not.toBeInTheDocument()
    expect(screen.getByRole('radio', { name: 'Debian-10.3.3-x64' })).toBeInTheDocument()
  })
})
