import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ChevronRightIcon, LoaderCircleIcon } from 'lucide-react'
import { toast } from 'sonner'

import { errorMessage } from '@/api/client'
import { closeAdminTicket, fetchAdminTicket, replyAdminTicket } from '@/api/adminTickets'
import type { TicketMessage } from '@/api/types'
import { paths } from '@/app/paths'
import { useAdminAuth } from '@/auth/adminAuthContext'
import NoPermission from '@/components/admin/NoPermission'
import ConfirmDialog from '@/components/common/ConfirmDialog'
import { EmptyBlock, ErrorState, LoadingBlock } from '@/components/common/PageState'
import StatusBadge from '@/components/common/StatusBadge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { useAsync } from '@/hooks/useAsync'
import { hasPermission } from '@/lib/adminRoles'
import { formatDateTime } from '@/lib/format'
import { ticketCategoryLabel, ticketStatusLabel, ticketStatusTone } from '@/lib/ticketStatus'
import { validateTicketContent } from '@/lib/validate'
import { cn } from '@/lib/utils'

/**
 * AdminTicketDetail 是管理后台工单详情页（契约 16.3）：完整消息流（**含内部备注**）+ 回复/备注 + 关闭。
 *
 * 与会员端同域页面（ConsoleTicketDetail）的差异：
 *   1. 消息流展示内部备注（会员端接口不下发），内部备注用警示色单独标记；
 *   2. 回复区多一个「内部备注」开关：公开回复会把状态转为 replied 并通知会员，
 *      内部备注不改变状态、不通知会员；
 *   3. 角色矩阵：admin / support 全权，finance 一律 403（含只读）——直接渲染无权占位且不发请求。
 */
export default function AdminTicketDetail() {
  const params = useParams<{ id: string }>()
  const id = params.id && /^\d+$/.test(params.id) ? Number(params.id) : null
  const { role } = useAdminAuth()
  const allowed = hasPermission(role, 'tickets.access')

  const [refreshKey, setRefreshKey] = useState(0)
  const [content, setContent] = useState('')
  // 内部备注开关：默认关（公开回复），每次渲染由用户显式切换。
  const [internal, setInternal] = useState(false)
  const [sending, setSending] = useState(false)
  const [error, setError] = useState('')
  const [closeOpen, setCloseOpen] = useState(false)
  const [closing, setClosing] = useState(false)

  // 无权限角色不发起任何工单请求（enabled=false 时 useAsync 不打网络）。
  const ticketState = useAsync(
    () => {
      if (id === null) {
        return Promise.reject(new Error('工单 ID 必须为正整数'))
      }
      return fetchAdminTicket(id)
    },
    [id, refreshKey],
    allowed,
  )
  const ticket = ticketState.data?.ticket
  const messages = ticketState.data?.messages ?? []
  const refresh = () => setRefreshKey((value) => value + 1)

  if (!allowed) {
    return <NoPermission permission="tickets.access" />
  }

  if (id === null) {
    return <EmptyBlock title="工单 ID 不正确" description="请从工单列表进入详情。" />
  }

  if (ticketState.loading) {
    return <LoadingBlock label="正在读取工单…" />
  }

  if (ticketState.error) {
    if (ticketState.error.includes('工单不存在')) {
      return (
        <EmptyBlock
          title="工单不存在"
          description={
            <Link className="text-primary hover:underline" to={paths.adminTickets}>
              返回工单列表
            </Link>
          }
        />
      )
    }
    return <ErrorState message={ticketState.error} onRetry={ticketState.reload} />
  }

  if (!ticket) {
    return null
  }

  const closed = ticket.status === 'closed'

  const handleReply = async () => {
    const message = validateTicketContent(content)
    if (message) {
      setError(message)
      return
    }
    setSending(true)
    setError('')
    const asInternal = internal
    try {
      await replyAdminTicket(ticket.id, { content: content.trim(), internal: asInternal })
      setContent('')
      // 内部备注提交后把开关复位为「公开回复」：避免下一条本该发给会员的内容误开内部备注。
      setInternal(false)
      toast.success(
        asInternal
          ? '内部备注已记录（会员不可见，工单状态不变）'
          : '回复已发送，工单转为待会员回复',
      )
      refresh()
    } catch (err) {
      setError(errorMessage(err, asInternal ? '内部备注提交失败，请稍后重试' : '回复失败，请稍后重试'))
    } finally {
      setSending(false)
    }
  }

  const handleClose = async () => {
    setClosing(true)
    try {
      const result = await closeAdminTicket(ticket.id)
      toast.success(result.already_closed ? '工单此前已关闭' : '工单已关闭')
      setCloseOpen(false)
      refresh()
    } catch (err) {
      toast.error(errorMessage(err, '关闭工单失败'))
    } finally {
      setClosing(false)
    }
  }

  return (
    <div className="space-y-5">
      <nav className="flex items-center gap-1 text-sm text-muted-foreground" aria-label="面包屑">
        <Link className="hover:text-foreground" to={paths.adminTickets}>
          工单
        </Link>
        <ChevronRightIcon className="size-3.5" aria-hidden />
        <span className="text-foreground">详情</span>
      </nav>

      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="text-lg font-semibold text-foreground">{ticket.subject}</h1>
          <p className="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <span className="font-mono">{ticket.trade_no}</span>
            <Badge variant="secondary">{ticketCategoryLabel(ticket.category)}</Badge>
            <span>
              会员 {ticket.member.username}（#{ticket.member_id} · {ticket.member.nickname || '—'}）
            </span>
            <span>· 创建于 {formatDateTime(ticket.created_at)}</span>
            <span>
              · 关联实例{' '}
              {ticket.instance ? (
                <span className="font-mono">{ticket.instance.name}</span>
              ) : (
                '无'
              )}
            </span>
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <StatusBadge
            tone={ticketStatusTone(ticket.status)}
            label={ticketStatusLabel(ticket.status)}
          />
          {!closed ? (
            <Button variant="outline" size="sm" onClick={() => setCloseOpen(true)}>
              关闭工单
            </Button>
          ) : null}
        </div>
      </header>

      {closed ? (
        <Alert>
          <AlertDescription>
            工单已于 {formatDateTime(ticket.closed_at ?? ticket.updated_at)} 关闭，双方都不能再回复；
            会员如需继续沟通请新开工单。
          </AlertDescription>
        </Alert>
      ) : null}

      <Card>
        <CardHeader className="border-b">
          <CardTitle className="text-base">消息流</CardTitle>
          <CardDescription>
            含内部备注（会员端看不到）：内部备注以警示色标记，仅管理端可见。
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {messages.length === 0 ? (
            <EmptyBlock title="暂无消息" className="my-0" />
          ) : (
            messages.map((message) => <MessageBubble key={message.id} message={message} />)
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="border-b">
          <CardTitle className="text-base">回复</CardTitle>
          <CardDescription>
            {closed
              ? '工单已关闭，无法继续回复或记录备注。'
              : '公开回复会通知会员并把工单转为「待会员回复」；内部备注仅管理端可见，不通知会员、也不改变工单状态。'}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <Textarea
            aria-label="回复内容"
            rows={4}
            value={content}
            disabled={closed}
            placeholder={closed ? '工单已关闭' : '输入回复内容（最多 5000 个字符）'}
            onChange={(event) => {
              setContent(event.target.value)
              setError('')
            }}
          />

          <div className="space-y-1">
            <div className="flex items-center gap-2">
              <Switch
                id="ticket-internal"
                checked={internal}
                disabled={closed || sending}
                aria-label="内部备注"
                onCheckedChange={setInternal}
              />
              <Label htmlFor="ticket-internal" className="text-sm font-normal">
                内部备注
              </Label>
              {internal ? <Badge variant="destructive">会员不可见</Badge> : null}
            </div>
            <p className="text-xs text-muted-foreground">
              {internal
                ? '当前为内部备注：不会通知会员，工单状态保持不变，会员端看不到这条内容。'
                : '当前为公开回复：会员会收到通知，工单状态转为「待会员回复」。'}
            </p>
          </div>

          {error ? <p className="text-xs text-destructive">{error}</p> : null}

          <div className="flex items-center gap-3">
            <Button
              variant={internal ? 'secondary' : 'default'}
              disabled={closed || sending || !content.trim()}
              onClick={handleReply}
            >
              {sending ? (
                <>
                  <LoaderCircleIcon className="animate-spin" data-icon="inline-start" aria-hidden />
                  正在发送…
                </>
              ) : internal ? (
                '记录内部备注'
              ) : (
                '发送回复'
              )}
            </Button>
            {closed ? (
              <span className="text-xs text-muted-foreground">如需继续请让会员新开工单。</span>
            ) : null}
          </div>
        </CardContent>
      </Card>

      <ConfirmDialog
        open={closeOpen}
        onOpenChange={setCloseOpen}
        title="确认关闭工单？"
        description="关闭后双方都不能再回复，且不可重新打开；会员如需继续沟通请新开工单。"
        confirmLabel="关闭工单"
        danger
        pending={closing}
        onConfirm={handleClose}
      />
    </div>
  )
}

// MessageBubble 是管理端视角的消息气泡：客服消息靠右（主色），会员消息靠左（中性色）。
// 内部备注（internal=true）用警示色边框/背景 + 「内部备注（会员不可见）」标签显著标记。
function MessageBubble({ message }: { message: TicketMessage }) {
  const fromAdmin = message.author_type === 'admin'
  const internal = message.internal

  return (
    <div className={cn('flex', fromAdmin ? 'justify-end' : 'justify-start')}>
      <div className="max-w-[85%] space-y-1">
        <p className={cn('text-xs text-muted-foreground', fromAdmin ? 'text-right' : undefined)}>
          {fromAdmin ? `客服 ${message.author_name || ''}` : `会员 ${message.author_name || ''}`} ·{' '}
          {formatDateTime(message.created_at)}
        </p>
        <div
          className={cn(
            'rounded-xl px-4 py-2.5 text-sm whitespace-pre-wrap',
            internal
              ? 'border border-warning/60 bg-warning/10 text-foreground'
              : fromAdmin
                ? 'bg-accent text-foreground'
                : 'bg-muted text-foreground',
          )}
        >
          {internal ? (
            <p className="mb-1.5 text-xs font-medium text-warning">内部备注（会员不可见）</p>
          ) : null}
          {message.content}
        </div>
      </div>
    </div>
  )
}
