import { useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { listAdminOrders } from '@/api/adminOrders'
import type { AdminOrder, OrderStatus, OrderType } from '@/api/types'
import { paths } from '@/app/paths'
import AdminTable, { type AdminColumn } from '@/components/admin/AdminTable'
import FilterSelect, { AdminFilterBar } from '@/components/admin/FilterSelect'
import Pager from '@/components/common/Pager'
import { EmptyBlock, ErrorState, LoadingBlock } from '@/components/common/PageState'
import StatusBadge from '@/components/common/StatusBadge'
import StatusFilter from '@/components/common/StatusFilter'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useAsync } from '@/hooks/useAsync'
import { formatCycleLabel, formatDateTime, formatMoney } from '@/lib/format'
import { ORDER_STATUS_TONES, orderStatusLabel } from '@/lib/orderStatus'
import { ADMIN_PAGE_SIZE } from '@/lib/pagination'

type StatusFilterValue = '' | OrderStatus
type TypeFilterValue = '' | OrderType

const STATUS_OPTIONS: { value: OrderStatus; label: string }[] = [
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
  // 文本筛选用「草稿 + 已提交」两份状态：只有点「查询」才落到请求参数上。
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
        page_size: ADMIN_PAGE_SIZE,
        ...(status ? { status } : {}),
        ...(orderType ? { type: orderType } : {}),
        ...(appliedMemberID ? { member_id: Number(appliedMemberID) } : {}),
        ...(appliedTradeNo ? { trade_no: appliedTradeNo } : {}),
      }),
    [page, status, orderType, appliedMemberID, appliedTradeNo],
  )
  const orders = ordersState.data?.items ?? []

  // 两个文本筛选用一个「查询」按钮统一提交（会员 ID 先做正整数校验）。
  const applyFilters = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
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
          className="font-mono text-xs text-foreground hover:text-primary"
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
              <span className="ml-1 text-xs text-muted-foreground">#{order.member.id}</span>
            </>
          ) : (
            <span className="text-muted-foreground">#{order.member_id}（资料缺失）</span>
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
          <span className="ml-1 text-xs text-muted-foreground">{formatCycleLabel(order.cycle)}</span>
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
        <span className="text-xs text-muted-foreground">
          {order.type === 'renew' ? '续费' : '新购'}
        </span>
      ),
    },
    {
      key: 'status',
      header: '状态',
      cell: (order) => (
        <StatusBadge
          tone={ORDER_STATUS_TONES[order.status] ?? 'pending'}
          label={orderStatusLabel(order.status)}
        />
      ),
    },
    {
      key: 'created_at',
      header: '下单时间',
      cell: (order) => (
        <span className="text-xs text-muted-foreground">{formatDateTime(order.created_at)}</span>
      ),
    },
    {
      key: 'actions',
      header: '操作',
      className: 'whitespace-nowrap',
      cell: (order) => (
        <Button size="sm" variant="outline" onClick={() => navigate(paths.adminOrderDetail(order.id))}>
          详情
        </Button>
      ),
    },
  ]

  return (
    <div className="space-y-5">
      <header>
        <h1 className="text-xl font-semibold text-foreground">订单</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          全站订单列表（按状态/类型/会员/单号筛选）。详情页可查看交付信息；
          交付失败或未触发交付的订单可由超级管理员重试交付。
        </p>
      </header>

      <form onSubmit={applyFilters} noValidate>
        <AdminFilterBar
          actions={
            <>
              <Button size="sm" variant="outline" onClick={ordersState.reload}>
                刷新
              </Button>
              {hasFilters ? (
                <Button size="sm" variant="ghost" type="button" onClick={resetFilters}>
                  重置筛选
                </Button>
              ) : null}
              <Button size="sm" type="submit">
                查询
              </Button>
            </>
          }
        >
          <div className="space-y-1">
            <Label htmlFor="order_member_id" className="text-xs text-muted-foreground">
              会员 ID
            </Label>
            <Input
              id="order_member_id"
              name="member_id"
              inputMode="numeric"
              className="w-36"
              placeholder="全部会员"
              value={memberID}
              aria-invalid={fieldError ? true : undefined}
              onChange={(event) => {
                setMemberID(event.target.value)
                setFieldError('')
              }}
            />
          </div>
          <div className="space-y-1">
            <Label htmlFor="order_trade_no" className="text-xs text-muted-foreground">
              订单号
            </Label>
            <Input
              id="order_trade_no"
              name="trade_no"
              className="w-56"
              placeholder="支持部分匹配，如 O20261008"
              value={tradeNo}
              onChange={(event) => setTradeNo(event.target.value)}
            />
          </div>
          <FilterSelect
            label="类型"
            value={orderType}
            options={TYPE_OPTIONS}
            onChange={(next) => {
              setOrderType(next)
              setPage(1)
            }}
          />
        </AdminFilterBar>
      </form>
      {fieldError ? <p className="text-xs text-destructive">{fieldError}</p> : null}

      <StatusFilter
        label="按状态筛选"
        options={STATUS_OPTIONS}
        value={status}
        onChange={(next) => {
          setStatus(next)
          setPage(1)
        }}
      />

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
              pageSize={ordersState.data.page_size || ADMIN_PAGE_SIZE}
              onChange={setPage}
            />
          ) : null}
        </>
      ) : null}
    </div>
  )
}
