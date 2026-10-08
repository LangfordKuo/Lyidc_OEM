import { http } from './client'
import type { BalanceInfo } from './types'

// 会员财务接口（契约 12.4）。本批（7a）只用到余额读取；
// 充值单与流水在会员区（7b）落地时接入 /recharges 与 /finance/ledger。

/** GET /api/v1/finance/balance —— 实时余额（会员 token）。 */
export function fetchBalance(): Promise<BalanceInfo> {
  return http.get<BalanceInfo>('/finance/balance', { auth: 'member' })
}
