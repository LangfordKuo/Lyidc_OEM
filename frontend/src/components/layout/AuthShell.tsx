import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { ServerIcon } from 'lucide-react'

import { paths } from '@/app/paths'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { SITE_NAME } from '@/lib/site'

/** 认证页（登录/注册）的居中卡片外壳；badge 用于标注「管理后台」等场景。 */
export default function AuthShell({
  title,
  description,
  badge,
  footer,
  children,
}: {
  title: string
  description?: string
  badge?: string
  footer?: ReactNode
  children: ReactNode
}) {
  return (
    <div className="mx-auto flex w-full max-w-md flex-col justify-center px-4 py-14 sm:py-20">
      <Link
        to={paths.home}
        className="mx-auto flex items-center gap-2 text-foreground"
        aria-label={SITE_NAME}
      >
        <span className="grid size-9 place-items-center rounded-xl bg-primary text-primary-foreground">
          <ServerIcon className="size-5" aria-hidden />
        </span>
        <span className="text-lg font-semibold">{SITE_NAME}</span>
        {badge ? (
          <span className="rounded-full border border-border bg-muted px-2 py-0.5 text-xs text-muted-foreground">
            {badge}
          </span>
        ) : null}
      </Link>

      <Card className="mt-6">
        <CardHeader>
          <CardTitle className="text-xl">{title}</CardTitle>
          {description ? <CardDescription>{description}</CardDescription> : null}
        </CardHeader>
        <CardContent>{children}</CardContent>
      </Card>

      {footer ? <div className="mt-5 text-center text-sm text-muted-foreground">{footer}</div> : null}
    </div>
  )
}
