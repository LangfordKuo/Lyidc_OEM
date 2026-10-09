import { useCallback, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { AlertCircleIcon, CheckCircle2Icon, ChevronRightIcon, LoaderCircleIcon, TicketIcon } from 'lucide-react'

import { errorMessage } from '@/api/client'
import { validateCoupon } from '@/api/coupons'
import { fetchBalance } from '@/api/finance'
import { cancelOrder, createOrder } from '@/api/orders'
import { fetchProductDetail } from '@/api/products'
import type {
  CouponInvalidReason,
  CouponValidation,
  Order,
  OrderPayResult,
  ProductDetail as ProductDetailData,
} from '@/api/types'
import { paths } from '@/app/paths'
import { useAuth } from '@/auth/authContext'
import PayDialog from '@/components/checkout/PayDialog'
import { EmptyBlock, ErrorState, LoadingBlock } from '@/components/common/PageState'
import StatusBadge from '@/components/common/StatusBadge'
import ConfigSelector from '@/components/product/ConfigSelector'
import CycleSelector from '@/components/product/CycleSelector'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import { useAsync } from '@/hooks/useAsync'
import { readCheckoutDraft, rememberOrder } from '@/lib/checkout'
import { defaultConfigValue, describeConfigSelection } from '@/lib/configControl'
import { cheapestCycle, type BillingCycle } from '@/lib/cycles'
import { formatCycleLabel, formatDateTimeOr, formatMoney } from '@/lib/format'
import { ORDER_STATUS_LABELS, ORDER_STATUS_TONES } from '@/lib/orderStatus'
import { optionLabel } from '@/lib/productText'

// 校验失败原因文案（契约 11.2 的 5 个 reason 枚举，固定不扩展）。
const COUPON_REASON_MESSAGES: Record<CouponInvalidReason, string> = {
  disabled: '优惠码已停用',
  not_started: '优惠码尚未生效',
  expired: '优惠码已过期',
  used_up: '优惠码使用次数已用尽',
  cycle_not_applicable: '优惠码不适用于该周期',
}

/**
 * 默认配置：选项型取第一个可选值，数量型取 qty_minimum（R6，契约 10.3）。
 * 会员端已过滤隐藏项/值，这里不再二次处理。
 */
function defaultConfig(product: ProductDetailData): Record<string, string> {
  const selected: Record<string, string> = {}
  for (const group of product.config_groups) {
    for (const option of group.options) {
      const value = defaultConfigValue(option)
      if (value !== null) {
        selected[String(option.id)] = value
      }
    }
  }
  return selected
}

/** 结算页：商品/周期/配置快照 + 优惠码校验 + 金额明细 + 提交订单并拉起支付弹窗。 */
export default function Checkout() {
  const params = useParams<{ productId: string }>()
  const navigate = useNavigate()
  const { member, setBalance } = useAuth()

  const productId =
    params.productId && /^\d+$/.test(params.productId) ? Number(params.productId) : null

  const productState = useAsync<ProductDetailData>(
    () => {
      if (productId === null) {
        return Promise.reject(new Error('商品 ID 必须为正整数'))
      }
      return fetchProductDetail(productId)
    },
    [productId],
  )
  const product = productState.data

  // 余额：登录后实时读取（结算页需展示余额并据此判断余额支付可用性）。
  const balanceState = useAsync(fetchBalance, [member?.id], Boolean(member))
  const balance = balanceState.data?.balance ?? member?.balance ?? '0.00'

  // 选择状态（周期 + 配置项）：默认值由「详情页草稿 → 最低价周期」推导。
  const [selection, setSelection] = useState<{
    productId: number
    cycle: BillingCycle
    config: Record<string, string>
  } | null>(null)

  const [couponCode, setCouponCode] = useState('')
  const [coupon, setCoupon] = useState<CouponValidation | null>(null)
  const [couponError, setCouponError] = useState('')
  const [couponLoading, setCouponLoading] = useState(false)

  const [order, setOrder] = useState<Order | null>(null)
  const [payOpen, setPayOpen] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [cancelling, setCancelling] = useState(false)
  const [actionError, setActionError] = useState('')
  const [notice, setNotice] = useState('')

  const defaults = useMemo(() => {
    if (!product || productId === null) {
      return { cycle: 'monthly' as BillingCycle, config: {} as Record<string, string> }
    }
    const draft = readCheckoutDraft(productId)
    if (draft && product.prices[draft.cycle] !== null) {
      return { cycle: draft.cycle, config: { ...defaultConfig(product), ...draft.config } }
    }
    return {
      cycle: cheapestCycle(product.prices)?.cycle ?? 'monthly',
      config: defaultConfig(product),
    }
  }, [product, productId])

  const active = selection && productId !== null && selection.productId === productId ? selection : null
  const cycle = active?.cycle ?? defaults.cycle
  const config = active?.config ?? defaults.config

  const price = product && product.prices[cycle] !== null ? product.prices[cycle] : null
  const discount = coupon?.valid ? coupon.discount_amount : '0.00'
  const finalAmount = coupon?.valid ? coupon.final_amount : price

  const changeCycle = (next: BillingCycle) => {
    if (productId === null) {
      return
    }
    setSelection({ productId, cycle: next, config })
    // 周期变化后原优惠码结论不再适用（契约 11.2 第 5 条按周期判定），清掉重新校验。
    setCoupon(null)
    setCouponError('')
  }

  const changeConfigValue = (optionId: number, valueId: string) => {
    if (productId === null) {
      return
    }
    setSelection({ productId, cycle, config: { ...config, [String(optionId)]: valueId } })
  }

  const handleValidateCoupon = useCallback(async () => {
    if (!product || productId === null) {
      return
    }
    const code = couponCode.trim()
    if (!code) {
      setCoupon(null)
      setCouponError('请输入优惠码')
      return
    }
    setCouponLoading(true)
    setCouponError('')
    try {
      const result = await validateCoupon(code, productId, cycle)
      setCoupon(result)
      if (!result.valid) {
        setCouponError(COUPON_REASON_MESSAGES[result.reason] ?? '优惠码不可用')
      }
    } catch (error) {
      setCoupon(null)
      setCouponError(errorMessage(error, '优惠码校验失败'))
    } finally {
      setCouponLoading(false)
    }
  }, [couponCode, cycle, product, productId])

  const clearCoupon = () => {
    setCouponCode('')
    setCoupon(null)
    setCouponError('')
  }

  // 提交订单：成功后立即拉起支付弹窗（在线跳收银台/二维码，余额直接扣）。
  const handleCreateOrder = async () => {
    if (!product || productId === null) {
      return
    }
    const code = couponCode.trim()
    // 已校验出「不可用」结论时直接拦下，避免用户误以为已享折扣。
    if (code && coupon && !coupon.valid) {
      setActionError('优惠码不可用，请清除或更换后再提交订单')
      return
    }

    setSubmitting(true)
    setActionError('')
    setNotice('')
    try {
      const created = await createOrder({
        product_id: productId,
        cycle,
        config,
        // 填了码就交给后端判定（后端是权威口径）；未填则明确传空串=不用码。
        coupon_code: code,
      })
      setOrder(created)
      // 记录最近订单：渠道同步回跳可能不带任何参数，支付结果页据此兜底找回。
      rememberOrder({ id: created.id, trade_no: created.trade_no, productId: created.product_id })
      setPayOpen(true)
    } catch (error) {
      setActionError(errorMessage(error, '下单失败，请稍后重试'))
    } finally {
      setSubmitting(false)
    }
  }

  const handleCancelOrder = async () => {
    if (!order) {
      return
    }
    setCancelling(true)
    setActionError('')
    try {
      const cancelled = await cancelOrder(order.id)
      setOrder(cancelled)
      setNotice('订单已取消。如需重新购买请点击「重新下单」。')
    } catch (error) {
      setActionError(errorMessage(error, '取消订单失败'))
    } finally {
      setCancelling(false)
    }
  }

  const resetOrder = () => {
    setOrder(null)
    setNotice('')
    setActionError('')
    setPayOpen(false)
  }

  const handleBalancePaid = (result: OrderPayResult) => {
    if (result.pay.balance_after) {
      setBalance(result.pay.balance_after)
    }
    setPayOpen(false)
    navigate(`${paths.payResult}?order=${result.order.id}&channel=balance`, { replace: true })
  }

  const configSummary = useMemo(() => {
    if (!product) {
      return []
    }
    const items: { label: string; value: string }[] = []
    for (const group of product.config_groups) {
      for (const option of group.options) {
        const raw = config[String(option.id)]
        if (raw === undefined || raw === '') {
          continue
        }
        // 数量型的快照值是数量（Qty 口径），展示走 describeConfigSelection 分支。
        items.push({ label: optionLabel(option.name), value: describeConfigSelection(option, raw) })
      }
    }
    return items
  }, [product, config])

  if (productId === null) {
    return (
      <div className="mx-auto max-w-3xl px-4 py-16 sm:px-6">
        <EmptyBlock title="下单参数不正确" description="请从商品详情页重新发起购买。" />
      </div>
    )
  }

  if (productState.loading) {
    return <LoadingBlock label="正在准备订单信息…" />
  }

  if (productState.error) {
    const missing = productState.error.includes('商品不存在')
    return (
      <div className="mx-auto max-w-3xl px-4 py-16 sm:px-6">
        {missing ? (
          <EmptyBlock
            title="商品不存在或已下架"
            description={
              <Link className="text-primary hover:underline" to={paths.products}>
                返回商品列表
              </Link>
            }
          />
        ) : (
          <ErrorState message={productState.error} onRetry={productState.reload} />
        )}
      </div>
    )
  }

  if (!product) {
    return null
  }

  const orderCreated = order !== null

  return (
    <div className="mx-auto max-w-6xl px-4 py-8 sm:px-6">
      <nav className="mb-4 flex items-center gap-1 text-sm text-muted-foreground" aria-label="面包屑">
        <Link className="hover:text-foreground" to={paths.products}>
          商品列表
        </Link>
        <ChevronRightIcon className="size-3.5" aria-hidden />
        <Link className="hover:text-foreground" to={paths.productDetail(product.id)}>
          {product.name}
        </Link>
        <ChevronRightIcon className="size-3.5" aria-hidden />
        <span className="text-foreground">确认订单</span>
      </nav>

      <h1 className="mb-6 text-2xl font-semibold text-foreground">确认订单</h1>

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_380px]">
        <div className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle className="text-base">商品与配置</CardTitle>
              <CardDescription>
                {product.group.name} · {product.name}
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              {orderCreated ? (
                <dl className="grid gap-2 text-sm">
                  <div className="flex justify-between gap-4">
                    <dt className="text-muted-foreground">商品</dt>
                    <dd className="text-right text-foreground">{order.product_name}</dd>
                  </div>
                  <div className="flex justify-between gap-4">
                    <dt className="text-muted-foreground">周期</dt>
                    <dd className="text-foreground">{formatCycleLabel(order.cycle)}</dd>
                  </div>
                  {configSummary.map((item) => (
                    <div key={item.label} className="flex justify-between gap-4">
                      <dt className="text-muted-foreground">{item.label}</dt>
                      <dd className="text-foreground">{item.value}</dd>
                    </div>
                  ))}
                  <div className="flex justify-between gap-4">
                    <dt className="text-muted-foreground">数量</dt>
                    <dd className="text-foreground">{order.qty}</dd>
                  </div>
                </dl>
              ) : (
                <>
                  <div>
                    <p className="mb-2 text-sm font-medium text-foreground">计费周期</p>
                    <CycleSelector prices={product.prices} value={cycle} onChange={changeCycle} />
                  </div>
                  <div>
                    <p className="mb-2 text-sm font-medium text-foreground">配置项</p>
                    {product.config_groups.length > 0 ? (
                      <ConfigSelector
                        groups={product.config_groups}
                        selected={config}
                        onChange={changeConfigValue}
                        idPrefix="checkout"
                      />
                    ) : (
                      <p className="text-sm text-muted-foreground">该商品无需选择配置。</p>
                    )}
                  </div>
                </>
              )}
            </CardContent>
          </Card>

          {orderCreated ? (
            <Card>
              <CardHeader>
                <CardTitle className="text-base">订单状态</CardTitle>
                <CardDescription>订单号 {order.trade_no}</CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="flex flex-wrap items-center gap-3">
                  <StatusBadge
                    tone={ORDER_STATUS_TONES[order.status]}
                    label={ORDER_STATUS_LABELS[order.status]}
                  />
                  <span className="text-sm text-muted-foreground">
                    创建于 {formatDateTimeOr(order.created_at)}
                  </span>
                </div>

                {order.status === 'pending' ? (
                  <div className="flex flex-wrap gap-3">
                    <Button onClick={() => setPayOpen(true)}>继续支付</Button>
                    <Button variant="outline" disabled={cancelling} onClick={handleCancelOrder}>
                      {cancelling ? '取消中…' : '取消订单'}
                    </Button>
                  </div>
                ) : (
                  <div className="space-y-3">
                    <p className="text-sm text-muted-foreground">
                      {order.status === 'cancelled'
                        ? '该订单已取消。'
                        : '该订单已支付，可前往支付结果页查看状态。'}
                    </p>
                    <div className="flex flex-wrap gap-3">
                      <Button onClick={() => navigate(`${paths.payResult}?order=${order.id}`)}>
                        查看订单状态
                      </Button>
                      <Button variant="outline" onClick={resetOrder}>
                        重新下单
                      </Button>
                    </div>
                  </div>
                )}
              </CardContent>
            </Card>
          ) : null}

          {notice ? (
            <Alert>
              <CheckCircle2Icon className="text-success dark:text-green-400" aria-hidden />
              <AlertDescription>{notice}</AlertDescription>
            </Alert>
          ) : null}
          {actionError ? (
            <Alert variant="destructive">
              <AlertCircleIcon aria-hidden />
              <AlertDescription>{actionError}</AlertDescription>
            </Alert>
          ) : null}
        </div>

        <aside className="lg:sticky lg:top-24 lg:self-start">
          <Card data-testid="checkout-summary">
            <CardHeader className="border-b">
              <CardTitle className="text-base">费用明细</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3 text-sm">
              <div className="flex justify-between">
                <span className="text-muted-foreground">{formatCycleLabel(cycle)}原价</span>
                <span className="text-foreground">{formatMoney(order?.amount ?? price)}</span>
              </div>

              {orderCreated ? (
                <div className="flex justify-between">
                  <span className="text-muted-foreground">优惠码</span>
                  <span className="text-foreground">
                    {order.coupon_code
                      ? `${order.coupon_code} · -${formatMoney(order.discount_amount)}`
                      : '未使用'}
                  </span>
                </div>
              ) : (
                <div className="space-y-2">
                  <label htmlFor="coupon-code" className="flex items-center gap-1.5 text-muted-foreground">
                    <TicketIcon className="size-4" aria-hidden />
                    优惠码
                  </label>
                  <div className="flex gap-2">
                    <Input
                      id="coupon-code"
                      value={couponCode}
                      placeholder="如 WELCOME10"
                      aria-invalid={couponError ? true : undefined}
                      onChange={(event) => {
                        setCouponCode(event.target.value)
                        setCoupon(null)
                        setCouponError('')
                      }}
                    />
                    <Button
                      variant="outline"
                      size="sm"
                      className="h-8 shrink-0"
                      disabled={couponLoading}
                      onClick={handleValidateCoupon}
                    >
                      {couponLoading ? '校验中…' : '校验'}
                    </Button>
                  </div>
                  {couponError ? <p className="text-xs text-destructive">{couponError}</p> : null}
                  {coupon?.valid ? (
                    <p className="flex items-center gap-2 text-xs text-success dark:text-green-400">
                      <Badge variant="outline" className="text-success dark:text-green-400">
                        {coupon.code}
                      </Badge>
                      可用：减免 {formatMoney(coupon.discount_amount)}
                      <button
                        type="button"
                        className="text-muted-foreground underline-offset-2 hover:underline"
                        onClick={clearCoupon}
                      >
                        清除
                      </button>
                    </p>
                  ) : null}
                </div>
              )}

              {orderCreated ? (
                <div className="flex justify-between">
                  <span className="text-muted-foreground">优惠减免</span>
                  <span className="text-foreground">
                    {Number(order.discount_amount) > 0 ? `-${formatMoney(order.discount_amount)}` : '—'}
                  </span>
                </div>
              ) : Number(discount) > 0 ? (
                <div className="flex justify-between">
                  <span className="text-muted-foreground">优惠减免</span>
                  <span className="text-foreground">-{formatMoney(discount)}</span>
                </div>
              ) : null}

              <Separator />

              <div className="flex items-baseline justify-between">
                <span className="text-muted-foreground">应付金额</span>
                <span
                  className="text-2xl font-semibold tabular-nums text-foreground"
                  data-testid="payable-amount"
                >
                  {formatMoney(order?.final_amount ?? finalAmount)}
                </span>
              </div>

              <div className="flex items-center justify-between text-xs text-muted-foreground">
                <span>账户余额</span>
                <span>{formatMoney(balance)}</span>
              </div>

              {orderCreated ? (
                <Button className="h-10 w-full text-base" onClick={() => setPayOpen(true)}>
                  继续支付
                </Button>
              ) : (
                <Button
                  className="h-10 w-full text-base"
                  disabled={submitting || price === null}
                  onClick={handleCreateOrder}
                >
                  {submitting ? (
                    <>
                      <LoaderCircleIcon className="animate-spin" data-icon="inline-start" aria-hidden />
                      正在创建订单…
                    </>
                  ) : (
                    '提交订单'
                  )}
                </Button>
              )}

              <p className="text-xs text-muted-foreground">
                {orderCreated
                  ? '订单创建后金额与优惠已快照，重复发起支付不会重复扣款。'
                  : '提交订单后生成待支付订单，可选择在线支付或余额支付。'}
              </p>

              {price === null ? (
                <Badge variant="destructive">当前周期不可售，请切换其他周期</Badge>
              ) : null}
            </CardContent>
          </Card>
        </aside>
      </div>

      <PayDialog
        order={order}
        open={payOpen}
        onOpenChange={setPayOpen}
        balance={balance}
        onPaid={handleBalancePaid}
      />
    </div>
  )
}
