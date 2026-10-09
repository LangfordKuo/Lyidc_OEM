import { useNavigate } from 'react-router-dom'
import { ShieldAlertIcon } from 'lucide-react'

import { paths } from '@/app/paths'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import type { AdminPermission } from '@/lib/adminRoles'
import { permissionHint, permissionLabel } from '@/lib/adminRoles'

/**
 * NoPermission 是整页无权限时的统一占位（如 finance 进入工单页、finance/support 进入设置页）。
 * 与「隐藏入口 + 服务端 403」配套：直接敲 URL 也不会看到空白页，也不会发起无权限请求。
 */
export default function NoPermission({
  permission,
  title = '无权访问该页面',
}: {
  permission: AdminPermission
  title?: string
}) {
  const navigate = useNavigate()

  return (
    <div className="space-y-4">
      <Alert className="border-warning/40 bg-warning/5">
        <ShieldAlertIcon className="text-warning" aria-hidden />
        <AlertTitle>{title}</AlertTitle>
        <AlertDescription>
          当前角色无权{permissionLabel(permission)}（{permissionHint(permission)}）。
          如确需该权限，请联系超级管理员调整账号角色。
        </AlertDescription>
      </Alert>
      <div className="flex gap-2">
        <Button size="sm" onClick={() => navigate(paths.admin)}>
          返回仪表盘
        </Button>
        <Button size="sm" variant="outline" onClick={() => navigate(paths.home)}>
          回到官网
        </Button>
      </div>
    </div>
  )
}
