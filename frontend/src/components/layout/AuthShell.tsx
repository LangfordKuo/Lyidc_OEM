import { Card } from '@heroui/react'
import type { ReactNode } from 'react'

import { SITE_NAME } from '../../lib/site'

// AuthShell 是登录/注册页的居中卡片外壳。
export default function AuthShell({
  title,
  subtitle,
  children,
  footer,
}: {
  title: string
  subtitle?: ReactNode
  children: ReactNode
  footer?: ReactNode
}) {
  return (
    <div className="mx-auto flex w-full max-w-md flex-col justify-center px-4 py-12 sm:py-16">
      <div className="mb-6 text-center">
        <span className="mx-auto grid size-11 place-items-center rounded-xl bg-accent text-base font-semibold text-accent-foreground">
          岭
        </span>
        <h1 className="mt-4 text-xl font-semibold text-foreground">{title}</h1>
        <p className="mt-1 text-sm text-muted">{subtitle ?? `${SITE_NAME} 会员账号`}</p>
      </div>

      <Card>
        <Card.Content className="space-y-5">{children}</Card.Content>
        {footer ? <Card.Footer className="justify-center text-sm text-muted">{footer}</Card.Footer> : null}
      </Card>
    </div>
  )
}
