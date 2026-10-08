import { http } from './client'
import type { CreateOrderInput, Order, OrderPayResult, PayOrderInput } from './types'

// 会员端订单与支付接口（契约 12.4）：全部要求会员 token，且只能操作本人数据。

/** POST /api/v1/orders —— 下单（新购），返回待支付订单。 */
export function createOrder(input: CreateOrderInput): Promise<Order> {
  return http.post<Order>('/orders', input, { auth: true })
}

/** GET /api/v1/orders/:id —— 本人订单详情（他人订单与不存在统一 404）。 */
export function fetchOrder(id: number): Promise<Order> {
  return http.get<Order>(`/orders/${id}`, { auth: true })
}

/**
 * POST /api/v1/orders/:id/pay —— 发起支付。
 * channel=epay 返回 payurl（跳转渠道）；channel=balance 直接扣款并入账。
 */
export function payOrder(id: number, input: PayOrderInput): Promise<OrderPayResult> {
  return http.post<OrderPayResult>(`/orders/${id}/pay`, input, { auth: true })
}

/** POST /api/v1/orders/:id/cancel —— 取消待支付订单。 */
export function cancelOrder(id: number): Promise<Order> {
  return http.post<Order>(`/orders/${id}/cancel`, undefined, { auth: true })
}
