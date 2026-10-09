// 上游配置项文案的展示化处理。
// 契约 10.3 备注 3：`name` 是上游文案原文（形如 `area|区域`、`1|HK^香港`），阶段 3a 不做清洗。
// 前端只做「去前缀」的可读化展示，不改动提交给后端的 id。

/** 配置项名：`area|区域` → `区域`。 */
export function optionLabel(name: string): string {
  const matched = /^[^|]+\|(.+)$/.exec(name.trim())
  return matched?.[1]?.trim() || name.trim()
}

/** 可选值名：`1|HK^香港` → `HK · 香港`；无分隔符时原样返回。 */
export function valueLabel(name: string): string {
  let text = name.trim()
  const idPrefix = /^\d+\|(.+)$/.exec(text)
  if (idPrefix?.[1]) {
    text = idPrefix[1].trim()
  }
  return text.replace(/\^/g, ' · ')
}

/**
 * 控件内的短显示名（R6）：系统/区域类取 `^` 之后的名称，其余去掉 `值ID|` 前缀。
 *
 * 例：`15|Debian^Debian-10.3.3-x64` → `Debian-10.3.3-x64`；`1|HK^香港` → `香港`；`vpc|VPC网络` → `VPC网络`。
 * 订单快照仍用 valueLabel（保留 `HK · 香港` 这类全量信息），此函数只服务于选择控件。
 */
export function valueDisplayName(name: string): string {
  let text = name.trim()
  const idPrefix = /^[^|]+\|(.+)$/.exec(text)
  if (idPrefix?.[1]) {
    text = idPrefix[1].trim()
  }
  const caret = /^([^^]*)\^(.+)$/.exec(text)
  if (caret?.[2]) {
    return caret[2].trim()
  }
  return text
}

/**
 * 解析值的「大类」（`^` 前缀）：`15|Debian^Debian-10.3.3-x64` → `Debian`；
 * 值不含 `^` 时返回空串（非二级型值的普遍形态）。
 */
export function valueGroupName(name: string): string {
  const text = name.trim()
  const idPrefix = /^[^|]+\|(.+)$/.exec(text)
  const body = idPrefix?.[1]?.trim() ?? text
  const caret = /^([^^]+)\^/.exec(body)
  return caret?.[1]?.trim() ?? ''
}

/** 配置项键名：`os|操作系统` → `os`；无 `|` 时原样返回。 */
export function optionKey(name: string): string {
  const text = name.trim()
  const matched = /^([^|]+)\|/.exec(text)
  return matched?.[1]?.trim() || text
}

/** 商品类型文案：上游 type 字段的展示映射（未知类型原样返回）。 */
const TYPE_LABELS: Record<string, string> = {
  dcimcloud: '云服务器',
  dcim: '独立服务器',
  server: '独立服务器',
  vps: 'VPS',
  cdn: 'CDN',
  other: '其他产品',
}

export function productTypeLabel(type: string): string {
  return TYPE_LABELS[type] ?? type
}
