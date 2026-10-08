import { useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { listAdminTickets } from '@/api/adminTickets'
import type { AdminTicket, TicketCategory, TicketStatus } from '@/api/types'
import { paths } from '@/app/paths'
import { useAdminAuth } from '@/auth/adminAuthContext'
import AdminTable, { type AdminColumn } from '@/components/admin/AdminTable'
import FilterSelect, { AdminFilterBar } from '@/components/admin/FilterSelect'
import NoPermission from '@/components/admin/NoPermission'
import Pager from '@/components/common/Pager'
import { EmptyBlock, ErrorState, LoadingBlock } from '@/components/common/PageState'
import StatusBadge from '@/components/common/StatusBadge'
import StatusFilter from '@/components/common/StatusFilter'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useAsync } from '@/hooks/useAsync'
import { hasPermission } from '@/lib/adminRoles'
import { formatDateTime } from '@/lib/format'
import { ADMIN_PAGE_SIZE } from '@/lib/pagination'
import {
  TICKET_CATEGORIES,
  ticketCategoryLabel,
  ticketStatusLabel,
  ticketStatusTone,
} from '@/lib/ticketStatus'

type StatusFilterValue = '' | TicketStatus
type CategoryFilterValue = '' | TicketCategory

// 状态筛选（契约 16.2）。
const STATUS_OPTIONS: { value: TicketStatus; label: string }[] = [
  { value: 'open', label: '待客服处理' },
  { value: 'replied', label: '待会员回复' },
  { value: 'closed', label: '已关闭' },
]

// 分类筛选（契约 16.1）。
const CATEGORY_OPTIONS: { value: CategoryFilterValue; label: string }[] = [
  { value: '', label: '全部分类' },
  ...TICKET_CATEGORIES.map((category) => ({
    value: category,
    label: ticketCategoryLabel(category),
  })),
]

/**
 * AdminTickets 是管理后台「工单」列表页（契约 16.3）：全站工单 + 状态/分类/关键词/会员筛选。
 *
 * 角色矩阵：工单域是**客服域**——admin / support 全权，finance 一律 403（含只读）。
 * finance 直接渲染无权占位，且**不发起任何工单接口请求**（useAsync 的 enabled 参数关闭加载）。
 */
export default function AdminTickets() {
  const navigate = useNavigate()
  const { role } = useAdminAuth()
  const allowed = hasPermission(role, 'tickets.access')

  const [status, setStatus] = useState<StatusFilterValue>('')
  const [category, setCategory] = useState<CategoryFilterValue>('')
  const [keyword, setKeyword] = useState('')
  const [memberID, setMemberID] = useState('')
  // 关键词与会员 ID 为「提交式」筛选：回车或点「查询」后才生效；状态/分类即时生效。
  const [applied, setApplied] = useState({ keyword: '', memberID: '' })
  const [memberError, setMemberError] = useState('')
  const [page, setPage] = useState(1)

  const ticketsState = useAsync(
    () =>
      listAdminTickets({
        page,
        page_size: ADMIN_PAGE_SIZE,
        ...(status ? { status } : {}),
        ...(category ? { category } : {}),
        ...(applied.keyword ? { keyword: applied.keyword } : {}),
        ...(applied.memberID ? { member_id: Number(applied.memberID) } : {}),
      }),
    [page, status, category, applied.keyword, applied.memberID],
    allowed,
  )
  const tickets = ticketsState.data?.items ?? []
  const filtered = Boolean(status || category || applied.keyword || applied.memberID)

  if (!allowed) {
    return <NoPermission permission="tickets.access" />
  }

  const submitFilters = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const trimmed = memberID.trim()
    if (trimmed && (!/^\d+$/.test(trimmed) || Number(trimmed) <= 0)) {
      setMemberError('会员 ID 必须为正整数')
      return
    }
    setMemberError('')
    setApplied({ keyword: keyword.trim(), memberID: trimmed })
    setPage(1)
  }

  const columns: AdminColumn<AdminTicket>[] = [
    {
      key: 'trade_no',
      header: '工单号',
      cell: (ticket) => <span className="font-mono text-xs">{ticket.trade_no}</span>,
    },
    {
      key: 'subject',
      header: '标题',
      cell: (ticket) => (
        <Link
          className="text-sm text-foreground hover:text-primary"
          to={paths.adminTicketDetail(ticket.id)}
        >
          {ticket.subject}
        </Link>
      ),
    },
    {
      key: 'member',
      header: '会员',
      cell: (ticket) => (
        <span className="block">
          <span className="block text-sm">{ticket.member.username}</span>
          <span className="block text-xs text-muted-foreground">
            {ticket.member.nickname || '—'} · #{ticket.member_id}
          </span>
        </span>
      ),
    },
    {
      key: 'category',
      header: '分类',
      cell: (ticket) => <span className="text-sm">{ticketCategoryLabel(ticket.category)}</span>,
    },
    {
      key: 'status',
      header: '状态',
      cell: (ticket) => (
        <StatusBadge
          tone={ticketStatusTone(ticket.status)}
          label={ticketStatusLabel(ticket.status)}
        />
      ),
    },
    {
      key: 'last_reply_at',
      header: '最近活动',
      cell: (ticket) => <span className="text-xs">{formatDateTime(ticket.last_reply_at)}</span>,
    },
    {
      key: 'actions',
      header: '操作',
      className: 'whitespace-nowrap',
      cell: (ticket) => (
        <Button
          size="sm"
          variant="outline"
          onClick={() => navigate(paths.adminTicketDetail(ticket.id))}
        >
          查看详情
        </Button>
      ),
    },
  ]

  return (
    <div className="space-y-5">
      <header>
        <h1 className="text-xl font-semibold text-foreground">工单</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          全站工单（按状态、分类、关键词或会员 ID 筛选）。客服可公开回复、记录内部备注与关闭工单；
          <span className="text-foreground">内部备注不会展示给会员</span>。
        </p>
      </header>

      <form onSubmit={submitFilters} noValidate>
        <AdminFilterBar
          actions={
            <>
              <Button size="sm" type="button" variant="outline" onClick={ticketsState.reload}>
                刷新
              </Button>
              <Button size="sm" type="submit">
                查询
              </Button>
            </>
          }
        >
          <div className="space-y-1">
            <Label htmlFor="ticket_keyword" className="text-xs text-muted-foreground">
              关键词
            </Label>
            <Input
              id="ticket_keyword"
              name="keyword"
              className="w-full sm:w-56"
              placeholder="标题或工单号"
              value={keyword}
              onChange={(event) => setKeyword(event.target.value)}
            />
          </div>
          <div className="space-y-1">
            <Label htmlFor="ticket_member_id" className="text-xs text-muted-foreground">
              会员 ID
            </Label>
            <Input
              id="ticket_member_id"
              name="member_id"
              inputMode="numeric"
              className="w-full sm:w-40"
              placeholder="全部会员"
              value={memberID}
              aria-invalid={memberError ? true : undefined}
              onChange={(event) => {
                setMemberID(event.target.value)
                setMemberError('')
              }}
            />
          </div>
          <FilterSelect
            label="分类"
            value={category}
            options={CATEGORY_OPTIONS}
            onChange={(next) => {
              setCategory(next)
              setPage(1)
            }}
          />
        </AdminFilterBar>
      </form>
      {memberError ? <p className="text-xs text-destructive">{memberError}</p> : null}

      <StatusFilter
        label="按状态筛选"
        options={STATUS_OPTIONS}
        value={status}
        onChange={(next) => {
          setStatus(next)
          setPage(1)
        }}
      />

      {ticketsState.loading ? <LoadingBlock label="正在读取工单…" /> : null}
      {ticketsState.error ? (
        <ErrorState message={ticketsState.error} onRetry={ticketsState.reload} />
      ) : null}

      {!ticketsState.loading && !ticketsState.error ? (
        <>
          <AdminTable
            ariaLabel="工单列表"
            columns={columns}
            rows={tickets}
            rowKey={(ticket) => ticket.id}
            empty={
              <EmptyBlock
                title={filtered ? '当前筛选下没有工单' : '还没有工单'}
                description={
                  filtered ? '换个状态、分类或关键词再试。' : '会员提交工单后会出现在这里。'
                }
              />
            }
          />
          {ticketsState.data && ticketsState.data.total > 0 ? (
            <Pager
              page={page}
              total={ticketsState.data.total}
              pageSize={ticketsState.data.page_size || ADMIN_PAGE_SIZE}
              onChange={setPage}
            />
          ) : null}
        </>
      ) : null}
    </div>
  )
}
