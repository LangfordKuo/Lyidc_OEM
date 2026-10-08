import { Alert, Button } from '@heroui/react'
import { useNavigate } from 'react-router-dom'

import { paths } from '../../app/paths'
import type { AdminPermission } from '../../lib/adminRoles'
import { permissionHint, permissionLabel } from '../../lib/adminRoles'

// NoPermission 是整页无权限时的统一占位（如 finance 进入工单页、finance/support 进入设置页）。
// 与「隐藏入口 + 服务端 403」配套：直接敲 URL 也不会看到空白页。
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
      <Alert status="warning">
        <Alert.Indicator />
        <Alert.Content>
          <Alert.Title>{title}</Alert.Title>
          <Alert.Description>
            当前角色无权{permissionLabel(permission)}（{permissionHint(permission)}）。
            如确需该权限，请联系超级管理员调整账号角色。
          </Alert.Description>
        </Alert.Content>
      </Alert>
      <div className="flex gap-2">
        <Button variant="primary" size="sm" onPress={() => navigate(paths.admin)}>
          返回仪表盘
        </Button>
        <Button variant="outline" size="sm" onPress={() => navigate(paths.home)}>
          回到官网
        </Button>
      </div>
    </div>
  )
}
