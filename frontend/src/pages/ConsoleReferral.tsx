import { useState } from 'react'
import { AlertCircleIcon, LoaderCircleIcon } from 'lucide-react'
import { toast } from 'sonner'

import { errorMessage } from '@/api/client'
import {
  createReferralCoupon,
  createReferralConversion,
  createReferralWithdrawal,
  fetchReferral,
  listReferralConversions,
  listReferralCoupons,
  listReferralInvitees,
  listReferralRebates,
  listReferralWithdrawals,
  updateReferral,
  updateReferralCoupon,
} from '@/api/referral'
import type {
  CreateMemberCouponInput,
  MemberCoupon,
  PayoutChannel,
  ReferralOverview,
  ReferralPayout,
  UpdateMemberCouponInput,
} from '@/api/types'
import Pager from '@/components/common/Pager'
import { EmptyBlock, ErrorState, LoadingBlock } from '@/components/common/PageState'
import StatusBadge from '@/components/common/StatusBadge'
import CopyButton from '@/components/common/CopyButton'
import { InfoList } from '@/components/common/InfoList'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { useAsync } from '@/hooks/useAsync'
import { BILLING_CYCLES, CYCLE_LABELS } from '@/lib/cycles'
import { formatDateTime, formatDateTimeOr, formatMoney } from '@/lib/format'
import { CONSOLE_PAGE_SIZE } from '@/lib/pagination'
import {
  couponStatusLabel,
  couponStatusTone,
  payoutChannelLabel,
  payoutKindLabel,
  payoutStatusLabel,
  payoutStatusTone,
  ratioSourceLabel,
  referralRebateStatusLabel,
  referralRebateStatusTone,
} from '@/lib/referralText'
import {
  validateCouponCode,
  validateCouponMaxUses,
  validateCouponValue,
  validatePayoutAccount,
  validatePayoutAmount,
  validateRemark,
} from '@/lib/validate'
import { cn } from '@/lib/utils'

// ConsoleReferral 是会员区「邀请有礼」（契约 18.5）：
//   返佣概览（开关 + 邀请码/链接 + 返现余额）→ 我的优惠码 → 提现 → 转余额 → 返佣流水 → 我邀请的会员。
//   区块顺序取舍：概览/优惠码是「拉新与收益」主叙事，流水与邀请列表是结果，提现/转余额是变现出口
//   ——但钱的出口紧跟概览会打断建券流程，故按「先做业务、后看账、最后出钱」排；负数余额时
//   只有提现与转换的提交入口被禁用（契约 18.7：其余功能照常），禁用原因就写在按钮旁。
//   提交类操作的失败提示一律原样展示后端 message（折扣上限、余额不足、负数余额等口径都已写好）。

export default function ConsoleReferral() {
  const [refreshKey, setRefreshKey] = useState(0)
  const overviewState = useAsync(fetchReferral, [refreshKey])
  // PUT /referral 成功返回新概览，就地覆盖，避免整页闪一下再重拉。
  const [overviewOverride, setOverviewOverride] = useState<ReferralOverview | null>(null)
  const overview = overviewOverride ?? overviewState.data

  const [toggling, setToggling] = useState(false)
  const [toggleError, setToggleError] = useState('')

  const handleToggle = async () => {
    if (!overview) {
      return
    }
    setToggling(true)
    setToggleError('')
    try {
      const next = await updateReferral({ enabled: !overview.enabled })
      setOverviewOverride(next)
      setRefreshKey((value) => value + 1)
      toast.success(next.enabled ? '已开通邀请返现' : '已关闭邀请返现')
    } catch (error) {
      setToggleError(errorMessage(error, '操作失败，请稍后重试'))
    } finally {
      setToggling(false)
    }
  }

  // 返现余额可为负：负数只禁提现与转换（契约 18.7），其余照常。
  const rebateBalance = overview?.rebate_balance ?? '0.00'
  const balanceNegative = rebateBalance.trim().startsWith('-')

  return (
    <div className="space-y-5">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-foreground">邀请有礼</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            邀请好友注册并下单，按订单实付结算返佣；返现余额可提现或转为消费余额。
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={() => setRefreshKey((value) => value + 1)}>
          刷新
        </Button>
      </header>

      {/* ---------- 1. 返佣概览 + 开通/关闭 ---------- */}
      <Card>
        <CardHeader className="border-b">
          <CardTitle className="text-base">返佣概览</CardTitle>
          <CardDescription>
            开关只影响你自己是否参与邀请返现；已生成的邀请码不会变更。
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {overviewState.loading && !overview ? <LoadingBlock label="正在读取返佣概览…" /> : null}
          {overviewState.error && !overview ? (
            <ErrorState message={overviewState.error} onRetry={overviewState.reload} />
          ) : null}
          {toggleError ? (
            <Alert variant="destructive">
              <AlertCircleIcon aria-hidden />
              <AlertDescription>{toggleError}</AlertDescription>
            </Alert>
          ) : null}

          {overview ? (
            <>
              <div className="rounded-xl border border-border px-4 py-3">
                <p className="text-xs text-muted-foreground">邀请返现余额</p>
                <p
                  className={cn(
                    'mt-1 text-3xl font-semibold',
                    balanceNegative ? 'text-destructive' : 'text-foreground',
                  )}
                >
                  {formatMoney(rebateBalance)}
                </p>
                {balanceNegative ? (
                  <p className="mt-1 text-xs font-medium text-destructive">
                    余额为负，暂不能提现或转余额（其余功能不受影响）。
                  </p>
                ) : (
                  <p className="mt-1 text-xs text-muted-foreground">
                    与消费余额完全独立；订单取消时已发放的返佣会被冲回，可能为负。
                  </p>
                )}
              </div>

              <InfoList
                columns={2}
                items={[
                  {
                    label: '邀请返现开关',
                    value: (
                      <StatusBadge
                        tone={overview.enabled ? 'ok' : 'pending'}
                        label={overview.enabled ? '已开通' : '未开通'}
                      />
                    ),
                  },
                  { label: '生效返佣比例', value: `${overview.effective_percent}%` },
                  { label: '已邀请人数', value: String(overview.invitee_count) },
                  { label: '累计返佣', value: formatMoney(overview.total_rebate) },
                ]}
              />

              {overview.invite_code ? (
                <div className="space-y-3 rounded-xl border border-border px-4 py-3">
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <div className="min-w-0">
                      <p className="text-xs text-muted-foreground">我的邀请码</p>
                      <p className="mt-0.5 font-mono text-base font-medium text-foreground">
                        {overview.invite_code}
                      </p>
                    </div>
                    <CopyButton value={overview.invite_code} label="复制邀请码" />
                  </div>
                  {overview.invite_link ? (
                    <div className="flex flex-wrap items-center justify-between gap-3">
                      <div className="min-w-0">
                        <p className="text-xs text-muted-foreground">邀请链接</p>
                        <p className="mt-0.5 break-all font-mono text-xs text-foreground">
                          {overview.invite_link}
                        </p>
                      </div>
                      <CopyButton value={overview.invite_link} label="复制邀请链接" />
                    </div>
                  ) : (
                    <p className="text-xs text-muted-foreground">
                      站点地址未配置，暂不生成邀请链接；邀请码仍可直接口述给好友。
                    </p>
                  )}
                  <p className="text-xs text-muted-foreground">
                    已生成的邀请码不会变更：关闭再开通仍沿用同一邀请码。
                  </p>
                </div>
              ) : (
                <p className="text-xs text-muted-foreground">
                  开通邀请返现后生成专属邀请码与邀请链接；邀请码一经生成不再变更。
                </p>
              )}

              <div className="flex flex-wrap items-center gap-3">
                <Button disabled={toggling} onClick={handleToggle}>
                  {toggling ? (
                    <>
                      <LoaderCircleIcon
                        className="animate-spin"
                        data-icon="inline-start"
                        aria-hidden
                      />
                      提交中…
                    </>
                  ) : overview.enabled ? (
                    '关闭邀请返现'
                  ) : (
                    '开通邀请返现'
                  )}
                </Button>
                <span className="text-xs text-muted-foreground">
                  关闭后不再产生新的返佣；已发放的返佣与已生成的邀请码不受影响。
                </span>
              </div>
            </>
          ) : null}
        </CardContent>
      </Card>

      {/* ---------- 2. 我的优惠码（自建券） ---------- */}
      <CouponSection refreshKey={refreshKey} onMutated={() => setRefreshKey((value) => value + 1)} />

      {/* ---------- 3. 提现申请 ---------- */}
      <PayoutSection
        kind="withdraw"
        disabled={balanceNegative}
        disabledReason="邀请返现余额为负，暂不能提现或转余额"
        refreshKey={refreshKey}
        onMutated={() => setRefreshKey((value) => value + 1)}
      />

      {/* ---------- 4. 转余额申请 ---------- */}
      <PayoutSection
        kind="convert"
        disabled={balanceNegative}
        disabledReason="邀请返现余额为负，暂不能提现或转余额"
        refreshKey={refreshKey}
        onMutated={() => setRefreshKey((value) => value + 1)}
      />

      {/* ---------- 5. 返佣流水 ---------- */}
      <RebateSection refreshKey={refreshKey} />

      {/* ---------- 6. 我邀请的会员 ---------- */}
      <InviteeSection refreshKey={refreshKey} />
    </div>
  )
}

// ---------------------------------------------------------------------------
// 我的优惠码：建券（只允许 percent）+ 列表 + 只改 status / expires_at
// ---------------------------------------------------------------------------

function CouponSection({
  refreshKey,
  onMutated,
}: {
  refreshKey: number
  onMutated: () => void
}) {
  const [page, setPage] = useState(1)
  const listState = useAsync(
    () => listReferralCoupons({ page, page_size: CONSOLE_PAGE_SIZE }),
    [page, refreshKey],
  )

  const [code, setCode] = useState('')
  const [value, setValue] = useState('')
  const [cycles, setCycles] = useState<string[]>([])
  const [expiresAt, setExpiresAt] = useState('')
  const [maxUses, setMaxUses] = useState('')
  const [comment, setComment] = useState('')
  const [formError, setFormError] = useState('')
  const [creating, setCreating] = useState(false)

  const toggleCycle = (cycle: string) => {
    setCycles((current) =>
      current.includes(cycle) ? current.filter((item) => item !== cycle) : [...current, cycle],
    )
  }

  const handleCreate = async () => {
    const codeError = validateCouponCode(code)
    const valueError = validateCouponValue(value)
    const maxUsesError = validateCouponMaxUses(maxUses)
    const commentError = validateRemark(comment)
    const firstError = codeError || valueError || maxUsesError || commentError
    if (firstError) {
      setFormError(firstError)
      return
    }

    const payload: CreateMemberCouponInput = {
      code: code.trim(),
      type: 'percent',
      value: value.trim(),
    }
    // 勾选了才限定周期；不勾 = 全部 6 周期（契约 11.1：省略 / 空数组同义）。
    if (cycles.length > 0) {
      payload.cycles = cycles
    }
    if (expiresAt) {
      payload.expires_at = new Date(expiresAt).toISOString()
    }
    if (maxUses.trim()) {
      payload.max_uses = Number(maxUses.trim())
    }
    if (comment.trim()) {
      payload.comment = comment.trim()
    }

    setCreating(true)
    setFormError('')
    try {
      const created = await createReferralCoupon(payload)
      toast.success(`优惠码 ${created.code} 已创建`)
      setCode('')
      setValue('')
      setCycles([])
      setExpiresAt('')
      setMaxUses('')
      setComment('')
      setPage(1)
      onMutated()
    } catch (error) {
      // 折扣上限、未开通邀请返现、code 冲突等一律原样展示后端 message（已含上限口径）。
      setFormError(errorMessage(error, '创建优惠码失败，请稍后重试'))
    } finally {
      setCreating(false)
    }
  }

  return (
    <Card>
      <CardHeader className="border-b">
        <CardTitle className="text-base">我的优惠码</CardTitle>
        <CardDescription>
          自建券只支持按比例折扣（percent）；折扣上限由系统按「设置上限 × 生效返佣比例」判定，
          创建后折扣与码都不可修改。
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="space-y-4 rounded-xl border border-border px-4 py-4">
          <p className="text-sm font-medium text-foreground">创建自建券</p>

          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="coupon_code">优惠码</Label>
              <Input
                id="coupon_code"
                name="coupon_code"
                placeholder="3-32 位字母、数字、下划线或短横线"
                value={code}
                onChange={(event) => {
                  setCode(event.target.value)
                  setFormError('')
                }}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="coupon_value">折扣百分比（%）</Label>
              <Input
                id="coupon_value"
                name="coupon_value"
                inputMode="decimal"
                placeholder="如 20 表示 8 折（减免 20%）"
                value={value}
                onChange={(event) => {
                  setValue(event.target.value)
                  setFormError('')
                }}
              />
            </div>
          </div>

          <div className="space-y-2">
            <p className="text-sm font-medium text-foreground">适用周期（不勾选 = 全部周期）</p>
            <div className="flex flex-wrap gap-x-4 gap-y-2">
              {BILLING_CYCLES.map((cycle) => (
                <Label
                  key={cycle}
                  className="flex cursor-pointer items-center gap-2 text-sm font-normal"
                >
                  <Checkbox
                    checked={cycles.includes(cycle)}
                    onCheckedChange={() => toggleCycle(cycle)}
                    aria-label={CYCLE_LABELS[cycle]}
                  />
                  {CYCLE_LABELS[cycle]}
                </Label>
              ))}
            </div>
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="coupon_expires_at">过期时间（可留空 = 永不过期）</Label>
              <Input
                id="coupon_expires_at"
                name="coupon_expires_at"
                type="datetime-local"
                value={expiresAt}
                onChange={(event) => {
                  setExpiresAt(event.target.value)
                  setFormError('')
                }}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="coupon_max_uses">使用次数上限（0 = 不限）</Label>
              <Input
                id="coupon_max_uses"
                name="coupon_max_uses"
                inputMode="numeric"
                placeholder="0"
                value={maxUses}
                onChange={(event) => {
                  setMaxUses(event.target.value)
                  setFormError('')
                }}
              />
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="coupon_comment">备注（可选，≤ 255 字符）</Label>
            <Input
              id="coupon_comment"
              name="coupon_comment"
              placeholder="仅自己可见"
              value={comment}
              onChange={(event) => {
                setComment(event.target.value)
                setFormError('')
              }}
            />
          </div>

          {formError ? (
            <Alert variant="destructive">
              <AlertCircleIcon aria-hidden />
              <AlertDescription>{formError}</AlertDescription>
            </Alert>
          ) : null}

          <Button disabled={creating} onClick={handleCreate}>
            {creating ? '创建中…' : '创建优惠码'}
          </Button>
        </div>

        {listState.loading ? <LoadingBlock label="正在读取优惠码…" /> : null}
        {listState.error ? <ErrorState message={listState.error} onRetry={listState.reload} /> : null}
        {!listState.loading && !listState.error && (listState.data?.items.length ?? 0) === 0 ? (
          <EmptyBlock
            title="还没有自建券"
            description="自建券用于拉新：未绑定邀请关系的客户可用，下单时自动绑定到你。"
            className="my-0"
          />
        ) : null}

        {listState.data?.items.map((coupon) => (
          <CouponRow key={coupon.id} coupon={coupon} onMutated={onMutated} />
        ))}

        {listState.data ? (
          <Pager
            page={page}
            total={listState.data.total}
            pageSize={listState.data.page_size || CONSOLE_PAGE_SIZE}
            onChange={setPage}
          />
        ) : null}
      </CardContent>
    </Card>
  )
}

/** 单张券：只给 status 启停与 expires_at 修改入口（value / code 创建后不可改，契约 18.5）。 */
function CouponRow({ coupon, onMutated }: { coupon: MemberCoupon; onMutated: () => void }) {
  const [expiresDraft, setExpiresDraft] = useState(
    coupon.expires_at ? toLocalInputValue(coupon.expires_at) : '',
  )
  const [pending, setPending] = useState(false)
  const [rowError, setRowError] = useState('')

  const submit = async (patch: UpdateMemberCouponInput, successText: string) => {
    setPending(true)
    setRowError('')
    try {
      await updateReferralCoupon(coupon.id, patch)
      toast.success(successText)
      onMutated()
    } catch (error) {
      setRowError(errorMessage(error, '保存失败，请稍后重试'))
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="space-y-3 rounded-lg border border-border px-4 py-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="font-mono text-sm font-medium text-foreground">{coupon.code}</p>
          <p className="mt-0.5 text-xs text-muted-foreground">
            折扣 {coupon.value}% · 用于 {coupon.used_count} 次
            {coupon.max_uses > 0 ? ` / 上限 ${coupon.max_uses} 次` : ' / 不限次数'}
          </p>
          <p className="mt-0.5 text-xs text-muted-foreground">
            适用 {coupon.cycles.length > 0 ? coupon.cycles.join(', ') : '全部周期'}
            {coupon.comment ? ` · ${coupon.comment}` : ''}
          </p>
          <p className="mt-0.5 text-xs text-muted-foreground">
            生效 {formatDateTimeOr(coupon.starts_at)} · 过期{' '}
            {coupon.expires_at ? formatDateTime(coupon.expires_at) : '永不过期'} · 创建{' '}
            {formatDateTime(coupon.created_at)}
          </p>
        </div>
        <div className="flex items-center gap-3">
          <StatusBadge
            tone={couponStatusTone(coupon.status)}
            label={couponStatusLabel(coupon.status)}
          />
          <Button
            size="sm"
            variant="outline"
            disabled={pending}
            onClick={() =>
              submit(
                { status: coupon.status === 'on' ? 'off' : 'on' },
                coupon.status === 'on' ? '已停用优惠码' : '已启用优惠码',
              )
            }
          >
            {coupon.status === 'on' ? '停用' : '启用'}
          </Button>
        </div>
      </div>

      <div className="flex flex-wrap items-end gap-2">
        <div className="space-y-1">
          <Label htmlFor={`coupon_expires_${coupon.id}`} className="text-xs text-muted-foreground">
            过期时间（清空 = 永不过期）
          </Label>
          <Input
            id={`coupon_expires_${coupon.id}`}
            name={`coupon_expires_${coupon.id}`}
            type="datetime-local"
            className="w-56"
            value={expiresDraft}
            onChange={(event) => setExpiresDraft(event.target.value)}
          />
        </div>
        <Button
          size="sm"
          variant="outline"
          disabled={pending}
          onClick={() =>
            submit(
              // 三态：这里始终按「表单当前值」提交——空 = null（永不过期），非空 = RFC3339 UTC。
              { expires_at: expiresDraft ? new Date(expiresDraft).toISOString() : null },
              '过期时间已更新',
            )
          }
        >
          保存过期时间
        </Button>
      </div>

      {rowError ? <p className="text-xs text-destructive">{rowError}</p> : null}
    </div>
  )
}

// ---------------------------------------------------------------------------
// 提现 / 转余额（共用一块，只有提现带渠道与收款账号）
// ---------------------------------------------------------------------------

function PayoutSection({
  kind,
  disabled,
  disabledReason,
  refreshKey,
  onMutated,
}: {
  kind: 'withdraw' | 'convert'
  disabled: boolean
  disabledReason: string
  refreshKey: number
  onMutated: () => void
}) {
  const [page, setPage] = useState(1)
  const listState = useAsync(
    () =>
      kind === 'withdraw'
        ? listReferralWithdrawals({ page, page_size: CONSOLE_PAGE_SIZE })
        : listReferralConversions({ page, page_size: CONSOLE_PAGE_SIZE }),
    [kind, page, refreshKey],
  )

  const [amount, setAmount] = useState('')
  const [channel, setChannel] = useState<PayoutChannel>('wechat')
  const [accountName, setAccountName] = useState('')
  const [accountNo, setAccountNo] = useState('')
  const [remark, setRemark] = useState('')
  const [formError, setFormError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const isWithdraw = kind === 'withdraw'
  const title = isWithdraw ? '提现申请' : '转余额申请'
  const description = isWithdraw
    ? '提交即冻结对应返现余额；管理员审核通过后线下打款，驳回则退回冻结额。'
    : '把返现余额转为消费余额；提交时不动钱，管理员审核通过时即时入账。'

  const handleSubmit = async () => {
    const amountError = validatePayoutAmount(amount)
    const nameError = isWithdraw ? validatePayoutAccount(accountName, '收款人姓名') : ''
    const noError = isWithdraw ? validatePayoutAccount(accountNo, '收款账号') : ''
    const remarkError = validateRemark(remark)
    const firstError = amountError || nameError || noError || remarkError
    if (firstError) {
      setFormError(firstError)
      return
    }

    setSubmitting(true)
    setFormError('')
    try {
      if (isWithdraw) {
        const payload: Parameters<typeof createReferralWithdrawal>[0] = {
          amount: amount.trim(),
          channel,
          account_name: accountName.trim(),
          account_no: accountNo.trim(),
        }
        if (remark.trim()) {
          payload.remark = remark.trim()
        }
        await createReferralWithdrawal(payload)
        toast.success('提现申请已提交，等待审核')
      } else {
        const payload: Parameters<typeof createReferralConversion>[0] = { amount: amount.trim() }
        if (remark.trim()) {
          payload.remark = remark.trim()
        }
        await createReferralConversion(payload)
        toast.success('转余额申请已提交，等待审核')
      }
      setAmount('')
      setAccountName('')
      setAccountNo('')
      setRemark('')
      setPage(1)
      onMutated()
    } catch (error) {
      // 余额不足 / 负数余额 / 金额非法等 message 已写好，原样展示。
      setFormError(errorMessage(error, '提交失败，请稍后重试'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Card>
      <CardHeader className="border-b">
        <CardTitle className="text-base">{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="space-y-4 rounded-xl border border-border px-4 py-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor={`${kind}_amount`}>
                {isWithdraw ? '提现金额（元）' : '转余额金额（元）'}
              </Label>
              <Input
                id={`${kind}_amount`}
                name={`${kind}_amount`}
                inputMode="decimal"
                placeholder="如 50.00"
                value={amount}
                disabled={disabled}
                onChange={(event) => {
                  setAmount(event.target.value)
                  setFormError('')
                }}
              />
            </div>

            {isWithdraw ? (
              <>
                <div className="space-y-1.5">
                  <p className="text-sm font-medium text-foreground">收款方式</p>
                  <RadioGroup
                    value={channel}
                    onValueChange={(value) => setChannel(value as PayoutChannel)}
                    aria-label="收款方式"
                    className="flex flex-wrap gap-4"
                  >
                    <Label className="flex cursor-pointer items-center gap-2 text-sm font-normal">
                      <RadioGroupItem value="wechat" />
                      微信
                    </Label>
                    <Label className="flex cursor-pointer items-center gap-2 text-sm font-normal">
                      <RadioGroupItem value="alipay" />
                      支付宝
                    </Label>
                  </RadioGroup>
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor={`${kind}_account_name`}>收款人姓名</Label>
                  <Input
                    id={`${kind}_account_name`}
                    name={`${kind}_account_name`}
                    placeholder="与收款账号一致"
                    value={accountName}
                    disabled={disabled}
                    onChange={(event) => {
                      setAccountName(event.target.value)
                      setFormError('')
                    }}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor={`${kind}_account_no`}>收款账号</Label>
                  <Input
                    id={`${kind}_account_no`}
                    name={`${kind}_account_no`}
                    placeholder="微信 / 支付宝账号"
                    value={accountNo}
                    disabled={disabled}
                    onChange={(event) => {
                      setAccountNo(event.target.value)
                      setFormError('')
                    }}
                  />
                </div>
              </>
            ) : null}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor={`${kind}_remark`}>
              {isWithdraw ? '提现备注（可选，≤ 255 字符）' : '转余额备注（可选，≤ 255 字符）'}
            </Label>
            <Input
              id={`${kind}_remark`}
              name={`${kind}_remark`}
              value={remark}
              disabled={disabled}
              onChange={(event) => {
                setRemark(event.target.value)
                setFormError('')
              }}
            />
          </div>

          {formError ? (
            <Alert variant="destructive">
              <AlertCircleIcon aria-hidden />
              <AlertDescription>{formError}</AlertDescription>
            </Alert>
          ) : null}

          <div className="flex flex-wrap items-center gap-3">
            <Button disabled={disabled || submitting} onClick={handleSubmit}>
              {submitting ? '提交中…' : isWithdraw ? '提交提现申请' : '提交转余额申请'}
            </Button>
            {disabled ? (
              <span className="text-xs font-medium text-destructive">{disabledReason}</span>
            ) : (
              <span className="text-xs text-muted-foreground">
                {isWithdraw
                  ? '收款账号仅用于线下打款，回显已脱敏。'
                  : '审核通过后即时转入消费余额，可在余额流水中查看。'}
              </span>
            )}
          </div>
        </div>

        {listState.loading ? <LoadingBlock label={`正在读取${title}…`} /> : null}
        {listState.error ? <ErrorState message={listState.error} onRetry={listState.reload} /> : null}
        {!listState.loading && !listState.error && (listState.data?.items.length ?? 0) === 0 ? (
          <EmptyBlock title={`暂无${title}`} description="提交后会显示在这里。" className="my-0" />
        ) : null}

        {listState.data?.items.map((item) => (
          <PayoutRow key={item.id} item={item} kind={kind} />
        ))}

        {listState.data ? (
          <Pager
            page={page}
            total={listState.data.total}
            pageSize={listState.data.page_size || CONSOLE_PAGE_SIZE}
            onChange={setPage}
          />
        ) : null}
      </CardContent>
    </Card>
  )
}

function PayoutRow({ item, kind }: { item: ReferralPayout; kind: 'withdraw' | 'convert' }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border px-4 py-3">
      <div className="min-w-0">
        <p className="text-sm text-foreground">
          {payoutKindLabel(item.kind || kind)}
          {item.channel ? ` · ${payoutChannelLabel(item.channel)}` : ''}
          {item.account_name ? ` · ${item.account_name}` : ''}
          {item.account_no ? ` · ${item.account_no}` : ''}
        </p>
        <p className="mt-0.5 text-xs text-muted-foreground">
          {item.remark || '—'} · 提交于 {formatDateTime(item.created_at)}
          {item.reviewed_at ? ` · 审核 ${formatDateTime(item.reviewed_at)}` : ''}
          {item.paid_at ? ` · 打款 ${formatDateTime(item.paid_at)}` : ''}
        </p>
        {item.review_note ? (
          <p className="mt-0.5 text-xs text-muted-foreground">审核备注：{item.review_note}</p>
        ) : null}
      </div>
      <div className="flex items-center gap-3">
        <span className="text-sm font-medium text-foreground">{formatMoney(item.amount)}</span>
        <StatusBadge tone={payoutStatusTone(item.status)} label={payoutStatusLabel(item.status)} />
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// 返佣流水 / 我邀请的会员
// ---------------------------------------------------------------------------

function RebateSection({ refreshKey }: { refreshKey: number }) {
  const [page, setPage] = useState(1)
  const listState = useAsync(
    () => listReferralRebates({ page, page_size: CONSOLE_PAGE_SIZE }),
    [page, refreshKey],
  )

  return (
    <Card>
      <CardHeader className="border-b">
        <CardTitle className="text-base">返佣流水</CardTitle>
        <CardDescription>
          按订单原价与生效比例结算；订单取消会冲回（金额为负、状态「已冲回」）。
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {listState.loading ? <LoadingBlock label="正在读取返佣流水…" /> : null}
        {listState.error ? <ErrorState message={listState.error} onRetry={listState.reload} /> : null}
        {!listState.loading && !listState.error && (listState.data?.items.length ?? 0) === 0 ? (
          <EmptyBlock
            title="暂无返佣流水"
            description="邀请的会员完成首笔支付后，这里会出现返佣记录。"
            className="my-0"
          />
        ) : null}

        {listState.data?.items.map((entry) => (
          <div
            key={entry.id}
            className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border px-4 py-3"
          >
            <div className="min-w-0">
              <p className="text-sm text-foreground">
                {entry.buyer_username || `会员 #${entry.buyer_id}`}
                <span className="ml-2 text-xs text-muted-foreground">{entry.order_trade_no}</span>
              </p>
              <p className="mt-0.5 text-xs text-muted-foreground">
                原价 {formatMoney(entry.base_amount)} · 券减 {formatMoney(entry.discount_amount)} ·
                比例 {entry.ratio_percent}%（{ratioSourceLabel(entry.ratio_source)}）·{' '}
                {formatDateTime(entry.created_at)}
              </p>
              {entry.note ? (
                <p className="mt-0.5 text-xs text-muted-foreground">{entry.note}</p>
              ) : null}
            </div>
            <div className="flex items-center gap-3">
              <span className="text-sm font-medium text-success dark:text-green-400">
                +{formatMoney(entry.rebate_amount)}
              </span>
              <StatusBadge
                tone={referralRebateStatusTone(entry.status)}
                label={referralRebateStatusLabel(entry.status)}
              />
            </div>
          </div>
        ))}

        {listState.data ? (
          <Pager
            page={page}
            total={listState.data.total}
            pageSize={listState.data.page_size || CONSOLE_PAGE_SIZE}
            onChange={setPage}
          />
        ) : null}
      </CardContent>
    </Card>
  )
}

function InviteeSection({ refreshKey }: { refreshKey: number }) {
  const [page, setPage] = useState(1)
  const listState = useAsync(
    () => listReferralInvitees({ page, page_size: CONSOLE_PAGE_SIZE }),
    [page, refreshKey],
  )

  return (
    <Card>
      <CardHeader className="border-b">
        <CardTitle className="text-base">我邀请的会员</CardTitle>
        <CardDescription>
          消费口径为已入账订单实付之和；贡献返佣只计仍处「已发放」的返佣。
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {listState.loading ? <LoadingBlock label="正在读取邀请列表…" /> : null}
        {listState.error ? <ErrorState message={listState.error} onRetry={listState.reload} /> : null}
        {!listState.loading && !listState.error && (listState.data?.items.length ?? 0) === 0 ? (
          <EmptyBlock
            title="还没有邀请到会员"
            description="把邀请链接发给好友，注册后即计入你的邀请。"
            className="my-0"
          />
        ) : null}

        {listState.data?.items.map((invitee) => (
          <div
            key={invitee.member_id}
            className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border px-4 py-3"
          >
            <div className="min-w-0">
              <p className="text-sm text-foreground">{invitee.username}</p>
              <p className="mt-0.5 text-xs text-muted-foreground">
                注册于 {formatDateTime(invitee.joined_at)} · 下单 {invitee.order_count} 笔
              </p>
            </div>
            <div className="text-right">
              <p className="text-sm font-medium text-foreground">
                消费 {formatMoney(invitee.total_spent)}
              </p>
              <p className="mt-0.5 text-xs text-muted-foreground">
                贡献返佣 {formatMoney(invitee.total_rebate)}
              </p>
            </div>
          </div>
        ))}

        {listState.data ? (
          <Pager
            page={page}
            total={listState.data.total}
            pageSize={listState.data.page_size || CONSOLE_PAGE_SIZE}
            onChange={setPage}
          />
        ) : null}
      </CardContent>
    </Card>
  )
}

/** RFC3339 UTC → `<input type="datetime-local">` 的本地值（用于过期时间回填）。 */
function toLocalInputValue(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return ''
  }
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(
    date.getHours(),
  )}:${pad(date.getMinutes())}`
}
