import { http } from './client'
import type {
  BalanceInfo,
  CreateRechargeInput,
  LedgerEntry,
  LedgerType,
  Paged,
  Recharge,
  RechargeResult,
  RechargeStatus,
} from './types'

// 会员财务接口（契约 12.4）：余额读取、充值单创建与查询、余额流水。

/** GET /api/v1/finance/balance —— 当前会员余额（实时读库）。 */
export function fetchBalance(): Promise<BalanceInfo> {
  return http.get<BalanceInfo>('/finance/balance', { auth: true })
}

/**
 * POST /api/v1/recharges —— 创建充值单并发起渠道支付（本批仅易支付）。
 * 渠道下单失败时充值单保持 pending，用户重新发起会生成新单号。
 */
export function createRecharge(input: CreateRechargeInput): Promise<RechargeResult> {
  return http.post<RechargeResult>('/recharges', input, { auth: true })
}

/** GET /api/v1/recharges —— 本人充值单分页（新建在前）。 */
export function listRecharges(
  params: { page?: number; page_size?: number; status?: RechargeStatus } = {},
): Promise<Paged<Recharge>> {
  return http.get<Paged<Recharge>>('/recharges', { auth: true, query: { ...params } })
}

/** GET /api/v1/finance/ledger —— 本人余额流水分页（新建在前）。 */
export function listLedger(
  params: { page?: number; page_size?: number; type?: LedgerType } = {},
): Promise<Paged<LedgerEntry>> {
  return http.get<Paged<LedgerEntry>>('/finance/ledger', { auth: true, query: { ...params } })
}
