import { useEffect, useState } from 'react'
import { AlertCircleIcon } from 'lucide-react'
import { toast } from 'sonner'

import { errorMessage } from '@/api/client'
import { createRecharge, fetchBalance, listLedger, listRecharges } from '@/api/finance'
import type { EpayType } from '@/api/types'
import { useAuth } from '@/auth/authContext'
import Pager from '@/components/common/Pager'
import { EmptyBlock, ErrorState, LoadingBlock } from '@/components/common/PageState'
import StatusBadge from '@/components/common/StatusBadge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useAsync } from '@/hooks/useAsync'
import { ledgerTypeLabel, rechargeStatusLabel, rechargeStatusTone } from '@/lib/financeText'
import { formatDateTime, formatMoney } from '@/lib/format'
import { EPAY_TYPES, gotoPayurl } from '@/lib/pay'
import { CONSOLE_PAGE_SIZE } from '@/lib/pagination'
import { validateRechargeAmount } from '@/lib/validate'
import { cn } from '@/lib/utils'

const QUICK_AMOUNTS = ['50', '100', '500', '1000', '2000']

// ConsoleRecharge 是会员区「余额充值」（契约 12.4）：余额卡片 + 充值表单 + 充值记录 / 余额流水。
// 支付通道与下单页一致（易支付：支付宝 / 微信），跳转渠道收银台完成付款。
export default function ConsoleRecharge() {
  const { member, setBalance } = useAuth()
  const [refreshKey, setRefreshKey] = useState(0)

  const balanceState = useAsync(fetchBalance, [refreshKey])
  const balance = balanceState.data?.balance ?? member?.balance ?? '0.00'

  // 余额与登录态里的缓存保持一致：顶栏用户菜单显示实时余额。
  useEffect(() => {
    if (balanceState.data?.balance) {
      setBalance(balanceState.data.balance)
    }
  }, [balanceState.data?.balance, setBalance])

  const [amount, setAmount] = useState('')
  const [payType, setPayType] = useState<EpayType>('alipay')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  const amountError = amount ? validateRechargeAmount(amount) : ''

  const [rechargePage, setRechargePage] = useState(1)
  const [ledgerPage, setLedgerPage] = useState(1)

  const rechargesState = useAsync(
    () => listRecharges({ page: rechargePage, page_size: CONSOLE_PAGE_SIZE }),
    [rechargePage, refreshKey],
  )
  const ledgerState = useAsync(
    () => listLedger({ page: ledgerPage, page_size: CONSOLE_PAGE_SIZE }),
    [ledgerPage, refreshKey],
  )

  const handleSubmit = async () => {
    const trimmed = amount.trim()
    const message = validateRechargeAmount(trimmed)
    if (message) {
      setError(message)
      return
    }
    setPending(true)
    setError('')
    try {
      const result = await createRecharge({
        amount: trimmed,
        channel: 'epay',
        pay_type: payType,
      })
      const payurl = result.pay.payurl
      if (!payurl) {
        setError('支付渠道未返回支付地址，请稍后重试')
        return
      }
      toast.info(`充值单 ${result.recharge.trade_no} 已创建，即将跳转支付渠道…`)
      gotoPayurl(payurl)
    } catch (err) {
      setError(errorMessage(err, '创建充值单失败，请稍后重试'))
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="space-y-5">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-foreground">余额充值</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            充值到账后可用于余额支付（即时开通，无需跳转渠道）。
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={() => setRefreshKey((value) => value + 1)}>
          刷新
        </Button>
      </header>

      <div className="grid gap-5 lg:grid-cols-[320px_minmax(0,1fr)]">
        <Card>
          <CardHeader className="border-b">
            <CardTitle className="text-base">账户余额</CardTitle>
            <CardDescription>以服务端实时值为准</CardDescription>
          </CardHeader>
          <CardContent className="space-y-2">
            <p className="text-3xl font-semibold text-foreground">{formatMoney(balance)}</p>
            {balanceState.error ? (
              <p className="text-xs text-destructive">余额读取失败：{balanceState.error}</p>
            ) : null}
            <p className="text-xs text-muted-foreground">会员 ID {member?.id ?? '—'}</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="border-b">
            <CardTitle className="text-base">在线充值</CardTitle>
            <CardDescription>金额范围 ¥1.00 ~ ¥50000.00（最多两位小数）</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="space-y-1.5">
              <Label htmlFor="recharge_amount">充值金额（元）</Label>
              <Input
                id="recharge_amount"
                inputMode="decimal"
                placeholder="如 100"
                value={amount}
                aria-invalid={Boolean(amount && amountError)}
                onChange={(event) => {
                  setAmount(event.target.value)
                  setError('')
                }}
              />
              {amount && amountError ? (
                <p className="text-xs text-destructive">{amountError}</p>
              ) : null}
            </div>

            <div className="flex flex-wrap gap-2">
              {QUICK_AMOUNTS.map((value) => (
                <Button
                  key={value}
                  size="sm"
                  variant={amount === value ? 'default' : 'outline'}
                  aria-pressed={amount === value}
                  onClick={() => {
                    setAmount(value)
                    setError('')
                  }}
                >
                  ¥{value}
                </Button>
              ))}
            </div>

            <div className="space-y-1.5">
              <p className="text-sm font-medium text-foreground">支付方式</p>
              <RadioGroup
                value={payType}
                onValueChange={(value) => setPayType(value as EpayType)}
                aria-label="支付方式"
                className="flex flex-wrap gap-4"
              >
                {EPAY_TYPES.map((item) => (
                  <Label key={item.value} className="flex cursor-pointer items-center gap-2 text-sm">
                    <RadioGroupItem value={item.value} />
                    {item.label}
                  </Label>
                ))}
              </RadioGroup>
            </div>

            {error ? (
              <Alert variant="destructive">
                <AlertCircleIcon aria-hidden />
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            ) : null}

            <div className="flex flex-wrap items-center gap-3">
              <Button disabled={pending || Boolean(amount && amountError)} onClick={handleSubmit}>
                {pending ? '正在创建充值单…' : '去支付'}
              </Button>
              <span className="text-xs text-muted-foreground">
                支付完成后余额自动到账；若未到账可稍后在充值记录中查看状态。
              </span>
            </div>
          </CardContent>
        </Card>
      </div>

      <Tabs defaultValue="recharges">
        <TabsList aria-label="充值记录与余额流水">
          <TabsTrigger value="recharges">充值记录</TabsTrigger>
          <TabsTrigger value="ledger">余额流水</TabsTrigger>
        </TabsList>

        <TabsContent value="recharges" className="pt-2">
          <Card>
            <CardHeader className="border-b">
              <CardTitle className="text-base">充值记录</CardTitle>
              <CardDescription>新建在前；渠道下单失败时充值单会保持「待支付」。</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              {rechargesState.loading ? <LoadingBlock label="正在读取充值记录…" /> : null}
              {rechargesState.error ? (
                <ErrorState message={rechargesState.error} onRetry={rechargesState.reload} />
              ) : null}
              {!rechargesState.loading &&
              !rechargesState.error &&
              (rechargesState.data?.items.length ?? 0) === 0 ? (
                <EmptyBlock
                  title="暂无充值记录"
                  description="完成一次在线充值后会显示在这里。"
                  className="my-0"
                />
              ) : null}

              {rechargesState.data?.items.map((recharge) => (
                <div
                  key={recharge.id}
                  className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border px-4 py-3"
                >
                  <div className="min-w-0">
                    <p className="font-mono text-xs text-muted-foreground">{recharge.trade_no}</p>
                    <p className="mt-0.5 text-xs text-muted-foreground">
                      创建于 {formatDateTime(recharge.created_at)}
                      {recharge.paid_at ? ` · 到账 ${formatDateTime(recharge.paid_at)}` : ''}
                    </p>
                  </div>
                  <div className="flex items-center gap-3">
                    <span className="text-sm font-medium text-foreground">
                      {formatMoney(recharge.amount)}
                    </span>
                    <StatusBadge
                      tone={rechargeStatusTone(recharge.status)}
                      label={rechargeStatusLabel(recharge.status)}
                    />
                  </div>
                </div>
              ))}

              {rechargesState.data ? (
                <Pager
                  page={rechargePage}
                  total={rechargesState.data.total}
                  pageSize={rechargesState.data.page_size || CONSOLE_PAGE_SIZE}
                  onChange={setRechargePage}
                />
              ) : null}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="ledger" className="pt-2">
          <Card>
            <CardHeader className="border-b">
              <CardTitle className="text-base">余额流水</CardTitle>
              <CardDescription>入账为正、出账为负；每笔都记录变动前后余额。</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              {ledgerState.loading ? <LoadingBlock label="正在读取余额流水…" /> : null}
              {ledgerState.error ? (
                <ErrorState message={ledgerState.error} onRetry={ledgerState.reload} />
              ) : null}
              {!ledgerState.loading && !ledgerState.error && (ledgerState.data?.items.length ?? 0) === 0 ? (
                <EmptyBlock
                  title="暂无余额流水"
                  description="充值入账与余额支付都会产生流水。"
                  className="my-0"
                />
              ) : null}

              {ledgerState.data?.items.map((entry) => {
                const negative = entry.amount.trim().startsWith('-')
                return (
                  <div
                    key={entry.id}
                    className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border px-4 py-3"
                  >
                    <div className="min-w-0">
                      <p className="text-sm text-foreground">{ledgerTypeLabel(entry.type)}</p>
                      <p className="mt-0.5 text-xs text-muted-foreground">
                        {entry.note || '—'} · {formatDateTime(entry.created_at)}
                      </p>
                    </div>
                    <div className="text-right">
                      <p
                        className={cn(
                          'text-sm font-medium',
                          negative
                            ? 'text-destructive'
                            : 'text-success dark:text-green-400',
                        )}
                      >
                        {negative ? '' : '+'}
                        {formatMoney(entry.amount)}
                      </p>
                      <p className="text-xs text-muted-foreground">
                        余额 {formatMoney(entry.balance_before)} → {formatMoney(entry.balance_after)}
                      </p>
                    </div>
                  </div>
                )
              })}

              {ledgerState.data ? (
                <Pager
                  page={ledgerPage}
                  total={ledgerState.data.total}
                  pageSize={ledgerState.data.page_size || CONSOLE_PAGE_SIZE}
                  onChange={setLedgerPage}
                />
              ) : null}
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  )
}
