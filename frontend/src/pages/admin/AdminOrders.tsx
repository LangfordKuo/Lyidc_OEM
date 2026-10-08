import { Alert, Button, Card, Input, Label, TextField } from '@heroui/react'
import { useState } from 'react'
import { Link } from 'react-router-dom'

import { errorMessage } from '../../api/client'
import { retryOrderDelivery } from '../../api/adminOrders'
import type { Order } from '../../api/types'
import { paths } from '../../app/paths'
import { useAdminAuth } from '../../auth/adminAuthContext'
import StatusBadge from '../../components/StatusBadge'
import ConfirmDialog from '../../components/common/ConfirmDialog'
import { InfoList } from '../../components/common/InfoList'
import { permissionHint } from '../../lib/adminRoles'
import { formatCycleLabel, formatDateTimeOr, formatMoney } from '../../lib/format'
import { ORDER_STATUS_DESCRIPTIONS, ORDER_STATUS_TONES, orderStatusLabel } from '../../lib/orderStatus'

// AdminOrders 是管理后台「订单」页：**交付处置工作台**。
//
// 现状（契约 14.4）：管理端**只有重试交付**一个订单接口——
// `GET /admin/orders`（列表）与 `GET /admin/orders/:id`（详情）在契约中不存在，
// 因此本页不伪造订单列表：只能按订单 ID 执行重试交付，并把返回的订单对象展示出来。
// 缺口已列入阶段 8 交付报告；订单 ID 可从会员详情的流水（订单支付）或实例列表的 order_id 找到。
export default function AdminOrders() {
  const { role } = useAdminAuth()
  const allowed = role === 'admin'

  const [orderId, setOrderId] = useState('')
  const [fieldError, setFieldError] = useState('')
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const [order, setOrder] = useState<Order | null>(null)

  const handleSubmit = () => {
    const trimmed = orderId.trim()
    if (!/^\d+$/.test(trimmed) || Number(trimmed) <= 0) {
      setFieldError('订单 ID 必须为正整数')
      return
    }
    setFieldError('')
    setError('')
    setConfirmOpen(true)
  }

  const handleConfirm = async () => {
    setPending(true)
    setError('')
    try {
      const result = await retryOrderDelivery(Number(orderId.trim()))
      setOrder(result)
      setConfirmOpen(false)
    } catch (err) {
      // 40002（订单尚未支付 / 正在交付中 / 已交付完成 / 已取消）、404（订单不存在）等
      // 由后端给出可直接展示的中文 message。
      setError(errorMessage(err, '重试交付失败，请稍后重试'))
      setConfirmOpen(false)
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="space-y-5">
      <header>
        <h1 className="text-xl font-semibold text-foreground">订单</h1>
        <p className="mt-1 text-sm text-muted">
          交付失败或未触发交付的订单可在此重试；重试会**同步调用上游**执行一次完整交付。
        </p>
      </header>

      <Alert status="warning">
        <Alert.Indicator />
        <Alert.Content>
          <Alert.Title>管理端订单列表接口暂缺</Alert.Title>
          <Alert.Description>
            契约（14.4）只定义了 `POST /api/v1/admin/orders/:id/retry-delivery`，没有管理端订单
            列表与详情接口，因此本页不做订单检索、也不展示「最近订单」。订单 ID 可从
            <Link className="mx-1 text-accent hover:underline" to={paths.adminMembers}>
              会员详情
            </Link>
            的余额流水（订单支付）或
            <Link className="mx-1 text-accent hover:underline" to={paths.adminInstances}>
              实例列表
            </Link>
            的 order_id 反查。该缺口已列入阶段 8 交付报告。
          </Alert.Description>
        </Alert.Content>
      </Alert>

      <Card>
        <Card.Header>
          <Card.Title className="text-base">重试交付</Card.Title>
          <Card.Description>
            仅 `paid`（未触发过交付）与 `failed`（交付失败）可重试；
            `pending` / `provisioning` / `active` / `cancelled` 会被拒绝（40002）。
          </Card.Description>
        </Card.Header>
        <Card.Content className="space-y-4">
          <div className="flex flex-wrap items-start gap-3">
            <TextField
              name="order_id"
              type="text"
              value={orderId}
              onChange={(value) => {
                setOrderId(value)
                setFieldError('')
              }}
              isInvalid={Boolean(fieldError)}
              isDisabled={!allowed}
              className="w-full sm:w-64"
            >
              <Label>订单 ID</Label>
              <Input placeholder="例如 12（本地订单 ID，非订单号）" inputMode="numeric" />
              {fieldError ? <p className="mt-1 text-xs text-danger">{fieldError}</p> : null}
            </TextField>

            <div className="flex flex-col gap-1 pt-1 sm:pt-6">
              <Button
                variant="primary"
                isDisabled={!allowed || pending || !orderId.trim()}
                onPress={handleSubmit}
              >
                {pending ? '正在交付…' : '重试交付'}
              </Button>
              {!allowed ? (
                <span className="text-xs text-muted">
                  {permissionHint('orders.retry')}（当前角色不可执行）
                </span>
              ) : null}
            </div>
          </div>

          {error ? (
            <Alert status="danger">
              <Alert.Indicator />
              <Alert.Content>
                <Alert.Title>重试交付未执行</Alert.Title>
                <Alert.Description>{error}</Alert.Description>
              </Alert.Content>
            </Alert>
          ) : null}
        </Card.Content>
      </Card>

      {order ? (
        <Card>
          <Card.Header>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <Card.Title className="text-base">最近一次重试结果</Card.Title>
                <Card.Description>同步执行的交付结果（上游回读后落库的最新订单）。</Card.Description>
              </div>
              <StatusBadge
                tone={ORDER_STATUS_TONES[order.status] ?? 'pending'}
                label={orderStatusLabel(order.status)}
              />
            </div>
          </Card.Header>
          <Card.Content className="space-y-4">
            <p className="text-sm text-muted">{ORDER_STATUS_DESCRIPTIONS[order.status] ?? ''}</p>

            {order.status === 'failed' && order.provision_error ? (
              <Alert status="danger">
                <Alert.Indicator />
                <Alert.Content>
                  <Alert.Title>交付失败原因（已脱敏）</Alert.Title>
                  <Alert.Description>{order.provision_error}</Alert.Description>
                </Alert.Content>
              </Alert>
            ) : null}

            <InfoList
              columns={2}
              items={[
                { label: '订单 ID', value: order.id },
                { label: '订单号', value: <span className="font-mono">{order.trade_no}</span> },
                { label: '会员', value: `#${order.member_id}` },
                { label: '类型', value: order.type === 'renew' ? '续费' : '新购' },
                { label: '商品', value: order.product_name || `#${order.product_id}` },
                { label: '周期', value: formatCycleLabel(order.cycle) },
                { label: '订单金额', value: formatMoney(order.final_amount) },
                { label: '支付时间', value: formatDateTimeOr(order.pay_time) },
                { label: '上游主机 ID', value: order.host_id ?? '—' },
                { label: '交付时间', value: formatDateTimeOr(order.delivered_at) },
                {
                  label: '关联实例',
                  value: order.instance_id ? (
                    <Link
                      className="text-accent hover:underline"
                      to={paths.adminInstanceDetail(order.instance_id)}
                    >
                      #{order.instance_id}
                    </Link>
                  ) : (
                    '—'
                  ),
                },
                { label: '优惠码', value: order.coupon_code || '—' },
              ]}
            />
          </Card.Content>
        </Card>
      ) : null}

      <ConfirmDialog
        isOpen={confirmOpen}
        title="确认重试交付？"
        description="将立即向上游提交一次开通请求（可能真实扣费），并回读结果写入订单。执行期间请勿关闭页面。"
        confirmLabel="开始重试交付"
        pending={pending}
        onCancel={() => setConfirmOpen(false)}
        onConfirm={handleConfirm}
      />
    </div>
  )
}
