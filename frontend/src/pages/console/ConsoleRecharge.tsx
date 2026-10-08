import { Alert, Button, Card, Input, Label, Radio, RadioGroup, Tabs, TextField, toast } from '@heroui/react'
import { useEffect, useState } from 'react'

import { errorMessage } from '../../api/client'
import { createRecharge, fetchBalance, listLedger, listRecharges } from '../../api/finance'
import type { EpayType } from '../../api/types'
import { useAuth } from '../../auth/authContext'
import StatusBadge from '../../components/StatusBadge'
import Pager from '../../components/common/Pager'
import { EmptyBlock, ErrorState, LoadingBlock } from '../../components/common/PageState'
import { useAsync } from '../../hooks/useAsync'
import { ledgerTypeLabel, rechargeStatusLabel, rechargeStatusTone } from '../../lib/financeText'
import { formatDateTime, formatMoney } from '../../lib/format'
import { EPAY_TYPES, gotoPayurl } from '../../lib/pay'
import { DEFAULT_PAGE_SIZE } from '../../lib/pagination'
import { validateRechargeAmount } from '../../lib/validate'

const QUICK_AMOUNTS = ['50', '100', '500', '1000', '2000']

// ConsoleRecharge 是会员区「余额充值」：余额卡片 + 充值表单 + 充值记录 / 余额流水两个页签。
// 支付通道与 7a 下单页一致（易支付：支付宝 / 微信），跳转渠道收银台完成付款。
export default function ConsoleRecharge() {
  const { member, setBalance } = useAuth()
  const [refreshKey, setRefreshKey] = useState(0)
  const [tab, setTab] = useState<'recharges' | 'ledger'>('recharges')

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
    () => listRecharges({ page: rechargePage, page_size: DEFAULT_PAGE_SIZE }),
    [rechargePage, refreshKey],
  )
  const ledgerState = useAsync(
    () => listLedger({ page: ledgerPage, page_size: DEFAULT_PAGE_SIZE }),
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
          <h1 className="text-xl font-semibold text-foreground">余额充值</h1>
          <p className="mt-1 text-sm text-muted">
            充值到账后可用于余额支付（即时开通，无需跳转渠道）。
          </p>
        </div>
        <Button variant="outline" size="sm" onPress={() => setRefreshKey((value) => value + 1)}>
          刷新
        </Button>
      </header>

      <div className="grid gap-5 lg:grid-cols-[320px_minmax(0,1fr)]">
        <Card>
          <Card.Header>
            <Card.Title className="text-base">账户余额</Card.Title>
            <Card.Description>以服务端实时值为准</Card.Description>
          </Card.Header>
          <Card.Content className="space-y-2">
            <p className="text-3xl font-semibold text-foreground">{formatMoney(balance)}</p>
            {balanceState.error ? (
              <p className="text-xs text-danger">余额读取失败：{balanceState.error}</p>
            ) : null}
            <p className="text-xs text-muted">会员 ID {member?.id ?? '—'}</p>
          </Card.Content>
        </Card>

        <Card>
          <Card.Header>
            <Card.Title className="text-base">在线充值</Card.Title>
            <Card.Description>金额范围 ¥1.00 ~ ¥50000.00（最多两位小数）</Card.Description>
          </Card.Header>
          <Card.Content className="space-y-4">
            <TextField
              name="recharge_amount"
              value={amount}
              onChange={(value) => {
                setAmount(value)
                setError('')
              }}
              isInvalid={Boolean(amount && amountError)}
            >
              <Label>充值金额（元）</Label>
              <Input placeholder="如 100" inputMode="decimal" />
              {amount && amountError ? (
                <p className="mt-1 text-xs text-danger">{amountError}</p>
              ) : null}
            </TextField>

            <div className="flex flex-wrap gap-2">
              {QUICK_AMOUNTS.map((value) => (
                <Button
                  key={value}
                  size="sm"
                  variant={amount === value ? 'primary' : 'outline'}
                  onPress={() => {
                    setAmount(value)
                    setError('')
                  }}
                >
                  ¥{value}
                </Button>
              ))}
            </div>

            <div>
              <p className="mb-2 text-sm font-medium text-foreground">支付方式</p>
              <RadioGroup
                aria-label="支付方式"
                value={payType}
                onChange={(value) => setPayType(value as EpayType)}
                orientation="horizontal"
                className="gap-3"
              >
                {EPAY_TYPES.map((item) => (
                  // HeroUI v3 的 Radio 必须用 Radio.Content 包裹才是可交互控件（否则渲染为不可选中的纯文本），
                  // 圆圈（Radio.Control / Radio.Indicator）也要放在 Radio.Content 内、文本之前。
                  <Radio key={item.value} value={item.value}>
                    <Radio.Content>
                      <Radio.Control>
                        <Radio.Indicator />
                      </Radio.Control>
                      {item.label}
                    </Radio.Content>
                  </Radio>
                ))}
              </RadioGroup>
            </div>

            {error ? (
              <Alert status="danger">
                <Alert.Indicator />
                <Alert.Content>
                  <Alert.Description>{error}</Alert.Description>
                </Alert.Content>
              </Alert>
            ) : null}

            <div className="flex items-center gap-3">
              <Button
                variant="primary"
                isDisabled={pending || Boolean(amount && amountError)}
                onPress={handleSubmit}
              >
                {pending ? '正在创建充值单…' : '去支付'}
              </Button>
              <span className="text-xs text-muted">
                支付完成后余额自动到账；若未到账可稍后在充值记录中查看状态。
              </span>
            </div>
          </Card.Content>
        </Card>
      </div>

      <Tabs selectedKey={tab} onSelectionChange={(key) => setTab(key as 'recharges' | 'ledger')}>
        <Tabs.ListContainer>
          <Tabs.List aria-label="充值记录与余额流水">
            <Tabs.Tab id="recharges">充值记录</Tabs.Tab>
            <Tabs.Tab id="ledger">余额流水</Tabs.Tab>
          </Tabs.List>
        </Tabs.ListContainer>

        <Tabs.Panel id="recharges" className="pt-4">
          <Card>
            <Card.Header>
              <Card.Title className="text-base">充值记录</Card.Title>
              <Card.Description>新建在前；渠道下单失败时充值单会保持「待支付」。</Card.Description>
            </Card.Header>
            <Card.Content className="space-y-3">
              {rechargesState.loading ? <LoadingBlock label="正在读取充值记录…" /> : null}
              {rechargesState.error ? (
                <ErrorState message={rechargesState.error} onRetry={rechargesState.reload} />
              ) : null}
              {!rechargesState.loading &&
              !rechargesState.error &&
              (rechargesState.data?.items.length ?? 0) === 0 ? (
                <EmptyBlock title="暂无充值记录" description="完成一次在线充值后会显示在这里。" />
              ) : null}

              {rechargesState.data?.items.map((recharge) => (
                <div
                  key={recharge.id}
                  className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border px-4 py-3"
                >
                  <div className="min-w-0">
                    <p className="font-mono text-xs text-muted">{recharge.trade_no}</p>
                    <p className="mt-0.5 text-xs text-muted">
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

              {rechargesState.data && rechargesState.data.total > 0 ? (
                <Pager
                  page={rechargePage}
                  total={rechargesState.data.total}
                  pageSize={rechargesState.data.page_size || DEFAULT_PAGE_SIZE}
                  onChange={setRechargePage}
                />
              ) : null}
            </Card.Content>
          </Card>
        </Tabs.Panel>

        <Tabs.Panel id="ledger" className="pt-4">
          <Card>
            <Card.Header>
              <Card.Title className="text-base">余额流水</Card.Title>
              <Card.Description>入账为正、出账为负；每笔都记录变动前后余额。</Card.Description>
            </Card.Header>
            <Card.Content className="space-y-3">
              {ledgerState.loading ? <LoadingBlock label="正在读取余额流水…" /> : null}
              {ledgerState.error ? (
                <ErrorState message={ledgerState.error} onRetry={ledgerState.reload} />
              ) : null}
              {!ledgerState.loading &&
              !ledgerState.error &&
              (ledgerState.data?.items.length ?? 0) === 0 ? (
                <EmptyBlock title="暂无余额流水" description="充值入账与余额支付都会产生流水。" />
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
                      <p className="mt-0.5 text-xs text-muted">
                        {entry.note || '—'} · {formatDateTime(entry.created_at)}
                      </p>
                    </div>
                    <div className="text-right">
                      <p
                        className={`text-sm font-medium ${negative ? 'text-danger' : 'text-success'}`}
                      >
                        {negative ? '' : '+'}
                        {formatMoney(entry.amount)}
                      </p>
                      <p className="text-xs text-muted">
                        余额 {formatMoney(entry.balance_before)} → {formatMoney(entry.balance_after)}
                      </p>
                    </div>
                  </div>
                )
              })}

              {ledgerState.data && ledgerState.data.total > 0 ? (
                <Pager
                  page={ledgerPage}
                  total={ledgerState.data.total}
                  pageSize={ledgerState.data.page_size || DEFAULT_PAGE_SIZE}
                  onChange={setLedgerPage}
                />
              ) : null}
            </Card.Content>
          </Card>
        </Tabs.Panel>
      </Tabs>
    </div>
  )
}
