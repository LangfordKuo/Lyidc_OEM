import { Card } from '@heroui/react'
import { useState } from 'react'

import { listInstanceLogs } from '../../api/instances'
import { useAsync } from '../../hooks/useAsync'
import { formatDateTime } from '../../lib/format'
import { ACTOR_TYPE_LABELS, instanceActionLabel } from '../../lib/instanceStatus'
import { DEFAULT_PAGE_SIZE } from '../../lib/pagination'
import { Timeline, type TimelineItem } from '../common/InfoList'
import Pager from '../common/Pager'
import { EmptyBlock, ErrorState, LoadingBlock } from '../common/PageState'

// InstanceLogsCard：实例操作记录（契约 15.4）。成功与失败尝试都会留痕，这里按时间倒序展示。
export default function InstanceLogsCard({ instanceId }: { instanceId: number }) {
  const [page, setPage] = useState(1)
  const logsState = useAsync(
    () => listInstanceLogs(instanceId, { page, page_size: DEFAULT_PAGE_SIZE }),
    [instanceId, page],
  )
  const logs = logsState.data?.items ?? []

  const items: TimelineItem[] = logs.map((log) => ({
    key: String(log.id),
    title: (
      <span className="flex flex-wrap items-center gap-2">
        <span>{instanceActionLabel(log.action)}</span>
        <span
          className={log.status === 'success' ? 'text-xs text-success' : 'text-xs text-danger'}
        >
          {log.status === 'success' ? '成功' : '失败'}
        </span>
        <span className="text-xs text-muted">
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
      <Card.Header>
        <Card.Title className="text-base">操作记录</Card.Title>
        <Card.Description>电源、重装、改密、续费、终止等操作的留痕（含失败尝试）。</Card.Description>
      </Card.Header>
      <Card.Content className="space-y-4">
        {logsState.loading ? <LoadingBlock label="正在读取操作记录…" /> : null}
        {logsState.error ? (
          <ErrorState message={logsState.error} onRetry={logsState.reload} />
        ) : null}
        {!logsState.loading && !logsState.error && items.length === 0 ? (
          <EmptyBlock title="暂无操作记录" />
        ) : null}
        {items.length > 0 ? <Timeline items={items} /> : null}
        {logsState.data && logsState.data.total > 0 ? (
          <Pager page={page} total={logsState.data.total} onChange={setPage} />
        ) : null}
      </Card.Content>
    </Card>
  )
}
