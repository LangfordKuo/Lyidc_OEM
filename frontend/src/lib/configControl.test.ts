import { describe, expect, it } from 'vitest'

import type { ConfigOption } from '@/api/types'
import {
  configControl,
  defaultConfigValue,
  describeConfigSelection,
  isQuantityOption,
  osGroupsOf,
  quantityRange,
  quantityUnit,
  selectedOsGroup,
} from './configControl'

// 样本取自 2026-10-09 上游实测（商品 1 / 161）：option_type 与值的形态一一对应。

function option(overrides: Partial<ConfigOption> = {}): ConfigOption {
  return { id: 1, name: 'area|区域', type: 12, upstream_id: 0, values: [], ...overrides }
}

describe('option_type → 控件映射（R6 实测口径）', () => {
  it.each([
    [1, 'network_type|网络类型', 'select'],
    [5, 'os|操作系统', 'os'],
    [6, 'cpu|CPU', 'chips'],
    [8, 'memory|内存', 'chips'],
    [10, '带宽', 'chips'],
    [12, 'area|区域', 'chips'],
    [13, 'system_disk_size|系统盘', 'chips'],
    [4, 'ip_num|IP数量', 'quantity'],
    [11, 'bw|带宽', 'quantity'],
    [14, 'data_disk_size|数据盘', 'quantity'],
    [15, 'ip_num|IP数量', 'quantity'],
    [19, '系统盘', 'quantity'],
  ] as const)('option_type=%d（%s）→ %s', (type, name, expected) => {
    expect(configControl(option({ type, name }))).toBe(expected)
  })

  it('未收录类型按值形态兜底：多个 ^ 大类 → 二级', () => {
    const unknown = option({
      type: 99,
      name: 'os|操作系统',
      values: [
        { id: 1, name: '15|Debian^Debian-10.3.3-x64', upstream_id: 0 },
        { id: 2, name: '66|CentOS^CentOS-7.9.2111-x64', upstream_id: 0 },
      ],
    })
    expect(configControl(unknown)).toBe('os')
  })

  it('未收录类型按值形态兜底：单值且无 `id|名` 前缀 → 数量型', () => {
    expect(
      configControl(option({ type: 99, name: 'bw|带宽', values: [{ id: 9, name: '带宽', upstream_id: 0 }] })),
    ).toBe('quantity')
  })

  it('未收录类型按值形态兜底：选项多则下拉，少则横条', () => {
    const many = Array.from({ length: 9 }, (_, i) => ({ id: i + 1, name: `${i + 1}|${i + 1}核`, upstream_id: 0 }))
    expect(configControl(option({ type: 99, values: many }))).toBe('select')
    expect(
      configControl(option({ type: 99, values: many.slice(0, 3) })),
    ).toBe('chips')
  })
})

describe('数量型（Qty 口径）', () => {
  const bw = option({
    id: 7,
    name: 'bw|带宽',
    type: 11,
    values: [{ id: 34, name: '带宽', upstream_id: 0, qty_minimum: 20, qty_maximum: 100 }],
  })

  it('默认值取 qty_minimum，单位按键名映射', () => {
    expect(isQuantityOption(bw)).toBe(true)
    expect(quantityRange(bw)).toEqual({ min: 20, max: 100 })
    expect(defaultConfigValue(bw)).toBe('20')
    expect(quantityUnit(bw)).toBe('Mbps')
  })

  it('范围缺省回退 0；min/max 颠倒时自动纠正', () => {
    const fixed = option({ type: 14, name: 'data_disk_size|数据盘', values: [{ id: 37, name: '数据盘', upstream_id: 0 }] })
    expect(quantityRange(fixed)).toEqual({ min: 0, max: 0 })

    const reversed = option({ type: 11, name: 'bw|带宽', values: [{ id: 1, name: '带宽', upstream_id: 0, qty_minimum: 50, qty_maximum: 10 }] })
    expect(quantityRange(reversed)).toEqual({ min: 10, max: 50 })
  })

  it('单位按配置项键名与显示名双查表', () => {
    expect(quantityUnit(option({ name: 'ip_num|IP数量', type: 15, values: [] }))).toBe('个')
    expect(quantityUnit(option({ name: 'data_disk_size|数据盘', type: 14, values: [] }))).toBe('GB')
    expect(quantityUnit(option({ name: '快照数量', type: 15, values: [] }))).toBe('')
  })

  it('选项型默认值取第一个可选值', () => {
    const os = option({
      type: 5,
      name: 'os|操作系统',
      values: [
        { id: 3, name: '15|Debian^Debian-10.3.3-x64', upstream_id: 0 },
        { id: 4, name: '17|Ubuntu^Ubuntu-18.04-x64', upstream_id: 0 },
      ],
    })
    expect(defaultConfigValue(os)).toBe('3')
  })

  it('无值可选的选项返回 null（调用方跳过）', () => {
    expect(defaultConfigValue(option({ type: 1, values: [] }))).toBeNull()
  })
})

describe('系统两级选择', () => {
  const os = option({
    id: 2,
    name: 'os|操作系统',
    type: 5,
    values: [
      { id: 3, name: '15|Debian^Debian-10.3.3-x64', upstream_id: 0 },
      { id: 8, name: '65|Debian^Debian-11.1-x64', upstream_id: 0 },
      { id: 9, name: '66|CentOS^CentOS-7.9.2111-x64', upstream_id: 0 },
      { id: 10, name: '67|Ubuntu^Ubuntu-22.04-x64', upstream_id: 0 },
    ],
  })

  it('按 ^ 前缀分组并保持出现顺序', () => {
    expect(osGroupsOf(os.values).map((group) => [group.name, group.values.length])).toEqual([
      ['Debian', 2],
      ['CentOS', 1],
      ['Ubuntu', 1],
    ])
  })

  it('已选值可回溯所属大类（修改时回显）', () => {
    expect(selectedOsGroup(os, '8')).toBe('Debian')
    expect(selectedOsGroup(os, '10')).toBe('Ubuntu')
    expect(selectedOsGroup(os, '999')).toBe('')
  })

  it('无 ^ 的值归入「其它」（异常数据兜底）', () => {
    const weird = osGroupsOf([{ id: 1, name: 'Debian-10', upstream_id: 0 }])
    expect(weird).toEqual([{ name: '其它', values: [{ id: 1, name: 'Debian-10', upstream_id: 0 }] }])
  })
})

describe('取值展示（订单快照）', () => {
  it('选项型显示 valueLabel（保留 HK · 香港 全量信息）', () => {
    const area = option({ type: 12, values: [{ id: 1, name: '1|HK^香港', upstream_id: 0 }] })
    expect(describeConfigSelection(area, '1')).toBe('HK · 香港')
  })

  it('数量型显示「单位名 + 数量 + 单位」（Qty 口径）', () => {
    const disk = option({
      type: 14,
      name: 'data_disk_size|数据盘',
      values: [{ id: 37, name: '数据盘', upstream_id: 0, qty_minimum: 0, qty_maximum: 256 }],
    })
    expect(describeConfigSelection(disk, '100')).toBe('数据盘 100GB')
    expect(describeConfigSelection(disk, '0')).toBe('数据盘 0GB')
  })

  it('数量型无单位名时只显示数量（如 ip_num 的空名值）', () => {
    const ip = option({
      type: 15,
      name: 'ip_num|IP数量',
      values: [{ id: 36, name: '', upstream_id: 0, qty_minimum: 1, qty_maximum: 1 }],
    })
    expect(describeConfigSelection(ip, '3')).toBe('3个')
  })

  it('选项型的未知 id 退化为「值 #id」，空值为占位符', () => {
    const area = option({ type: 12, values: [{ id: 1, name: '1|HK^香港', upstream_id: 0 }] })
    expect(describeConfigSelection(area, '999')).toBe('值 #999')
    expect(describeConfigSelection(area, '')).toBe('—')
  })
})
