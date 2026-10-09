// 产品配置项的「选择方式」判定（R6，契约 10.3 备注）。
//
// option_type 的控件映射来自 2026-10-09 对上游 159 个商品的实测（get_product_config 全量拉取）
// 与 lyew.com 前台配置页（/cart?action=configureproduct&pid=N）渲染对照，结论：
//
//   option_type | 上游形态            | 实测键名样例                  | 本项目控件
//   ------------|--------------------|------------------------------|----------
//   1           | <select> 下拉      | network_type / 快照数量/备份数量 | select
//   4           | 滑条 + 数字输入     | ip_num                        | quantity
//   5           | 两级（大类卡→版本） | os                            | os（二级）
//   6           | radio 横条          | cpu / 网络类型 / 系统盘        | chips
//   8           | radio 横条          | memory / 流量 / 域名           | chips
//   10          | radio 横条          | 带宽（档位）/ 流入带宽          | chips
//   11          | 滑条 + 数字输入     | bw                            | quantity
//   12          | radio 横条（带图标）| area / 数据中心                | chips
//   13          | radio 横条          | system_disk_size / data_disk_size | chips
//   14          | 滑条 + 数字输入     | data_disk_size                | quantity
//   15          | 滑条 + 数字输入     | ip_num / 流量                  | quantity
//   19          | 滑条 + 数字输入     | 系统盘 / 数据盘                 | quantity
//
// 数量型（quantity）的提交口径：`configoption[<配置项 id>] = <数量>`（上游官方文档：
// 「所选择的子项ID，拉条传数量」，契约 12.4 的 configoption 口径）；范围取值的
// qty_minimum / qty_maximum，默认值为 qty_minimum。
//
// 未收录的 option_type 按值形态兜底：值含 `^` 且存在多个大类 → 二级；单值且值名不含
// `id|名` 前缀 → 数量型；选项数 > 8 → 下拉；其余 → 横条。

import type { ConfigOption, ConfigOptionValue } from '@/api/types'
import { optionKey, valueDisplayName, valueGroupName, valueLabel } from './productText'

export type ConfigControl = 'select' | 'chips' | 'os' | 'quantity'

const CONTROL_BY_TYPE: Record<number, ConfigControl> = {
  1: 'select',
  4: 'quantity',
  5: 'os',
  6: 'chips',
  8: 'chips',
  10: 'chips',
  11: 'quantity',
  12: 'chips',
  13: 'chips',
  14: 'quantity',
  15: 'quantity',
  19: 'quantity',
}

/** 值名是否是「值ID|显示名」形态（数量型/展示型的值是裸单位名，如 `带宽`、`数据盘`、空串）。 */
function hasIdPrefix(name: string): boolean {
  return /^[^|]+\|/.test(name.trim())
}

/** 判定一个配置项用什么控件（option_type 优先，未收录时按值形态兜底）。 */
export function configControl(option: ConfigOption): ConfigControl {
  const mapped = CONTROL_BY_TYPE[option.type]
  if (mapped) {
    return mapped
  }
  const values = option.values
  if (osGroupsOf(values).length > 1) {
    return 'os'
  }
  if (values.length === 1 && !hasIdPrefix(values[0]?.name ?? '')) {
    return 'quantity'
  }
  return values.length > 8 ? 'select' : 'chips'
}

// ---------------------------------------------------------------------------
// 数量型（quantity）
// ---------------------------------------------------------------------------

/** 数量型配置的取数口径：值的裸单位名（'带宽'/'数据盘'/''）只作展示，提交的是数量。 */
export function isQuantityOption(option: ConfigOption): boolean {
  return configControl(option) === 'quantity'
}

/** 数量型的取值范围与默认值（取自值的 qty_minimum / qty_maximum，缺省回退 0）。 */
export function quantityRange(option: ConfigOption): { min: number; max: number } {
  const value = option.values[0]
  const min = value?.qty_minimum ?? 0
  const max = value?.qty_maximum ?? min
  return max < min ? { min: max, max: min } : { min, max }
}

/** 数量型的默认值（= qty_minimum，与上游前台初始值一致）。 */
export function defaultQuantity(option: ConfigOption): number {
  return quantityRange(option).min
}

/**
 * 配置项的默认取值（下单/结算页推导初始选择用）：
 * 数量型取 qty_minimum（提交的是数量），其余取第一个可选值的 id。
 * 无可用值时返回 null（调用方跳过该项）。
 */
export function defaultConfigValue(option: ConfigOption): string | null {
  if (isQuantityOption(option)) {
    return String(defaultQuantity(option))
  }
  const first = option.values[0]
  return first ? String(first.id) : null
}

const UNIT_BY_KEY: Record<string, string> = {
  bw: 'Mbps',
  bandwidth: 'Mbps',
  带宽: 'Mbps',
  流入带宽: 'Mbps',
  宽带: 'Mbps',
  ip_num: '个',
  ip: '个',
  IP数量: '个',
  data_disk_size: 'GB',
  system_disk_size: 'GB',
  数据盘: 'GB',
  系统盘: 'GB',
  traffic: 'GB',
  流量: 'GB',
}

/**
 * 数量型的展示单位：上游接口不直接下发单位（前台按配置项模板映射），
 * 这里按配置项键名（`bw|带宽` → bw）与显示名双查表，未知时返回空串。
 */
export function quantityUnit(option: ConfigOption): string {
  const key = optionKey(option.name)
  return UNIT_BY_KEY[key] ?? UNIT_BY_KEY[option.name.trim()] ?? ''
}

// ---------------------------------------------------------------------------
// 系统类（os）：两级选择
// ---------------------------------------------------------------------------

export interface ConfigValueGroup {
  name: string
  values: ConfigOptionValue[]
}

/**
 * 按值的 `^` 前缀分组（大类），保持上游出现顺序。
 * 无 `^` 的值归入「其它」（上游系统值恒带 `^`，此分支只兜底异常数据）。
 */
export function osGroupsOf(values: ConfigOptionValue[]): ConfigValueGroup[] {
  const groups: ConfigValueGroup[] = []
  const index = new Map<string, ConfigValueGroup>()
  for (const value of values) {
    const name = valueGroupName(value.name) || '其它'
    let group = index.get(name)
    if (!group) {
      group = { name, values: [] }
      index.set(name, group)
      groups.push(group)
    }
    group.values.push(value)
  }
  return groups
}

/** 配置项是否是「两级系统选择」形态（option_type=5，或值含多个 `^` 大类）。 */
export function isOsOption(option: ConfigOption): boolean {
  return configControl(option) === 'os'
}

/** 已选值所属的大类名（未选中时返回空串）。 */
export function selectedOsGroup(option: ConfigOption, valueId: string): string {
  const value = option.values.find((item) => String(item.id) === valueId)
  return value ? valueGroupName(value.name) || '其它' : ''
}

// ---------------------------------------------------------------------------
// 取值展示（订单快照 / 结算页摘要）
// ---------------------------------------------------------------------------

/**
 * 把配置项快照里的原始取值翻译成展示文案（R6）：
 * - 数量型：值是数量，显示为 `<单位名> <数量><单位>`（如「数据盘 100GB」），无单位名时只显示数量；
 * - 选项型：值是最优显示名（valueLabel，保留 `HK · 香港` 的全量信息）；id 不在选项里时退化为 `值 #id`。
 */
export function describeConfigSelection(option: ConfigOption, raw: string): string {
  const text = raw.trim()
  if (isQuantityOption(option)) {
    const amount = Number.parseInt(text, 10)
    if (!Number.isFinite(amount)) {
      return text || '—'
    }
    const unit = quantityUnit(option)
    const quantity = `${amount}${unit}`
    const name = valueDisplayName(option.values[0]?.name ?? '')
    return name ? `${name} ${quantity}` : quantity
  }
  const value = option.values.find((item) => String(item.id) === text)
  if (value) {
    return valueLabel(value.name)
  }
  return text ? `值 #${text}` : '—'
}
