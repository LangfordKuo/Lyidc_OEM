import { http } from './client'
import type {
  MarkPaidInput,
  Paged,
  PayoutReviewInput,
  PayoutReviewResult,
  ReferralPayout,
} from './types'

// 管理端返佣管理接口（契约 18.5 / 18.6）。
// 角色矩阵：查看类（列表）admin + finance；审核与打款**仅 admin**（finance / support 一律 403，
// 界面侧按同一矩阵隐藏/禁用入口，且不发起无权限请求）。

/** GET /api/v1/admin/referral/withdrawals —— 提现申请分页（kind 缺省 withdraw，传 all 两类都查）。 */
export function listAdminWithdrawals(
  params: {
    page?: number
    page_size?: number
    status?: string
    member_id?: number
    kind?: string
  } = {},
): Promise<Paged<ReferralPayout>> {
  return http.get<Paged<ReferralPayout>>('/admin/referral/withdrawals', {
    auth: 'admin',
    query: { ...params },
  })
}

/** GET /api/v1/admin/referral/conversions —— 转余额申请分页（kind=convert）。 */
export function listAdminConversions(
  params: { page?: number; page_size?: number; status?: string; member_id?: number } = {},
): Promise<Paged<ReferralPayout>> {
  return http.get<Paged<ReferralPayout>>('/admin/referral/conversions', {
    auth: 'admin',
    query: { ...params },
  })
}

/**
 * POST /api/v1/admin/referral/withdrawals/:id/review —— 审核提现（仅 admin）。
 * approve 只改状态；reject 退回冻结额。重复审核幂等（already=true）。
 */
export function reviewAdminWithdrawal(
  id: number,
  input: PayoutReviewInput,
): Promise<PayoutReviewResult> {
  return http.post<PayoutReviewResult>(`/admin/referral/withdrawals/${id}/review`, input, {
    auth: 'admin',
  })
}

/**
 * POST /api/v1/admin/referral/withdrawals/:id/mark-paid —— 标记线下已打款（仅 admin）。
 * 仅 approved 可标 paid；重复打款幂等且不覆盖首次 paid_at。
 */
export function markAdminWithdrawalPaid(
  id: number,
  input: MarkPaidInput = {},
): Promise<PayoutReviewResult> {
  return http.post<PayoutReviewResult>(`/admin/referral/withdrawals/${id}/mark-paid`, input, {
    auth: 'admin',
  })
}

/**
 * POST /api/v1/admin/referral/conversions/:id/review —— 审核转余额（仅 admin）。
 * approve 同事务换算入消费余额；reject 不动余额。换算只生效一次。
 */
export function reviewAdminConversion(
  id: number,
  input: PayoutReviewInput,
): Promise<PayoutReviewResult> {
  return http.post<PayoutReviewResult>(`/admin/referral/conversions/${id}/review`, input, {
    auth: 'admin',
  })
}
