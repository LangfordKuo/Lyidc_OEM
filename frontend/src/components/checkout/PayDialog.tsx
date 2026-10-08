import { useState } from 'react'
import { AlertCircleIcon, ExternalLinkIcon, LoaderCircleIcon, QrCodeIcon } from 'lucide-react'

import { errorMessage } from '@/api/client'
import { payOrder } from '@/api/orders'
import type { EpayType, Order, OrderPayResult, PayChannel } from '@/api/types'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { rememberOrder } from '@/lib/checkout'
import { formatMoney } from '@/lib/format'
import { EPAY_TYPES, PAY_METHODS, gotoPayurl } from '@/lib/pay'
import { cn } from '@/lib/utils'

interface PayDialogProps {
  /** 待支付订单；为 null 时弹窗内容为空（保持关闭）。 */
  order: Order | null
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 当前余额（用于余额支付可用性与不足提示）。 */
  balance: string
  /** 余额支付成功（本地已入账）后的回调。 */
  onPaid: (result: OrderPayResult) => void
}

/**
 * 支付弹窗：选择「在线支付（支付宝 / 微信）」或「余额支付」并提交。
 * - 在线支付：跳转易支付收银台；若渠道返回二维码（pay.extra.qrcode）则在弹窗内展示。
 * - 余额支付：本地单事务扣款并直接入账，回调 onPaid 由调用方跳结果页。
 */
export default function PayDialog({ order, open, onOpenChange, balance, onPaid }: PayDialogProps) {
  const [channel, setChannel] = useState<PayChannel>('epay')
  const [payType, setPayType] = useState<EpayType>('alipay')
  const [paying, setPaying] = useState(false)
  const [error, setError] = useState('')
  const [qr, setQr] = useState<{ payurl: string; qrcode: string } | null>(null)

  // 每次打开（或切换订单）重置内部状态，避免上次的支付方式/二维码残留。
  // 采用「渲染期间调整 state」模式（React 官方推荐），避免 effect 里级联渲染。
  const resetKey = `${open}-${order?.id ?? ''}`
  const [lastResetKey, setLastResetKey] = useState(resetKey)
  if (lastResetKey !== resetKey) {
    setLastResetKey(resetKey)
    if (open) {
      setChannel('epay')
      setPayType('alipay')
      setPaying(false)
      setError('')
      setQr(null)
    }
  }

  const balanceInsufficient = order !== null && Number(balance) < Number(order.final_amount)

  const handlePay = async () => {
    if (!order) {
      return
    }
    setPaying(true)
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
        // 记录订单号：渠道回跳结果页时据此找回订单。
        rememberOrder({
          id: result.order.id,
          trade_no: result.order.trade_no,
          productId: result.order.product_id,
        })
        const qrcode = result.pay.extra?.qrcode
        if (qrcode) {
          setQr({ payurl, qrcode })
          return
        }
        // 跳出本站前往渠道收银台（回跳地址由后端 return_url 设置决定）。
        gotoPayurl(payurl)
        return
      }
      // 余额支付：本地已入账，交给调用方处理跳转。
      onPaid(result)
    } catch (err) {
      setError(errorMessage(err, '支付失败，请稍后重试'))
    } finally {
      setPaying(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>支付订单</DialogTitle>
          <DialogDescription>
            {order ? (
              <>
                订单号 <span className="font-mono text-xs">{order.trade_no}</span> · 应付{' '}
                <span className="font-medium text-foreground">{formatMoney(order.final_amount)}</span>
              </>
            ) : null}
          </DialogDescription>
        </DialogHeader>

        {qr ? (
          <div className="space-y-4">
            <div className="flex flex-col items-center gap-3 rounded-xl border border-border p-4">
              {/^https?:\/\//i.test(qr.qrcode) ? (
                <img
                  src={qr.qrcode}
                  alt="支付二维码"
                  className="size-44 rounded-lg border border-border object-contain"
                />
              ) : (
                <span className="grid size-44 place-items-center rounded-lg border border-dashed border-border text-muted-foreground">
                  <QrCodeIcon className="size-10" aria-hidden />
                </span>
              )}
              <p className="text-center text-sm text-muted-foreground">
                请使用支付宝或微信扫码完成支付；
                {/^https?:\/\//i.test(qr.qrcode) ? '也可点击下方按钮打开收银台。' : '也可复制下方地址打开收银台。'}
              </p>
              <p className="w-full truncate rounded-md bg-muted px-2 py-1.5 text-center font-mono text-xs text-muted-foreground">
                {qr.payurl}
              </p>
            </div>
            <DialogFooter className="gap-2 sm:justify-between">
              <Button variant="ghost" onClick={() => setQr(null)}>
                返回修改
              </Button>
              <Button onClick={() => gotoPayurl(qr.payurl)}>
                <ExternalLinkIcon data-icon="inline-start" aria-hidden />
                打开收银台
              </Button>
            </DialogFooter>
          </div>
        ) : (
          <div className="space-y-4">
            <RadioGroup
              value={channel}
              onValueChange={(value) => setChannel(value as PayChannel)}
              aria-label="支付方式"
              className="gap-2"
            >
              {PAY_METHODS.map((method) => (
                <Label
                  key={method.value}
                  className={cn(
                    'flex cursor-pointer items-start gap-3 rounded-xl border p-3 transition-colors',
                    channel === method.value
                      ? 'border-primary bg-primary/5'
                      : 'border-border hover:bg-muted/50',
                  )}
                >
                  <RadioGroupItem value={method.value} className="mt-0.5" />
                  <span className="flex flex-col gap-0.5">
                    <span className="text-sm font-medium text-foreground">
                      {method.title}
                      {method.value === 'balance' ? `（余额 ${formatMoney(balance)}）` : ''}
                    </span>
                    <span className="text-xs text-muted-foreground">{method.description}</span>
                  </span>
                </Label>
              ))}
            </RadioGroup>

            {channel === 'epay' ? (
              <RadioGroup
                value={payType}
                onValueChange={(value) => setPayType(value as EpayType)}
                aria-label="在线支付方式"
                className="flex gap-3"
              >
                {EPAY_TYPES.map((item) => (
                  <Label key={item.value} className="flex cursor-pointer items-center gap-2 text-sm">
                    <RadioGroupItem value={item.value} />
                    {item.label}
                  </Label>
                ))}
              </RadioGroup>
            ) : null}

            {channel === 'balance' && balanceInsufficient ? (
              <Alert variant="destructive">
                <AlertCircleIcon aria-hidden />
                <AlertDescription>
                  余额不足（当前 {formatMoney(balance)}，应付 {formatMoney(order?.final_amount)}），
                  请改用在线支付。
                </AlertDescription>
              </Alert>
            ) : null}

            {error ? (
              <Alert variant="destructive">
                <AlertCircleIcon aria-hidden />
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            ) : null}

            <DialogFooter className="gap-2 sm:justify-between">
              <Button variant="outline" onClick={() => onOpenChange(false)} disabled={paying}>
                稍后再付
              </Button>
              <Button
                onClick={handlePay}
                disabled={paying || (channel === 'balance' && balanceInsufficient)}
              >
                {paying ? (
                  <>
                    <LoaderCircleIcon className="animate-spin" data-icon="inline-start" aria-hidden />
                    正在发起支付…
                  </>
                ) : channel === 'epay' ? (
                  '前往支付'
                ) : (
                  '余额支付'
                )}
              </Button>
            </DialogFooter>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
