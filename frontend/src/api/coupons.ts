import { http } from './client'
import type { BillingCycle } from '../lib/cycles'
import type { CouponValidation } from './types'

// 优惠码公开校验（契约 11.4）：无需鉴权，用于下单前的折扣试算。
// 注意：校验通过不代表下单一定成功（并发下 max_uses 可能被抢占），
// 下单接口会重新判定并返回 40002 + 明确 message。

/** GET /api/v1/coupons/:code/validate?product_id&cycle —— 折扣试算。 */
export function validateCoupon(
  code: string,
  productId: number,
  cycle: BillingCycle,
): Promise<CouponValidation> {
  return http.get<CouponValidation>(`/coupons/${encodeURIComponent(code)}/validate`, {
    query: { product_id: productId, cycle },
  })
}
