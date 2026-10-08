import { Button, Input, Label, TextField } from '@heroui/react'
import { useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { listAdminTickets } from '../../api/adminTickets'
import type { AdminTicket, TicketCategory, TicketStatus } from '../../api/types'
import { paths } from '../../app/paths'
import { useAdminAuth } from '../../auth/adminAuthContext'
import StatusBadge from '../../components/StatusBadge'
import AdminTable, { type AdminColumn } from '../../components/admin/AdminTable'
import FilterSelect, {
  AdminFilterBar,
  type FilterSelectOption,
} from '../../components/admin/FilterSelect'
import NoPermission from '../../components/admin/NoPermission'
import Pager from '../../components/common/Pager'
import { EmptyBlock, ErrorState, LoadingBlock } from '../../components/common/PageState'
import StatusFilter from '../../components/common/StatusFilter'
import { useAsync } from '../../hooks/useAsync'
import { hasPermission } from '../../lib/adminRoles'
import { formatDateTime } from '../../lib/format'
import { DEFAULT_PAGE_SIZE } from '../../lib/pagination'
import {
  TICKET_CATEGORIES,
  ticketCategoryLabel,
  ticketStatusLabel,
  ticketStatusTone,
} from '../../lib/ticketStatus'

type StatusFilterValue = '' | TicketStatus
type CategoryFilterValue = '' | TicketCategory

// 状态筛选（契约 16.2）：空串表示「全部」。
const STATUS_OPTIONS: { value: StatusFilterValue; label: string }[] = [
  { value: '', label: '全部' },
  { value: 'open', label: '待客服处理' },
  { value: 'replied', label: '待会员回复' },
  { value: 'closed', label: '已关闭' },
]

// 分类筛选（契约 16.1）。
const CATEGORY_OPTIONS: FilterSelectOption<CategoryFilterValue>[] = [
  { value: '', label: '全部' },
  ...TICKET_CATEGORIES.map((category) => ({
    value: category,
    label: ticketCategoryLabel(category),
  })),
]

// AdminTickets 是管理后台「工单」列表页（契约 16.3）：全站工单 + 状态/分类/关键词/会员筛选。
//
// 角色矩阵：工单域是**客服域**——admin / support 全权，finance 一律 403（含只读）。
// finance 直接渲染无权占位，且**不发起任何工单接口请求**（useAsync 的 enabled 参数关闭加载）。
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
        page_size: DEFAULT_PAGE_SIZE,
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
          className="text-sm text-foreground hover:text-accent"
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
          <span className="block text-xs text-muted">{ticket.member.nickname || '—'}</span>
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
      cell: (ticket) => (
        <Button
          size="sm"
          variant="outline"
          onPress={() => navigate(paths.adminTicketDetail(ticket.id))}
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
        <p className="mt-1 text-sm text-muted">
          全站工单（按状态、分类、关键词或会员 ID 筛选）。客服可公开回复、记录内部备注与关闭工单；
          <span className="text-foreground">内部备注不会展示给会员</span>。
        </p>
      </header>

      <form className="space-y-3" onSubmit={submitFilters} noValidate>
        <AdminFilterBar
          actions={
            <>
              <Button size="sm" type="button" variant="outline" onPress={ticketsState.reload}>
                刷新
              </Button>
              <Button size="sm" type="submit" variant="primary">
                查询
              </Button>
            </>
          }
        >
          <StatusFilter
            label="按状态筛选"
            options={STATUS_OPTIONS}
            value={status}
            onChange={(next) => {
              setStatus(next)
              setPage(1)
            }}
          />
          <FilterSelect
            label="分类"
            value={category}
            options={CATEGORY_OPTIONS}
            onChange={(next) => {
              setCategory(next)
              setPage(1)
            }}
          />
          <TextField
            name="keyword"
            type="text"
            value={keyword}
            onChange={setKeyword}
            className="w-full sm:w-56"
          >
            <Label>关键词</Label>
            <Input placeholder="标题或工单号" />
          </TextField>
          <TextField
            name="member_id"
            type="text"
            value={memberID}
            onChange={(value) => {
              setMemberID(value)
              setMemberError('')
            }}
            isInvalid={Boolean(memberError)}
            className="w-full sm:w-40"
          >
            <Label>会员 ID</Label>
            <Input placeholder="全部会员" inputMode="numeric" />
          </TextField>
        </AdminFilterBar>
      </form>
      {memberError ? <p className="text-xs text-danger">{memberError}</p> : null}

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
              pageSize={ticketsState.data.page_size || DEFAULT_PAGE_SIZE}
              onChange={setPage}
            />
          ) : null}
        </>
      ) : null}
    </div>
  )
}
