import {
  Alert,
  Button,
  Card,
  Chip,
  Input,
  Label,
  Modal,
  Radio,
  RadioGroup,
  TextArea,
  TextField,
  toast,
} from '@heroui/react'
import { useEffect, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'

import { errorMessage } from '../../api/client'
import { listInstances } from '../../api/instances'
import { createTicket, listTickets } from '../../api/tickets'
import type { Ticket, TicketCategory, TicketStatus } from '../../api/types'
import { paths } from '../../app/paths'
import StatusBadge from '../../components/StatusBadge'
import Pager from '../../components/common/Pager'
import { EmptyBlock, ErrorState, LoadingBlock } from '../../components/common/PageState'
import StatusFilter from '../../components/common/StatusFilter'
import { useAsync } from '../../hooks/useAsync'
import { formatDateTime } from '../../lib/format'
import { DEFAULT_PAGE_SIZE } from '../../lib/pagination'
import {
  TICKET_CATEGORIES,
  ticketCategoryLabel,
  ticketStatusLabel,
  ticketStatusTone,
} from '../../lib/ticketStatus'
import { validateTicketContent, validateTicketSubject } from '../../lib/validate'

type StatusFilterValue = '' | TicketStatus

const STATUS_OPTIONS: { value: StatusFilterValue; label: string }[] = [
  { value: '', label: '全部' },
  { value: 'open', label: '待客服处理' },
  { value: 'replied', label: '待会员回复' },
  { value: 'closed', label: '已关闭' },
]

// ConsoleTickets 是会员区「工单」列表页：状态筛选 + 提交工单（弹窗表单）+ 进入详情。
// 支持从订单页带参进入（?compose=1&subject=…&content=…）自动打开并预填表单。
export default function ConsoleTickets() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const [status, setStatus] = useState<StatusFilterValue>('')
  const [page, setPage] = useState(1)
  // null = 未打开；非空 = 打开并以此为初始值（惰性初始化直接从 URL 取一次）。
  const [compose, setCompose] = useState<{ subject: string; content: string } | null>(() =>
    searchParams.get('compose') === '1'
      ? { subject: searchParams.get('subject') ?? '', content: searchParams.get('content') ?? '' }
      : null,
  )

  const ticketsState = useAsync(
    () =>
      listTickets({
        page,
        page_size: DEFAULT_PAGE_SIZE,
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
          <h1 className="text-xl font-semibold text-foreground">工单</h1>
          <p className="mt-1 text-sm text-muted">
            技术、财务或其他问题都可提交工单，客服回复后会通过站内通知提醒你。
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onPress={ticketsState.reload}>
            刷新
          </Button>
          <Button
            variant="primary"
            size="sm"
            onPress={() => setCompose({ subject: '', content: '' })}
          >
            提交工单
          </Button>
        </div>
      </header>

      <StatusFilter
        label="按状态筛选"
        options={STATUS_OPTIONS}
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
          <TicketRow key={ticket.id} ticket={ticket} onOpen={() => navigate(paths.consoleTicketDetail(ticket.id))} />
        ))}
      </div>

      {ticketsState.data && ticketsState.data.total > 0 ? (
        <Pager
          page={page}
          total={ticketsState.data.total}
          pageSize={ticketsState.data.page_size || DEFAULT_PAGE_SIZE}
          onChange={setPage}
        />
      ) : null}

      {compose ? (
        <ComposeTicketDialog
          initialSubject={compose.subject}
          initialContent={compose.content}
          onClose={() => setCompose(null)}
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
    <Card>
      <Card.Content className="space-y-2">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <button type="button" className="min-w-0 text-left" onClick={onOpen}>
            <span className="block truncate text-sm font-medium text-foreground hover:text-accent">
              {ticket.subject}
            </span>
            <span className="mt-0.5 block font-mono text-xs text-muted">{ticket.trade_no}</span>
          </button>
          <div className="flex flex-wrap items-center gap-2">
            <Chip size="sm" variant="soft" color="default">
              {ticketCategoryLabel(ticket.category)}
            </Chip>
            <StatusBadge
              tone={ticketStatusTone(ticket.status)}
              label={ticketStatusLabel(ticket.status)}
            />
          </div>
        </div>
        <p className="text-xs text-muted">
          最近活动 {formatDateTime(ticket.last_reply_at)}
          {ticket.instance ? ` · 关联实例 ${ticket.instance.name}` : ''}
        </p>
      </Card.Content>
    </Card>
  )
}

/** 提交工单弹窗（契约 16.3：标题 5-100、内容 1-5000、分类三选一、实例可选）。 */
// 由父组件按需挂载（打开即挂载、关闭即卸载），因此状态直接用 props 初始化，无需在 effect 里重置。
function ComposeTicketDialog({
  initialSubject,
  initialContent,
  onClose,
  onCreated,
}: {
  initialSubject: string
  initialContent: string
  onClose: () => void
  onCreated: (ticket: Ticket) => void
}) {
  const [subject, setSubject] = useState(initialSubject)
  const [content, setContent] = useState(initialContent)
  const [category, setCategory] = useState<TicketCategory>('technical')
  const [instanceId, setInstanceId] = useState<string>('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  const instancesState = useAsync(() => listInstances({ page: 1, page_size: 100 }), [])
  const instances = instancesState.data?.items ?? []

  const subjectError = subject ? validateTicketSubject(subject) : ''
  const contentError = content ? validateTicketContent(content) : ''

  const handleSubmit = async () => {
    const subjectMessage = validateTicketSubject(subject)
    const contentMessage = validateTicketContent(content)
    if (subjectMessage || contentMessage) {
      setError(subjectMessage || contentMessage)
      return
    }
    setPending(true)
    setError('')
    try {
      const result = await createTicket({
        subject: subject.trim(),
        content: content.trim(),
        category,
        ...(instanceId ? { instance_id: Number(instanceId) } : {}),
      })
      toast.success(`工单已提交（${result.ticket.trade_no}）`)
      onCreated(result.ticket)
    } catch (err) {
      setError(errorMessage(err, '提交工单失败，请稍后重试'))
    } finally {
      setPending(false)
    }
  }

  return (
    <Modal
      isOpen
      onOpenChange={(open) => {
        if (!open && !pending) {
          onClose()
        }
      }}
    >
      <Modal.Backdrop>
        <Modal.Container size="lg" placement="center">
          <Modal.Dialog>
            <Modal.Header>
              <Modal.Heading>提交工单</Modal.Heading>
            </Modal.Header>
            <Modal.Body className="space-y-4">
              <TextField
                name="ticket_subject"
                value={subject}
                onChange={setSubject}
                isInvalid={Boolean(subject && subjectError)}
              >
                <Label>标题</Label>
                <Input placeholder="一句话描述问题（5-100 个字符）" />
                {subject && subjectError ? (
                  <p className="mt-1 text-xs text-danger">{subjectError}</p>
                ) : null}
              </TextField>

              <div>
                <p className="mb-2 text-sm font-medium text-foreground">分类</p>
                <RadioGroup
                  aria-label="工单分类"
                  value={category}
                  onChange={(value) => setCategory(value as TicketCategory)}
                  orientation="horizontal"
                  className="gap-3"
                >
                  {TICKET_CATEGORIES.map((item) => (
                    // HeroUI v3 的 Radio 必须用 Radio.Content 包裹才是可交互控件（否则渲染为不可选中的纯文本），
                    // 圆圈（Radio.Control / Radio.Indicator）也要放在 Radio.Content 内、文本之前。
                    <Radio key={item} value={item}>
                      <Radio.Content>
                        <Radio.Control>
                          <Radio.Indicator />
                        </Radio.Control>
                        {ticketCategoryLabel(item)}
                      </Radio.Content>
                    </Radio>
                  ))}
                </RadioGroup>
              </div>

              <div>
                <p className="mb-2 text-sm font-medium text-foreground">关联服务器（可选）</p>
                {instancesState.loading ? (
                  <p className="text-xs text-muted">正在读取实例列表…</p>
                ) : null}
                {!instancesState.loading && instances.length === 0 ? (
                  <p className="text-xs text-muted">暂无可关联的实例。</p>
                ) : null}
                {instances.length > 0 ? (
                  <RadioGroup
                    aria-label="关联服务器"
                    value={instanceId}
                    onChange={(value) => setInstanceId(String(value))}
                  >
                    <div className="max-h-40 space-y-2 overflow-y-auto pr-1">
                      <Radio value="">
                        <Radio.Content>
                          <Radio.Control>
                            <Radio.Indicator />
                          </Radio.Control>
                          不关联实例
                        </Radio.Content>
                      </Radio>
                      {instances.map((instance) => (
                        <Radio key={instance.id} value={String(instance.id)}>
                          <Radio.Content>
                            <Radio.Control>
                              <Radio.Indicator />
                            </Radio.Control>
                            <span className="flex flex-col">
                              <span className="font-mono text-sm">{instance.name}</span>
                              <span className="text-xs text-muted">{instance.product_name}</span>
                            </span>
                          </Radio.Content>
                        </Radio>
                      ))}
                    </div>
                  </RadioGroup>
                ) : null}
              </div>

              <TextField
                name="ticket_content"
                value={content}
                onChange={setContent}
                isInvalid={Boolean(content && contentError)}
              >
                <Label>问题描述</Label>
                <TextArea rows={6} placeholder="请描述现象、发生时间与已尝试的操作（最多 5000 个字符）" />
                {content && contentError ? (
                  <p className="mt-1 text-xs text-danger">{contentError}</p>
                ) : null}
              </TextField>

              {error ? (
                <Alert status="danger">
                  <Alert.Indicator />
                  <Alert.Content>
                    <Alert.Description>{error}</Alert.Description>
                  </Alert.Content>
                </Alert>
              ) : null}
            </Modal.Body>
            <Modal.Footer>
              <Button variant="outline" isDisabled={pending} onPress={onClose}>
                取消
              </Button>
              <Button variant="primary" isDisabled={pending} onPress={handleSubmit}>
                {pending ? '正在提交…' : '提交工单'}
              </Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  )
}
