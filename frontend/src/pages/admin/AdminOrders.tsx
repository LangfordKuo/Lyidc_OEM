import { Button, Input, Label, TextField } from '@heroui/react'
import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { listAdminOrders } from '../../api/adminOrders'
import type { AdminOrder, OrderStatus, OrderType } from '../../api/types'
import { paths } from '../../app/paths'
import StatusBadge from '../../components/StatusBadge'
import AdminTable, { type AdminColumn } from '../../components/admin/AdminTable'
import FilterSelect, { AdminFilterBar } from '../../components/admin/FilterSelect'
import Pager from '../../components/common/Pager'
import { EmptyBlock, ErrorState, LoadingBlock } from '../../components/common/PageState'
import { useAsync } from '../../hooks/useAsync'
import { formatCycleLabel, formatDateTime, formatMoney } from '../../lib/format'
import { ORDER_STATUS_TONES, orderStatusLabel } from '../../lib/orderStatus'
import { DEFAULT_PAGE_SIZE } from '../../lib/pagination'

type StatusFilterValue = '' | OrderStatus
type TypeFilterValue = '' | OrderType

const STATUS_OPTIONS: { value: StatusFilterValue; label: string }[] = [
  { value: '', label: '全部状态' },
  { value: 'pending', label: '待支付' },
  { value: 'paid', label: '已支付' },
  { value: 'provisioning', label: '开通中' },
  { value: 'active', label: '已开通' },
  { value: 'failed', label: '交付失败' },
  { value: 'cancelled', label: '已取消' },
]

const TYPE_OPTIONS: { value: TypeFilterValue; label: string }[] = [
  { value: '', label: '全部类型' },
  { value: 'new', label: '新购' },
  { value: 'renew', label: '续费' },
]

// AdminOrders 是管理后台「订单」列表页（契约 12.6）：全站订单 + 状态/类型/会员/单号筛选。
// 查看类接口对三角色开放（客服协助会员查询是日常）；重试交付仅 admin，在详情页执行。
export default function AdminOrders() {
  const navigate = useNavigate()
  const [status, setStatus] = useState<StatusFilterValue>('')
  const [orderType, setOrderType] = useState<TypeFilterValue>('')
  const [memberID, setMemberID] = useState('')
  const [appliedMemberID, setAppliedMemberID] = useState('')
  const [tradeNo, setTradeNo] = useState('')
  const [appliedTradeNo, setAppliedTradeNo] = useState('')
  const [fieldError, setFieldError] = useState('')
  const [page, setPage] = useState(1)

  const ordersState = useAsync(
    () =>
      listAdminOrders({
        page,
        page_size: DEFAULT_PAGE_SIZE,
        ...(status ? { status } : {}),
        ...(orderType ? { type: orderType } : {}),
        ...(appliedMemberID ? { member_id: Number(appliedMemberID) } : {}),
        ...(appliedTradeNo ? { trade_no: appliedTradeNo } : {}),
      }),
    [page, status, orderType, appliedMemberID, appliedTradeNo],
  )
  const orders = ordersState.data?.items ?? []

  // 两个文本筛选用一个「查询」按钮统一提交（会员 ID 先做正整数校验）。
  const applyFilters = () => {
    const trimmedMember = memberID.trim()
    if (trimmedMember && (!/^\d+$/.test(trimmedMember) || Number(trimmedMember) <= 0)) {
      setFieldError('会员 ID 必须为正整数')
      return
    }
    setFieldError('')
    setAppliedMemberID(trimmedMember)
    setAppliedTradeNo(tradeNo.trim())
    setPage(1)
  }

  const resetFilters = () => {
    setStatus('')
    setOrderType('')
    setMemberID('')
    setAppliedMemberID('')
    setTradeNo('')
    setAppliedTradeNo('')
    setFieldError('')
    setPage(1)
  }

  const hasFilters = Boolean(status || orderType || appliedMemberID || appliedTradeNo)

  const columns: AdminColumn<AdminOrder>[] = [
    {
      key: 'trade_no',
      header: '订单号',
      cell: (order) => (
        <Link
          className="font-mono text-xs text-foreground hover:text-accent"
          to={paths.adminOrderDetail(order.id)}
        >
          {order.trade_no}
        </Link>
      ),
    },
    {
      key: 'member',
      header: '会员',
      cell: (order) => (
        <span className="text-sm">
          {order.member ? (
            <>
              <span className="text-foreground">{order.member.username}</span>
              <span className="ml-1 text-xs text-muted">#{order.member.id}</span>
            </>
          ) : (
            <span className="text-muted">#{order.member_id}（资料缺失）</span>
          )}
        </span>
      ),
    },
    {
      key: 'product',
      header: '商品',
      cell: (order) => (
        <span className="text-sm">
          {order.product_name || `#${order.product_id}`}
          <span className="ml-1 text-xs text-muted">{formatCycleLabel(order.cycle)}</span>
        </span>
      ),
    },
    {
      key: 'amount',
      header: '金额',
      cell: (order) => <span className="text-sm">{formatMoney(order.final_amount)}</span>,
    },
    {
      key: 'type',
      header: '类型',
      cell: (order) => (
        <span className="text-xs text-muted">{order.type === 'renew' ? '续费' : '新购'}</span>
      ),
    },
    {
      key: 'status',
      header: '状态',
      cell: (order) => (
        <StatusBadge tone={ORDER_STATUS_TONES[order.status] ?? 'pending'} label={orderStatusLabel(order.status)} />
      ),
    },
    {
      key: 'created_at',
      header: '下单时间',
      cell: (order) => <span className="text-xs text-muted">{formatDateTime(order.created_at)}</span>,
    },
    {
      key: 'actions',
      header: '操作',
      cell: (order) => (
        <Button size="sm" variant="outline" onPress={() => navigate(paths.adminOrderDetail(order.id))}>
          详情
        </Button>
      ),
    },
  ]

  return (
    <div className="space-y-5">
      <header>
        <h1 className="text-xl font-semibold text-foreground">订单</h1>
        <p className="mt-1 text-sm text-muted">
          全站订单列表（按状态/类型/会员/单号筛选）。详情页可查看交付信息；
          交付失败或未触发交付的订单可由超级管理员重试交付。
        </p>
      </header>

      <AdminFilterBar
        actions={
          <>
            <Button size="sm" variant="outline" onPress={ordersState.reload}>
              刷新
            </Button>
            {hasFilters ? (
              <Button size="sm" variant="ghost" onPress={resetFilters}>
                重置筛选
              </Button>
            ) : null}
          </>
        }
      >
        <FilterSelect
          label="状态"
          value={status}
          options={STATUS_OPTIONS}
          onChange={(next) => {
            setStatus(next)
            setPage(1)
          }}
        />
        <FilterSelect
          label="类型"
          value={orderType}
          options={TYPE_OPTIONS}
          onChange={(next) => {
            setOrderType(next)
            setPage(1)
          }}
        />
        <TextField
          name="member_id"
          type="text"
          value={memberID}
          onChange={(value) => {
            setMemberID(value)
            setFieldError('')
          }}
          isInvalid={Boolean(fieldError)}
          className="w-36"
        >
          <Label>会员 ID</Label>
          <Input placeholder="全部会员" inputMode="numeric" />
        </TextField>
        <TextField name="trade_no" type="text" value={tradeNo} onChange={setTradeNo} className="w-56">
          <Label>订单号</Label>
          <Input placeholder="支持部分匹配，如 O20261008" />
        </TextField>
        <Button size="sm" variant="outline" onPress={applyFilters}>
          查询
        </Button>
      </AdminFilterBar>
      {fieldError ? <p className="text-xs text-danger">{fieldError}</p> : null}

      {ordersState.loading ? <LoadingBlock label="正在读取订单…" /> : null}
      {ordersState.error ? <ErrorState message={ordersState.error} onRetry={ordersState.reload} /> : null}

      {!ordersState.loading && !ordersState.error ? (
        <>
          <AdminTable
            ariaLabel="订单列表"
            columns={columns}
            rows={orders}
            rowKey={(order) => order.id}
            empty={
              <EmptyBlock
                title={hasFilters ? '当前筛选下没有订单' : '还没有订单'}
                description={
                  hasFilters
                    ? '换个状态、类型、会员或单号再试。'
                    : '会员下单后，订单会出现在这里。'
                }
              />
            }
          />
          {ordersState.data && ordersState.data.total > 0 ? (
            <Pager
              page={page}
              total={ordersState.data.total}
              pageSize={ordersState.data.page_size || DEFAULT_PAGE_SIZE}
              onChange={setPage}
            />
          ) : null}
        </>
      ) : null}
    </div>
  )
}
