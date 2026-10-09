import { Link } from 'react-router-dom'
import { ServerIcon } from 'lucide-react'

import { paths } from '@/app/paths'
import { SITE_NAME, SITE_TAGLINE } from '@/lib/site'

// 版权年份在模块加载时取一次即可（避免在渲染期调用 Date）。
const CURRENT_YEAR = new Date().getFullYear()

// 页脚分组导航：全部指向真实路由，不放站内锚点，避免出现「点了没反应」的死链。
const FOOTER_GROUPS = [
  {
    title: '产品',
    links: [
      { to: paths.products, label: '全部商品' },
      { to: paths.consoleServers, label: '我的服务器' },
    ],
  },
  {
    title: '服务保障',
    links: [
      { to: paths.consoleTickets, label: '提交工单' },
      { to: paths.console, label: '会员中心' },
    ],
  },
  {
    title: '账户',
    links: [
      { to: paths.login, label: '登录' },
      { to: paths.register, label: '免费注册' },
    ],
  },
] as const

/** 官网公共页脚：深蓝纯色底（与首页 Hero 呼应）+ 分组导航 + 版权与备案位。 */
export default function SiteFooter() {
  return (
    <footer className="mt-auto bg-brand-deep text-white">
      <div className="mx-auto grid max-w-6xl gap-10 px-4 py-12 sm:px-6 md:grid-cols-[1.5fr_1fr_1fr_1fr]">
        <div>
          <p className="flex items-center gap-2">
            <span className="grid size-8 place-items-center rounded-lg bg-white/10">
              <ServerIcon className="size-4.5" aria-hidden />
            </span>
            <span className="text-base font-semibold">{SITE_NAME}</span>
          </p>
          <p className="mt-4 max-w-xs text-sm leading-relaxed text-blue-100/80">{SITE_TAGLINE}</p>
          <p className="mt-3 text-xs text-blue-100/60">支付后自动开通 · 7×24 工单支持</p>
        </div>

        {FOOTER_GROUPS.map((group) => (
          <div key={group.title} className="text-sm">
            <p className="font-medium text-white">{group.title}</p>
            <ul className="mt-3 space-y-2">
              {group.links.map((link) => (
                <li key={link.label}>
                  <Link className="text-blue-100/80 transition-colors hover:text-white" to={link.to}>
                    {link.label}
                  </Link>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>

      <div className="border-t border-white/10 px-4 py-5 sm:px-6">
        <div className="mx-auto flex max-w-6xl flex-col items-center justify-between gap-2 text-xs text-blue-100/70 sm:flex-row">
          <p>
            © {CURRENT_YEAR} {SITE_NAME} · Lyidc_OEM
          </p>
          {/* 备案位占位：待主体信息补充后替换为真实备案号。 */}
          <p>ICP 备案号：待备案 · 公安备案号：待备案</p>
        </div>
      </div>
    </footer>
  )
}
