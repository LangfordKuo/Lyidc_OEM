import { Button } from '@heroui/react'
import { useNavigate } from 'react-router-dom'

import { paths } from '../../app/paths'
import ConsolePlaceholder from '../../components/layout/ConsolePlaceholder'

// 会员区「我的订单」占位页（阶段 7b 填充订单列表与详情）。
export default function ConsoleOrders() {
  const navigate = useNavigate()

  return (
    <ConsolePlaceholder
      title="我的订单"
      description="订单列表、状态筛选、继续支付与取消将在阶段 7b 接入（接口：GET /orders、GET /orders/:id、POST /orders/:id/pay、POST /orders/:id/cancel）。"
      planned={[
        '订单列表：按状态筛选（待支付 / 已支付 / 开通中 / 已开通 / 失败 / 已取消）',
        '订单详情：配置项快照、金额明细、支付与交付时间线',
        '待支付订单：继续支付、取消订单',
        '交付失败订单：查看失败原因并提交工单',
      ]}
      extra={
        <div className="flex flex-wrap gap-3">
          <Button variant="primary" size="sm" onPress={() => navigate(paths.products)}>
            去选购商品
          </Button>
          <Button variant="outline" size="sm" onPress={() => navigate(paths.console)}>
            返回概览
          </Button>
        </div>
      }
    />
  )
}
