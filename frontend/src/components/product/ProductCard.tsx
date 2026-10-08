import { Button, Card, Chip } from '@heroui/react'
import { useNavigate } from 'react-router-dom'

import { paths } from '../../app/paths'
import type { ProductSummary } from '../../api/types'
import { cheapestCycle } from '../../lib/cycles'
import { formatCycleLabel, formatMoney } from '../../lib/format'
import { productTypeLabel } from '../../lib/productText'

// ProductCard 是商品列表卡片：名称/类型/最低价与周期提示/库存与试用标签 + 两个入口。
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
    <Card className="flex h-full flex-col">
      <Card.Header>
        <div className="flex items-start justify-between gap-2">
          <Card.Title className="text-base leading-snug">{product.name}</Card.Title>
          <Chip size="sm" variant="soft" color="accent">
            {productTypeLabel(product.type)}
          </Chip>
        </div>
        {groupName ? <Card.Description>{groupName}</Card.Description> : null}
      </Card.Header>

      <Card.Content className="flex-1 space-y-3">
        <div>
          {cheapest ? (
            <p className="flex items-baseline gap-1.5">
              <span className="text-2xl font-semibold text-foreground">
                {formatMoney(cheapest.amount)}
              </span>
              <span className="text-sm text-muted">/ {formatCycleLabel(cheapest.cycle)}起</span>
            </p>
          ) : (
            <p className="text-sm text-muted">暂不可售</p>
          )}
        </div>

        <div className="flex flex-wrap gap-1.5">
          {product.stock_control === 1 ? (
            <Chip size="sm" variant="secondary" color={hasStock ? 'success' : 'danger'}>
              {hasStock ? `库存 ${product.stock_qty}` : '库存不足'}
            </Chip>
          ) : (
            <Chip size="sm" variant="secondary" color="default">
              不限库存
            </Chip>
          )}
          {product.ontrial_max > 0 ? (
            <Chip size="sm" variant="secondary" color="warning">
              支持试用
            </Chip>
          ) : null}
        </div>
      </Card.Content>

      <Card.Footer className="flex gap-2">
        <Button
          fullWidth
          variant="outline"
          size="sm"
          onPress={() => navigate(paths.productDetail(product.id))}
        >
          查看详情
        </Button>
        <Button
          fullWidth
          variant="primary"
          size="sm"
          isDisabled={!cheapest}
          onPress={() => navigate(paths.productDetail(product.id))}
        >
          立即购买
        </Button>
      </Card.Footer>
    </Card>
  )
}
