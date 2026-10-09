import { useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { AlertTriangleIcon, ChevronRightIcon, ShoppingCartIcon } from 'lucide-react'

import { fetchProductDetail } from '@/api/products'
import type { ProductDetail as ProductDetailData } from '@/api/types'
import { paths } from '@/app/paths'
import { useAuth } from '@/auth/authContext'
import { EmptyBlock, ErrorState, LoadingBlock } from '@/components/common/PageState'
import ConfigSelector from '@/components/product/ConfigSelector'
import CyclePriceTable from '@/components/product/CyclePriceTable'
import CycleSelector from '@/components/product/CycleSelector'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import { useAsync } from '@/hooks/useAsync'
import { saveCheckoutDraft } from '@/lib/checkout'
import { defaultConfigValue } from '@/lib/configControl'
import { availableCycles, cheapestCycle, type BillingCycle } from '@/lib/cycles'
import { decodeDescription, formatCycleLabel, formatDuration, formatMoney, monthlyEquivalent } from '@/lib/format'
import { productTypeLabel } from '@/lib/productText'
import { buildLoginUrl } from '@/lib/redirect'

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

/** 商品详情页：信息 + 六周期价格表 + 配置项（有则显）+ 购买入口。 */
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

  // 选择状态（周期 + 配置项）：用户在页面上改动后记在 selection 里，
  // 默认值按「最低价周期 + 各配置项首个可选值」推导（不在 effect 里回写 state）。
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
  const perMonth = useMemo(() => (price ? monthlyEquivalent(price, cycle) : null), [price, cycle])

  const handleBuy = () => {
    if (!data) {
      return
    }
    saveCheckoutDraft({ productId: data.id, cycle, config, productName: data.name })
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
                <Link className="mx-1 text-primary hover:underline" to={paths.products}>
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
      <nav className="mb-4 flex items-center gap-1 text-sm text-muted-foreground" aria-label="面包屑">
        <Link className="hover:text-foreground" to={paths.products}>
          商品列表
        </Link>
        <ChevronRightIcon className="size-3.5" aria-hidden />
        <span className="text-foreground">{data.name}</span>
      </nav>

      <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_360px]">
        <div className="space-y-8">
          <header>
            <div className="flex flex-wrap items-center gap-2">
              <Badge className="bg-accent text-primary">{data.group.name}</Badge>
              <Badge variant="secondary">{productTypeLabel(data.type)}</Badge>
              {data.stock_control === 1 ? (
                <Badge
                  variant="outline"
                  className={outOfStock ? 'text-destructive' : 'text-success dark:text-green-400'}
                >
                  {outOfStock ? '库存不足' : `库存 ${data.stock_qty}`}
                </Badge>
              ) : (
                <Badge variant="outline" className="text-muted-foreground">
                  不限库存
                </Badge>
              )}
              {data.ontrial_max > 0 ? (
                <Badge variant="outline" className="text-warning dark:text-amber-400">
                  支持试用
                </Badge>
              ) : null}
            </div>
            <h1 className="mt-3 text-2xl font-semibold text-foreground sm:text-3xl">{data.name}</h1>
            {/* 配置速览（R5）：与商品卡同一份 description_lines，进入详情第一屏即可读配置。 */}
            {data.description_lines.length > 0 ? (
              <ul
                className="mt-4 grid gap-x-8 gap-y-1.5 text-sm leading-relaxed text-muted-foreground sm:grid-cols-2"
                data-testid="product-config-overview"
              >
                {data.description_lines.map((line, index) => (
                  <li key={index} className="break-words">
                    {line}
                  </li>
                ))}
              </ul>
            ) : null}
          </header>

          <section>
            <h2 className="mb-3 text-base font-medium text-foreground">商品说明</h2>
            {description ? (
              <div className="rounded-xl border border-border bg-muted/30 p-4 text-sm leading-relaxed whitespace-pre-line text-muted-foreground">
                {description}
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">该商品暂无描述。</p>
            )}
          </section>

          <section>
            <h2 className="mb-3 text-base font-medium text-foreground">
              配置项
              {hasConfig ? null : (
                <span className="ml-2 text-sm font-normal text-muted-foreground">
                  （该商品无需选择配置）
                </span>
              )}
            </h2>
            <ConfigSelector groups={data.config_groups} selected={config} onChange={setConfigValue} />
          </section>

          <section>
            <h2 className="mb-3 text-base font-medium text-foreground">价格一览</h2>
            <CyclePriceTable prices={data.prices} highlight={cycle} />
            <p className="mt-2 text-xs text-muted-foreground">
              价格为本地售价（含后台定价规则），最终以提交订单时的金额为准。
            </p>
          </section>
        </div>

        <aside className="lg:sticky lg:top-24 lg:self-start">
          <Card data-testid="product-summary">
            <CardHeader>
              <CardTitle className="text-base">选择计费周期</CardTitle>
              <CardDescription>不可售周期已置灰，共 6 档可选周期</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <CycleSelector prices={data.prices} value={cycle} onChange={setCycle} />

              <Separator />

              <div>
                <div className="flex items-baseline gap-2">
                  <span className="text-2xl font-semibold text-foreground" data-testid="current-cycle-price">
                    {formatMoney(price)}
                  </span>
                  <span className="text-sm text-muted-foreground">/ {formatCycleLabel(cycle)}</span>
                </div>
                <p className="mt-1 text-xs text-muted-foreground">
                  {perMonth && cycle !== 'monthly'
                    ? `折合约 ¥${perMonth} / 月 · 时长 ${formatDuration(cycle)}`
                    : `时长 ${formatDuration(cycle)}`}
                </p>
              </div>

              {outOfStock ? (
                <div className="flex items-start gap-2 rounded-lg border border-warning/30 bg-warning/10 p-3 text-sm text-warning dark:text-amber-400">
                  <AlertTriangleIcon className="mt-0.5 size-4 shrink-0" aria-hidden />
                  <span>当前库存不足，仍可下单，实际开通结果由上游库存决定。</span>
                </div>
              ) : null}

              <Button
                className="h-10 w-full text-base"
                size="lg"
                disabled={price === null}
                onClick={handleBuy}
              >
                <ShoppingCartIcon data-icon="inline-start" aria-hidden />
                {price === null ? '当前周期不可售' : '立即购买'}
              </Button>
              <p className="text-center text-xs text-muted-foreground">
                {isAuthenticated
                  ? '下单后可在线支付或使用余额支付'
                  : '未登录将先跳转登录，登录后自动回到下单页'}
              </p>
            </CardContent>
          </Card>
        </aside>
      </div>
    </div>
  )
}
