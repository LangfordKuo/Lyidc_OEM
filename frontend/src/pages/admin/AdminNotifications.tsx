import { Button, Card, toast } from '@heroui/react'
import { useEffect, useState } from 'react'

import {
  listAdminNotifications,
  readAdminNotification,
  readAllAdminNotifications,
} from '../../api/adminNotifications'
import { errorMessage } from '../../api/client'
import type { Notification } from '../../api/types'
import { useAdminAuth } from '../../auth/adminAuthContext'
import StatusBadge from '../../components/StatusBadge'
import Pager from '../../components/common/Pager'
import { EmptyBlock, ErrorState, LoadingBlock } from '../../components/common/PageState'
import StatusFilter from '../../components/common/StatusFilter'
import { setAdminUnreadCount, useAdminUnreadCount } from '../../hooks/useAdminUnreadCount'
import { useAsync } from '../../hooks/useAsync'
import { formatDateTime } from '../../lib/format'
import { notificationEventLabel, notificationEventTone } from '../../lib/notificationText'
import { DEFAULT_PAGE_SIZE } from '../../lib/pagination'

// AdminNotifications 是管理后台「通知」页（契约 17.2）：与会员区 ConsoleNotifications **同构**，
// 读的是**当前管理员本人**的收件箱（工单事件等；与会员端通知相互独立）。
// 三类角色都可访问（通知是个人收件箱，不属工单域），故不设权限闸门；
// 列表响应回带的未读数会同步到全局 store，驱动后台侧栏徽章。

type UnreadFilter = '' | 'unread'

const FILTER_OPTIONS: { value: UnreadFilter; label: string }[] = [
  { value: '', label: '全部' },
  { value: 'unread', label: '未读' },
]

export default function AdminNotifications() {
  const { role } = useAdminAuth()
  const [filter, setFilter] = useState<UnreadFilter>('')
  const [page, setPage] = useState(1)
  const [refreshKey, setRefreshKey] = useState(0)
  const [markingAll, setMarkingAll] = useState(false)
  const [readingId, setReadingId] = useState<number | null>(null)
  const unreadCount = useAdminUnreadCount()

  const listState = useAsync(
    () =>
      listAdminNotifications({
        page,
        page_size: DEFAULT_PAGE_SIZE,
        ...(filter === 'unread' ? { unread: true } : {}),
      }),
    [page, filter, refreshKey],
  )
  const notifications = listState.data?.items ?? []

  // 列表响应自带未读数：同步到管理端 store，让侧栏通知徽章即时更新。
  useEffect(() => {
    if (listState.data) {
      setAdminUnreadCount(listState.data.unread)
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
      await readAdminNotification(notification.id)
      // 重新拉列表：既刷新已读样式，也顺带同步未读数（列表响应回带 unread）。
      setRefreshKey((value) => value + 1)
    } catch (error) {
      toast.danger(errorMessage(error, '标记已读失败'))
    } finally {
      setReadingId(null)
    }
  }

  const handleReadAll = async () => {
    setMarkingAll(true)
    try {
      const result = await readAllAdminNotifications()
      setAdminUnreadCount(result.unread)
      setRefreshKey((value) => value + 1)
      toast.success(result.updated > 0 ? `已将 ${result.updated} 条通知标为已读` : '没有未读通知')
    } catch (error) {
      toast.danger(errorMessage(error, '全部已读失败'))
    } finally {
      setMarkingAll(false)
    }
  }

  return (
    <div className="space-y-5">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold text-foreground">通知</h1>
          <p className="mt-1 text-sm text-muted">
            这里是<strong className="font-medium text-foreground">管理员本人</strong>
            的收件箱（与会员端通知相互独立）：工单提交、客服回复等事件会推送给管理员与客服；
            {role === 'finance' ? '财务角色通常不接收工单类通知，为空属于正常。' : '点击条目即标记为已读。'}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <span className="text-sm text-muted">
            未读 <span className="font-medium text-foreground">{unreadCount}</span> 条
          </span>
          <Button
            variant="outline"
            size="sm"
            isDisabled={markingAll || unreadCount === 0}
            onPress={handleReadAll}
          >
            {markingAll ? '处理中…' : '全部已读'}
          </Button>
          <Button variant="outline" size="sm" onPress={() => listState.reload()}>
            刷新
          </Button>
        </div>
      </header>

      <StatusFilter label="按未读筛选" options={FILTER_OPTIONS} value={filter} onChange={changeFilter} />

      {listState.loading ? <LoadingBlock label="正在读取通知…" /> : null}
      {listState.error ? (
        <ErrorState message={listState.error} onRetry={listState.reload} />
      ) : null}

      {!listState.loading && !listState.error && notifications.length === 0 ? (
        <EmptyBlock
          title={filter === 'unread' ? '没有未读通知' : '暂无通知'}
          description={
            filter === 'unread'
              ? '所有通知都已读完，新的工单动态会在这里出现。'
              : role === 'finance'
                ? '财务角色的收件箱通常为空：工单类通知只推送给超级管理员与客服。'
                : '工单提交、客服回复等事件会推送到这里。'
          }
        />
      ) : null}

      <div className="space-y-3">
        {notifications.map((notification) => (
          <Card
            key={notification.id}
            variant={notification.read ? 'default' : 'secondary'}
            className={notification.read ? '' : 'border-l-2 border-l-accent'}
          >
            <Card.Content className="space-y-2">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="flex min-w-0 items-center gap-2">
                  {!notification.read ? (
                    <span className="size-2 shrink-0 rounded-full bg-accent" aria-label="未读" />
                  ) : null}
                  <span
                    className={[
                      'truncate text-sm',
                      notification.read ? 'text-muted' : 'font-medium text-foreground',
                    ].join(' ')}
                  >
                    {notification.title}
                  </span>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  <StatusBadge
                    tone={notificationEventTone(notification.event)}
                    label={notificationEventLabel(notification.event)}
                  />
                  {!notification.read ? (
                    <Button
                      size="sm"
                      variant="ghost"
                      isDisabled={readingId === notification.id}
                      onPress={() => handleRead(notification)}
                    >
                      {readingId === notification.id ? '处理中…' : '标记已读'}
                    </Button>
                  ) : null}
                </div>
              </div>
              <p className="text-sm whitespace-pre-wrap text-muted">{notification.content}</p>
              <p className="text-xs text-muted">
                {formatDateTime(notification.created_at)}
                {notification.read && notification.read_at
                  ? ` · 已于 ${formatDateTime(notification.read_at)} 读`
                  : ''}
              </p>
            </Card.Content>
          </Card>
        ))}
      </div>

      {listState.data && listState.data.total > 0 ? (
        <Pager
          page={page}
          total={listState.data.total}
          pageSize={listState.data.page_size || DEFAULT_PAGE_SIZE}
          onChange={setPage}
        />
      ) : null}
    </div>
  )
}
