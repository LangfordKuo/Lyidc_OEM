import { Link, useNavigate } from 'react-router-dom'

import { paths } from '@/app/paths'
import type { ProductSummary } from '@/api/types'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { availableCycles, cheapestCycle } from '@/lib/cycles'
import { formatCycleLabel, formatMoney } from '@/lib/format'
import { productTypeLabel } from '@/lib/productText'
import { cn } from '@/lib/utils'

/**
 * 商品卡片：名称 / 类型 + 价格大字重 + 配置摘要行（选购要素小标签）+ 两个入口。
 *
 * 说明：商品列表接口（ProductSummary）不下发 CPU/内存等配置明细，配置项只在商品详情的
 * config_groups 里；这里不做编造，摘要行只用真实可得的选购要素：所属分组（区域）、
 * 可售计费档数、试用与库存状态，详情页再展示完整配置项。
 */
export default function ProductCard({
  product,
  groupName,
}: {
  product: ProductSummary
  groupName?: string
}) {
  const navigate = useNavigate()
  const cheapest = cheapestCycle(product.prices)
  const cycleCount = availableCycles(product.prices).length
  const hasStock = product.stock_control !== 1 || product.stock_qty > 0
  const openDetail = () => navigate(paths.productDetail(product.id))

  return (
    <Card className="flex h-full flex-col transition-all duration-200 hover:-translate-y-0.5 hover:ring-primary hover:shadow-md">
      <CardHeader>
        <div className="flex items-start justify-between gap-2">
          <CardTitle className="text-base leading-snug">
            <Link
              to={paths.productDetail(product.id)}
              className="transition-colors hover:text-primary"
            >
              {product.name}
            </Link>
          </CardTitle>
          <Badge variant="secondary">{productTypeLabel(product.type)}</Badge>
        </div>
      </CardHeader>

      <CardContent className="flex-1 space-y-3">
        {cheapest ? (
          <p className="flex items-baseline gap-1.5">
            <span className="text-3xl leading-none font-semibold tabular-nums text-foreground">
              {formatMoney(cheapest.amount)}
            </span>
            <span className="text-sm text-muted-foreground">
              / {formatCycleLabel(cheapest.cycle)}起
            </span>
          </p>
        ) : (
          <p className="text-sm text-muted-foreground">暂不可售</p>
        )}

        {/* 配置摘要行：区域分组 / 计费档数 / 试用，统一 outline 小标签。 */}
        <div className="flex flex-wrap gap-1.5">
          {groupName ? <Badge variant="outline">{groupName}</Badge> : null}
          {cycleCount > 0 ? <Badge variant="outline">{cycleCount} 档计费</Badge> : null}
          {product.ontrial_max > 0 ? <Badge variant="outline">支持试用</Badge> : null}
        </div>

        <div className="flex flex-wrap gap-1.5">
          {product.stock_control === 1 ? (
            <Badge variant="outline" className={cn(hasStock ? 'text-foreground' : 'text-destructive')}>
              {hasStock ? `库存 ${product.stock_qty}` : '库存不足'}
            </Badge>
          ) : (
            <Badge variant="outline" className="text-muted-foreground">
              不限库存
            </Badge>
          )}
        </div>
      </CardContent>

      <CardFooter className="gap-2">
        <Button className="flex-1" variant="outline" size="sm" onClick={openDetail}>
          查看详情
        </Button>
        <Button className="flex-1" size="sm" disabled={!cheapest} onClick={openDetail}>
          立即购买
        </Button>
      </CardFooter>
    </Card>
  )
}
