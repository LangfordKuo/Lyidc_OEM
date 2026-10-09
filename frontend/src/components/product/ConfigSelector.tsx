import { useState } from 'react'
import { MinusIcon, PlusIcon } from 'lucide-react'

import type { ConfigGroup, ConfigOption, ConfigOptionValue } from '@/api/types'
import ChoiceChips from '@/components/common/ChoiceChips'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  configControl,
  osGroupsOf,
  quantityRange,
  quantityUnit,
  selectedOsGroup,
} from '@/lib/configControl'
import { optionLabel, valueDisplayName } from '@/lib/productText'
import { cn } from '@/lib/utils'

/**
 * 渲染商品的配置项（契约 10.3：会员端已过滤上游隐藏项与隐藏值）。
 *
 * 控件形态按 option_type 与上游前台同步（R6，映射与实测见 lib/configControl.ts）：
 * 下拉 / 单选横条 / 两级系统选择 / 数量（数字输入 + 步进）。
 * 提交口径不变：选项型提交 `options[].id → values[].id`；数量型提交
 * `configoption[<配置项 id>] = <数量>`（值域取该值的 qty_minimum~qty_maximum）。
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
    <div className="space-y-5">
      {options.map((option) => (
        <ConfigField
          key={option.id}
          option={option}
          value={selected[String(option.id)] ?? ''}
          onChange={onChange}
          idPrefix={idPrefix}
        />
      ))}
    </div>
  )
}

/** 单个配置项：按控件形态分发（与上游同一份 option_type 判定）。 */
function ConfigField({
  option,
  value,
  onChange,
  idPrefix,
}: {
  option: ConfigOption
  value: string
  onChange: (optionId: number, valueId: string) => void
  idPrefix: string
}) {
  switch (configControl(option)) {
    case 'quantity':
      return <QuantityField option={option} value={value} onChange={onChange} idPrefix={idPrefix} />
    case 'os':
      return <OsField option={option} value={value} onChange={onChange} idPrefix={idPrefix} />
    case 'select':
      return <SelectField option={option} value={value} onChange={onChange} idPrefix={idPrefix} />
    default:
      return <ChipsField option={option} value={value} onChange={onChange} idPrefix={idPrefix} />
  }
}

/** 配置项标题（各控件形态共用）。 */
function FieldTitle({ text, htmlFor }: { text: string; htmlFor?: string }) {
  return htmlFor ? (
    <Label htmlFor={htmlFor}>{text}</Label>
  ) : (
    <span className="text-sm font-medium leading-none text-foreground">{text}</span>
  )
}

// ---------------------------------------------------------------------------
// 下拉（option_type=1 等普通枚举；值多且无需并排对比）
// ---------------------------------------------------------------------------

function SelectField({
  option,
  value,
  onChange,
  idPrefix,
}: {
  option: ConfigOption
  value: string
  onChange: (optionId: number, valueId: string) => void
  idPrefix: string
}) {
  const label = optionLabel(option.name)
  const selectId = `${idPrefix}-option-${option.id}`
  return (
    <div className="space-y-2">
      <FieldTitle text={label} htmlFor={selectId} />
      <Select value={value || undefined} onValueChange={(next) => onChange(option.id, next)}>
        <SelectTrigger id={selectId} className="w-full" aria-label={label}>
          <SelectValue placeholder="请选择" />
        </SelectTrigger>
        <SelectContent>
          {option.values.map((item) => (
            <SelectItem key={item.id} value={String(item.id)}>
              {valueDisplayName(item.name)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )
}

// ---------------------------------------------------------------------------
// 单选横条（option_type=6/8/10/12/13：CPU / 内存 / 带宽档位 / 区域 / 磁盘）
// ---------------------------------------------------------------------------

function ChipsField({
  option,
  value,
  onChange,
  idPrefix,
}: {
  option: ConfigOption
  value: string
  onChange: (optionId: number, valueId: string) => void
  idPrefix: string
}) {
  const label = optionLabel(option.name)
  const items = option.values.map((item) => ({
    value: String(item.id),
    label: valueDisplayName(item.name),
  }))
  return (
    <div className="space-y-2">
      <FieldTitle text={label} />
      <ChoiceChips
        items={items}
        value={value}
        onChange={(next) => onChange(option.id, next)}
        ariaLabel={label}
        idPrefix={`${idPrefix}-option-${option.id}`}
      />
    </div>
  )
}

// ---------------------------------------------------------------------------
// 系统两级选择（option_type=5）：大类 → 版本
// ---------------------------------------------------------------------------

function OsField({
  option,
  value,
  onChange,
  idPrefix,
}: {
  option: ConfigOption
  value: string
  onChange: (optionId: number, valueId: string) => void
  idPrefix: string
}) {
  const label = optionLabel(option.name)
  const groups = osGroupsOf(option.values)
  const activeGroup = selectedOsGroup(option, value)
  const versions = groups.find((group) => group.name === activeGroup)?.values ?? []

  // 切换大类：当前值仍属于该大类时保持（回显），否则落到该大类第一个版本。
  const selectGroup = (name: string) => {
    const group = groups.find((item) => item.name === name)
    if (!group || group.values.length === 0) {
      return
    }
    const kept = group.values.find((item) => String(item.id) === value)
    onChange(option.id, String((kept ?? group.values[0]).id))
  }

  return (
    <div className="space-y-2">
      <FieldTitle text={label} />
      <div className="space-y-2">
        <p className="text-xs text-muted-foreground">系统大类</p>
        <ChoiceChips
          items={groups.map((group) => ({ value: group.name, label: group.name }))}
          value={activeGroup}
          onChange={selectGroup}
          ariaLabel={`${label}大类`}
          idPrefix={`${idPrefix}-option-${option.id}-group`}
        />
      </div>
      <div className="space-y-2">
        <p className="text-xs text-muted-foreground">具体版本</p>
        {activeGroup ? (
          <VersionGroup
            versions={versions}
            value={value}
            onChange={(next) => onChange(option.id, next)}
            ariaLabel={`${label}版本`}
            idPrefix={`${idPrefix}-option-${option.id}-version`}
          />
        ) : (
          <p className="text-sm text-muted-foreground">请先选择系统大类</p>
        )}
      </div>
    </div>
  )
}

/** 版本列表：条目多（最多 36 个）时横向换行并限高滚动，避免撑长页面。 */
function VersionGroup({
  versions,
  value,
  onChange,
  ariaLabel,
  idPrefix,
}: {
  versions: ConfigOptionValue[]
  value: string
  onChange: (value: string) => void
  ariaLabel: string
  idPrefix: string
}) {
  return (
    <div className="max-h-44 overflow-y-auto pr-1">
      <ChoiceChips
        items={versions.map((item) => ({
          value: String(item.id),
          label: valueDisplayName(item.name),
        }))}
        value={value}
        onChange={onChange}
        ariaLabel={ariaLabel}
        idPrefix={idPrefix}
      />
    </div>
  )
}

// ---------------------------------------------------------------------------
// 数量型（option_type=4/11/14/15/19）：数字输入 + 步进，提交数量（Qty 口径）
// ---------------------------------------------------------------------------

function QuantityField({
  option,
  value,
  onChange,
  idPrefix,
}: {
  option: ConfigOption
  value: string
  onChange: (optionId: number, valueId: string) => void
  idPrefix: string
}) {
  const label = optionLabel(option.name)
  const unit = quantityUnit(option)
  const { min, max } = quantityRange(option)
  const inputId = `${idPrefix}-option-${option.id}`
  // 输入过程中的草稿（允许临时空串/越界），失焦后回到已提交的合法值。
  const [draft, setDraft] = useState<string | null>(null)

  const committed = clampQuantity(Number.parseInt(value, 10), min, max)
  const shown = draft ?? String(committed)
  const fixed = min === max

  const commit = (next: number) => onChange(option.id, String(clampQuantity(next, min, max)))

  return (
    <div className="space-y-2">
      <FieldTitle text={label} htmlFor={inputId} />
      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="button"
          variant="outline"
          size="icon"
          className="size-9 shrink-0"
          aria-label={`减少${label}`}
          disabled={fixed || committed <= min}
          onClick={() => commit(committed - 1)}
        >
          <MinusIcon aria-hidden />
        </Button>
        <div className="relative">
          <Input
            id={inputId}
            type="number"
            inputMode="numeric"
            min={min}
            max={max}
            step={1}
            value={shown}
            aria-label={label}
            aria-describedby={`${inputId}-range`}
            className={cn('w-28 tabular-nums', unit ? 'pr-12' : undefined)}
            onChange={(event) => {
              const text = event.target.value
              setDraft(text)
              const parsed = Number.parseInt(text, 10)
              if (text !== '' && Number.isFinite(parsed) && parsed >= min && parsed <= max) {
                onChange(option.id, String(parsed))
              }
            }}
            onBlur={() => setDraft(null)}
          />
          {unit ? (
            <span className="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-muted-foreground">
              {unit}
            </span>
          ) : null}
        </div>
        <Button
          type="button"
          variant="outline"
          size="icon"
          className="size-9 shrink-0"
          aria-label={`增加${label}`}
          disabled={fixed || committed >= max}
          onClick={() => commit(committed + 1)}
        >
          <PlusIcon aria-hidden />
        </Button>
        <span id={`${inputId}-range`} className="text-xs text-muted-foreground tabular-nums">
          {fixed
            ? `固定 ${min}${unit}`
            : `范围 ${min}${unit} ~ ${max}${unit}`}
        </span>
      </div>
    </div>
  )
}

/** 数量取整并夹紧到 [min, max]；非法输入回落到下界。 */
function clampQuantity(value: number, min: number, max: number): number {
  if (!Number.isFinite(value)) {
    return min
  }
  return Math.min(Math.max(Math.trunc(value), min), max)
}
