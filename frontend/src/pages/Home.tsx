import { useMemo } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import {
  BadgeCheckIcon,
  CalendarClockIcon,
  ChevronRightIcon,
  CreditCardIcon,
  LifeBuoyIcon,
  PackageIcon,
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
import { SITE_DESCRIPTION, SITE_TAGLINE } from '@/lib/site'

// 卖点：图标统一走浅蓝圆角方块（bg-accent + text-primary），文案量化到具体数字。
const FEATURES = [
  {
    icon: ZapIcon,
    title: '即时开通',
    description: '支付到账后自动向上游提交开通，通常 1 分钟内完成，实例直接落到会员区。',
  },
  {
    icon: ShieldCheckIcon,
    title: '稳定可靠',
    description: '香港与内地多区域机房、CN2 优质线路，库存与状态实时同步，避免超卖。',
  },
  {
    icon: WalletIcon,
    title: '灵活计费',
    description: '月付到三年付六档周期自由选择，支持优惠码抵扣与余额支付，账目逐条可查。',
  },
  {
    icon: LifeBuoyIcon,
    title: '专属支持',
    description: '7×24 工单直达客服，到期前自动提醒，续费 / 重装 / 开关机在会员区自助完成。',
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

/** 官网首页：深蓝 Hero + 信任数据条 + 卖点 + 热门商品（真实数据）+ 购买流程 + FAQ。 */
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

  // 信任数据条：商品数为接口返回的真实在售总数，其余为产品能力与承诺。
  const trustStats = [
    {
      icon: PackageIcon,
      value: data ? String(data.total) : '—',
      label: '在售商品',
      hint: data ? '已上架可直接下单' : '正在读取商品数',
    },
    { icon: CalendarClockIcon, value: '6 种', label: '计费周期', hint: '月付到三年付' },
    { icon: ZapIcon, value: '秒级', label: '自动开通', hint: '支付后免人工等待' },
    { icon: LifeBuoyIcon, value: '7×24', label: '工单支持', hint: '客服与管理员直达' },
  ]

  return (
    <div>
      {/* Hero：深蓝纯色底（无渐变）+ 白字，左侧价值主张 + 双 CTA + 信任数据条。 */}
      <section className="bg-brand-deep text-white">
        <div className="mx-auto max-w-6xl px-4 py-18 sm:px-6 sm:py-24">
          <Badge className="border-transparent bg-white/10 text-blue-50">
            <ShieldCheckIcon aria-hidden />
            {SITE_TAGLINE}
          </Badge>

          <h1 className="mt-6 max-w-4xl text-[2.25rem] leading-[1.2] font-semibold tracking-tight text-white sm:text-[2.75rem]">
            稳定可靠的云服务器 · 支付后秒级自动开通
          </h1>
          <p className="mt-5 max-w-2xl text-base leading-relaxed text-blue-100/85 sm:text-lg">
            {SITE_DESCRIPTION}
          </p>

          <div className="mt-9 flex flex-wrap gap-3">
            <Button
              size="lg"
              className="h-11 bg-white px-6 text-base text-brand-blue hover:bg-brand-soft active:bg-brand-soft"
              onClick={() => navigate(paths.products)}
            >
              浏览商品
              <ChevronRightIcon data-icon="inline-end" aria-hidden />
            </Button>
            <Button
              size="lg"
              variant="outline"
              className="h-11 border-white/40 bg-transparent px-6 text-base text-white hover:bg-white/10 hover:text-white"
              onClick={() => navigate(isAuthenticated ? '/console' : paths.register)}
            >
              {isAuthenticated ? '进入会员区' : '了解会员区'}
            </Button>
          </div>

          <dl className="mt-14 grid grid-cols-2 gap-x-6 gap-y-8 border-t border-white/10 pt-8 sm:grid-cols-4">
            {trustStats.map(({ icon: Icon, value, label, hint }) => (
              <div key={label}>
                <dt className="flex items-center gap-2 text-sm text-blue-100/80">
                  <Icon className="size-4" aria-hidden />
                  {label}
                </dt>
                <dd className="mt-2 text-3xl font-semibold tabular-nums text-white">{value}</dd>
                <dd className="mt-1 text-xs text-blue-100/60">{hint}</dd>
              </div>
            ))}
          </dl>
        </div>
      </section>

      <section className="mx-auto max-w-6xl px-4 py-16 sm:px-6 sm:py-20">
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

      <section className="border-y border-border bg-secondary">
        <div className="mx-auto max-w-6xl px-4 py-16 sm:px-6 sm:py-20">
          <SectionHeading title="为什么选择我们" description="从下单到运维，全流程在线自助" />
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            {FEATURES.map(({ icon: Icon, title, description }) => (
              <Card key={title} className="h-full">
                <CardHeader>
                  <span className="grid size-11 place-items-center rounded-xl bg-accent text-primary">
                    <Icon className="size-5" aria-hidden />
                  </span>
                  <CardTitle className="mt-3 text-lg">{title}</CardTitle>
                </CardHeader>
                <CardContent>
                  <p className="text-sm leading-relaxed text-muted-foreground">{description}</p>
                </CardContent>
              </Card>
            ))}
          </div>
        </div>
      </section>

      <section className="mx-auto max-w-6xl px-4 py-16 sm:px-6 sm:py-20">
        <SectionHeading title="购买流程" description="四步完成选购与开通" />
        <ol className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {PURCHASE_STEPS.map((step, index) => (
            <li key={step.title} className="rounded-xl bg-card p-5 ring-1 ring-border">
              <span className="grid size-8 place-items-center rounded-full bg-accent text-sm font-semibold text-primary tabular-nums">
                {index + 1}
              </span>
              <p className="mt-3 font-medium text-foreground">{step.title}</p>
              <p className="mt-1 text-sm text-muted-foreground">{step.description}</p>
            </li>
          ))}
        </ol>
        <div className="mt-8 flex flex-wrap items-center gap-x-6 gap-y-3 text-sm text-muted-foreground">
          <span className="inline-flex items-center gap-1.5">
            <CreditCardIcon className="size-4 text-primary" aria-hidden />
            支付宝 / 微信 / 余额支付
          </span>
          <span className="inline-flex items-center gap-1.5">
            <RocketIcon className="size-4 text-primary" aria-hidden />
            支付后自动开通
          </span>
          <span className="inline-flex items-center gap-1.5">
            <BadgeCheckIcon className="size-4 text-primary" aria-hidden />
            订单与资源状态全程可查
          </span>
        </div>
      </section>

      <section className="border-t border-border bg-secondary">
        <div className="mx-auto max-w-3xl px-4 py-16 sm:px-6 sm:py-20">
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
          <p className="mt-8 text-center text-sm text-muted-foreground">
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
