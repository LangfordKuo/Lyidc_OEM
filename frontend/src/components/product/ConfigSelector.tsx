import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type { ConfigGroup } from '@/api/types'
import { optionLabel, valueLabel } from '@/lib/productText'

/**
 * 渲染商品的配置项（契约 10.3：会员端已过滤上游隐藏项与隐藏值）。
 * 提交给后端的键是 options[].id，值是其 values[].id（字符串形式）。
 */
export default function ConfigSelector({
  groups,
  selected,
  onChange,
  idPrefix = 'config',
}: {
  groups: ConfigGroup[]
  selected: Record<string, string>
  onChange: (optionId: number, valueId: string) => void
  idPrefix?: string
}) {
  const options = groups.flatMap((group) => group.options)
  if (options.length === 0) {
    return null
  }

  return (
    <div className="grid gap-4 sm:grid-cols-2">
      {options.map((option) => {
        const optionKey = String(option.id)
        const selectId = `${idPrefix}-option-${option.id}`
        return (
          <div key={option.id} className="space-y-2">
            <Label htmlFor={selectId}>{optionLabel(option.name)}</Label>
            <Select
              value={selected[optionKey] ?? undefined}
              onValueChange={(value) => onChange(option.id, value)}
            >
              <SelectTrigger id={selectId} className="w-full" aria-label={optionLabel(option.name)}>
                <SelectValue placeholder="请选择" />
              </SelectTrigger>
              <SelectContent>
                {option.values.map((item) => (
                  <SelectItem key={item.id} value={String(item.id)}>
                    {valueLabel(item.name)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )
      })}
    </div>
  )
}
