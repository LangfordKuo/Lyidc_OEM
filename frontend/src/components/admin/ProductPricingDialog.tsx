import { Alert, Button, Input, Label, Modal, Radio, RadioGroup, TextField } from '@heroui/react'
import { useState } from 'react'

import { updateAdminProduct } from '../../api/adminProducts'
import { errorMessage } from '../../api/client'
import type { AdminProduct, PricingMode, PricingRule } from '../../api/types'
import { BILLING_CYCLES, CYCLE_LABELS, type BillingCycle } from '../../lib/cycles'
import { formatMoney } from '../../lib/format'

// 商品定价弹窗（契约 10.2 / 10.4）：三模式单选 + 按模式收集参数，提交 PUT /admin/products/:id。
// 仅 admin / finance（products.write）可打开——由页面按角色矩阵控制挂载，这里不做校验重复。
// 弹窗内的「当前定价」全部来自列表项下发的 pricing / prices 字段，不改写、不臆造。

/** 金额写法：非负十进制、最多两位小数（与后端 pricing.ParseAmount 口径一致）。 */
const MONEY_PATTERN = /^\d+(\.\d{1,2})?$/

const MODE_OPTIONS: { value: PricingMode; label: string; description: string }[] = [
  {
    value: 'upstream',
    label: '上游价',
    description: '直接使用上游价格，无需额外配置。',
  },
  {
    value: 'markup',
    label: '按上游价加价',
    description: '在上游价基础上按百分比加价（如 10 表示加价 10%）。',
  },
  {
    value: 'fixed',
    label: '固定价覆盖',
    description: '为指定周期设置固定售价；留空的周期回退上游价（或加价后的上游价）。',
  },
]

/** 加价率校验：必填、大于 0、最多两位小数（上限对齐后端 1000%）。 */
function validateMarkup(value: string): string {
  const trimmed = value.trim()
  if (!trimmed) {
    return '请输入加价率'
  }
  if (!MONEY_PATTERN.test(trimmed)) {
    return '加价率需为数字，最多两位小数'
  }
  if (Number(trimmed) <= 0) {
    return '加价率需大于 0'
  }
  if (Number(trimmed) > 1000) {
    return '加价率不能超过 1000（%）'
  }
  return ''
}

/** 固定价金额校验：留空表示该周期不覆盖；填写时须为非负十进制、最多两位小数。 */
function validateAmount(value: string): string {
  const trimmed = value.trim()
  if (!trimmed) {
    return ''
  }
  if (!MONEY_PATTERN.test(trimmed)) {
    return '需为非负数字，最多两位小数'
  }
  return ''
}

/** 归一化模式：后端缺省（空串）按 upstream 处理，未知值也一律回落到 upstream，避免误提交。 */
function toPricingMode(value: string): PricingMode {
  return value === 'markup' || value === 'fixed' ? value : 'upstream'
}

/** 当前定价规则的一句话摘要（用于回显，不参与提交）。 */
function pricingSummary(rule: PricingRule): string {
  if (rule.mode === 'markup') {
    return `按上游价加价 ${rule.markup_percent ?? '—'}%`
  }
  if (rule.mode === 'fixed') {
    const parts: string[] = []
    for (const cycle of BILLING_CYCLES) {
      const amount = rule.fixed?.[cycle]
      if (amount) {
        parts.push(`${CYCLE_LABELS[cycle]} ${formatMoney(amount)}`)
      }
    }
    return parts.length > 0 ? `固定价：${parts.join('、')}` : '固定价：未配置任何周期'
  }
  return '直接使用上游价格'
}

export default function ProductPricingDialog({
  product,
  onClose,
  onUpdated,
}: {
  product: AdminProduct
  /** 关闭弹窗（保存中或失败时不关闭，避免丢失已填内容）。 */
  onClose: () => void
  /** 保存成功回调：父组件据此 toast 并刷新列表。 */
  onUpdated: () => void
}) {
  const [mode, setMode] = useState<PricingMode>(() => toPricingMode(product.pricing.mode))
  // 加价率回显当前值（未配置时留空，由用户填写）。
  const [markup, setMarkup] = useState(
    product.pricing.markup_percent === undefined ? '' : String(product.pricing.markup_percent),
  )
  // 固定价六个周期：已配置的回显，未配置的留空（= 该周期不覆盖）。
  const [fixed, setFixed] = useState<Record<BillingCycle, string>>(() => {
    const initial = {} as Record<BillingCycle, string>
    for (const cycle of BILLING_CYCLES) {
      initial[cycle] = product.pricing.fixed?.[cycle] ?? ''
    }
    return initial
  })
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  const markupError = validateMarkup(markup)
  const modeDescription = MODE_OPTIONS.find((option) => option.value === mode)?.description ?? ''

  const handleSubmit = async () => {
    let pricing: PricingRule
    if (mode === 'markup') {
      const message = validateMarkup(markup)
      if (message) {
        setError(message)
        return
      }
      pricing = { mode: 'markup', markup_percent: Number(markup.trim()) }
    } else if (mode === 'fixed') {
      const filled: Partial<Record<BillingCycle, string>> = {}
      for (const cycle of BILLING_CYCLES) {
        const value = fixed[cycle].trim()
        if (!value) {
          continue
        }
        const message = validateAmount(value)
        if (message) {
          setError(`${CYCLE_LABELS[cycle]} ${message}`)
          return
        }
        filled[cycle] = value
      }
      if (Object.keys(filled).length === 0) {
        setError('固定价模式至少填写一个周期的金额')
        return
      }
      pricing = { mode: 'fixed', fixed: filled }
    } else {
      // upstream：不带加价率与固定价（契约 10.2：mode=upstream 携带这两项会被判 40002）。
      pricing = { mode: 'upstream' }
    }

    setPending(true)
    setError('')
    try {
      await updateAdminProduct(product.id, { pricing_json: pricing })
      onUpdated()
    } catch (err) {
      // 含后端 409「商品没有任何可用周期的价格，无法上架…」等业务提示，原样展示。
      setError(errorMessage(err, '保存定价失败，请稍后重试'))
    } finally {
      setPending(false)
    }
  }

  return (
    <Modal
      isOpen
      onOpenChange={(open) => {
        if (!open && !pending) {
          onClose()
        }
      }}
    >
      <Modal.Backdrop>
        <Modal.Container size="lg" placement="center">
          <Modal.Dialog>
            <Modal.Header>
              <Modal.Heading>定价设置 · {product.name}</Modal.Heading>
            </Modal.Header>
            <Modal.Body className="space-y-4">
              <div className="rounded-lg border border-border bg-surface-secondary/40 p-3">
                <p className="text-xs text-muted">
                  当前定价：{pricingSummary(product.pricing)}
                </p>
                <dl className="mt-2 grid gap-1 sm:grid-cols-2">
                  {BILLING_CYCLES.map((cycle) => {
                    const amount = product.prices?.[cycle] ?? null
                    return (
                      <div key={cycle} className="flex items-center justify-between gap-3 text-xs">
                        <dt className="text-muted">{CYCLE_LABELS[cycle]}</dt>
                        <dd className={amount ? 'text-foreground' : 'text-muted'}>
                          {amount ? formatMoney(amount) : '不可售'}
                        </dd>
                      </div>
                    )
                  })}
                </dl>
              </div>

              <div>
                <p className="mb-2 text-sm font-medium text-foreground">定价模式</p>
                <RadioGroup
                  aria-label="定价模式"
                  value={mode}
                  onChange={(value) => {
                    setMode(value as PricingMode)
                    setError('')
                  }}
                  orientation="horizontal"
                  className="gap-3"
                >
                  {MODE_OPTIONS.map((option) => (
                    // HeroUI v3 的 Radio 必须用 Radio.Content 包裹才是可交互控件（否则渲染为不可选中的纯文本）；
                    // Radio.Control / Radio.Indicator 同样要放在 Radio.Content 内，圆圈才随文本一起可点。
                    <Radio key={option.value} value={option.value}>
                      <Radio.Content>
                        <Radio.Control>
                          <Radio.Indicator />
                        </Radio.Control>
                        {option.label}
                      </Radio.Content>
                    </Radio>
                  ))}
                </RadioGroup>
                <p className="mt-2 text-xs text-muted">{modeDescription}</p>
              </div>

              {mode === 'markup' ? (
                <TextField
                  name="markup_percent"
                  value={markup}
                  onChange={(value) => {
                    setMarkup(value)
                    setError('')
                  }}
                  isInvalid={Boolean(markup && markupError)}
                >
                  <Label>加价率（%）</Label>
                  <Input placeholder="如 10（最多两位小数）" inputMode="decimal" />
                  {markup && markupError ? (
                    <p className="mt-1 text-xs text-danger">{markupError}</p>
                  ) : null}
                </TextField>
              ) : null}

              {mode === 'fixed' ? (
                <div className="grid gap-3 sm:grid-cols-2">
                  {BILLING_CYCLES.map((cycle) => {
                    const value = fixed[cycle]
                    const message = value ? validateAmount(value) : ''
                    return (
                      <TextField
                        key={cycle}
                        name={`fixed_${cycle}`}
                        value={value}
                        onChange={(next) => {
                          setFixed((prev) => ({ ...prev, [cycle]: next }))
                          setError('')
                        }}
                        isInvalid={Boolean(message)}
                      >
                        <Label>{CYCLE_LABELS[cycle]}固定价（元）</Label>
                        <Input placeholder="留空 = 该周期不覆盖" inputMode="decimal" />
                        {message ? <p className="mt-1 text-xs text-danger">{message}</p> : null}
                      </TextField>
                    )
                  })}
                </div>
              ) : null}

              {error ? (
                <Alert status="danger">
                  <Alert.Indicator />
                  <Alert.Content>
                    <Alert.Description>{error}</Alert.Description>
                  </Alert.Content>
                </Alert>
              ) : null}
            </Modal.Body>
            <Modal.Footer>
              <Button variant="outline" isDisabled={pending} onPress={onClose}>
                取消
              </Button>
              <Button variant="primary" isDisabled={pending} onPress={handleSubmit}>
                {pending ? '正在保存…' : '保存定价'}
              </Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  )
}
