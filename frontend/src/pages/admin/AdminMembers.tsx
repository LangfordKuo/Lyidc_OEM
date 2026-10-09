import { useState, type FormEvent } from 'react'
import { AlertCircleIcon } from 'lucide-react'
import { toast } from 'sonner'

import {
  listAdminLedger,
  listAdminMembers,
  listAdminRecharges,
  updateMemberStatus,
} from '@/api/adminMembers'
import { errorMessage } from '@/api/client'
import type { LedgerEntry, LedgerType, Member, MemberStatus, Recharge } from '@/api/types'
import { useAdminAuth } from '@/auth/adminAuthContext'
import AdminTable, { type AdminColumn } from '@/components/admin/AdminTable'
import FilterSelect, { AdminFilterBar } from '@/components/admin/FilterSelect'
import ConfirmDialog from '@/components/common/ConfirmDialog'
import { InfoList } from '@/components/common/InfoList'
import Pager from '@/components/common/Pager'
import { EmptyBlock, ErrorState, LoadingBlock } from '@/components/common/PageState'
import StatusBadge from '@/components/common/StatusBadge'
import StatusFilter from '@/components/common/StatusFilter'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
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
import { useAsync } from '@/hooks/useAsync'
import { ADMIN_ROLE_LABELS, hasPermission, permissionHint } from '@/lib/adminRoles'
import { ledgerTypeLabel, rechargeStatusLabel, rechargeStatusTone } from '@/lib/financeText'
import { formatDateTime, formatDateTimeOr, formatMoney } from '@/lib/format'
import { memberStatusLabel, memberStatusTone } from '@/lib/memberText'
import { ADMIN_PAGE_SIZE } from '@/lib/pagination'

// AdminMembers 是管理后台「会员」页（契约 6.3 / 6.4 / 12.6 / 12.7）：
//   列表：用户名/邮箱模糊搜索 + 状态筛选 + 分页；操作列可查看详情、启用/禁用。
//   详情：会员资料 + 余额 + 余额流水 + 充值单（后两者属财务对账，support 不请求、只给说明）。
//   角色矩阵：列表三类角色均可读；禁用/启用仅 admin/finance（support 按钮禁用 + 文案提示）；
//   流水与充值单属 finance.reconcile（仅 admin/finance）——与服务端 403 语义一致，界面提前规避。

type StatusFilterValue = '' | MemberStatus

const STATUS_OPTIONS: { value: MemberStatus; label: string }[] = [
  { value: 'active', label: '正常' },
  { value: 'disabled', label: '已禁用' },
]

/** 详情弹窗里的流水/充值单页大小（对账窗口，比列表页小，避免弹窗过长）。 */
const DETAIL_PAGE_SIZE = 10

const LEDGER_TYPE_OPTIONS: { value: '' | LedgerType; label: string }[] = [
  { value: '', label: '全部类型' },
  { value: 'recharge', label: ledgerTypeLabel('recharge') },
  { value: 'order_pay', label: ledgerTypeLabel('order_pay') },
  { value: 'refund', label: ledgerTypeLabel('refund') },
  { value: 'adjust', label: ledgerTypeLabel('adjust') },
]

/** 流水关联单据类型（契约 12.3：ref_type 为 recharge / order）。 */
const REF_TYPE_LABELS: Record<string, string> = {
  recharge: '充值单',
  order: '订单',
}

export default function AdminMembers() {
  const { role } = useAdminAuth()
  const canManageStatus = hasPermission(role, 'members.status')

  // 输入框草稿与「已提交」的筛选条件分开：只有查询/回车才落到 filters 上。
  const [usernameDraft, setUsernameDraft] = useState('')
  const [emailDraft, setEmailDraft] = useState('')
  const [usernameFilter, setUsernameFilter] = useState('')
  const [emailFilter, setEmailFilter] = useState('')
  const [status, setStatus] = useState<StatusFilterValue>('')
  const [page, setPage] = useState(1)
  const [refreshKey, setRefreshKey] = useState(0)

  const [detail, setDetail] = useState<Member | null>(null)
  // 待二次确认的目标会员（null = 未打开确认弹窗）。
  const [pendingToggle, setPendingToggle] = useState<Member | null>(null)
  const [togglePending, setTogglePending] = useState(false)
  const [toggleError, setToggleError] = useState('')

  const listState = useAsync(
    () =>
      listAdminMembers({
        page,
        page_size: ADMIN_PAGE_SIZE,
        ...(usernameFilter ? { username: usernameFilter } : {}),
        ...(emailFilter ? { email: emailFilter } : {}),
        ...(status ? { status } : {}),
      }),
    [page, usernameFilter, emailFilter, status, refreshKey],
  )
  const members = listState.data?.items ?? []
  const hasFilters = Boolean(usernameFilter || emailFilter || status)

  const handleSearch = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setUsernameFilter(usernameDraft.trim())
    setEmailFilter(emailDraft.trim())
    setPage(1)
  }

  const handleReset = () => {
    setUsernameDraft('')
    setEmailDraft('')
    setUsernameFilter('')
    setEmailFilter('')
    setStatus('')
    setPage(1)
  }

  const openToggle = (member: Member) => {
    setToggleError('')
    setPendingToggle(member)
  }

  // 禁用 / 启用：二次确认后提交，成功即关弹窗并刷新列表（失败在弹窗内展示后端 message）。
  const confirmToggle = async () => {
    if (!pendingToggle) {
      return
    }
    const nextStatus: MemberStatus = pendingToggle.status === 'active' ? 'disabled' : 'active'
    setTogglePending(true)
    setToggleError('')
    try {
      const updated = await updateMemberStatus(pendingToggle.id, nextStatus)
      toast.success(
        nextStatus === 'disabled'
          ? `已禁用会员 ${updated.username}`
          : `已启用会员 ${updated.username}`,
      )
      setPendingToggle(null)
      setRefreshKey((value) => value + 1)
      // 详情弹窗若正展示同一会员，同步最新状态（避免详情与列表口径不一致）。
      setDetail((current) => (current && current.id === updated.id ? updated : current))
    } catch (error) {
      setToggleError(
        errorMessage(error, nextStatus === 'disabled' ? '禁用会员失败' : '启用会员失败'),
      )
    } finally {
      setTogglePending(false)
    }
  }

  const columns: AdminColumn<Member>[] = [
    {
      key: 'id',
      header: 'ID',
      className: 'w-16',
      cell: (member) => <span className="text-xs text-muted-foreground">#{member.id}</span>,
    },
    {
      key: 'username',
      header: '用户名',
      cell: (member) => (
        <span className="flex flex-col">
          <span className="font-medium">{member.username}</span>
          {member.nickname ? (
            <span className="text-xs text-muted-foreground">{member.nickname}</span>
          ) : null}
        </span>
      ),
    },
    { key: 'email', header: '邮箱', cell: (member) => member.email },
    {
      key: 'status',
      header: '状态',
      cell: (member) => (
        <StatusBadge
          tone={memberStatusTone(member.status)}
          label={memberStatusLabel(member.status)}
        />
      ),
    },
    {
      key: 'balance',
      header: '余额',
      className: 'text-right',
      cell: (member) => formatMoney(member.balance),
    },
    {
      key: 'created_at',
      header: '注册时间',
      cell: (member) => (
        <span className="text-xs text-muted-foreground">{formatDateTime(member.created_at)}</span>
      ),
    },
    {
      key: 'actions',
      header: '操作',
      className: 'whitespace-nowrap',
      cell: (member) => {
        const disableAction = member.status === 'active'
        return (
          <span className="flex gap-2">
            <Button size="sm" variant="outline" onClick={() => setDetail(member)}>
              详情
            </Button>
            {/* support 无 members.status 权限：入口保留但禁用，原因见页面头部说明 */}
            <Button
              size="sm"
              variant={disableAction ? 'destructive' : 'default'}
              disabled={!canManageStatus}
              title={canManageStatus ? undefined : permissionHint('members.status')}
              onClick={() => openToggle(member)}
            >
              {disableAction ? '禁用' : '启用'}
            </Button>
          </span>
        )
      },
    },
  ]

  return (
    <div className="space-y-5">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-foreground">会员</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            按用户名或邮箱检索会员，查看资料、余额流水与充值单；必要时可禁用账号。
          </p>
          {!canManageStatus ? (
            <p className="mt-1 text-xs text-muted-foreground">
              当前角色（{role ? ADMIN_ROLE_LABELS[role] : '—'}）无权禁用或启用会员：
              {permissionHint('members.status')}，按钮已禁用。
            </p>
          ) : null}
        </div>
        <Button variant="outline" size="sm" onClick={() => setRefreshKey((value) => value + 1)}>
          刷新
        </Button>
      </header>

      <form onSubmit={handleSearch} noValidate>
        <AdminFilterBar
          actions={
            <span className="text-xs text-muted-foreground">
              模糊匹配用户名与邮箱；筛选条件变化后回到第 1 页。
            </span>
          }
        >
          <div className="space-y-1">
            <Label htmlFor="member_username" className="text-xs text-muted-foreground">
              用户名
            </Label>
            <Input
              id="member_username"
              name="member_username"
              className="w-48"
              placeholder="用户名（模糊匹配）"
              value={usernameDraft}
              onChange={(event) => setUsernameDraft(event.target.value)}
            />
          </div>
          <div className="space-y-1">
            <Label htmlFor="member_email" className="text-xs text-muted-foreground">
              邮箱
            </Label>
            <Input
              id="member_email"
              name="member_email"
              className="w-56"
              placeholder="邮箱（模糊匹配）"
              value={emailDraft}
              onChange={(event) => setEmailDraft(event.target.value)}
            />
          </div>
          <Button type="submit" size="sm">
            查询
          </Button>
          <Button type="button" variant="outline" size="sm" onClick={handleReset}>
            重置
          </Button>
        </AdminFilterBar>
      </form>

      <StatusFilter
        label="按状态筛选"
        options={STATUS_OPTIONS}
        value={status}
        onChange={(next) => {
          setStatus(next)
          setPage(1)
        }}
      />

      {listState.loading ? <LoadingBlock label="正在读取会员…" /> : null}
      {listState.error ? <ErrorState message={listState.error} onRetry={listState.reload} /> : null}

      {!listState.loading && !listState.error ? (
        <AdminTable
          ariaLabel="会员列表"
          columns={columns}
          rows={members}
          rowKey={(member) => member.id}
          empty={
            <EmptyBlock
              title={hasFilters ? '没有匹配的会员' : '暂无会员'}
              description={
                hasFilters
                  ? '换个用户名、邮箱或状态筛选再试。'
                  : '会员在官网注册后会自动出现在这里。'
              }
            />
          }
        />
      ) : null}

      {listState.data && listState.data.total > 0 ? (
        <Pager
          page={page}
          total={listState.data.total}
          pageSize={listState.data.page_size || ADMIN_PAGE_SIZE}
          onChange={setPage}
        />
      ) : null}

      {detail ? <MemberDetailDialog member={detail} onClose={() => setDetail(null)} /> : null}

      <ConfirmDialog
        open={pendingToggle !== null}
        onOpenChange={(open) => {
          if (!open) {
            setPendingToggle(null)
            setToggleError('')
          }
        }}
        title={pendingToggle?.status === 'disabled' ? '启用会员' : '禁用会员'}
        description={
          pendingToggle ? (
            <span>
              会员 <span className="text-foreground">{pendingToggle.username}</span>（
              {pendingToggle.email}）
              {pendingToggle.status === 'active'
                ? '：禁用后该会员无法登录，已签发的 token 会立即失效。'
                : '：启用后该会员可重新登录，历史订单、实例与余额不受影响。'}
            </span>
          ) : undefined
        }
        confirmLabel={pendingToggle?.status === 'disabled' ? '确认启用' : '确认禁用'}
        danger={pendingToggle?.status !== 'disabled'}
        pending={togglePending}
        onConfirm={confirmToggle}
      >
        {toggleError ? (
          <Alert variant="destructive">
            <AlertCircleIcon aria-hidden />
            <AlertDescription>{toggleError}</AlertDescription>
          </Alert>
        ) : null}
      </ConfirmDialog>
    </div>
  )
}

/**
 * 会员详情弹窗：资料（InfoList）+ 余额 + 余额流水 + 充值单。
 * 流水与充值单属财务对账（finance.reconcile = admin/finance）：support 打开时**不请求**这两个
 * 接口（useAsync 的 enabled=false，避免无谓的 403），改为展示权限说明。
 */
function MemberDetailDialog({ member, onClose }: { member: Member; onClose: () => void }) {
  const { role } = useAdminAuth()
  const canReconcile = hasPermission(role, 'finance.reconcile')

  const [ledgerPage, setLedgerPage] = useState(1)
  const [ledgerType, setLedgerType] = useState<'' | LedgerType>('')

  const ledgerState = useAsync(
    () =>
      listAdminLedger({
        member_id: member.id,
        page: ledgerPage,
        page_size: DETAIL_PAGE_SIZE,
        ...(ledgerType ? { type: ledgerType } : {}),
      }),
    [member.id, ledgerPage, ledgerType],
    canReconcile,
  )
  const rechargesState = useAsync(
    () => listAdminRecharges({ member_id: member.id, page: 1, page_size: DETAIL_PAGE_SIZE }),
    [member.id],
    canReconcile,
  )

  const ledgerColumns: AdminColumn<LedgerEntry>[] = [
    {
      key: 'created_at',
      header: '时间',
      cell: (entry) => (
        <span className="text-xs text-muted-foreground">{formatDateTime(entry.created_at)}</span>
      ),
    },
    { key: 'type', header: '类型', cell: (entry) => ledgerTypeLabel(entry.type) },
    {
      key: 'amount',
      header: '金额',
      className: 'text-right whitespace-nowrap',
      cell: (entry) => {
        const negative = entry.amount.trim().startsWith('-')
        return (
          <span
            className={
              negative ? 'font-medium text-destructive' : 'font-medium text-success'
            }
          >
            {negative ? '' : '+'}
            {formatMoney(entry.amount)}
          </span>
        )
      },
    },
    {
      key: 'balance',
      header: '余额变化',
      className: 'whitespace-nowrap',
      cell: (entry) => (
        <span className="text-xs text-muted-foreground">
          {formatMoney(entry.balance_before)} → {formatMoney(entry.balance_after)}
        </span>
      ),
    },
    {
      key: 'note',
      header: '备注 / 关联单号',
      cell: (entry) => (
        <span className="flex flex-col">
          <span>{entry.note || '—'}</span>
          {entry.ref_id > 0 ? (
            <span className="text-xs text-muted-foreground">
              关联 {REF_TYPE_LABELS[entry.ref_type] ?? entry.ref_type} #{entry.ref_id}
            </span>
          ) : null}
        </span>
      ),
    },
  ]

  const rechargeColumns: AdminColumn<Recharge>[] = [
    {
      key: 'trade_no',
      header: '单号',
      cell: (recharge) => <span className="font-mono text-xs">{recharge.trade_no}</span>,
    },
    {
      key: 'amount',
      header: '金额',
      className: 'text-right whitespace-nowrap',
      cell: (recharge) => formatMoney(recharge.amount),
    },
    {
      key: 'status',
      header: '状态',
      cell: (recharge) => (
        <StatusBadge
          tone={rechargeStatusTone(recharge.status)}
          label={rechargeStatusLabel(recharge.status)}
        />
      ),
    },
    {
      key: 'created_at',
      header: '创建时间',
      className: 'whitespace-nowrap',
      cell: (recharge) => (
        <span className="text-xs text-muted-foreground">{formatDateTime(recharge.created_at)}</span>
      ),
    },
    {
      key: 'paid_at',
      header: '到账时间',
      className: 'whitespace-nowrap',
      cell: (recharge) => (
        <span className="text-xs text-muted-foreground">{formatDateTimeOr(recharge.paid_at)}</span>
      ),
    },
  ]

  return (
    <Dialog open onOpenChange={(open) => (!open ? onClose() : undefined)}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>会员详情 · {member.username}</DialogTitle>
          <DialogDescription>资料、余额与对账数据（流水/充值单仅 admin 与 finance 可见）。</DialogDescription>
        </DialogHeader>

        <div className="space-y-5">
          <InfoList
            columns={2}
            items={[
              { label: 'ID', value: `#${member.id}` },
              { label: '用户名', value: member.username },
              { label: '昵称', value: member.nickname || '—' },
              { label: '邮箱', value: member.email },
              { label: '手机', value: member.phone || '—' },
              {
                label: '状态',
                value: (
                  <StatusBadge
                    tone={memberStatusTone(member.status)}
                    label={memberStatusLabel(member.status)}
                  />
                ),
              },
              { label: '注册时间', value: formatDateTime(member.created_at) },
              { label: '最近登录', value: formatDateTimeOr(member.last_login_at) },
            ]}
          />

          <div className="rounded-xl border border-border px-4 py-3">
            <p className="text-xs text-muted-foreground">账户余额</p>
            <p className="mt-1 text-2xl font-semibold text-foreground">
              {formatMoney(member.balance)}
            </p>
          </div>

          {canReconcile ? (
            <>
              <section className="space-y-3">
                <div className="flex flex-wrap items-end justify-between gap-3">
                  <div>
                    <h3 className="text-sm font-medium text-foreground">余额流水</h3>
                    <p className="mt-0.5 text-xs text-muted-foreground">
                      入账为正、出账为负；每笔都记录变动前后余额。
                    </p>
                  </div>
                  <FilterSelect
                    label="流水类型"
                    value={ledgerType}
                    options={LEDGER_TYPE_OPTIONS}
                    onChange={(value) => {
                      setLedgerType(value)
                      setLedgerPage(1)
                    }}
                  />
                </div>

                {ledgerState.loading ? <LoadingBlock label="正在读取余额流水…" /> : null}
                {ledgerState.error ? (
                  <ErrorState message={ledgerState.error} onRetry={ledgerState.reload} />
                ) : null}
                {!ledgerState.loading && !ledgerState.error ? (
                  <AdminTable
                    ariaLabel="会员余额流水"
                    columns={ledgerColumns}
                    rows={ledgerState.data?.items ?? []}
                    rowKey={(entry) => entry.id}
                    empty={
                      <EmptyBlock
                        title="暂无余额流水"
                        description="该会员还没有充值入账或余额支付记录。"
                        className="my-0"
                      />
                    }
                  />
                ) : null}
                {ledgerState.data && ledgerState.data.total > 0 ? (
                  <Pager
                    page={ledgerPage}
                    total={ledgerState.data.total}
                    pageSize={ledgerState.data.page_size || DETAIL_PAGE_SIZE}
                    onChange={setLedgerPage}
                  />
                ) : null}
              </section>

              <section className="space-y-3">
                <div>
                  <h3 className="text-sm font-medium text-foreground">充值单</h3>
                  <p className="mt-0.5 text-xs text-muted-foreground">最近 10 条，新建在前。</p>
                </div>

                {rechargesState.loading ? <LoadingBlock label="正在读取充值单…" /> : null}
                {rechargesState.error ? (
                  <ErrorState message={rechargesState.error} onRetry={rechargesState.reload} />
                ) : null}
                {!rechargesState.loading && !rechargesState.error ? (
                  <AdminTable
                    ariaLabel="会员充值单"
                    columns={rechargeColumns}
                    rows={rechargesState.data?.items ?? []}
                    rowKey={(recharge) => recharge.id}
                    empty={
                      <EmptyBlock
                        title="暂无充值单"
                        description="该会员还没有在线充值记录。"
                        className="my-0"
                      />
                    }
                  />
                ) : null}
              </section>
            </>
          ) : (
            <Alert>
              <AlertTitle>财务对账数据不可见</AlertTitle>
              <AlertDescription>
                财务对账数据仅超级管理员与财务可见（{permissionHint('finance.reconcile')}），
                余额流水与充值单不在这里展示。
              </AlertDescription>
            </Alert>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
