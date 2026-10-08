import {
  Alert,
  Button,
  Card,
  Chip,
  Input,
  Label,
  Radio,
  RadioGroup,
  Separator,
  TextField,
} from '@heroui/react'
import { useCallback, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { errorMessage } from '../api/client'
import { validateCoupon } from '../api/coupons'
import { fetchBalance } from '../api/finance'
import { cancelOrder, createOrder, payOrder } from '../api/orders'
import { fetchProductDetail } from '../api/products'
import type {
  CouponInvalidReason,
  CouponValidation,
  EpayType,
  Order,
  PayChannel,
  ProductDetail as ProductDetailData,
} from '../api/types'
import { paths } from '../app/paths'
import { useAuth } from '../auth/authContext'
import { EmptyBlock, ErrorState, LoadingBlock } from '../components/common/PageState'
import ConfigSelector from '../components/product/ConfigSelector'
import CycleSelector from '../components/product/CycleSelector'
import StatusBadge from '../components/StatusBadge'
import { useAsync } from '../hooks/useAsync'
import { cheapestCycle, type BillingCycle } from '../lib/cycles'
import { readCheckoutDraft, rememberOrder } from '../lib/checkout'
import { formatCycleLabel, formatDateTimeOr, formatMoney } from '../lib/format'
import { ORDER_STATUS_LABELS, ORDER_STATUS_TONES } from '../lib/orderStatus'
import { optionLabel, valueLabel } from '../lib/productText'

// 校验失败原因文案（契约 11.2 的 5 个 reason 枚举，固定不扩展）。
const COUPON_REASON_MESSAGES: Record<CouponInvalidReason, string> = {
  disabled: '优惠码已停用',
  not_started: '优惠码尚未生效',
  expired: '优惠码已过期',
  used_up: '优惠码使用次数已用尽',
  cycle_not_applicable: '优惠码不适用于该周期',
}

function defaultConfig(product: ProductDetailData): Record<string, string> {
  const selected: Record<string, string> = {}
  for (const group of product.config_groups) {
    for (const option of group.options) {
      const first = option.values[0]
      if (first) {
        selected[String(option.id)] = String(first.id)
      }
    }
  }
  return selected
}

const PAY_METHODS: { value: PayChannel; title: string; description: string }[] = [
  { value: 'epay', title: '在线支付', description: '跳转易支付收银台，支持支付宝 / 微信' },
  { value: 'balance', title: '余额支付', description: '使用账户余额即时扣款并自动开通' },
]

// Checkout 是下单确认页：展示「商品/周期/配置/金额/优惠码」，提交后创建订单并发起支付。
export default function Checkout() {
  const params = useParams<{ productId: string }>()
  const navigate = useNavigate()
  const { member, setBalance } = useAuth()

  const productId = params.productId && /^\d+$/.test(params.productId) ? Number(params.productId) : null

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

  const balanceState = useAsync(fetchBalance, [], Boolean(member))
  const balance = balanceState.data?.balance ?? member?.balance ?? '0.00'

  // 选择状态（周期 + 配置项）：默认值由「详情页草稿 → 最低价周期」推导，
  // 用户改动后记在 selection 里，不在 effect 里回写 state。
  const [selection, setSelection] = useState<{
    productId: number
    cycle: BillingCycle
    config: Record<string, string>
  } | null>(null)

  const [couponCode, setCouponCode] = useState('')
  const [coupon, setCoupon] = useState<CouponValidation | null>(null)
  const [couponError, setCouponError] = useState('')
  const [couponLoading, setCouponLoading] = useState(false)

  const [channel, setChannel] = useState<PayChannel>('epay')
  const [payType, setPayType] = useState<EpayType>('alipay')

  const [order, setOrder] = useState<Order | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [paying, setPaying] = useState(false)
  const [actionError, setActionError] = useState('')
  const [notice, setNotice] = useState('')

  // 默认选择：优先用商品详情页存下的草稿；草稿缺失或该周期不可售时回落到最低价周期。
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
  const balanceInsufficient = finalAmount !== null && Number(balance) < Number(finalAmount)

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
      setNotice('订单已创建，请在下方完成支付。')
    } catch (error) {
      setActionError(errorMessage(error, '下单失败，请稍后重试'))
    } finally {
      setSubmitting(false)
    }
  }

  const handlePay = async () => {
    if (!order) {
      return
    }
    setPaying(true)
    setActionError('')
    setNotice('')
    try {
      const result = await payOrder(order.id, {
        channel,
        ...(channel === 'epay' ? { pay_type: payType } : {}),
      })
      if (channel === 'epay') {
        const payurl = result.pay.payurl
        if (!payurl) {
          setActionError('支付渠道未返回支付地址，请稍后重试或改用余额支付')
          return
        }
        rememberOrder({
          id: result.order.id,
          trade_no: result.order.trade_no,
          productId: result.order.product_id,
        })
        // 跳出本站前往渠道收银台；回跳地址由后端 return_url 设置决定。
        window.location.assign(payurl)
        return
      }
      // 余额支付：本地已入账，直接进入结果页。
      if (result.pay.balance_after) {
        setBalance(result.pay.balance_after)
      }
      navigate(`${paths.payResult}?order=${result.order.id}&channel=balance`, { replace: true })
    } catch (error) {
      setActionError(errorMessage(error, '支付失败，请稍后重试'))
    } finally {
      setPaying(false)
    }
  }

  const handleCancelOrder = async () => {
    if (!order) {
      return
    }
    setSubmitting(true)
    setActionError('')
    try {
      const cancelled = await cancelOrder(order.id)
      setOrder(cancelled)
      setNotice('订单已取消。如需重新购买请点击「重新下单」。')
    } catch (error) {
      setActionError(errorMessage(error, '取消订单失败'))
    } finally {
      setSubmitting(false)
    }
  }

  const resetOrder = () => {
    setOrder(null)
    setNotice('')
    setActionError('')
  }

  const configSummary = useMemo(() => {
    if (!product) {
      return []
    }
    const items: { label: string; value: string }[] = []
    for (const group of product.config_groups) {
      for (const option of group.options) {
        const valueId = config[String(option.id)]
        const value = option.values.find((item) => String(item.id) === valueId)
        if (value) {
          items.push({ label: optionLabel(option.name), value: valueLabel(value.name) })
        }
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
              <Link className="text-accent hover:underline" to={paths.products}>
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

  return (
    <div className="mx-auto max-w-6xl px-4 py-8 sm:px-6">
      <nav className="mb-4 text-sm text-muted" aria-label="面包屑">
        <Link className="hover:text-foreground" to={paths.products}>
          商品列表
        </Link>
        <span className="mx-2">/</span>
        <Link className="hover:text-foreground" to={paths.productDetail(product.id)}>
          {product.name}
        </Link>
        <span className="mx-2">/</span>
        <span className="text-foreground">确认订单</span>
      </nav>

      <h1 className="mb-6 text-2xl font-semibold text-foreground">确认订单</h1>

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_380px]">
        <div className="space-y-6">
          <Card>
            <Card.Header>
              <Card.Title className="text-base">商品与配置</Card.Title>
              <Card.Description>
                {product.group.name} · {product.name}
              </Card.Description>
            </Card.Header>
            <Card.Content className="space-y-6">
              {order ? (
                <div className="grid gap-2 text-sm">
                  <div className="flex justify-between gap-4">
                    <span className="text-muted">商品</span>
                    <span className="text-right text-foreground">{order.product_name}</span>
                  </div>
                  <div className="flex justify-between gap-4">
                    <span className="text-muted">周期</span>
                    <span className="text-foreground">{formatCycleLabel(order.cycle)}</span>
                  </div>
                  {configSummary.map((item) => (
                    <div key={item.label} className="flex justify-between gap-4">
                      <span className="text-muted">{item.label}</span>
                      <span className="text-foreground">{item.value}</span>
                    </div>
                  ))}
                  <div className="flex justify-between gap-4">
                    <span className="text-muted">数量</span>
                    <span className="text-foreground">{order.qty}</span>
                  </div>
                </div>
              ) : (
                <>
                  <div>
                    <p className="mb-2 text-sm font-medium text-foreground">计费周期</p>
                    <CycleSelector prices={product.prices} value={cycle} onChange={changeCycle} size="sm" />
                  </div>
                  <div>
                    <p className="mb-2 text-sm font-medium text-foreground">配置项</p>
                    <ConfigSelector
                      groups={product.config_groups}
                      selected={config}
                      onChange={changeConfigValue}
                    />
                    {product.config_groups.length === 0 ? (
                      <p className="text-sm text-muted">该商品无需选择配置。</p>
                    ) : null}
                  </div>
                </>
              )}
            </Card.Content>
          </Card>

          {order ? (
            <Card>
              <Card.Header>
                <Card.Title className="text-base">订单支付</Card.Title>
                <Card.Description>订单号 {order.trade_no}</Card.Description>
              </Card.Header>
              <Card.Content className="space-y-4">
                <div className="flex flex-wrap items-center gap-2">
                  <StatusBadge
                    tone={ORDER_STATUS_TONES[order.status]}
                    label={ORDER_STATUS_LABELS[order.status]}
                  />
                  {order.status === 'pending' ? (
                    <>
                      <span className="text-sm text-muted">创建于 {formatDateTimeOr(order.created_at)}</span>
                    </>
                  ) : null}
                </div>

                {order.status === 'pending' ? (
                  <>
                    <RadioGroup
                      aria-label="支付方式"
                      value={channel}
                      onChange={(value) => setChannel(value as PayChannel)}
                      className="gap-3"
                    >
                      {PAY_METHODS.map((method) => (
                        <Radio key={method.value} value={method.value}>
                          <Radio.Content>
                            <div className="flex flex-col">
                              <span className="text-sm font-medium">
                                {method.title}
                                {method.value === 'balance'
                                  ? `（余额 ${formatMoney(balance)}）`
                                  : ''}
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
                        <Radio value="alipay">支付宝</Radio>
                        <Radio value="wxpay">微信支付</Radio>
                      </RadioGroup>
                    ) : null}

                    {channel === 'balance' && balanceInsufficient ? (
                      <Alert status="warning">
                        <Alert.Indicator />
                        <Alert.Content>
                          <Alert.Description>
                            余额不足（当前 {formatMoney(balance)}，应付 {formatMoney(order.final_amount)}），
                            请改用在线支付或在会员区充值。
                          </Alert.Description>
                        </Alert.Content>
                      </Alert>
                    ) : null}

                    <div className="flex flex-wrap gap-3">
                      <Button
                        variant="primary"
                        isDisabled={paying || (channel === 'balance' && balanceInsufficient)}
                        onPress={handlePay}
                      >
                        {paying ? '正在发起支付…' : channel === 'epay' ? '前往支付' : '余额支付'}
                      </Button>
                      <Button variant="outline" isDisabled={submitting} onPress={handleCancelOrder}>
                        取消订单
                      </Button>
                    </div>
                  </>
                ) : (
                  <div className="space-y-3">
                    <p className="text-sm text-muted">
                      {order.status === 'cancelled'
                        ? '该订单已取消。'
                        : '该订单已支付，可前往会员区查看订单状态与交付进度。'}
                    </p>
                    <div className="flex flex-wrap gap-3">
                      <Button variant="primary" onPress={() => navigate(paths.consoleOrders)}>
                        查看订单
                      </Button>
                      <Button variant="outline" onPress={resetOrder}>
                        重新下单
                      </Button>
                    </div>
                  </div>
                )}
              </Card.Content>
            </Card>
          ) : null}

          {notice ? (
            <Alert status="success">
              <Alert.Indicator />
              <Alert.Content>
                <Alert.Description>{notice}</Alert.Description>
              </Alert.Content>
            </Alert>
          ) : null}
          {actionError ? (
            <Alert status="danger">
              <Alert.Indicator />
              <Alert.Content>
                <Alert.Description>{actionError}</Alert.Description>
              </Alert.Content>
            </Alert>
          ) : null}
        </div>

        <aside className="lg:sticky lg:top-24 lg:self-start">
          <Card>
            <Card.Header>
              <Card.Title className="text-base">费用明细</Card.Title>
            </Card.Header>
            <Card.Content className="space-y-3 text-sm">
              <div className="flex justify-between">
                <span className="text-muted">{formatCycleLabel(cycle)}原价</span>
                <span className="text-foreground">{formatMoney(order?.amount ?? price)}</span>
              </div>

              {order ? (
                <div className="flex justify-between">
                  <span className="text-muted">优惠码</span>
                  <span className="text-foreground">
                    {order.coupon_code ? `${order.coupon_code} · -${formatMoney(order.discount_amount)}` : '未使用'}
                  </span>
                </div>
              ) : (
                <div className="space-y-2">
                  <TextField
                    name="coupon_code"
                    value={couponCode}
                    onChange={(value) => {
                      setCouponCode(value)
                      setCoupon(null)
                      setCouponError('')
                    }}
                    isInvalid={Boolean(couponError)}
                    isDisabled={Boolean(order)}
                  >
                    <Label>优惠码</Label>
                    <div className="flex gap-2">
                      <Input placeholder="输入优惠码，如 WELCOME10" />
                      <Button
                        variant="outline"
                        size="sm"
                        isDisabled={couponLoading}
                        onPress={handleValidateCoupon}
                      >
                        {couponLoading ? '校验中…' : '校验'}
                      </Button>
                    </div>
                  </TextField>
                  {couponError ? <p className="text-xs text-danger">{couponError}</p> : null}
                  {coupon?.valid ? (
                    <p className="text-xs text-success">
                      {coupon.code} 可用：减免 {formatMoney(coupon.discount_amount)}
                      <button
                        type="button"
                        className="ml-2 text-muted underline-offset-2 hover:underline"
                        onClick={clearCoupon}
                      >
                        清除
                      </button>
                    </p>
                  ) : null}
                </div>
              )}

              {order ? (
                <div className="flex justify-between">
                  <span className="text-muted">优惠减免</span>
                  <span className="text-foreground">
                    {Number(order.discount_amount) > 0 ? `-${formatMoney(order.discount_amount)}` : '—'}
                  </span>
                </div>
              ) : Number(discount) > 0 ? (
                <div className="flex justify-between">
                  <span className="text-muted">优惠减免</span>
                  <span className="text-foreground">-{formatMoney(discount)}</span>
                </div>
              ) : null}

              <Separator />

              <div className="flex items-baseline justify-between">
                <span className="text-muted">应付金额</span>
                <span className="text-xl font-semibold text-foreground">
                  {formatMoney(order?.final_amount ?? finalAmount)}
                </span>
              </div>

              {order ? null : (
                <Button
                  fullWidth
                  variant="primary"
                  size="lg"
                  isDisabled={submitting || price === null}
                  onPress={handleCreateOrder}
                >
                  {submitting ? '正在创建订单…' : '提交订单'}
                </Button>
              )}

              <p className="text-xs text-muted">
                {order
                  ? '订单创建后金额与优惠已快照，重复发起支付不会重复扣款。'
                  : '提交订单后生成待支付订单，可选择在线支付或余额支付。'}
              </p>

              {price === null ? (
                <Chip color="danger" variant="soft" size="sm">
                  当前周期不可售，请切换其他周期
                </Chip>
              ) : null}
            </Card.Content>
          </Card>
        </aside>
      </div>
    </div>
  )
}
