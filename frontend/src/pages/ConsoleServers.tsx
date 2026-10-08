import { useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { listInstances } from '@/api/instances'
import type { InstanceStatus, InstanceSummary } from '@/api/types'
import { paths } from '@/app/paths'
import Pager from '@/components/common/Pager'
import { CardSkeletonGrid, EmptyBlock, ErrorState } from '@/components/common/PageState'
import StatusBadge from '@/components/common/StatusBadge'
import StatusFilter from '@/components/common/StatusFilter'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { useAsync } from '@/hooks/useAsync'
import { expiryColorClass, expiryState, expiryText } from '@/lib/expiry'
import { formatCycleLabel, formatDateTimeOr } from '@/lib/format'
import { INSTANCE_STATUS_FILTERS, cancelStatusLabel, instanceStatusLabel, instanceStatusTone } from '@/lib/instanceStatus'
import { CONSOLE_PAGE_SIZE } from '@/lib/pagination'

type StatusFilterValue = '' | InstanceStatus

// ConsoleServers 是会员区「我的服务器」列表页：状态筛选 + 分页 + 到期时间临期高亮（契约 14.4）。
export default function ConsoleServers() {
  const navigate = useNavigate()
  const [status, setStatus] = useState<StatusFilterValue>('')
  const [page, setPage] = useState(1)

  const instancesState = useAsync(
    () =>
      listInstances({
        page,
        page_size: CONSOLE_PAGE_SIZE,
        ...(status ? { status } : {}),
      }),
    [page, status],
  )
  const instances = instancesState.data?.items ?? []

  const changeStatus = (next: StatusFilterValue) => {
    setStatus(next)
    setPage(1)
  }

  return (
    <div className="space-y-5">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold text-foreground">我的服务器</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            查看实例状态与到期时间，执行开关机、重装、改密、续费与终止申请等操作。
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={instancesState.reload}>
            刷新
          </Button>
          <Button size="sm" onClick={() => navigate(paths.products)}>
            选购服务器
          </Button>
        </div>
      </header>

      <StatusFilter
        label="按状态筛选"
        options={INSTANCE_STATUS_FILTERS}
        value={status}
        onChange={changeStatus}
      />

      {instancesState.loading ? <CardSkeletonGrid count={3} /> : null}
      {instancesState.error ? (
        <ErrorState message={instancesState.error} onRetry={instancesState.reload} />
      ) : null}

      {!instancesState.loading && !instancesState.error && instances.length === 0 ? (
        <EmptyBlock
          title={status ? '该状态下没有实例' : '还没有服务器'}
          description={
            status ? (
              '换个状态筛选看看，或查看全部实例。'
            ) : (
              <span>
                还没有已开通的实例，去
                <button
                  type="button"
                  className="mx-1 text-primary hover:underline"
                  onClick={() => navigate(paths.products)}
                >
                  选购商品
                </button>
                下单后实例会自动开通到这里。
              </span>
            )
          }
        />
      ) : null}

      <div className="space-y-3">
        {instances.map((instance) => (
          <InstanceCard key={instance.id} instance={instance} />
        ))}
      </div>

      {instancesState.data ? (
        <Pager
          page={page}
          total={instancesState.data.total}
          pageSize={instancesState.data.page_size || CONSOLE_PAGE_SIZE}
          onChange={setPage}
        />
      ) : null}
    </div>
  )
}

function InstanceCard({ instance }: { instance: InstanceSummary }) {
  const navigate = useNavigate()
  const state = expiryState(instance.next_due_date)
  const openDetail = () => navigate(paths.consoleServerDetail(instance.id))

  return (
    <Card data-testid={`instance-card-${instance.id}`}>
      <CardContent className="space-y-3">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <button type="button" className="min-w-0 text-left" onClick={openDetail}>
            <span className="block truncate font-mono text-sm font-medium text-foreground hover:text-primary">
              {instance.name}
            </span>
            <span className="mt-0.5 block text-xs text-muted-foreground">
              {instance.product_name} · {formatCycleLabel(instance.billing_cycle)}
            </span>
          </button>
          <div className="flex flex-wrap items-center gap-2">
            <StatusBadge
              tone={instanceStatusTone(instance.status)}
              label={instanceStatusLabel(instance.status)}
            />
            {instance.cancel_status === 'pending' ? (
              <StatusBadge tone="warn" label={cancelStatusLabel(instance.cancel_status)} />
            ) : null}
          </div>
        </div>

        <div className="grid gap-2 text-xs sm:grid-cols-3">
          <span className="text-muted-foreground">
            IP：<span className="text-foreground">{instance.dedicated_ip || '—'}</span>
          </span>
          <span className="text-muted-foreground">
            到期：
            <span className={expiryColorClass(state)}>
              {formatDateTimeOr(instance.next_due_date)}
              {instance.next_due_date ? ` · ${expiryText(instance.next_due_date)}` : ''}
            </span>
          </span>
          <span className="text-muted-foreground">
            上游状态：<span className="text-foreground">{instance.upstream_status || '—'}</span>
          </span>
        </div>

        <div className="flex flex-wrap gap-2">
          <Button size="sm" variant="outline" onClick={openDetail}>
            查看详情与操作
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
