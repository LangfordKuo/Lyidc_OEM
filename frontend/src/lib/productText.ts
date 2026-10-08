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

/** 商品类型文案：上游 type 字段的展示映射（未知类型原样返回）。 */
const TYPE_LABELS: Record<string, string> = {
  dcimcloud: '云服务器',
  dcim: '独立服务器',
  vps: 'VPS',
}

export function productTypeLabel(type: string): string {
  return TYPE_LABELS[type] ?? type
}
