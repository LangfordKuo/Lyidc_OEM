import { http } from './client'
import type { BalanceInfo } from './types'

// 会员余额（契约 12.4）：实时读库返回当前余额。

/** GET /api/v1/finance/balance —— 当前会员余额。 */
export function fetchBalance(): Promise<BalanceInfo> {
  return http.get<BalanceInfo>('/finance/balance', { auth: true })
}
