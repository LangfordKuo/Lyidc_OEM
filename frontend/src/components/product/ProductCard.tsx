import { Link, useNavigate } from 'react-router-dom'

import { paths } from '@/app/paths'
import type { ProductSummary } from '@/api/types'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { cheapestCycle } from '@/lib/cycles'
import { formatCycleLabel, formatMoney } from '@/lib/format'
import { productTypeLabel } from '@/lib/productText'

/** 商品卡片：名称/类型/最低价与周期提示/库存与试用标签 + 两个入口。 */
export default function ProductCard({
  product,
  groupName,
}: {
  product: ProductSummary
  groupName?: string
}) {
  const navigate = useNavigate()
  const cheapest = cheapestCycle(product.prices)
  const hasStock = product.stock_control !== 1 || product.stock_qty > 0

  return (
    <Card className="flex h-full flex-col transition-shadow hover:shadow-md">
      <CardHeader>
        <div className="flex items-start justify-between gap-2">
          <CardTitle className="text-base leading-snug">
            <Link to={paths.productDetail(product.id)} className="hover:text-primary">
              {product.name}
            </Link>
          </CardTitle>
          <Badge variant="secondary">{productTypeLabel(product.type)}</Badge>
        </div>
        {groupName ? <CardDescription>{groupName}</CardDescription> : null}
      </CardHeader>

      <CardContent className="flex-1 space-y-3">
        {cheapest ? (
          <p className="flex items-baseline gap-1.5">
            <span className="text-2xl font-semibold text-foreground">
              {formatMoney(cheapest.amount)}
            </span>
            <span className="text-sm text-muted-foreground">/ {formatCycleLabel(cheapest.cycle)}起</span>
          </p>
        ) : (
          <p className="text-sm text-muted-foreground">暂不可售</p>
        )}

        <div className="flex flex-wrap gap-1.5">
          {product.stock_control === 1 ? (
            <Badge
              variant="outline"
              className={hasStock ? 'text-emerald-700 dark:text-emerald-400' : 'text-destructive'}
            >
              {hasStock ? `库存 ${product.stock_qty}` : '库存不足'}
            </Badge>
          ) : (
            <Badge variant="outline" className="text-muted-foreground">
              不限库存
            </Badge>
          )}
          {product.ontrial_max > 0 ? (
            <Badge variant="outline" className="text-amber-700 dark:text-amber-400">
              支持试用
            </Badge>
          ) : null}
        </div>
      </CardContent>

      <CardFooter className="gap-2">
        <Button
          className="flex-1"
          variant="outline"
          size="sm"
          onClick={() => navigate(paths.productDetail(product.id))}
        >
          查看详情
        </Button>
        <Button
          className="flex-1"
          size="sm"
          disabled={!cheapest}
          onClick={() => navigate(paths.productDetail(product.id))}
        >
          立即购买
        </Button>
      </CardFooter>
    </Card>
  )
}
