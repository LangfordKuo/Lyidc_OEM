import { http } from './client'
import type {
  CreateConversionInput,
  CreateMemberCouponInput,
  CreateWithdrawalInput,
  MemberCoupon,
  Paged,
  ReferralInvitee,
  ReferralOverview,
  ReferralPayout,
  ReferralRebate,
  UpdateMemberCouponInput,
  UpdateReferralInput,
} from './types'

// 会员邀请返佣接口（契约 18.5）：全部仅本人（会员 token）。
// 提交类操作的失败 message 由后端写好（含上限 / 余额不足 / 负数余额等口径），调用方原样展示。

/** GET /api/v1/referral —— 我的返佣概览。 */
export function fetchReferral(): Promise<ReferralOverview> {
  return http.get<ReferralOverview>('/referral', { auth: true })
}

/** PUT /api/v1/referral —— 开通 / 关闭邀请返现（首次开通惰性生成邀请码）。 */
export function updateReferral(input: UpdateReferralInput): Promise<ReferralOverview> {
  return http.put<ReferralOverview>('/referral', input, { auth: true })
}

/** GET /api/v1/referral/rebates —— 我的返佣流水分页（新建在前）。 */
export function listReferralRebates(
  params: { page?: number; page_size?: number } = {},
): Promise<Paged<ReferralRebate>> {
  return http.get<Paged<ReferralRebate>>('/referral/rebates', { auth: true, query: { ...params } })
}

/** GET /api/v1/referral/invitees —— 我邀请的会员分页。 */
export function listReferralInvitees(
  params: { page?: number; page_size?: number } = {},
): Promise<Paged<ReferralInvitee>> {
  return http.get<Paged<ReferralInvitee>>('/referral/invitees', { auth: true, query: { ...params } })
}

/** GET /api/v1/referral/coupons —— 我创建的自建券分页（新建在前）。 */
export function listReferralCoupons(
  params: { page?: number; page_size?: number } = {},
): Promise<Paged<MemberCoupon>> {
  return http.get<Paged<MemberCoupon>>('/referral/coupons', { auth: true, query: { ...params } })
}

/**
 * POST /api/v1/referral/coupons —— 创建自建券（必须已开通邀请返现）。
 * 折扣上限由后端判定，40002 的 message 已含「上限 = min(设置 x, 生效比例 y)」，前端原样展示。
 */
export function createReferralCoupon(input: CreateMemberCouponInput): Promise<MemberCoupon> {
  return http.post<MemberCoupon>('/referral/coupons', input, { auth: true })
}

/**
 * PUT /api/v1/referral/coupons/:id —— 只允许改 status 与 expires_at。
 * 不可改字段（value / code / type / owner）出现在请求体会被 40001 拒绝，故封装层也不接受它们。
 */
export function updateReferralCoupon(
  id: number,
  input: UpdateMemberCouponInput,
): Promise<MemberCoupon> {
  return http.put<MemberCoupon>(`/referral/coupons/${id}`, input, { auth: true })
}

/**
 * POST /api/v1/referral/withdrawals —— 提交提现申请（提交即冻结返现余额）。
 * 账号字段提交后回执已脱敏，列表同口径。
 */
export function createReferralWithdrawal(input: CreateWithdrawalInput): Promise<ReferralPayout> {
  return http.post<ReferralPayout>('/referral/withdrawals', input, { auth: true })
}

/** GET /api/v1/referral/withdrawals —— 我的提现申请分页（新建在前，可按 status 过滤）。 */
export function listReferralWithdrawals(
  params: { page?: number; page_size?: number; status?: string } = {},
): Promise<Paged<ReferralPayout>> {
  return http.get<Paged<ReferralPayout>>('/referral/withdrawals', {
    auth: true,
    query: { ...params },
  })
}

/** POST /api/v1/referral/conversions —— 申请返现余额转消费余额（提交时不动钱）。 */
export function createReferralConversion(input: CreateConversionInput): Promise<ReferralPayout> {
  return http.post<ReferralPayout>('/referral/conversions', input, { auth: true })
}

/** GET /api/v1/referral/conversions —— 我的转余额申请分页（kind=convert）。 */
export function listReferralConversions(
  params: { page?: number; page_size?: number; status?: string } = {},
): Promise<Paged<ReferralPayout>> {
  return http.get<Paged<ReferralPayout>>('/referral/conversions', {
    auth: true,
    query: { ...params },
  })
}
