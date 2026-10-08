import { Alert, Button, Modal, Radio, RadioGroup, toast } from '@heroui/react'
import { useState } from 'react'

import { errorMessage } from '../../api/client'
import { fetchBalance } from '../../api/finance'
import { payOrder } from '../../api/orders'
import type { EpayType, Order, PayChannel } from '../../api/types'
import { useAuth } from '../../auth/authContext'
import { useAsync } from '../../hooks/useAsync'
import { rememberOrder } from '../../lib/checkout'
import { formatMoney } from '../../lib/format'
import { EPAY_TYPES, gotoPayurl, PAY_METHODS } from '../../lib/pay'

// PayOrderDialog 是「对既有待支付订单发起支付」的弹窗：会员区继续支付与实例续费共用。
// 在线支付跳转渠道收银台；余额支付即时入账并按 pay.balance_after 刷新本地余额。
export default function PayOrderDialog({
  order,
  onClose,
  onPaid,
}: {
  order: Order
  onClose: () => void
  onPaid?: (order: Order) => void
}) {
  const { member, setBalance } = useAuth()
  const [channel, setChannel] = useState<PayChannel>('epay')
  const [payType, setPayType] = useState<EpayType>('alipay')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  // 打开时刷新一次余额：余额支付是否可用要按最新值判断。
  const balanceState = useAsync(fetchBalance, [order.id])
  const balance = balanceState.data?.balance ?? member?.balance ?? '0.00'
  const insufficient = Number(balance) < Number(order.final_amount)

  const handlePay = async () => {
    setPending(true)
    setError('')
    try {
      const result = await payOrder(order.id, {
        channel,
        ...(channel === 'epay' ? { pay_type: payType } : {}),
      })
      if (channel === 'epay') {
        const payurl = result.pay.payurl
        if (!payurl) {
          setError('支付渠道未返回支付地址，请稍后重试或改用余额支付')
          return
        }
        rememberOrder({
          id: result.order.id,
          trade_no: result.order.trade_no,
          productId: result.order.product_id,
        })
        toast.info('即将跳转支付渠道…')
        gotoPayurl(payurl)
        return
      }
      if (result.pay.balance_after) {
        setBalance(result.pay.balance_after)
      }
      toast.success('余额支付成功，订单已进入开通流程')
      onPaid?.(result.order)
      onClose()
    } catch (err) {
      setError(errorMessage(err, '支付失败，请稍后重试'))
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
              <Modal.Heading>订单支付</Modal.Heading>
            </Modal.Header>
            <Modal.Body className="space-y-4">
              {order ? (
                <div className="grid gap-1.5 text-sm">
                  <div className="flex justify-between gap-4">
                    <span className="text-muted">订单号</span>
                    <span className="font-mono text-xs text-foreground">{order.trade_no}</span>
                  </div>
                  <div className="flex justify-between gap-4">
                    <span className="text-muted">商品</span>
                    <span className="text-right text-foreground">{order.product_name}</span>
                  </div>
                  <div className="flex items-baseline justify-between gap-4">
                    <span className="text-muted">应付金额</span>
                    <span className="text-lg font-semibold text-foreground">
                      {formatMoney(order.final_amount)}
                    </span>
                  </div>
                </div>
              ) : null}

              <RadioGroup
                aria-label="支付方式"
                value={channel}
                onChange={(value) => setChannel(value as PayChannel)}
                className="gap-3"
              >
                {PAY_METHODS.map((method) => (
                  // HeroUI v3 的 Radio 必须用 Radio.Content 包裹才是可交互控件（否则渲染为不可选中的纯文本），
                  // 圆圈（Radio.Control / Radio.Indicator）也要放在 Radio.Content 内、文本之前。
                  <Radio key={method.value} value={method.value}>
                    <Radio.Content>
                      <Radio.Control>
                        <Radio.Indicator />
                      </Radio.Control>
                      <div className="flex flex-col">
                        <span className="text-sm font-medium">
                          {method.title}
                          {method.value === 'balance' ? `（余额 ${formatMoney(balance)}）` : ''}
                        </span>
                        <span className="text-xs text-muted">{method.description}</span>
                      </div>
                    </Radio.Content>
                  </Radio>
                ))}
              </RadioGroup>

              {channel === 'epay' ? (
                <RadioGroup
                  aria-label="在线支付方式"
                  value={payType}
                  onChange={(value) => setPayType(value as EpayType)}
                  orientation="horizontal"
                  className="gap-3"
                >
                  {EPAY_TYPES.map((item) => (
                    <Radio key={item.value} value={item.value}>
                      <Radio.Content>
                        <Radio.Control>
                          <Radio.Indicator />
                        </Radio.Control>
                        {item.label}
                      </Radio.Content>
                    </Radio>
                  ))}
                </RadioGroup>
              ) : null}

              {channel === 'balance' && insufficient ? (
                <Alert status="warning">
                  <Alert.Indicator />
                  <Alert.Content>
                    <Alert.Description>
                      余额不足（当前 {formatMoney(balance)}），请改用在线支付或在会员区充值。
                    </Alert.Description>
                  </Alert.Content>
                </Alert>
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
              <Button
                variant="primary"
                isDisabled={pending || !order || (channel === 'balance' && insufficient)}
                onPress={handlePay}
              >
                {pending ? '正在发起支付…' : channel === 'epay' ? '前往支付' : '余额支付'}
              </Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  )
}
