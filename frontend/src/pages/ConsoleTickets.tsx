import { useEffect, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'

import { listTickets } from '@/api/tickets'
import type { Ticket, TicketStatus } from '@/api/types'
import { paths } from '@/app/paths'
import Pager from '@/components/common/Pager'
import { EmptyBlock, ErrorState, LoadingBlock } from '@/components/common/PageState'
import StatusBadge from '@/components/common/StatusBadge'
import StatusFilter from '@/components/common/StatusFilter'
import ComposeTicketDialog from '@/components/console/ComposeTicketDialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { useAsync } from '@/hooks/useAsync'
import { formatDateTime } from '@/lib/format'
import { CONSOLE_PAGE_SIZE } from '@/lib/pagination'
import {
  TICKET_STATUS_FILTERS,
  ticketCategoryLabel,
  ticketStatusLabel,
  ticketStatusTone,
} from '@/lib/ticketStatus'

type StatusFilterValue = '' | TicketStatus

// ConsoleTickets 是会员区「工单」列表页（契约 16.3）：状态筛选 + 提交工单（弹窗表单）+ 进入详情。
// 支持从订单页带参进入（?compose=1&subject=…&content=…）自动打开并预填表单。
export default function ConsoleTickets() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const [status, setStatus] = useState<StatusFilterValue>('')
  const [page, setPage] = useState(1)
  // null = 未打开；非空 = 打开并以此为初始值（惰性初始化直接从 URL 取一次）。
  const [compose, setCompose] = useState<{
    subject: string
    content: string
    instanceId: string
  } | null>(() =>
    searchParams.get('compose') === '1'
      ? {
          subject: searchParams.get('subject') ?? '',
          content: searchParams.get('content') ?? '',
          instanceId: searchParams.get('instance_id') ?? '',
        }
      : null,
  )

  const ticketsState = useAsync(
    () =>
      listTickets({
        page,
        page_size: CONSOLE_PAGE_SIZE,
        ...(status ? { status } : {}),
      }),
    [page, status],
  )
  const tickets = ticketsState.data?.items ?? []

  // 带参进入（如订单页「提交工单」）：首屏用 URL 参数初始化预填内容；随后只清理 URL，
  // 不再回写 state（弹窗的开合与内容此后完全由用户操作决定）。
  useEffect(() => {
    if (searchParams.get('compose') !== '1') {
      return
    }
    const next = new URLSearchParams(searchParams)
    next.delete('compose')
    next.delete('subject')
    next.delete('content')
    next.delete('instance_id')
    setSearchParams(next, { replace: true })
  }, [searchParams, setSearchParams])

  const changeStatus = (next: StatusFilterValue) => {
    setStatus(next)
    setPage(1)
  }

  return (
    <div className="space-y-5">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-foreground">工单</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            技术、财务或其他问题都可提交工单，客服回复后会通过站内通知提醒你。
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={ticketsState.reload}>
            刷新
          </Button>
          <Button size="sm" onClick={() => setCompose({ subject: '', content: '', instanceId: '' })}>
            提交工单
          </Button>
        </div>
      </header>

      <StatusFilter
        label="按状态筛选"
        options={TICKET_STATUS_FILTERS}
        value={status}
        onChange={changeStatus}
      />

      {ticketsState.loading ? <LoadingBlock label="正在读取工单…" /> : null}
      {ticketsState.error ? (
        <ErrorState message={ticketsState.error} onRetry={ticketsState.reload} />
      ) : null}

      {!ticketsState.loading && !ticketsState.error && tickets.length === 0 ? (
        <EmptyBlock
          title={status ? '该状态下没有工单' : '还没有工单'}
          description={
            status ? '换个状态筛选看看。' : '遇到问题可以随时提交工单，我们会尽快回复。'
          }
        />
      ) : null}

      <div className="space-y-3">
        {tickets.map((ticket) => (
          <TicketRow
            key={ticket.id}
            ticket={ticket}
            onOpen={() => navigate(paths.consoleTicketDetail(ticket.id))}
          />
        ))}
      </div>

      {ticketsState.data ? (
        <Pager
          page={page}
          total={ticketsState.data.total}
          pageSize={ticketsState.data.page_size || CONSOLE_PAGE_SIZE}
          onChange={setPage}
        />
      ) : null}

      {compose ? (
        <ComposeTicketDialog
          initialSubject={compose.subject}
          initialContent={compose.content}
          initialInstanceId={compose.instanceId}
          onOpenChange={(open) => (!open ? setCompose(null) : undefined)}
          onCreated={(ticket) => {
            setCompose(null)
            navigate(paths.consoleTicketDetail(ticket.id))
          }}
        />
      ) : null}
    </div>
  )
}

function TicketRow({ ticket, onOpen }: { ticket: Ticket; onOpen: () => void }) {
  return (
    <Card data-testid={`ticket-card-${ticket.id}`}>
      <CardContent className="space-y-2">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <button type="button" className="min-w-0 text-left" onClick={onOpen}>
            <span className="block truncate text-sm font-medium text-foreground hover:text-primary">
              {ticket.subject}
            </span>
            <span className="mt-0.5 block font-mono text-xs text-muted-foreground">
              {ticket.trade_no}
            </span>
          </button>
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="secondary">{ticketCategoryLabel(ticket.category)}</Badge>
            <StatusBadge
              tone={ticketStatusTone(ticket.status)}
              label={ticketStatusLabel(ticket.status)}
            />
          </div>
        </div>
        <p className="text-xs text-muted-foreground">
          最近活动 {formatDateTime(ticket.last_reply_at)}
          {ticket.instance ? ` · 关联实例 ${ticket.instance.name}` : ''}
        </p>
      </CardContent>
    </Card>
  )
}
