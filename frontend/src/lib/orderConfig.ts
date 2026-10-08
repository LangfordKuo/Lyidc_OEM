import type { ProductDetail } from '../api/types'
import { optionLabel, valueLabel } from './productText'

// 订单配置快照的可读化：`orders.config` 存的是「配置项 id → 所选值 id」（契约 12.3）。
// 商品详情能取到时翻译成中文名；取不到（商品已下架等）时退化为 id 展示，不隐藏信息。

export interface OrderConfigEntry {
  key: string
  label: string
  value: string
}

export function describeOrderConfig(
  product: ProductDetail | null,
  config: Record<string, string> | null | undefined,
): OrderConfigEntry[] {
  const entries = Object.entries(config ?? {})
  if (entries.length === 0) {
    return []
  }
  if (!product) {
    return entries.map(([key, value]) => ({
      key,
      label: `配置项 #${key}`,
      value: value ? `值 #${value}` : '—',
    }))
  }

  return entries.map(([key, value]) => {
    const option = product.config_groups
      .flatMap((group) => group.options)
      .find((item) => String(item.id) === key)
    const optionValue = option?.values.find((item) => String(item.id) === value)
    return {
      key,
      label: option ? optionLabel(option.name) : `配置项 #${key}`,
      value: optionValue ? valueLabel(optionValue.name) : value ? `值 #${value}` : '—',
    }
  })
}
