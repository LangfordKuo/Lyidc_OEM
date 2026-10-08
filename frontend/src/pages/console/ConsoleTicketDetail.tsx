import { Alert, Button, Card, Chip, TextArea, toast } from '@heroui/react'
import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'

import { errorMessage } from '../../api/client'
import { closeTicket, fetchTicket, replyTicket } from '../../api/tickets'
import type { TicketMessage } from '../../api/types'
import { paths } from '../../app/paths'
import StatusBadge from '../../components/StatusBadge'
import ConfirmDialog from '../../components/common/ConfirmDialog'
import { EmptyBlock, ErrorState, LoadingBlock } from '../../components/common/PageState'
import { useAsync } from '../../hooks/useAsync'
import { formatDateTime } from '../../lib/format'
import { ticketCategoryLabel, ticketStatusLabel, ticketStatusTone } from '../../lib/ticketStatus'
import { validateTicketContent } from '../../lib/validate'

// ConsoleTicketDetail 是工单详情页（契约 16.3）：消息流（会员/客服分侧气泡）+ 回复 + 关闭。
// 会员端接口不下发内部备注，因此这里展示的就是「对会员可见」的完整对话。
export default function ConsoleTicketDetail() {
  const params = useParams<{ id: string }>()
  const id = params.id && /^\d+$/.test(params.id) ? Number(params.id) : null

  const [refreshKey, setRefreshKey] = useState(0)
  const [content, setContent] = useState('')
  const [sending, setSending] = useState(false)
  const [error, setError] = useState('')
  const [closeOpen, setCloseOpen] = useState(false)
  const [closing, setClosing] = useState(false)

  const ticketState = useAsync(
    () => {
      if (id === null) {
        return Promise.reject(new Error('工单 ID 必须为正整数'))
      }
      return fetchTicket(id)
    },
    [id, refreshKey],
  )
  // 详情接口返回嵌套的 {ticket, messages}（契约 16.3，阶段 8 起与实现统一）。
  const ticket = ticketState.data?.ticket ?? null
  const messages = ticketState.data?.messages ?? []
  const closed = ticket?.status === 'closed'

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
            <Link className="text-accent hover:underline" to={paths.consoleTickets}>
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

  const handleReply = async () => {
    const message = validateTicketContent(content)
    if (message) {
      setError(message)
      return
    }
    setSending(true)
    setError('')
    try {
      await replyTicket(ticket.id, content.trim())
      setContent('')
      toast.success('回复已发送')
      setRefreshKey((value) => value + 1)
    } catch (err) {
      setError(errorMessage(err, '回复失败，请稍后重试'))
    } finally {
      setSending(false)
    }
  }

  const handleClose = async () => {
    setClosing(true)
    try {
      const result = await closeTicket(ticket.id)
      toast.success(result.already_closed ? '工单此前已关闭' : '工单已关闭')
      setCloseOpen(false)
      setRefreshKey((value) => value + 1)
    } catch (err) {
      toast.danger(errorMessage(err, '关闭工单失败'))
    } finally {
      setClosing(false)
    }
  }

  return (
    <div className="space-y-5">
      <nav className="text-sm text-muted" aria-label="面包屑">
        <Link className="hover:text-foreground" to={paths.consoleTickets}>
          工单
        </Link>
        <span className="mx-2">/</span>
        <span className="text-foreground">工单详情</span>
      </nav>

      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="text-lg font-semibold text-foreground">{ticket.subject}</h1>
          <p className="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted">
            <span className="font-mono">{ticket.trade_no}</span>
            <Chip size="sm" variant="soft" color="default">
              {ticketCategoryLabel(ticket.category)}
            </Chip>
            <span>创建于 {formatDateTime(ticket.created_at)}</span>
            {ticket.instance ? (
              <span>
                · 关联实例 <span className="font-mono">{ticket.instance.name}</span>
              </span>
            ) : null}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <StatusBadge
            tone={ticketStatusTone(ticket.status)}
            label={ticketStatusLabel(ticket.status)}
          />
          {!closed ? (
            <Button variant="outline" size="sm" onPress={() => setCloseOpen(true)}>
              关闭工单
            </Button>
          ) : null}
        </div>
      </header>

      {closed ? (
        <Alert status="default">
          <Alert.Indicator />
          <Alert.Content>
            <Alert.Description>
              工单已于 {formatDateTime(ticket.closed_at ?? ticket.updated_at)} 关闭，如需继续沟通请
              <Link className="mx-1 text-accent hover:underline" to={paths.consoleTickets}>
                新开工单
              </Link>
              。
            </Alert.Description>
          </Alert.Content>
        </Alert>
      ) : null}

      <Card>
        <Card.Header>
          <Card.Title className="text-base">对话记录</Card.Title>
          <Card.Description>客服的内部备注不会展示在这里。</Card.Description>
        </Card.Header>
        <Card.Content className="space-y-4">
          {messages.length === 0 ? (
            <EmptyBlock title="暂无消息" />
          ) : (
            messages.map((message) => <MessageBubble key={message.id} message={message} />)
          )}
        </Card.Content>
      </Card>

      <Card>
        <Card.Header>
          <Card.Title className="text-base">回复</Card.Title>
          <Card.Description>
            {closed ? '工单已关闭，无法继续回复。' : '补充说明或追问，客服会收到通知。'}
          </Card.Description>
        </Card.Header>
        <Card.Content className="space-y-3">
          <TextArea
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
          {error ? <p className="text-xs text-danger">{error}</p> : null}
          <div className="flex items-center gap-3">
            <Button
              variant="primary"
              isDisabled={closed || sending || !content.trim()}
              onPress={handleReply}
            >
              {sending ? '正在发送…' : '发送回复'}
            </Button>
            {closed ? <span className="text-xs text-muted">如需继续请新开工单。</span> : null}
          </div>
        </Card.Content>
      </Card>

      <ConfirmDialog
        isOpen={closeOpen}
        title="确认关闭工单？"
        description="关闭后双方都不能再回复，且不可重新打开；如需继续请新开工单。"
        confirmLabel="关闭工单"
        isDanger
        pending={closing}
        onCancel={() => setCloseOpen(false)}
        onConfirm={handleClose}
      />
    </div>
  )
}

function MessageBubble({ message }: { message: TicketMessage }) {
  const fromMember = message.author_type === 'member'
  return (
    <div className={`flex ${fromMember ? 'justify-end' : 'justify-start'}`}>
      <div className="max-w-[85%] space-y-1">
        <p className={`text-xs text-muted ${fromMember ? 'text-right' : ''}`}>
          {fromMember ? '我' : `客服 ${message.author_name || ''}`} ·{' '}
          {formatDateTime(message.created_at)}
        </p>
        <div
          className={[
            'rounded-xl px-4 py-2.5 text-sm whitespace-pre-wrap',
            fromMember
              ? 'bg-accent-soft text-accent-soft-foreground'
              : 'bg-surface-secondary text-foreground',
          ].join(' ')}
        >
          {message.content}
        </div>
      </div>
    </div>
  )
}
