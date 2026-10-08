import { useState } from 'react'
import { AlertCircleIcon, LoaderCircleIcon } from 'lucide-react'
import { toast } from 'sonner'

import { errorMessage } from '@/api/client'
import { listInstances } from '@/api/instances'
import { createTicket } from '@/api/tickets'
import type { Ticket, TicketCategory } from '@/api/types'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Textarea } from '@/components/ui/textarea'
import { useAsync } from '@/hooks/useAsync'
import { TICKET_CATEGORIES, ticketCategoryLabel } from '@/lib/ticketStatus'
import { validateTicketContent, validateTicketSubject } from '@/lib/validate'
import { cn } from '@/lib/utils'

/** 提交工单弹窗（契约 16.3：标题 5-100、内容 1-5000、分类三选一、实例可选）。 */
// 由父组件按需挂载（打开即挂载、关闭即卸载），因此状态直接用 props 初始化，无需在 effect 里重置。
export default function ComposeTicketDialog({
  initialSubject = '',
  initialContent = '',
  initialInstanceId = '',
  onOpenChange,
  onCreated,
}: {
  initialSubject?: string
  initialContent?: string
  initialInstanceId?: string
  onOpenChange: (open: boolean) => void
  onCreated: (ticket: Ticket) => void
}) {
  const [subject, setSubject] = useState(initialSubject)
  const [content, setContent] = useState(initialContent)
  const [category, setCategory] = useState<TicketCategory>('technical')
  const [instanceId, setInstanceId] = useState(initialInstanceId)
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
    <Dialog open onOpenChange={(open) => (!pending ? onOpenChange(open) : undefined)}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>提交工单</DialogTitle>
          <DialogDescription>
            技术、财务或其他问题都可提交；客服回复后会通过站内通知提醒你。
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-1.5">
          <Label htmlFor="ticket_subject">标题</Label>
          <Input
            id="ticket_subject"
            value={subject}
            placeholder="一句话描述问题（5-100 个字符）"
            aria-invalid={Boolean(subject && subjectError)}
            onChange={(event) => {
              setSubject(event.target.value)
              setError('')
            }}
          />
          {subject && subjectError ? (
            <p className="text-xs text-destructive">{subjectError}</p>
          ) : null}
        </div>

        <div className="space-y-1.5">
          <p className="text-sm font-medium text-foreground">分类</p>
          <RadioGroup
            value={category}
            onValueChange={(value) => setCategory(value as TicketCategory)}
            aria-label="工单分类"
            className="flex flex-wrap gap-4"
          >
            {TICKET_CATEGORIES.map((item) => (
              <Label key={item} className="flex cursor-pointer items-center gap-2 text-sm">
                <RadioGroupItem value={item} />
                {ticketCategoryLabel(item)}
              </Label>
            ))}
          </RadioGroup>
        </div>

        <div className="space-y-1.5">
          <p className="text-sm font-medium text-foreground">关联服务器（可选）</p>
          {instancesState.loading ? (
            <p className="text-xs text-muted-foreground">正在读取实例列表…</p>
          ) : null}
          {!instancesState.loading && instances.length === 0 ? (
            <p className="text-xs text-muted-foreground">暂无可关联的实例。</p>
          ) : null}
          {instances.length > 0 ? (
            <RadioGroup
              value={instanceId}
              onValueChange={setInstanceId}
              aria-label="关联服务器"
              className="max-h-40 gap-2 overflow-y-auto pr-1"
            >
              <Label className="flex cursor-pointer items-center gap-2 text-sm">
                <RadioGroupItem value="" />
                不关联实例
              </Label>
              {instances.map((instance) => (
                <Label
                  key={instance.id}
                  className={cn(
                    'flex cursor-pointer items-start gap-2 rounded-lg border p-2.5 transition-colors',
                    instanceId === String(instance.id)
                      ? 'border-primary bg-primary/5'
                      : 'border-border hover:bg-muted/50',
                  )}
                >
                  <RadioGroupItem value={String(instance.id)} className="mt-0.5" />
                  <span className="flex flex-col gap-0.5">
                    <span className="font-mono text-sm">{instance.name}</span>
                    <span className="text-xs text-muted-foreground">{instance.product_name}</span>
                  </span>
                </Label>
              ))}
            </RadioGroup>
          ) : null}
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="ticket_content">问题描述</Label>
          <Textarea
            id="ticket_content"
            rows={5}
            value={content}
            placeholder="请描述现象、发生时间与已尝试的操作（最多 5000 个字符）"
            aria-invalid={Boolean(content && contentError)}
            onChange={(event) => {
              setContent(event.target.value)
              setError('')
            }}
          />
          {content && contentError ? (
            <p className="text-xs text-destructive">{contentError}</p>
          ) : null}
        </div>

        {error ? (
          <Alert variant="destructive">
            <AlertCircleIcon aria-hidden />
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}

        <DialogFooter className="gap-2">
          <Button variant="outline" disabled={pending} onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button disabled={pending} onClick={handleSubmit}>
            {pending ? (
              <>
                <LoaderCircleIcon className="animate-spin" data-icon="inline-start" aria-hidden />
                正在提交…
              </>
            ) : (
              '提交工单'
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
