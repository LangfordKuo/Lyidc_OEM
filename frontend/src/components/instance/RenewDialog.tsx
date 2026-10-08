import { Alert, Button, Modal, toast } from '@heroui/react'
import { useMemo, useState } from 'react'

import { errorMessage } from '../../api/client'
import { renewInstance } from '../../api/instances'
import { fetchProductDetail } from '../../api/products'
import type { InstanceSummary, Order, ProductDetail } from '../../api/types'
import { useAsync } from '../../hooks/useAsync'
import { BILLING_CYCLES, cheapestCycle, type BillingCycle } from '../../lib/cycles'
import { formatCycleLabel, formatDateTimeOr, formatMoney } from '../../lib/format'
import { expiryText } from '../../lib/expiry'

// RenewDialog：续费下单（契约 15.2 / 15.5）。
// 续费金额按「当前商品」该周期本地售价（商品下架仍可续费），因此这里尝试读取商品详情展示价格，
// 读不到价格时仍允许选择周期，最终金额以服务端下单结果为准。
export default function RenewDialog({
  instance,
  onClose,
  onOrderCreated,
}: {
  instance: InstanceSummary
  onClose: () => void
  onOrderCreated: (order: Order) => void
}) {
  const [selected, setSelected] = useState<BillingCycle | null>(null)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  const productState = useAsync(() => fetchProductDetail(instance.product_id), [
    instance.product_id,
  ])
  const product: ProductDetail | null = productState.data
  const prices = product?.prices ?? null

  // 默认周期：当前计费周期仍可售则沿用，否则取最低价周期（价格未知时沿用当前周期）。
  // 用「选择值 + 默认值」派生，避免在 effect 里回写状态。
  const defaultCycle = useMemo<BillingCycle>(() => {
    if (!prices) {
      return instance.billing_cycle || 'monthly'
    }
    if (prices[instance.billing_cycle] !== null) {
      return instance.billing_cycle
    }
    return cheapestCycle(prices)?.cycle ?? (instance.billing_cycle || 'monthly')
  }, [prices, instance.billing_cycle])

  const cycle = selected ?? defaultCycle

  const amount = useMemo(() => {
    if (!prices) {
      return null
    }
    return prices[cycle] ?? null
  }, [prices, cycle])

  const handleConfirm = async () => {
    setPending(true)
    setError('')
    try {
      const order = await renewInstance(instance.id, cycle)
      toast.success(`续费订单已创建（${order.trade_no}），请完成支付`)
      onClose()
      onOrderCreated(order)
    } catch (err) {
      setError(errorMessage(err, '创建续费订单失败'))
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
        <Modal.Container size="md" placement="center">
          <Modal.Dialog>
            <Modal.Header>
              <Modal.Heading>实例续费</Modal.Heading>
            </Modal.Header>
            <Modal.Body className="space-y-4">
              <div className="grid gap-1.5 text-sm">
                <div className="flex justify-between gap-4">
                  <span className="text-muted">实例</span>
                  <span className="text-foreground">{instance.name}</span>
                </div>
                <div className="flex justify-between gap-4">
                  <span className="text-muted">当前到期时间</span>
                  <span className="text-foreground">
                    {formatDateTimeOr(instance.next_due_date)}
                    {instance.next_due_date ? (
                      <span className="ml-2 text-xs text-muted">
                        {expiryText(instance.next_due_date)}
                      </span>
                    ) : null}
                  </span>
                </div>
              </div>

              <div>
                <p className="mb-2 text-sm font-medium text-foreground">选择续费周期</p>
                <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
                  {BILLING_CYCLES.map((item) => {
                    const price = prices ? prices[item] : null
                    const disabled = prices !== null && price === null
                    return (
                      <Button
                        key={item}
                        isDisabled={disabled}
                        variant={item === cycle ? 'primary' : 'outline'}
                        size="sm"
                        className="h-auto flex-col items-start gap-0.5 py-2.5"
                        onPress={() => setSelected(item)}
                      >
                        <span className="text-sm font-medium">{formatCycleLabel(item)}</span>
                        <span className="text-xs opacity-80">
                          {disabled ? '不可售' : price ? formatMoney(price) : '价格以下单为准'}
                        </span>
                      </Button>
                    )
                  })}
                </div>
              </div>

              {productState.error ? (
                <Alert status="warning">
                  <Alert.Indicator />
                  <Alert.Content>
                    <Alert.Description>
                      未能读取商品价格（{productState.error}），可继续选择周期，最终金额以服务端下单结果为准。
                    </Alert.Description>
                  </Alert.Content>
                </Alert>
              ) : null}

              <div className="flex items-baseline justify-between">
                <span className="text-sm text-muted">应付金额</span>
                <span className="text-lg font-semibold text-foreground">
                  {amount ? formatMoney(amount) : '以下单结果为准'}
                </span>
              </div>

              <p className="text-xs text-muted">
                续费订单不支持优惠码；支付成功后自动续费并顺延到期时间，已暂停的实例会一并恢复。
              </p>

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
              <Button variant="primary" isDisabled={pending} onPress={handleConfirm}>
                {pending ? '正在创建订单…' : '创建续费订单'}
              </Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  )
}
