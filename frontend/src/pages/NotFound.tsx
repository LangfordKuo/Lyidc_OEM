import { Link, useNavigate } from 'react-router-dom'
import { CompassIcon } from 'lucide-react'

import { paths } from '@/app/paths'
import { Button } from '@/components/ui/button'

/** 404 页：站点兜底路由（含会员区/管理后台等后续期入口的说明）。 */
export default function NotFound() {
  const navigate = useNavigate()

  return (
    <div className="mx-auto flex max-w-2xl flex-col items-center px-4 py-20 text-center sm:px-6">
      <span className="grid size-14 place-items-center rounded-full bg-muted text-muted-foreground">
        <CompassIcon className="size-7" aria-hidden />
      </span>
      <p className="mt-6 text-5xl font-semibold tracking-tight text-foreground">404</p>
      <h1 className="mt-3 text-xl font-semibold text-foreground">页面不存在或尚未开放</h1>
      <p className="mt-2 text-sm text-muted-foreground">
        您访问的页面可能已被移除，或属于尚未上线的功能（会员区与管理后台将在后续版本开放）。
      </p>
      <div className="mt-8 flex flex-wrap justify-center gap-3">
        <Button onClick={() => navigate(paths.home)}>返回首页</Button>
        <Button variant="outline" onClick={() => navigate(paths.products)}>
          浏览商品
        </Button>
      </div>
      <p className="mt-6 text-xs text-muted-foreground">
        也可以直接访问
        <Link className="mx-1 text-primary hover:underline" to={paths.products}>
          商品列表
        </Link>
        选购云服务器。
      </p>
    </div>
  )
}
