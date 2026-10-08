import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import Pager from './Pager'

// 分页条：汇总文案、页码跳转、上一页/下一页边界禁用。
describe('Pager', () => {
  it('展示汇总文案与页码，点击页码回调目标页', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(<Pager page={2} total={45} pageSize={20} onChange={onChange} />)

    expect(screen.getByText('共 45 条 · 第 2/3 页')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '3' }))
    expect(onChange).toHaveBeenCalledWith(3)
  })

  it('首页禁用「上一页」，末页禁用「下一页」', () => {
    const { unmount } = render(<Pager page={1} total={100} pageSize={20} onChange={vi.fn()} />)
    expect(screen.getByRole('button', { name: /上一页/ })).toBeDisabled()
    unmount()

    render(<Pager page={5} total={100} pageSize={20} onChange={vi.fn()} />)
    expect(screen.getByRole('button', { name: /下一页/ })).toBeDisabled()
  })

  it('只有一页时只显示汇总文案', () => {
    render(<Pager page={1} total={3} pageSize={20} onChange={vi.fn()} />)

    expect(screen.getByText('共 3 条 · 第 1/1 页')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /下一页/ })).not.toBeInTheDocument()
  })
})
