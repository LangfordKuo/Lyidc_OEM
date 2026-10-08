import { Link } from 'react-router-dom'

import { paths } from '../../app/paths'
import { SITE_NAME, SITE_TAGLINE } from '../../lib/site'

// 版权年份在模块加载时取一次即可（避免在渲染期调用 Date）。
const CURRENT_YEAR = new Date().getFullYear()

// SiteFooter 是官网公共页脚：站点信息与常用入口。
export default function SiteFooter() {
  return (
    <footer className="mt-auto border-t border-border bg-surface-secondary/50">
      <div className="mx-auto grid max-w-6xl gap-8 px-4 py-10 sm:px-6 md:grid-cols-3">
        <div>
          <div className="flex items-center gap-2">
            <span className="grid size-7 place-items-center rounded-lg bg-accent text-xs font-semibold text-accent-foreground">
              岭
            </span>
            <span className="font-semibold text-foreground">{SITE_NAME}</span>
          </div>
          <p className="mt-3 text-sm text-muted">{SITE_TAGLINE}</p>
        </div>

        <div className="text-sm">
          <p className="font-medium text-foreground">产品</p>
          <ul className="mt-3 space-y-2 text-muted">
            <li>
              <Link className="hover:text-foreground" to={paths.products}>
                全部商品
              </Link>
            </li>
            <li>
              <Link className="hover:text-foreground" to={paths.console}>
                会员控制台
              </Link>
            </li>
          </ul>
        </div>

        <div className="text-sm">
          <p className="font-medium text-foreground">账户</p>
          <ul className="mt-3 space-y-2 text-muted">
            <li>
              <Link className="hover:text-foreground" to={paths.login}>
                登录
              </Link>
            </li>
            <li>
              <Link className="hover:text-foreground" to={paths.register}>
                注册
              </Link>
            </li>
            <li>
              <Link className="hover:text-foreground" to={paths.consoleTickets}>
                提交工单
              </Link>
            </li>
          </ul>
        </div>
      </div>

      <div className="border-t border-border px-4 py-4 text-center text-xs text-muted sm:px-6">
        © {CURRENT_YEAR} {SITE_NAME} · Lyidc_OEM
      </div>
    </footer>
  )
}
