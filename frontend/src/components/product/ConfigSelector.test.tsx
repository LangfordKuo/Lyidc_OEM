import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { ConfigGroup } from '@/api/types'
import ConfigSelector from './ConfigSelector'

// 配置样本取自 2026-10-09 上游实测（商品 1 的 get_product_config）：
// 同一商品同时含下拉（network_type=1）、横条（cpu=6）、数量（bw=11）、系统二级（os=5）。

function makeGroups(): ConfigGroup[] {
  return [
    {
      id: 1,
      name: '魔方云-香港CN2',
      options: [
        {
          id: 2,
          name: 'os|操作系统',
          type: 5,
          upstream_id: 0,
          values: [
            { id: 3, name: '15|Debian^Debian-10.3.3-x64', upstream_id: 0 },
            { id: 8, name: '65|Debian^Debian-11.1-x64', upstream_id: 0 },
            { id: 9, name: '66|CentOS^CentOS-7.9.2111-x64', upstream_id: 0 },
            { id: 10, name: '67|Ubuntu^Ubuntu-22.04-x64', upstream_id: 0 },
          ],
        },
        {
          id: 6,
          name: 'network_type|网络类型',
          type: 1,
          upstream_id: 0,
          values: [
            { id: 32, name: 'vpc|VPC网络', upstream_id: 0 },
            { id: 33, name: 'normal|经典网络', upstream_id: 0 },
          ],
        },
        {
          id: 3,
          name: 'cpu|CPU',
          type: 6,
          upstream_id: 0,
          values: [
            { id: 14, name: '2|2核', upstream_id: 0 },
            { id: 15, name: '4|4核', upstream_id: 0 },
          ],
        },
        {
          id: 7,
          name: 'bw|带宽',
          type: 11,
          upstream_id: 0,
          values: [{ id: 34, name: '带宽', upstream_id: 0, qty_minimum: 20, qty_maximum: 100 }],
        },
      ],
    },
  ]
}

function renderSelector(selected: Record<string, string> = {}) {
  const onChange = vi.fn()
  render(<ConfigSelector groups={makeGroups()} selected={selected} onChange={onChange} />)
  return onChange
}

describe('配置项控件矩阵（R6）', () => {
  it('同一商品按 option_type 渲染多种控件：下拉 / 横条 / 数量并存', () => {
    renderSelector({ '2': '3', '6': '32', '3': '14', '7': '20' })

    // 下拉：网络类型是 select（combobox）
    expect(screen.getByRole('combobox', { name: '网络类型' })).toBeInTheDocument()
    // 横条：CPU 是单选横条（radio）
    expect(screen.getByRole('radio', { name: '2核' })).toBeInTheDocument()
    expect(screen.getByRole('radio', { name: '4核' })).toBeInTheDocument()
    // 数量：带宽是数字输入 + 步进按钮
    expect(screen.getByRole('spinbutton', { name: '带宽' })).toHaveValue(20)
    expect(screen.getByRole('button', { name: '增加带宽' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '减少带宽' })).toBeInTheDocument()
    // 系统：两级（大类 + 版本）
    expect(screen.getByRole('radiogroup', { name: '操作系统大类' })).toBeInTheDocument()
    expect(screen.getByRole('radiogroup', { name: '操作系统版本' })).toBeInTheDocument()
  })

  it('单选横条点击后以「值 id」字符串回调（提交口径不变）', async () => {
    const user = userEvent.setup()
    const onChange = renderSelector({ '2': '3', '6': '32', '3': '14', '7': '20' })

    await user.click(screen.getByRole('radio', { name: '4核' }))
    expect(onChange).toHaveBeenCalledWith(3, '15')
  })

  it('下拉选项显示去前缀后的名称并回显当前值', async () => {
    renderSelector({ '6': '33' })
    expect(screen.getByRole('combobox', { name: '网络类型' })).toHaveTextContent('经典网络')
  })
})

describe('系统两级选择（大类 → 版本）', () => {
  it('第一级为去重后的大类，第二级按所选大类过滤版本', async () => {
    const user = userEvent.setup()
    const onChange = renderSelector({ '2': '3' })

    // 大类：Debian / CentOS / Ubuntu（按上游出现顺序去重）
    const groupBox = screen.getByRole('radiogroup', { name: '操作系统大类' })
    expect(within(groupBox).getAllByRole('radio').map((item) => item.getAttribute('aria-label'))).toEqual([
      'Debian',
      'CentOS',
      'Ubuntu',
    ])

    // 默认回显：选中值 3 属 Debian → 版本区只有 Debian 的两个版本
    const versionBox = screen.getByRole('radiogroup', { name: '操作系统版本' })
    expect(within(versionBox).getAllByRole('radio').map((item) => item.getAttribute('aria-label'))).toEqual([
      'Debian-10.3.3-x64',
      'Debian-11.1-x64',
    ])
    expect(screen.getByRole('radio', { name: 'Debian-10.3.3-x64' })).toHaveAttribute('aria-checked', 'true')

    // 切换大类 → 版本列表过滤，并自动选中该大类第一个版本（值 id 口径）
    await user.click(screen.getByRole('radio', { name: 'Ubuntu' }))
    expect(onChange).toHaveBeenCalledWith(2, '10')
  })

  it('修改已选值时回显大类与版本（草稿/订单快照场景）', () => {
    renderSelector({ '2': '9' })
    expect(screen.getByRole('radio', { name: 'CentOS' })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByRole('radio', { name: 'CentOS-7.9.2111-x64' })).toHaveAttribute('aria-checked', 'true')
    expect(screen.queryByRole('radio', { name: 'Debian-10.3.3-x64' })).not.toBeInTheDocument()
  })

  it('未选大类时第二级置灰并提示', () => {
    renderSelector()
    expect(screen.getByText('请先选择系统大类')).toBeInTheDocument()
    expect(screen.queryByRole('radiogroup', { name: '操作系统版本' })).not.toBeInTheDocument()
  })

  it('点击版本后回调该版本的值 id', async () => {
    const user = userEvent.setup()
    const onChange = renderSelector({ '2': '3' })

    await user.click(screen.getByRole('radio', { name: 'Debian-11.1-x64' }))
    expect(onChange).toHaveBeenCalledWith(2, '8')
  })
})

describe('数量型配置（提交 Qty 口径）', () => {
  it('默认值取 qty_minimum，步进按钮按数量回调（而非值 id）', async () => {
    const user = userEvent.setup()
    const onChange = renderSelector({ '7': '20' })

    const input = screen.getByRole('spinbutton', { name: '带宽' })
    expect(input).toHaveValue(20)
    expect(screen.getByText('范围 20Mbps ~ 100Mbps')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '增加带宽' }))
    expect(onChange).toHaveBeenCalledWith(7, '21')
  })

  it('减少按钮在下界、增加按钮在上界时禁用（范围夹紧）', async () => {
    renderSelector({ '7': '20' })
    expect(screen.getByRole('button', { name: '减少带宽' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '增加带宽' })).toBeEnabled()
  })

  it('手工输入范围内数量时回调数量字符串，越界输入不提交', async () => {
    const user = userEvent.setup()
    const onChange = renderSelector({ '7': '20' })

    const input = screen.getByRole('spinbutton', { name: '带宽' })
    await user.clear(input)
    await user.type(input, '66')
    expect(onChange).toHaveBeenLastCalledWith(7, '66')

    // 低于下界的输入不提交（草稿态），失焦后回落到已提交的合法值
    onChange.mockClear()
    await user.clear(input)
    await user.type(input, '5')
    expect(onChange).not.toHaveBeenCalled()
    await user.tab()
    expect(input).toHaveValue(20)
  })

  it('固定值（min=max）时步进禁用并提示固定值', () => {
    const groups: ConfigGroup[] = [
      {
        id: 1,
        name: '固定',
        options: [
          {
            id: 4,
            name: 'ip_num|IP数量',
            type: 15,
            upstream_id: 0,
            values: [{ id: 36, name: '', upstream_id: 0, qty_minimum: 1, qty_maximum: 1 }],
          },
        ],
      },
    ]
    render(<ConfigSelector groups={groups} selected={{ '4': '1' }} onChange={vi.fn()} />)

    expect(screen.getByRole('spinbutton', { name: 'IP数量' })).toHaveValue(1)
    expect(screen.getByRole('button', { name: '增加IP数量' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '减少IP数量' })).toBeDisabled()
    expect(screen.getByText('固定 1个')).toBeInTheDocument()
  })
})
