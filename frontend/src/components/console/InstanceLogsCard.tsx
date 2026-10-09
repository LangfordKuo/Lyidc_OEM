import { useState } from 'react'

import { listInstanceLogs } from '@/api/instances'
import Pager from '@/components/common/Pager'
import { Timeline, type TimelineItem } from '@/components/common/InfoList'
import { EmptyBlock, ErrorState, LoadingBlock } from '@/components/common/PageState'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { useAsync } from '@/hooks/useAsync'
import { formatDateTime } from '@/lib/format'
import { ACTOR_TYPE_LABELS, instanceActionLabel } from '@/lib/instanceStatus'
import { CONSOLE_PAGE_SIZE } from '@/lib/pagination'

// InstanceLogsCard：实例操作记录（契约 15.4）。成功与失败尝试都会留痕，这里按时间倒序展示。
export default function InstanceLogsCard({ instanceId }: { instanceId: number }) {
  const [page, setPage] = useState(1)
  const logsState = useAsync(
    () => listInstanceLogs(instanceId, { page, page_size: CONSOLE_PAGE_SIZE }),
    [instanceId, page],
  )
  const logs = logsState.data?.items ?? []

  const items: TimelineItem[] = logs.map((log) => ({
    key: String(log.id),
    title: (
      <span className="flex flex-wrap items-center gap-2">
        <span>{instanceActionLabel(log.action)}</span>
        <span
          className={
            log.status === 'success'
              ? 'text-xs text-success dark:text-green-400'
              : 'text-xs text-destructive'
          }
        >
          {log.status === 'success' ? '成功' : '失败'}
        </span>
        <span className="text-xs text-muted-foreground">
          {ACTOR_TYPE_LABELS[log.actor_type] ?? log.actor_type}
          {log.actor_id ? ` #${log.actor_id}` : ''}
        </span>
      </span>
    ),
    time: formatDateTime(log.created_at),
    description: log.message || undefined,
    tone: log.status === 'success' ? 'success' : 'danger',
  }))

  return (
    <Card>
      <CardHeader className="border-b">
        <CardTitle className="text-base">操作记录</CardTitle>
        <CardDescription>电源、重装、改密、续费、终止等操作的留痕（含失败尝试）。</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {logsState.loading ? <LoadingBlock label="正在读取操作记录…" /> : null}
        {logsState.error ? (
          <ErrorState message={logsState.error} onRetry={logsState.reload} />
        ) : null}
        {!logsState.loading && !logsState.error && items.length === 0 ? (
          <EmptyBlock title="暂无操作记录" className="my-0" />
        ) : null}
        {items.length > 0 ? <Timeline items={items} /> : null}
        {logsState.data ? (
          <Pager
            page={page}
            total={logsState.data.total}
            pageSize={logsState.data.page_size || CONSOLE_PAGE_SIZE}
            onChange={setPage}
          />
        ) : null}
      </CardContent>
    </Card>
  )
}
