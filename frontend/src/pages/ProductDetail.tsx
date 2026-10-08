import { Alert, Button, Card, Chip, Separator } from '@heroui/react'
import { useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { fetchProductDetail } from '../api/products'
import type { ProductDetail as ProductDetailData } from '../api/types'
import { paths } from '../app/paths'
import { useAuth } from '../auth/authContext'
import { EmptyBlock, ErrorState, LoadingBlock } from '../components/common/PageState'
import ConfigSelector from '../components/product/ConfigSelector'
import CyclePriceTable from '../components/product/CyclePriceTable'
import CycleSelector from '../components/product/CycleSelector'
import { useAsync } from '../hooks/useAsync'
import { availableCycles, cheapestCycle, type BillingCycle } from '../lib/cycles'
import { saveCheckoutDraft } from '../lib/checkout'
import { decodeDescription, formatCycleLabel, formatDuration, formatMoney, monthlyEquivalent } from '../lib/format'
import { productTypeLabel } from '../lib/productText'
import { buildLoginUrl } from '../lib/redirect'

// 默认配置：每个可配置项取第一个可选值（契约 10.3：会员端已过滤隐藏项/值）。
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

export default function ProductDetail() {
  const params = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { isAuthenticated } = useAuth()

  const productId = params.id && /^\d+$/.test(params.id) ? Number(params.id) : null
  const { data, loading, error, reload } = useAsync<ProductDetailData>(
    () => {
      if (productId === null) {
        return Promise.reject(new Error('商品 ID 必须为正整数'))
      }
      return fetchProductDetail(productId)
    },
    [productId],
  )

  // 选择状态：商品加载完成前先按「最低价周期 + 各配置项首个可选值」推导默认值，
  // 用户改动后记在 selection 里（避免用 effect 回写 state 造成额外渲染）。
  const [selection, setSelection] = useState<{
    productId: number
    cycle: BillingCycle
    config: Record<string, string>
  } | null>(null)

  const defaults = useMemo(() => {
    if (!data) {
      return { cycle: 'monthly' as BillingCycle, config: {} as Record<string, string> }
    }
    const cycles = availableCycles(data.prices)
    return {
      cycle: cheapestCycle(data.prices)?.cycle ?? cycles[0] ?? 'monthly',
      config: defaultConfig(data),
    }
  }, [data])

  const active = selection && data && selection.productId === data.id ? selection : null
  const cycle = active?.cycle ?? defaults.cycle
  const config = active?.config ?? defaults.config

  const setCycle = (next: BillingCycle) => {
    if (data) {
      setSelection({ productId: data.id, cycle: next, config })
    }
  }

  const setConfigValue = (optionId: number, valueId: string) => {
    if (data) {
      setSelection({ productId: data.id, cycle, config: { ...config, [String(optionId)]: valueId } })
    }
  }

  const price = data?.prices[cycle] ?? null
  const perMonth = useMemo(
    () => (price ? monthlyEquivalent(price, cycle) : null),
    [price, cycle],
  )

  const handleBuy = () => {
    if (!data) {
      return
    }
    saveCheckoutDraft({
      productId: data.id,
      cycle,
      config,
      productName: data.name,
    })
    const target = paths.checkout(data.id)
    navigate(isAuthenticated ? target : buildLoginUrl(target))
  }

  if (loading) {
    return <LoadingBlock label="正在加载商品详情…" />
  }

  if (error) {
    const missing = error.includes('商品不存在')
    return (
      <div className="mx-auto max-w-3xl px-4 py-16 sm:px-6">
        {missing ? (
          <EmptyBlock
            title="商品不存在或已下架"
            description={
              <span>
                该商品可能已被管理员下架。可以
                <Link className="mx-1 text-accent underline-offset-4 hover:underline" to={paths.products}>
                  返回商品列表
                </Link>
                查看其他商品。
              </span>
            }
          />
        ) : (
          <ErrorState message={error} onRetry={reload} />
        )}
      </div>
    )
  }

  if (!data) {
    return null
  }

  const hasConfig = data.config_groups.some((group) => group.options.length > 0)
  const description = decodeDescription(data.description)
  const outOfStock = data.stock_control === 1 && data.stock_qty <= 0

  return (
    <div className="mx-auto max-w-6xl px-4 py-8 sm:px-6">
      <nav className="mb-4 text-sm text-muted" aria-label="面包屑">
        <Link className="hover:text-foreground" to={paths.products}>
          商品列表
        </Link>
        <span className="mx-2">/</span>
        <span className="text-foreground">{data.name}</span>
      </nav>

      <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_360px]">
        <div className="space-y-8">
          <header>
            <div className="flex flex-wrap items-center gap-2">
              <Chip size="sm" variant="soft" color="accent">
                {data.group.name}
              </Chip>
              <Chip size="sm" variant="secondary" color="default">
                {productTypeLabel(data.type)}
              </Chip>
              {data.stock_control === 1 ? (
                <Chip size="sm" variant="secondary" color={outOfStock ? 'danger' : 'success'}>
                  {outOfStock ? '库存不足' : `库存 ${data.stock_qty}`}
                </Chip>
              ) : (
                <Chip size="sm" variant="secondary" color="default">
                  不限库存
                </Chip>
              )}
              {data.ontrial_max > 0 ? (
                <Chip size="sm" variant="secondary" color="warning">
                  支持试用
                </Chip>
              ) : null}
            </div>
            <h1 className="mt-3 text-2xl font-semibold text-foreground sm:text-3xl">{data.name}</h1>
          </header>

          <section>
            <h2 className="mb-3 text-base font-medium text-foreground">商品说明</h2>
            {description ? (
              <div className="whitespace-pre-line rounded-xl border border-border bg-surface p-4 text-sm leading-relaxed text-muted">
                {description}
              </div>
            ) : (
              <p className="text-sm text-muted">该商品暂无描述。</p>
            )}
          </section>

          <section>
            <h2 className="mb-3 text-base font-medium text-foreground">
              配置项
              {hasConfig ? null : <span className="ml-2 text-sm text-muted">（该商品无需选择配置）</span>}
            </h2>
            <ConfigSelector groups={data.config_groups} selected={config} onChange={setConfigValue} />
          </section>

          <section>
            <h2 className="mb-3 text-base font-medium text-foreground">价格一览</h2>
            <CyclePriceTable prices={data.prices} highlight={cycle} />
            <p className="mt-2 text-xs text-muted">
              价格为本地售价（含后台定价规则），最终以提交订单时的金额为准。
            </p>
          </section>
        </div>

        <aside className="lg:sticky lg:top-24 lg:self-start">
          <Card>
            <Card.Header>
              <Card.Title className="text-base">选择计费周期</Card.Title>
              <Card.Description>不可售周期已置灰，共 6 档可选周期</Card.Description>
            </Card.Header>
            <Card.Content className="space-y-4">
              <CycleSelector prices={data.prices} value={cycle} onChange={setCycle} size="sm" />

              <Separator />

              <div>
                <div className="flex items-baseline gap-2">
                  <span className="text-2xl font-semibold text-foreground">
                    {formatMoney(price)}
                  </span>
                  <span className="text-sm text-muted">/ {formatCycleLabel(cycle)}</span>
                </div>
                <p className="mt-1 text-xs text-muted">
                  {perMonth && cycle !== 'monthly'
                    ? `折合约 ¥${perMonth} / 月 · 时长 ${formatDuration(cycle)}`
                    : `时长 ${formatDuration(cycle)}`}
                </p>
              </div>

              {outOfStock ? (
                <Alert status="warning">
                  <Alert.Indicator />
                  <Alert.Content>
                    <Alert.Description>
                      当前库存不足，仍可下单，实际开通结果由上游库存决定。
                    </Alert.Description>
                  </Alert.Content>
                </Alert>
              ) : null}

              <Button
                fullWidth
                variant="primary"
                size="lg"
                isDisabled={price === null}
                onPress={handleBuy}
              >
                {price === null ? '当前周期不可售' : '立即购买'}
              </Button>
              <p className="text-center text-xs text-muted">
                {isAuthenticated ? '下单后可在线支付或使用余额支付' : '未登录将先跳转登录，登录后自动回到下单页'}
              </p>
            </Card.Content>
          </Card>
        </aside>
      </div>
    </div>
  )
}
