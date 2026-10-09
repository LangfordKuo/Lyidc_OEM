import { useEffect, useState } from 'react'
import { toast } from 'sonner'

import { errorMessage } from '@/api/client'
import { listNotifications, readAllNotifications, readNotification } from '@/api/notifications'
import type { Notification } from '@/api/types'
import Pager from '@/components/common/Pager'
import { EmptyBlock, ErrorState, LoadingBlock } from '@/components/common/PageState'
import StatusBadge from '@/components/common/StatusBadge'
import StatusFilter from '@/components/common/StatusFilter'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { useAsync } from '@/hooks/useAsync'
import { refreshUnreadCount, setUnreadCount, useUnreadCount } from '@/hooks/useUnreadCount'
import { formatDateTime } from '@/lib/format'
import { notificationEventLabel, notificationEventTone } from '@/lib/notificationText'
import { CONSOLE_PAGE_SIZE } from '@/lib/pagination'
import { cn } from '@/lib/utils'

type UnreadFilter = '' | 'unread'

const FILTER_OPTIONS: { value: UnreadFilter; label: string }[] = [
  { value: 'unread', label: '未读' },
]

// ConsoleNotifications 是会员区「通知」（契约 17.2）：未读筛选 + 分页列表 + 点击即读 + 全部已读。
export default function ConsoleNotifications() {
  const [filter, setFilter] = useState<UnreadFilter>('')
  const [page, setPage] = useState(1)
  const [refreshKey, setRefreshKey] = useState(0)
  const [markingAll, setMarkingAll] = useState(false)
  const [readingId, setReadingId] = useState<number | null>(null)
  const unreadCount = useUnreadCount()

  const listState = useAsync(
    () =>
      listNotifications({
        page,
        page_size: CONSOLE_PAGE_SIZE,
        ...(filter === 'unread' ? { unread: true } : {}),
      }),
    [page, filter, refreshKey],
  )
  const notifications = listState.data?.items ?? []

  // 列表响应自带未读数：同步到全局 store，让侧栏徽章即时更新。
  useEffect(() => {
    if (listState.data) {
      setUnreadCount(listState.data.unread)
    }
  }, [listState.data])

  const changeFilter = (next: UnreadFilter) => {
    setFilter(next)
    setPage(1)
  }

  const handleRead = async (notification: Notification) => {
    if (notification.read || readingId !== null) {
      return
    }
    setReadingId(notification.id)
    try {
      await readNotification(notification.id)
      setRefreshKey((value) => value + 1)
      await refreshUnreadCount()
    } catch (error) {
      toast.error(errorMessage(error, '标记已读失败'))
    } finally {
      setReadingId(null)
    }
  }

  const handleReadAll = async () => {
    setMarkingAll(true)
    try {
      const result = await readAllNotifications()
      setUnreadCount(result.unread)
      setRefreshKey((value) => value + 1)
      toast.success(result.updated > 0 ? `已将 ${result.updated} 条通知标为已读` : '没有未读通知')
    } catch (error) {
      toast.error(errorMessage(error, '全部已读失败'))
    } finally {
      setMarkingAll(false)
    }
  }

  return (
    <div className="space-y-5">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-foreground">通知</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            订单交付、续费、实例暂停与工单回复等事件的站内通知，点击条目即标记为已读。
          </p>
        </div>
        <div className="flex items-center gap-2">
          <span className="text-sm text-muted-foreground">
            未读{' '}
            <span className="font-medium text-foreground" data-testid="unread-count">
              {unreadCount}
            </span>{' '}
            条
          </span>
          <Button
            variant="outline"
            size="sm"
            disabled={markingAll || unreadCount === 0}
            onClick={handleReadAll}
          >
            {markingAll ? '处理中…' : '全部已读'}
          </Button>
          <Button variant="outline" size="sm" onClick={() => listState.reload()}>
            刷新
          </Button>
        </div>
      </header>

      <StatusFilter
        label="按未读筛选"
        options={FILTER_OPTIONS}
        value={filter}
        onChange={changeFilter}
      />

      {listState.loading ? <LoadingBlock label="正在读取通知…" /> : null}
      {listState.error ? (
        <ErrorState message={listState.error} onRetry={listState.reload} />
      ) : null}

      {!listState.loading && !listState.error && notifications.length === 0 ? (
        <EmptyBlock
          title={filter === 'unread' ? '没有未读通知' : '暂无通知'}
          description={
            filter === 'unread'
              ? '所有通知都已读完，最新动态会在这里出现。'
              : '订单交付、到期提醒与工单回复等消息会推送到这里。'
          }
        />
      ) : null}

      <div className="space-y-3">
        {notifications.map((notification) => (
          <Card
            key={notification.id}
            className={cn(
              notification.read ? undefined : 'border-l-2 border-l-primary bg-muted/30',
            )}
          >
            <CardContent className="space-y-2">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <button
                  type="button"
                  className="flex min-w-0 items-center gap-2 text-left"
                  aria-label={notification.read ? '已读通知' : '标记为已读'}
                  disabled={notification.read}
                  onClick={() => handleRead(notification)}
                >
                  {!notification.read ? (
                    <span className="size-2 shrink-0 rounded-full bg-primary" aria-hidden />
                  ) : null}
                  <span
                    className={cn(
                      'truncate text-sm',
                      notification.read
                        ? 'text-muted-foreground'
                        : 'font-medium text-foreground',
                    )}
                  >
                    {notification.title}
                  </span>
                </button>
                <div className="flex shrink-0 items-center gap-2">
                  <StatusBadge
                    tone={notificationEventTone(notification.event)}
                    label={notificationEventLabel(notification.event)}
                  />
                  {!notification.read ? (
                    <Button
                      size="sm"
                      variant="ghost"
                      disabled={readingId === notification.id}
                      onClick={() => handleRead(notification)}
                    >
                      {readingId === notification.id ? '处理中…' : '标记已读'}
                    </Button>
                  ) : null}
                </div>
              </div>
              <p className="text-sm whitespace-pre-wrap text-muted-foreground">
                {notification.content}
              </p>
              <p className="text-xs text-muted-foreground">
                {formatDateTime(notification.created_at)}
                {notification.read && notification.read_at
                  ? ` · 已于 ${formatDateTime(notification.read_at)} 读`
                  : ''}
              </p>
            </CardContent>
          </Card>
        ))}
      </div>

      {listState.data ? (
        <Pager
          page={page}
          total={listState.data.total}
          pageSize={listState.data.page_size || CONSOLE_PAGE_SIZE}
          onChange={setPage}
        />
      ) : null}
    </div>
  )
}
