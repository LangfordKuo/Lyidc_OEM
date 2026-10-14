import { useState, type FormEvent } from 'react'
import { useSearchParams } from 'react-router-dom'
import { AlertCircleIcon, InfoIcon } from 'lucide-react'
import { toast } from 'sonner'

import { clearOperationLogs, listOperationLogs } from '@/api/adminOperationLogs'
import { errorMessage } from '@/api/client'
import type { OperationLog, OplogChannel, OplogStatus } from '@/api/types'
import { useAdminAuth } from '@/auth/adminAuthContext'
import AdminTable, { type AdminColumn } from '@/components/admin/AdminTable'
import { AdminFilterBar } from '@/components/admin/FilterSelect'
import NoPermission from '@/components/admin/NoPermission'
import ConfirmDialog from '@/components/common/ConfirmDialog'
import Pager from '@/components/common/Pager'
import { EmptyBlock, ErrorState, LoadingBlock } from '@/components/common/PageState'
import StatusBadge from '@/components/common/StatusBadge'
import StatusFilter from '@/components/common/StatusFilter'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useAsync } from '@/hooks/useAsync'
import { hasPermission } from '@/lib/adminRoles'
import { formatDateTime } from '@/lib/format'
import { ADMIN_PAGE_SIZE } from '@/lib/pagination'
import {
  OPLOG_CHANNEL_LABELS,
  OPLOG_LARGE_RESULT_THRESHOLD,
  OPLOG_STATUS_LABELS,
  oplogActorName,
  oplogLargeResultHint,
  oplogStatusTone,
} from '@/lib/oplogText'

// AdminOperationLogs 是管理后台「日志」页（契约 23.4 / 23.8 / 23.11，R18 批 2）：
//   侧栏「日志」展开后的三项（用户 / 定时任务 / 管理员日志）都进入本页，只是预置的 channel 不同。
//   列表：时间 / 操作人 / 动作 / 方法 + 路径 / 结果 / IP / 耗时 / 摘要 + 分页；
//   高级搜索：关键字（≥2 字符）/ 时间范围（可留空 = 全表）/ 结果 / 操作人 actor_id，可一键清空；
//   清空：按当前通道（二次确认）+ 清空全部（更强确认，写明不可恢复）。
// **没有**按条删除或编辑：后端也没有对应接口（契约 23.4.3），界面绝不提供这类控件。
// 权限：仅 admin（finance / support 连直接敲 URL 也只得到无权占位，且不发任何请求）。

type StatusValue = '' | OplogStatus
type ClearTarget = '' | OplogChannel | 'all'

const STATUS_OPTIONS: { value: OplogStatus; label: string }[] = [
  { value: 'success', label: OPLOG_STATUS_LABELS.success },
  { value: 'failed', label: OPLOG_STATUS_LABELS.failed },
]

/** 从 URL 读取通道：只认三个合法取值，其它一律当「未指定」。 */
function channelFromParam(raw: string | null): '' | OplogChannel {
  return raw === 'admin' || raw === 'member' || raw === 'scheduler' ? raw : ''
}

/** 关键字的合法性口径与后端一致（契约 23.5）：每个词至少 2 个字符（中文按字、英文按字母）。 */
function keywordError(keyword: string): string {
  if (!keyword) {
    return ''
  }
  const terms = keyword.split(/\s+/).filter(Boolean)
  if (terms.length === 0 || terms.some((term) => [...term].length < 2)) {
    return '关键字至少 2 个字符（中文按字、英文按字母），且不能包含单字符的词'
  }
  return ''
}

export default function AdminOperationLogs() {
  const { role } = useAdminAuth()
  const allowed = hasPermission(role, 'logs.read')
  const [searchParams, setSearchParams] = useSearchParams()

  // 通道与操作人从 URL 预置（契约 23.11）：链接可分享、可刷新；侧栏三项即带 channel 的链接。
  const channel = channelFromParam(searchParams.get('channel'))
  const actorParamRaw = (searchParams.get('actor_id') ?? '').trim()
  const actorParam = /^\d+$/.test(actorParamRaw) ? actorParamRaw : ''

  const [keywordDraft, setKeywordDraft] = useState('')
  const [startDraft, setStartDraft] = useState('')
  const [endDraft, setEndDraft] = useState('')
  const [actorDraft, setActorDraft] = useState(actorParam)
  const [status, setStatus] = useState<StatusValue>('')
  // applied 是**已提交**的筛选（只有查询/回车才落到这里，避免边打字边请求）。
  const [applied, setApplied] = useState({ keyword: '', start: '', end: '', actorID: actorParam })
  const [formError, setFormError] = useState('')
  const [page, setPage] = useState(1)
  const [refreshKey, setRefreshKey] = useState(0)
  const [clearTarget, setClearTarget] = useState<ClearTarget>('')
  const [clearing, setClearing] = useState(false)
  const [clearError, setClearError] = useState('')

  // URL 预置变化（换通道 / 从会员页带 actor_id 进入）时**在渲染期就地纠正**派生状态：
  // 回到第 1 页、把操作人筛选同步成 URL 的值。用渲染期调整（React 官方推荐的
  //「prop 变化时调整 state」写法）而不是 effect，避免「先按旧页码发一次请求、再重发」的多余请求。
  const presetKey = `${channel}\u0000${actorParam}`
  const [preset, setPreset] = useState(presetKey)
  if (preset !== presetKey) {
    setPreset(presetKey)
    setPage(1)
    setActorDraft(actorParam)
    setApplied((prev) => ({ ...prev, actorID: actorParam }))
  }

  const listState = useAsync(
    () =>
      listOperationLogs({
        page,
        page_size: ADMIN_PAGE_SIZE,
        ...(channel ? { channel } : {}),
        ...(status ? { status } : {}),
        ...(applied.keyword ? { keyword: applied.keyword } : {}),
        ...(applied.start ? { start: applied.start } : {}),
        ...(applied.end ? { end: applied.end } : {}),
        ...(applied.actorID !== '' ? { actor_id: Number(applied.actorID) } : {}),
      }),
    [page, channel, status, applied.keyword, applied.start, applied.end, applied.actorID, refreshKey],
    allowed,
  )
  const logs = listState.data?.items ?? []
  const total = listState.data?.total ?? 0
  const hasFilters = Boolean(
    applied.keyword || applied.start || applied.end || applied.actorID !== '' || status,
  )

  if (!allowed) {
    // 非 admin：整页占位，侧栏也没有入口；上面的 useAsync 被 enabled=false 关掉，一个请求都不发。
    return <NoPermission permission="logs.read" title="无权访问操作日志" />
  }

  /** 操作人写回 URL（其余筛选是本地状态）；channel 保持当前值不变。 */
  const applyActorParam = (actorID: string) => {
    const next = new URLSearchParams()
    if (channel) {
      next.set('channel', channel)
    }
    if (actorID) {
      next.set('actor_id', actorID)
    }
    setSearchParams(next, { replace: true })
  }

  const handleSearch = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const keyword = keywordDraft.trim()
    const bad = keywordError(keyword)
    if (bad) {
      setFormError(bad)
      return
    }
    if (startDraft && endDraft && startDraft > endDraft) {
      setFormError('开始日期不能晚于结束日期')
      return
    }
    const actorID = actorDraft.trim()
    if (actorID && !/^\d+$/.test(actorID)) {
      setFormError('操作人 ID 必须为非负整数（0 = 系统 / 未登录尝试）')
      return
    }
    setFormError('')
    setApplied({ keyword, start: startDraft, end: endDraft, actorID })
    setPage(1)
    applyActorParam(actorID)
  }

  const handleResetFilters = () => {
    setKeywordDraft('')
    setStartDraft('')
    setEndDraft('')
    setActorDraft('')
    setStatus('')
    setApplied({ keyword: '', start: '', end: '', actorID: '' })
    setFormError('')
    setPage(1)
    applyActorParam('')
  }

  const openClear = (target: ClearTarget) => {
    setClearError('')
    setClearTarget(target)
  }

  const confirmClear = async () => {
    if (!clearTarget) {
      return
    }
    setClearing(true)
    setClearError('')
    try {
      const result = await clearOperationLogs(clearTarget)
      const scope = result.channel === 'all' ? '全部通道' : OPLOG_CHANNEL_LABELS[result.channel]
      toast.success(
        `已清空${scope}，共删除 ${result.deleted} 条日志（留痕写入${OPLOG_CHANNEL_LABELS[result.record_channel]}）`,
      )
      setClearTarget('')
      setPage(1)
      setRefreshKey((value) => value + 1)
    } catch (error) {
      // 失败保持弹窗打开并展示后端原因（成功才关闭，与仓库其它二次确认一致）。
      setClearError(errorMessage(error, '清空日志失败，请稍后重试'))
    } finally {
      setClearing(false)
    }
  }

  const columns: AdminColumn<OperationLog>[] = [
    {
      key: 'created_at',
      header: '时间',
      className: 'whitespace-nowrap',
      cell: (log) => (
        <span className="text-xs text-muted-foreground">{formatDateTime(log.created_at)}</span>
      ),
    },
    {
      key: 'actor',
      header: '操作人',
      className: 'whitespace-nowrap',
      cell: (log) => (
        <span className="flex flex-col">
          <span>{oplogActorName(log)}</span>
          {log.actor_id > 0 ? (
            <span className="text-xs text-muted-foreground">#{log.actor_id}</span>
          ) : null}
        </span>
      ),
    },
    {
      key: 'action',
      header: '动作',
      className: 'whitespace-nowrap',
      cell: (log) => log.action || '—',
    },
    {
      key: 'route',
      header: '方法 + 路径',
      cell: (log) =>
        log.method || log.path ? (
          <span className="flex flex-col gap-0.5">
            <span className="font-mono text-xs text-muted-foreground">{log.method || '—'}</span>
            <span className="font-mono text-xs break-all">{log.path || '—'}</span>
          </span>
        ) : (
          <span className="text-muted-foreground">—</span>
        ),
    },
    {
      key: 'status',
      header: '结果',
      className: 'whitespace-nowrap',
      cell: (log) => (
        <span className="flex flex-col gap-1">
          <StatusBadge label={OPLOG_STATUS_LABELS[log.status]} tone={oplogStatusTone(log.status)} />
          {log.http_status > 0 ? (
            <span className="text-xs text-muted-foreground">HTTP {log.http_status}</span>
          ) : null}
        </span>
      ),
    },
    {
      key: 'ip',
      header: 'IP',
      className: 'whitespace-nowrap',
      cell: (log) => <span className="font-mono text-xs">{log.ip || '—'}</span>,
    },
    {
      key: 'duration_ms',
      header: '耗时',
      className: 'text-right whitespace-nowrap',
      cell: (log) => <span className="text-xs">{log.duration_ms} ms</span>,
    },
    {
      key: 'summary',
      header: '摘要',
      cell: (log) => (
        <span className="block max-w-[28rem] text-xs whitespace-pre-wrap text-muted-foreground">
          {log.summary || '—'}
        </span>
      ),
    },
  ]

  const channelLabel = channel ? OPLOG_CHANNEL_LABELS[channel] : '全部通道'
  const showLargeHint = Boolean(
    applied.keyword && !listState.loading && total >= OPLOG_LARGE_RESULT_THRESHOLD,
  )

  return (
    <div className="space-y-5">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-foreground">操作日志</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            当前通道：{channelLabel}
            {channel ? '（由侧栏「日志」的入口或链接参数决定）' : '（未指定通道，三通道一起查）'}
            ；只允许整体清空，没有按条删除或修改。
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={() => setRefreshKey((value) => value + 1)}>
          刷新
        </Button>
      </header>

      <form onSubmit={handleSearch} noValidate>
        <AdminFilterBar
          actions={
            <span className="text-xs text-muted-foreground">
              筛选条件变化后回到第 1 页；时间按 UTC 解释，结束日期含当天 23:59:59。
            </span>
          }
        >
          <div className="space-y-1">
            <Label htmlFor="oplog_keyword" className="text-xs text-muted-foreground">
              关键字
            </Label>
            <Input
              id="oplog_keyword"
              name="oplog_keyword"
              className="w-56"
              placeholder="至少 2 个字符"
              value={keywordDraft}
              onChange={(event) => {
                setKeywordDraft(event.target.value)
                setFormError('')
              }}
            />
            <p className="text-xs text-muted-foreground">
              搜索动作、摘要与操作人用户名；<span className="text-foreground">至少 2 个字符</span>
              （中文按字、英文按字母），多个词要同时命中。
            </p>
          </div>
          <div className="space-y-1">
            <Label htmlFor="oplog_start" className="text-xs text-muted-foreground">
              开始日期
            </Label>
            <Input
              id="oplog_start"
              name="oplog_start"
              type="date"
              className="w-40"
              value={startDraft}
              onChange={(event) => {
                setStartDraft(event.target.value)
                setFormError('')
              }}
            />
          </div>
          <div className="space-y-1">
            <Label htmlFor="oplog_end" className="text-xs text-muted-foreground">
              结束日期
            </Label>
            <Input
              id="oplog_end"
              name="oplog_end"
              type="date"
              className="w-40"
              value={endDraft}
              onChange={(event) => {
                setEndDraft(event.target.value)
                setFormError('')
              }}
            />
          </div>
          <div className="space-y-1">
            <Label htmlFor="oplog_actor" className="text-xs text-muted-foreground">
              操作人 ID
            </Label>
            <Input
              id="oplog_actor"
              name="oplog_actor"
              className="w-40"
              inputMode="numeric"
              placeholder="例如 7（0 = 系统）"
              value={actorDraft}
              onChange={(event) => {
                setActorDraft(event.target.value)
                setFormError('')
              }}
            />
          </div>
          <Button type="submit" size="sm">
            查询
          </Button>
          <Button type="button" variant="outline" size="sm" onClick={handleResetFilters}>
            清空筛选
          </Button>
        </AdminFilterBar>
      </form>

      {formError ? (
        <Alert variant="destructive">
          <AlertCircleIcon aria-hidden />
          <AlertDescription>{formError}</AlertDescription>
        </Alert>
      ) : null}

      <StatusFilter
        label="按结果筛选"
        options={STATUS_OPTIONS}
        value={status}
        onChange={(next) => {
          setStatus(next)
          setPage(1)
        }}
      />

      <section className="space-y-3 rounded-xl border border-border px-4 py-3">
        <div>
          <h2 className="text-sm font-medium text-foreground">清空日志</h2>
          <p className="mt-0.5 text-xs text-muted-foreground">
            清空按通道进行，清空后不可恢复（本表不做自动清理，也不会提供按条删除或修改）。
            {channel
              ? `当前通道：${channelLabel}。`
              : '未指定通道时不能按通道清空——请从侧栏「日志」进入某一通道，或使用「清空全部」。'}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={!channel}
            title={channel ? undefined : '未指定通道：请从侧栏进入具体通道日志'}
            onClick={() => openClear(channel)}
          >
            清空当前通道
          </Button>
          <Button variant="destructive" size="sm" onClick={() => openClear('all')}>
            清空全部
          </Button>
        </div>
      </section>

      {showLargeHint ? (
        <Alert>
          <InfoIcon aria-hidden />
          <AlertTitle>命中较多</AlertTitle>
          <AlertDescription>{oplogLargeResultHint(total)}</AlertDescription>
        </Alert>
      ) : null}

      {listState.loading ? <LoadingBlock label="正在读取操作日志…" /> : null}
      {listState.error ? <ErrorState message={listState.error} onRetry={listState.reload} /> : null}

      {!listState.loading && !listState.error ? (
        <AdminTable
          ariaLabel="操作日志列表"
          columns={columns}
          rows={logs}
          rowKey={(log) => log.id}
          empty={
            <EmptyBlock
              title={hasFilters ? '没有匹配的日志' : '暂无日志'}
              description={
                hasFilters
                  ? '换个关键字、时间范围或结果筛选再试；关键字至少 2 个字符。'
                  : '该通道还没有留痕：写操作（含登录失败与定时任务每轮扫描）发生后会出现在这里。'
              }
            />
          }
        />
      ) : null}

      {listState.data && total > 0 ? (
        <Pager page={page} total={total} pageSize={listState.data.page_size || ADMIN_PAGE_SIZE} onChange={setPage} />
      ) : null}

      <ConfirmDialog
        open={clearTarget !== ''}
        onOpenChange={(open) => {
          if (!open) {
            setClearTarget('')
            setClearError('')
          }
        }}
        title={clearTarget === 'all' ? '清空全部通道的操作日志' : '清空当前通道的操作日志'}
        description={
          clearTarget === 'all' ? (
            <span>
              将删除<span className="text-foreground">全部三个通道</span>（管理员 / 用户 / 定时任务）的
              <span className="text-foreground">所有</span>操作日志，
              <span className="text-foreground">清空后不可恢复</span>
              ，且自增 ID 会从 1 重新开始（TRUNCATE）。请先确认这些日志确实不再需要。
            </span>
          ) : clearTarget ? (
            <span>
              将删除「{OPLOG_CHANNEL_LABELS[clearTarget]}」通道的
              <span className="text-foreground">全部日志</span>，
              <span className="text-foreground">清空后不可恢复</span>
              ；其它通道不受影响，清空动作本身会在未被清空的通道留痕。
            </span>
          ) : undefined
        }
        confirmLabel={clearTarget === 'all' ? '确认清空全部' : '确认清空'}
        danger
        pending={clearing}
        onConfirm={confirmClear}
      >
        {clearError ? (
          <Alert variant="destructive">
            <AlertCircleIcon aria-hidden />
            <AlertDescription>{clearError}</AlertDescription>
          </Alert>
        ) : null}
      </ConfirmDialog>
    </div>
  )
}
