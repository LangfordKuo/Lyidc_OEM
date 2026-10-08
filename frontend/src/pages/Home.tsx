import { useMemo } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import {
  BadgeCheckIcon,
  ChevronRightIcon,
  CreditCardIcon,
  LifeBuoyIcon,
  RocketIcon,
  ShieldCheckIcon,
  WalletIcon,
  ZapIcon,
} from 'lucide-react'

import { fetchProductCatalog } from '@/api/products'
import { paths } from '@/app/paths'
import { useAuth } from '@/auth/authContext'
import { CardSkeletonGrid, EmptyBlock, ErrorState } from '@/components/common/PageState'
import SectionHeading from '@/components/common/SectionHeading'
import ProductCard from '@/components/product/ProductCard'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'
import { useAsync } from '@/hooks/useAsync'
import { SITE_DESCRIPTION, SITE_NAME, SITE_TAGLINE } from '@/lib/site'

const HIGHLIGHTS = [
  { value: '6 种', label: '计费周期', hint: '月付 / 季付 / 半年付 / 年付 / 两年付 / 三年付' },
  { value: '自动', label: '开通交付', hint: '支付成功后自动向上游开通，无需等待人工' },
  { value: '在线', label: '支付充值', hint: '支持支付宝、微信与余额支付' },
  { value: '7×24', label: '工单支持', hint: '会员工单直达客服与管理员' },
]

const FEATURES = [
  {
    icon: ZapIcon,
    title: '即时开通',
    description: '支付到账后自动向上游提交开通请求，实例落库即可在会员区查看。',
  },
  {
    icon: ShieldCheckIcon,
    title: '稳定可靠',
    description: '多区域机房与优质线路，库存与状态实时同步，避免超卖。',
  },
  {
    icon: WalletIcon,
    title: '灵活计费',
    description: '六档计费周期自由选择，支持优惠码抵扣与余额支付，账目清晰可查。',
  },
  {
    icon: LifeBuoyIcon,
    title: '专属支持',
    description: '工单系统与到期提醒通知，续费、重装、开关机在会员区自助完成。',
  },
]

const PURCHASE_STEPS = [
  { title: '选择商品', description: '按分组浏览已上架商品，查看配置与六周期价格。' },
  { title: '确认配置', description: '选择计费周期与配置项，可使用优惠码抵扣。' },
  { title: '支付订单', description: '在线支付（支付宝 / 微信）或余额支付，即时到账。' },
  { title: '自动开通', description: '系统自动向上游开通并发送站内通知。' },
]

const FAQS = [
  {
    question: '下单后多久可以开通？',
    answer:
      '支付到账后系统会立即向上游提交开通请求，通常几十秒内完成；开通结果会通过站内通知与订单状态同步展示。',
  },
  {
    question: '支持哪些支付方式？',
    answer: '支持易支付在线支付（支付宝、微信）与账户余额支付；余额可通过在线充值获得。',
  },
  {
    question: '优惠码怎么使用？',
    answer:
      '在确认订单页输入优惠码并点击「校验」，校验通过后金额会自动抵扣；优惠码按计费周期限定适用范围。',
  },
  {
    question: '价格包含哪些费用？',
    answer: '页面展示的价格为本地售价（已含定价规则），最终以提交订单时的应付金额为准。',
  },
]

/** 官网首页：Hero + 卖点 + 热门商品（真实数据）+ 购买流程 + FAQ。 */
export default function Home() {
  const navigate = useNavigate()
  const { isAuthenticated } = useAuth()
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
      <section className="border-b border-border bg-gradient-to-b from-primary/5 via-background to-background">
        <div className="mx-auto max-w-6xl px-4 py-16 sm:px-6 sm:py-24">
          <Badge variant="secondary" className="bg-primary/10 text-primary">
            {SITE_TAGLINE}
          </Badge>
          <h1 className="mt-5 text-3xl font-semibold tracking-tight text-foreground sm:text-5xl">
            {SITE_NAME}
          </h1>
          <p className="mt-4 max-w-2xl text-base text-muted-foreground sm:text-lg">
            {SITE_DESCRIPTION}
          </p>

          <div className="mt-8 flex flex-wrap gap-3">
            <Button size="lg" className="h-11 px-6 text-base" onClick={() => navigate(paths.products)}>
              浏览商品
              <ChevronRightIcon data-icon="inline-end" aria-hidden />
            </Button>
            <Button
              size="lg"
              variant="outline"
              className="h-11 px-6 text-base"
              onClick={() => navigate(isAuthenticated ? '/console' : paths.register)}
            >
              {isAuthenticated ? '进入会员区' : '免费注册'}
            </Button>
          </div>

          <dl className="mt-12 grid grid-cols-2 gap-6 sm:grid-cols-4">
            {HIGHLIGHTS.map((item) => (
              <div key={item.label}>
                <dt className="text-2xl font-semibold text-foreground">{item.value}</dt>
                <dd className="mt-1 text-sm text-muted-foreground">{item.label}</dd>
                <dd className="mt-0.5 hidden text-xs text-muted-foreground/80 sm:block">{item.hint}</dd>
              </div>
            ))}
          </dl>
        </div>
      </section>

      <section className="mx-auto max-w-6xl px-4 py-14 sm:px-6">
        <SectionHeading
          title="热门商品"
          description="已上架商品按分组展示，价格与库存实时同步"
          action={
            <Button variant="ghost" size="sm" onClick={() => navigate(paths.products)}>
              查看全部商品
              <ChevronRightIcon data-icon="inline-end" aria-hidden />
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

      <section className="border-t border-border bg-muted/30">
        <div className="mx-auto max-w-6xl px-4 py-14 sm:px-6">
          <SectionHeading title="为什么选择我们" description="从下单到运维，全流程在线自助" />
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            {FEATURES.map(({ icon: Icon, title, description }) => (
              <Card key={title} className="h-full">
                <CardHeader>
                  <span className="grid size-10 place-items-center rounded-lg bg-primary/10 text-primary">
                    <Icon className="size-5" aria-hidden />
                  </span>
                  <CardTitle className="mt-3 text-base">{title}</CardTitle>
                </CardHeader>
                <CardContent>
                  <p className="text-sm text-muted-foreground">{description}</p>
                </CardContent>
              </Card>
            ))}
          </div>
        </div>
      </section>

      <section className="mx-auto max-w-6xl px-4 py-14 sm:px-6">
        <SectionHeading title="购买流程" description="四步完成选购与开通" />
        <ol className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {PURCHASE_STEPS.map((step, index) => (
            <li key={step.title} className="rounded-xl border border-border bg-card p-5">
              <span className="grid size-8 place-items-center rounded-full bg-primary/10 text-sm font-semibold text-primary">
                {index + 1}
              </span>
              <p className="mt-3 font-medium text-foreground">{step.title}</p>
              <p className="mt-1 text-sm text-muted-foreground">{step.description}</p>
            </li>
          ))}
        </ol>
        <div className="mt-6 flex flex-wrap items-center gap-4 text-sm text-muted-foreground">
          <span className="inline-flex items-center gap-1.5">
            <CreditCardIcon className="size-4" aria-hidden />
            支付宝 / 微信 / 余额支付
          </span>
          <span className="inline-flex items-center gap-1.5">
            <RocketIcon className="size-4" aria-hidden />
            支付后自动开通
          </span>
          <span className="inline-flex items-center gap-1.5">
            <BadgeCheckIcon className="size-4" aria-hidden />
            订单与资源状态全程可查
          </span>
        </div>
      </section>

      <section className="border-t border-border bg-muted/30">
        <div className="mx-auto max-w-3xl px-4 py-14 sm:px-6">
          <SectionHeading title="常见问题" description="还有疑问？注册后可提交工单咨询" />
          <Accordion type="single" collapsible className="w-full">
            {FAQS.map((faq, index) => (
              <AccordionItem key={faq.question} value={`faq-${index}`}>
                <AccordionTrigger className="text-left text-sm font-medium">
                  {faq.question}
                </AccordionTrigger>
                <AccordionContent className="text-sm text-muted-foreground">
                  {faq.answer}
                </AccordionContent>
              </AccordionItem>
            ))}
          </Accordion>
          <p className="mt-6 text-center text-sm text-muted-foreground">
            还没有账号？
            <Link className="ml-1 font-medium text-primary hover:underline" to={paths.register}>
              免费注册
            </Link>
            ，即可下单与提交工单。
          </p>
        </div>
      </section>
    </div>
  )
}
