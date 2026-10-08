import { http } from './client'
import type { AdminOrder, Order, OrderStatus, OrderType, Paged } from './types'

// 管理端订单接口（契约 12.6 / 14.4）。
//
// 角色矩阵：
//   - 列表（`GET /admin/orders`）与详情（`GET /admin/orders/:id`）：**admin + finance + support 均可读**
//     （客服协助会员查询是日常）；
//   - 重试交付（`POST /admin/orders/:id/retry-delivery`）：**仅 admin**（会真实调用上游开通，其余角色 403）。
// 界面侧按同一矩阵隐藏或禁用入口，服务端仍会独立校验（403 统一处理）。

/**
 * GET /api/v1/admin/orders —— 全站订单分页（新建在前，带会员概要）。
 * 筛选：status / type / member_id / trade_no（模糊匹配）；缺省项不参与筛选。
 */
export function listAdminOrders(
  params: {
    page?: number
    page_size?: number
    status?: OrderStatus
    type?: OrderType
    member_id?: number
    trade_no?: string
  } = {},
): Promise<Paged<AdminOrder>> {
  return http.get<Paged<AdminOrder>>('/admin/orders', { auth: 'admin', query: { ...params } })
}

/** GET /api/v1/admin/orders/:id —— 订单详情（订单全字段 + 会员概要 + 交付信息）。 */
export function fetchAdminOrder(id: number): Promise<AdminOrder> {
  return http.get<AdminOrder>(`/admin/orders/${id}`, { auth: 'admin' })
}

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
