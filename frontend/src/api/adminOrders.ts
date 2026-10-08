import { http } from './client'
import type { Order } from './types'

// 管理端订单接口（契约 14.4）。
//
// 现状说明（阶段 8）：契约只定义了**重试交付**一个管理端订单接口——
// `GET /admin/orders`（列表）与 `GET /admin/orders/:id`（详情）**尚不存在**，
// 因此后台「订单」页只能按订单 ID 执行重试交付，不能列举/搜索订单（缺口已列入交付报告）。

/**
 * POST /api/v1/admin/orders/:id/retry-delivery —— 重试交付（**仅 admin 角色**，同步执行一次完整交付）。
 * 交付执行失败时订单已置 failed 并记录脱敏原因，接口仍返回 200 + 最新订单视图。
 */
export function retryOrderDelivery(orderId: number): Promise<Order> {
  return http.post<Order>(`/admin/orders/${orderId}/retry-delivery`, undefined, {
    auth: 'admin',
    // 同步执行一次完整交付（上游开通 + 回读），超时放宽到 2 分钟。
    timeoutMs: 120_000,
  })
}
