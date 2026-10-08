import { Label, ListBox, Select } from '@heroui/react'

import type { ConfigGroup } from '../../api/types'
import { optionLabel, valueLabel } from '../../lib/productText'

// ConfigSelector 渲染商品的配置项（契约 10.3：会员端已过滤上游隐藏项与隐藏值）。
// 提交给后端的键是 options[].id，值是其 values[].id（字符串形式）。
export default function ConfigSelector({
  groups,
  selected,
  onChange,
}: {
  groups: ConfigGroup[]
  selected: Record<string, string>
  onChange: (optionId: number, valueId: string) => void
}) {
  const options = groups.flatMap((group) => group.options)
  if (options.length === 0) {
    return null
  }

  return (
    <div className="grid gap-4 sm:grid-cols-2">
      {options.map((option) => {
        const optionKey = String(option.id)
        return (
          <Select
            key={option.id}
            selectedKey={selected[optionKey] ?? null}
            onSelectionChange={(key) => {
              if (key !== null) {
                onChange(option.id, String(key))
              }
            }}
            placeholder="请选择"
            className="w-full"
          >
            <Label>{optionLabel(option.name)}</Label>
            <Select.Trigger>
              <Select.Value />
              <Select.Indicator />
            </Select.Trigger>
            <Select.Popover>
              <ListBox>
                {option.values.map((item) => (
                  <ListBox.Item
                    key={item.id}
                    id={String(item.id)}
                    textValue={valueLabel(item.name)}
                  >
                    {valueLabel(item.name)}
                    <ListBox.ItemIndicator />
                  </ListBox.Item>
                ))}
              </ListBox>
            </Select.Popover>
          </Select>
        )
      })}
    </div>
  )
}
