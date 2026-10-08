import { Button, Card, Chip } from '@heroui/react'
import { useMemo } from 'react'
import { useNavigate } from 'react-router-dom'

import { fetchProductCatalog } from '../api/products'
import ProductCard from '../components/product/ProductCard'
import SectionHeading from '../components/common/SectionHeading'
import { CardSkeletonGrid, EmptyBlock, ErrorState } from '../components/common/PageState'
import {
  IconBolt,
  IconLifeRing,
  IconShield,
  IconWallet,
} from '../components/common/icons'
import { useAsync } from '../hooks/useAsync'
import { paths } from '../app/paths'
import { SITE_DESCRIPTION, SITE_NAME, SITE_TAGLINE } from '../lib/site'

const HIGHLIGHTS = [
  { value: '6 种', label: '计费周期', hint: '月付 / 季付 / 半年付 / 年付 / 两年付 / 三年付' },
  { value: '自动', label: '开通交付', hint: '支付成功后自动向上游开通，无需等待人工' },
  { value: '在线', label: '支付充值', hint: '支持支付宝、微信与余额支付' },
  { value: '7×24', label: '工单支持', hint: '会员工单直达客服与管理员' },
]

const FEATURES = [
  {
    icon: IconBolt,
    title: '即时开通',
    description: '支付到账后自动向上游提交开通请求，实例落库即可在控制台查看。',
  },
  {
    icon: IconShield,
    title: '稳定可靠',
    description: '多区域机房与 CN2 优质线路，库存与状态实时同步，避免超卖。',
  },
  {
    icon: IconWallet,
    title: '灵活计费',
    description: '六档计费周期自由选择，支持优惠码抵扣与余额支付，账目清晰可查。',
  },
  {
    icon: IconLifeRing,
    title: '专属支持',
    description: '会员工单系统与到期提醒通知，续费、重装、开关机在控制台自助完成。',
  },
]

// Home 是官网首页：Hero + 精选商品 + 服务保障。
export default function Home() {
  const navigate = useNavigate()
  const { data, loading, error, reload } = useAsync(fetchProductCatalog, [])

  const featured = useMemo(() => {
    if (!data) {
      return []
    }
    return data.groups
      .flatMap((group) => group.products.map((product) => ({ product, groupName: group.name })))
      .slice(0, 6)
  }, [data])

  return (
    <div>
      <section className="border-b border-border bg-linear-to-b from-accent-soft/70 via-background to-background">
        <div className="mx-auto max-w-6xl px-4 py-16 sm:px-6 sm:py-24">
          <Chip variant="soft" color="accent" size="sm">
            {SITE_TAGLINE}
          </Chip>
          <h1 className="mt-5 text-3xl font-semibold tracking-tight text-foreground sm:text-5xl">
            {SITE_NAME}
          </h1>
          <p className="mt-4 max-w-2xl text-base text-muted sm:text-lg">{SITE_DESCRIPTION}</p>

          <div className="mt-8 flex flex-wrap gap-3">
            <Button variant="primary" size="lg" onPress={() => navigate(paths.products)}>
              浏览商品
            </Button>
            <Button variant="outline" size="lg" onPress={() => navigate(paths.console)}>
              进入控制台
            </Button>
          </div>

          <dl className="mt-12 grid grid-cols-2 gap-6 sm:grid-cols-4">
            {HIGHLIGHTS.map((item) => (
              <div key={item.label}>
                <dt className="text-2xl font-semibold text-foreground">{item.value}</dt>
                <dd className="mt-1 text-sm text-muted">{item.label}</dd>
                <dd className="mt-0.5 hidden text-xs text-muted/80 sm:block">{item.hint}</dd>
              </div>
            ))}
          </dl>
        </div>
      </section>

      <section className="mx-auto max-w-6xl px-4 py-14 sm:px-6">
        <SectionHeading
          title="精选商品"
          description="已上架商品按分组展示，价格与库存实时同步"
          action={
            <Button variant="ghost" size="sm" onPress={() => navigate(paths.products)}>
              查看全部商品
            </Button>
          }
        />

        {loading ? <CardSkeletonGrid count={3} /> : null}
        {!loading && error ? <ErrorState message={error} onRetry={reload} /> : null}
        {!loading && !error && featured.length === 0 ? (
          <EmptyBlock
            title="暂无已上架商品"
            description="管理员在后台导入并上架商品后，这里会自动展示。"
          />
        ) : null}
        {!loading && !error && featured.length > 0 ? (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {featured.map(({ product, groupName }) => (
              <ProductCard key={product.id} product={product} groupName={groupName} />
            ))}
          </div>
        ) : null}
      </section>

      <section className="border-t border-border bg-surface-secondary/40">
        <div className="mx-auto max-w-6xl px-4 py-14 sm:px-6">
          <SectionHeading title="为什么选择我们" description="从下单到运维，全流程在线自助" />
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            {FEATURES.map(({ icon: Icon, title, description }) => (
              <Card key={title} className="h-full">
                <Card.Header>
                  <span className="grid size-10 place-items-center rounded-lg bg-accent-soft text-accent-soft-foreground">
                    <Icon className="size-5" />
                  </span>
                  <Card.Title className="mt-3 text-base">{title}</Card.Title>
                </Card.Header>
                <Card.Content>
                  <p className="text-sm text-muted">{description}</p>
                </Card.Content>
              </Card>
            ))}
          </div>
        </div>
      </section>
    </div>
  )
}
