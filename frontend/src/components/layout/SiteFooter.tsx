import { Link } from 'react-router-dom'

import { paths } from '@/app/paths'
import { SITE_NAME, SITE_TAGLINE } from '@/lib/site'

// 版权年份在模块加载时取一次即可（避免在渲染期调用 Date）。
const CURRENT_YEAR = new Date().getFullYear()

/** 官网公共页脚：站点信息与常用入口。 */
export default function SiteFooter() {
  return (
    <footer className="mt-auto border-t border-border bg-muted/40">
      <div className="mx-auto grid max-w-6xl gap-8 px-4 py-10 sm:px-6 md:grid-cols-3">
        <div>
          <p className="font-semibold text-foreground">{SITE_NAME}</p>
          <p className="mt-3 text-sm text-muted-foreground">{SITE_TAGLINE}</p>
        </div>

        <div className="text-sm">
          <p className="font-medium text-foreground">产品</p>
          <ul className="mt-3 space-y-2 text-muted-foreground">
            <li>
              <Link className="hover:text-foreground" to={paths.products}>
                全部商品
              </Link>
            </li>
            <li>
              <Link className="hover:text-foreground" to={paths.home}>
                服务保障
              </Link>
            </li>
          </ul>
        </div>

        <div className="text-sm">
          <p className="font-medium text-foreground">账户</p>
          <ul className="mt-3 space-y-2 text-muted-foreground">
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
          </ul>
        </div>
      </div>

      <div className="border-t border-border px-4 py-4 text-center text-xs text-muted-foreground sm:px-6">
        © {CURRENT_YEAR} {SITE_NAME} · Lyidc_OEM
      </div>
    </footer>
  )
}
