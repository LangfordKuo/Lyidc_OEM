import { useState, type FormEvent } from 'react'
import { AlertCircleIcon, LoaderCircleIcon } from 'lucide-react'
import { toast } from 'sonner'

import {
  listAdminConversions,
  listAdminWithdrawals,
  markAdminWithdrawalPaid,
  reviewAdminConversion,
  reviewAdminWithdrawal,
} from '@/api/adminReferral'
import { listAdminLedger } from '@/api/adminMembers'
import { errorMessage } from '@/api/client'
import type {
  LedgerEntry,
  ReferralPayout,
  ReferralPayoutMember,
} from '@/api/types'
import { useAdminAuth } from '@/auth/adminAuthContext'
import AdminTable, { type AdminColumn } from '@/components/admin/AdminTable'
import FilterSelect, { AdminFilterBar } from '@/components/admin/FilterSelect'
import NoPermission from '@/components/admin/NoPermission'
import ConfirmDialog from '@/components/common/ConfirmDialog'
import Pager from '@/components/common/Pager'
import { EmptyBlock, ErrorState, LoadingBlock } from '@/components/common/PageState'
import StatusBadge from '@/components/common/StatusBadge'
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
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useAsync } from '@/hooks/useAsync'
import { ADMIN_ROLE_LABELS, hasPermission, permissionHint } from '@/lib/adminRoles'
import { ledgerTypeLabel } from '@/lib/financeText'
import { formatDateTime, formatMoney } from '@/lib/format'
import { ADMIN_PAGE_SIZE } from '@/lib/pagination'
import {
  payoutChannelLabel,
  payoutStatusLabel,
  payoutStatusTone,
} from '@/lib/referralText'

// AdminReferral 是管理后台「返佣管理」页（契约 18.5 / 18.6 / 12.6）：
//   页签一「提现审核」：GET /admin/referral/withdrawals + review（approve/reject）+ mark-paid；
//   页签二「转余额审核」：GET /admin/referral/conversions + review；
//   页签三「返佣流水」：复用 GET /admin/ledger?type=referral_rebate 对账（契约 12.6 明确支持该类型，
//     覆盖发放/冲回/提现冻结/退回/转换的有符号记账，比只看 referral_rebates 表更适合对账）。
//   角色矩阵（18.6）：查看类 admin + finance；**审核与打款仅 admin**；support 无入口。
//   finance 只读时审核/打款按钮禁用 + permissionHint 原因，且**不发越权请求**（服务端仍会独立校验 403）。

const STATUS_OPTIONS: { value: '' | 'pending' | 'approved' | 'rejected' | 'paid'; label: string }[] =
  [
    { value: '', label: '全部状态' },
    { value: 'pending', label: '待审核' },
    { value: 'approved', label: '已通过' },
    { value: 'rejected', label: '已驳回' },
    { value: 'paid', label: '已打款' },
  ]

type StatusFilterValue = (typeof STATUS_OPTIONS)[number]['value']

export default function AdminReferral() {
  const { role } = useAdminAuth()
  const canRead = hasPermission(role, 'referral.read')
  const canReview = hasPermission(role, 'referral.review')

  if (!canRead) {
    // support 无查看权限（18.6）：整页占位，侧栏也已隐藏入口，不发任何请求。
    return <NoPermission permission="referral.read" title="无权访问返佣管理" />
  }

  return (
    <div className="space-y-5">
      <header>
        <h1 className="text-2xl font-semibold tracking-tight text-foreground">返佣管理</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          审核会员的提现与转余额申请，标记线下打款，并按返佣流水对账。
        </p>
        {!canReview ? (
          <p className="mt-1 text-xs text-muted-foreground">
            当前角色（{role ? ADMIN_ROLE_LABELS[role] : '—'}）可查看但不能审核：
            {permissionHint('referral.review')}，审核与打款按钮已禁用。
          </p>
        ) : null}
      </header>

      <Tabs defaultValue="withdrawals">
        <TabsList aria-label="返佣管理页签">
          <TabsTrigger value="withdrawals">提现审核</TabsTrigger>
          <TabsTrigger value="conversions">转余额审核</TabsTrigger>
          <TabsTrigger value="ledger">返佣流水</TabsTrigger>
        </TabsList>

        <TabsContent value="withdrawals" className="pt-2">
          <PayoutReviewPanel kind="withdraw" canReview={canReview} />
        </TabsContent>
        <TabsContent value="conversions" className="pt-2">
          <PayoutReviewPanel kind="convert" canReview={canReview} />
        </TabsContent>
        <TabsContent value="ledger" className="pt-2">
          <LedgerPanel />
        </TabsContent>
      </Tabs>
    </div>
  )
}

// ---------------------------------------------------------------------------
// 提现 / 转余额审核（同一块，只有提现有 mark-paid）
// ---------------------------------------------------------------------------

function PayoutReviewPanel({
  kind,
  canReview,
}: {
  kind: 'withdraw' | 'convert'
  canReview: boolean
}) {
  const isWithdraw = kind === 'withdraw'

  const [status, setStatus] = useState<StatusFilterValue>('')
  const [memberDraft, setMemberDraft] = useState('')
  const [memberId, setMemberId] = useState<number | undefined>(undefined)
  const [page, setPage] = useState(1)
  const [refreshKey, setRefreshKey] = useState(0)

  const [reviewTarget, setReviewTarget] = useState<ReferralPayout | null>(null)
  const [paidTarget, setPaidTarget] = useState<ReferralPayout | null>(null)

  const listState = useAsync(
    () => {
      const params = {
        page,
        page_size: ADMIN_PAGE_SIZE,
        ...(status ? { status } : {}),
        ...(memberId ? { member_id: memberId } : {}),
      }
      return isWithdraw ? listAdminWithdrawals(params) : listAdminConversions(params)
    },
    // 查看类 admin/finance 都可请求；页面已按 referral.read 闸门，故不再单独限流。
    [kind, page, status, memberId, refreshKey],
  )

  const handleSearch = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const trimmed = memberDraft.trim()
    setMemberId(trimmed && /^\d+$/.test(trimmed) ? Number(trimmed) : undefined)
    setPage(1)
  }

  const columns: AdminColumn<ReferralPayout>[] = [
    {
      key: 'id',
      header: 'ID',
      className: 'w-16',
      cell: (item) => <span className="text-xs text-muted-foreground">#{item.id}</span>,
    },
    {
      key: 'member',
      header: '会员',
      cell: (item) => <MemberCell member={item.member} memberId={item.member_id} />,
    },
    {
      key: 'amount',
      header: '金额',
      className: 'text-right whitespace-nowrap',
      cell: (item) => <span className="font-medium">{formatMoney(item.amount)}</span>,
    },
    {
      key: 'channel',
      header: '收款方式 / 账号',
      cell: (item) =>
        item.channel ? (
          <span className="flex flex-col">
            <span>{payoutChannelLabel(item.channel)}</span>
            <span className="text-xs text-muted-foreground">
              {item.account_name} · {item.account_no}
            </span>
          </span>
        ) : (
          <span className="text-xs text-muted-foreground">—</span>
        ),
    },
    {
      key: 'status',
      header: '状态',
      cell: (item) => (
        <StatusBadge tone={payoutStatusTone(item.status)} label={payoutStatusLabel(item.status)} />
      ),
    },
    {
      key: 'created_at',
      header: '提交时间',
      className: 'whitespace-nowrap',
      cell: (item) => (
        <span className="text-xs text-muted-foreground">{formatDateTime(item.created_at)}</span>
      ),
    },
    {
      key: 'note',
      header: '备注 / 审核',
      cell: (item) => (
        <span className="flex flex-col">
          <span className="text-xs">{item.remark || '—'}</span>
          {item.review_note ? (
            <span className="text-xs text-muted-foreground">审核备注：{item.review_note}</span>
          ) : null}
          {item.paid_at ? (
            <span className="text-xs text-muted-foreground">
              打款 {formatDateTime(item.paid_at)}
            </span>
          ) : null}
        </span>
      ),
    },
    {
      key: 'actions',
      header: '操作',
      className: 'whitespace-nowrap',
      cell: (item) => {
        const pending = item.status === 'pending'
        const approved = item.status === 'approved'
        return (
          <span className="flex gap-2">
            <Button
              size="sm"
              variant="outline"
              disabled={!canReview || !pending}
              title={canReview ? undefined : permissionHint('referral.review')}
              onClick={() => setReviewTarget(item)}
            >
              审核
            </Button>
            {isWithdraw ? (
              <Button
                size="sm"
                variant="default"
                disabled={!canReview || !approved}
                title={canReview ? undefined : permissionHint('referral.review')}
                onClick={() => setPaidTarget(item)}
              >
                标记打款
              </Button>
            ) : null}
          </span>
        )
      },
    },
  ]

  return (
    <div className="space-y-4">
      <form onSubmit={handleSearch} noValidate>
        <AdminFilterBar
          actions={
            <span className="text-xs text-muted-foreground">
              {isWithdraw
                ? '缺省只看待审核的提现；「标记打款」用于线下出款后的记账标记。'
                : '审核通过时才把返现余额换算入消费余额；驳回不动余额。'}
            </span>
          }
        >
          <FilterSelect
            label="状态"
            value={status}
            options={STATUS_OPTIONS}
            onChange={(next) => {
              setStatus(next)
              setPage(1)
            }}
          />
          <div className="space-y-1">
            <Label htmlFor={`${kind}_member_id`} className="text-xs text-muted-foreground">
              会员 ID
            </Label>
            <Input
              id={`${kind}_member_id`}
              name={`${kind}_member_id`}
              className="w-32"
              inputMode="numeric"
              placeholder="不过滤"
              value={memberDraft}
              onChange={(event) => setMemberDraft(event.target.value)}
            />
          </div>
          <Button type="submit" size="sm">
            查询
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => {
              setMemberDraft('')
              setMemberId(undefined)
              setStatus('')
              setPage(1)
            }}
          >
            重置
          </Button>
        </AdminFilterBar>
      </form>

      {listState.loading ? <LoadingBlock label="正在读取申请…" /> : null}
      {listState.error ? <ErrorState message={listState.error} onRetry={listState.reload} /> : null}

      {!listState.loading && !listState.error ? (
        <AdminTable
          ariaLabel={isWithdraw ? '提现申请列表' : '转余额申请列表'}
          columns={columns}
          rows={listState.data?.items ?? []}
          rowKey={(item) => item.id}
          empty={
            <EmptyBlock
              title={isWithdraw ? '暂无提现申请' : '暂无转余额申请'}
              description="会员提交申请后会出现在这里。"
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

      {reviewTarget ? (
        <ReviewDialog
          kind={kind}
          target={reviewTarget}
          onClose={() => setReviewTarget(null)}
          onDone={() => {
            setReviewTarget(null)
            setRefreshKey((value) => value + 1)
          }}
        />
      ) : null}

      {paidTarget ? (
        <MarkPaidDialog
          target={paidTarget}
          onClose={() => setPaidTarget(null)}
          onDone={() => {
            setPaidTarget(null)
            setRefreshKey((value) => value + 1)
          }}
        />
      ) : null}
    </div>
  )
}

function MemberCell({
  member,
  memberId,
}: {
  member: ReferralPayoutMember | null | undefined
  memberId: number
}) {
  if (!member) {
    return <span className="text-xs text-muted-foreground">会员 #{memberId}（已注销）</span>
  }
  return (
    <span className="flex flex-col">
      <span className="font-medium">{member.username}</span>
      {member.nickname ? (
        <span className="text-xs text-muted-foreground">{member.nickname}</span>
      ) : null}
    </span>
  )
}

/** 审核弹窗：approve / reject + 审核备注（reject 会退回冻结额，弹窗里写清后果）。 */
function ReviewDialog({
  kind,
  target,
  onClose,
  onDone,
}: {
  kind: 'withdraw' | 'convert'
  target: ReferralPayout
  onClose: () => void
  onDone: () => void
}) {
  const [action, setAction] = useState<'approve' | 'reject'>('approve')
  const [note, setNote] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  const handleConfirm = async () => {
    setPending(true)
    setError('')
    try {
      const payload = note.trim() ? { action, review_note: note.trim() } : { action }
      const result =
        kind === 'withdraw'
          ? await reviewAdminWithdrawal(target.id, payload)
          : await reviewAdminConversion(target.id, payload)
      toast.success(
        result.already
          ? '该申请此前已审核，本次未重复生效'
          : action === 'approve'
            ? '已通过审核'
            : '已驳回申请',
      )
      onDone()
    } catch (err) {
      setError(errorMessage(err, '审核失败，请稍后重试'))
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => (!open ? onClose() : undefined)}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            审核{kind === 'withdraw' ? '提现' : '转余额'}申请 #{target.id}
          </DialogTitle>
          <DialogDescription>
            会员 {target.member?.username ?? `#${target.member_id}`} · 金额 {formatMoney(target.amount)}
            。驳回{kind === 'withdraw' ? '会退回冻结额' : '不动余额（提交时未扣）'}。
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <RadioGroup
            value={action}
            onValueChange={(value) => setAction(value as 'approve' | 'reject')}
            aria-label="审核结果"
            className="flex flex-wrap gap-4"
          >
            <Label className="flex cursor-pointer items-center gap-2 text-sm font-normal">
              <RadioGroupItem value="approve" />
              通过
            </Label>
            <Label className="flex cursor-pointer items-center gap-2 text-sm font-normal">
              <RadioGroupItem value="reject" />
              驳回
            </Label>
          </RadioGroup>

          <div className="space-y-1.5">
            <Label htmlFor={`review_note_${target.id}`}>审核备注（可选，≤ 255 字符）</Label>
            <Input
              id={`review_note_${target.id}`}
              name={`review_note_${target.id}`}
              value={note}
              placeholder="将随申请记录留档"
              onChange={(event) => setNote(event.target.value)}
            />
          </div>

          {error ? (
            <Alert variant="destructive">
              <AlertCircleIcon aria-hidden />
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={pending}>
            取消
          </Button>
          <Button onClick={handleConfirm} disabled={pending}>
            {pending ? (
              <>
                <LoaderCircleIcon className="animate-spin" data-icon="inline-start" aria-hidden />
                提交中…
              </>
            ) : action === 'approve' ? (
              '确认通过'
            ) : (
              '确认驳回'
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** 线下已打款标记：仅 approved 可标；可带一条追加备注。 */
function MarkPaidDialog({
  target,
  onClose,
  onDone,
}: {
  target: ReferralPayout
  onClose: () => void
  onDone: () => void
}) {
  const [note, setNote] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  const handleConfirm = async () => {
    setPending(true)
    setError('')
    try {
      const payload = note.trim() ? { review_note: note.trim() } : {}
      const result = await markAdminWithdrawalPaid(target.id, payload)
      toast.success(result.already ? '该申请此前已打款，本次未覆盖首次打款时间' : '已标记为已打款')
      onDone()
    } catch (err) {
      setError(errorMessage(err, '标记失败，请稍后重试'))
    } finally {
      setPending(false)
    }
  }

  return (
    <ConfirmDialog
      open
      onOpenChange={(open) => (!open ? onClose() : undefined)}
      title="标记线下已打款"
      description={
        <span>
          提现申请 #{target.id}（会员 {target.member?.username ?? `#${target.member_id}`} ·{' '}
          {formatMoney(target.amount)}）：确认已在线下完成打款？系统不对接支付渠道出款，此操作只是记账标记。
        </span>
      }
      confirmLabel="确认已打款"
      pending={pending}
      onConfirm={handleConfirm}
    >
      <div className="space-y-1.5">
        <Label htmlFor={`paid_note_${target.id}`}>打款备注（可选，追加留档）</Label>
        <Input
          id={`paid_note_${target.id}`}
          name={`paid_note_${target.id}`}
          value={note}
          onChange={(event) => setNote(event.target.value)}
        />
      </div>
      {error ? (
        <Alert variant="destructive">
          <AlertCircleIcon aria-hidden />
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
    </ConfirmDialog>
  )
}

// ---------------------------------------------------------------------------
// 返佣流水：复用 GET /admin/ledger?type=referral_rebate（契约 12.6 对账口径）
// ---------------------------------------------------------------------------

function LedgerPanel() {
  const [page, setPage] = useState(1)
  const listState = useAsync(
    () => listAdminLedger({ page, page_size: ADMIN_PAGE_SIZE, type: 'referral_rebate' }),
    [page],
  )

  const columns: AdminColumn<LedgerEntry>[] = [
    {
      key: 'created_at',
      header: '时间',
      className: 'whitespace-nowrap',
      cell: (entry) => (
        <span className="text-xs text-muted-foreground">{formatDateTime(entry.created_at)}</span>
      ),
    },
    {
      key: 'member',
      header: '会员',
      cell: (entry) => <span className="text-xs text-muted-foreground">#{entry.member_id}</span>,
    },
    {
      key: 'amount',
      header: '金额',
      className: 'text-right whitespace-nowrap',
      cell: (entry) => {
        const negative = entry.amount.trim().startsWith('-')
        return (
          <span className={negative ? 'font-medium text-destructive' : 'font-medium text-success'}>
            {negative ? '' : '+'}
            {formatMoney(entry.amount)}
          </span>
        )
      },
    },
    {
      key: 'type',
      header: '类型',
      cell: (entry) => ledgerTypeLabel(entry.type),
    },
    {
      key: 'note',
      header: '备注 / 关联单号',
      cell: (entry) => (
        <span className="flex flex-col">
          <span>{entry.note || '—'}</span>
          {entry.ref_id > 0 ? (
            <span className="text-xs text-muted-foreground">
              关联 {entry.ref_type} #{entry.ref_id}
            </span>
          ) : null}
        </span>
      ),
    },
    {
      key: 'balance',
      header: '返现余额变化',
      className: 'whitespace-nowrap',
      cell: (entry) => (
        <span className="text-xs text-muted-foreground">
          {formatMoney(entry.balance_before)} → {formatMoney(entry.balance_after)}
        </span>
      ),
    },
  ]

  return (
    <div className="space-y-4">
      <Alert>
        <AlertTitle>对账口径</AlertTitle>
        <AlertDescription>
          按 ledger 类型 <code>referral_rebate</code> 过滤：金额为正 = 发放/退回，为负 = 冲回/冻结；
          转余额的消费侧入账是 <code>adjust</code>，不在此列表。
        </AlertDescription>
      </Alert>

      {listState.loading ? <LoadingBlock label="正在读取返佣流水…" /> : null}
      {listState.error ? <ErrorState message={listState.error} onRetry={listState.reload} /> : null}

      {!listState.loading && !listState.error ? (
        <AdminTable
          ariaLabel="返佣流水"
          columns={columns}
          rows={listState.data?.items ?? []}
          rowKey={(entry) => entry.id}
          empty={
            <EmptyBlock
              title="暂无返佣流水"
              description="返佣发放、冲回与提现冻结/退回都会在这里留痕。"
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
    </div>
  )
}
