import { http } from './client'
import type {
  AdminProduct,
  AdminProductDetail,
  AdminProductGroup,
  Paged,
  ProductImportResult,
  UpdateProductInput,
} from './types'

// 管理端商品接口（契约 10.4）。
// 角色矩阵：查看类（列表/详情/分组）三类角色均可读；导入 / 改定价 / 上下架 / 改分组为
// admin + finance，support 一律 403（界面侧按同一矩阵隐藏/禁用入口）。

/** GET /api/v1/admin/products —— 商品分页（含下架商品；group_id/status/keyword 过滤）。 */
export function listAdminProducts(
  params: {
    page?: number
    page_size?: number
    group_id?: number
    status?: 'on' | 'off'
    keyword?: string
  } = {},
): Promise<Paged<AdminProduct>> {
  return http.get<Paged<AdminProduct>>('/admin/products', { auth: 'admin', query: { ...params } })
}

/** GET /api/v1/admin/products/:id —— 商品详情（含描述、可配置项与上游价格缓存原文）。 */
export function fetchAdminProduct(id: number): Promise<AdminProductDetail> {
  return http.get<AdminProductDetail>(`/admin/products/${id}`, { auth: 'admin' })
}

/**
 * PUT /api/v1/admin/products/:id —— 改定价 / 上下架 / 排序（admin / finance）。
 * pricing_json 传 null 表示重置为缺省规则（直接用上游价）；至少提供一个字段。
 */
export function updateAdminProduct(
  id: number,
  input: UpdateProductInput,
): Promise<AdminProductDetail> {
  return http.put<AdminProductDetail>(`/admin/products/${id}`, input, { auth: 'admin' })
}

/** POST /api/v1/admin/products/import —— 从上游导入商品目录（admin / finance，可能耗时数十秒）。 */
export function importAdminProducts(): Promise<ProductImportResult> {
  return http.post<ProductImportResult>('/admin/products/import', undefined, {
    auth: 'admin',
    // 导入整次上限 5 分钟（契约 10.4），前端超时放宽到 5 分钟 + 缓冲。
    timeoutMs: 320_000,
  })
}

/** GET /api/v1/admin/product-groups —— 全部分组（含空分组）与商品计数。 */
export function listAdminProductGroups(): Promise<{ items: AdminProductGroup[] }> {
  return http.get<{ items: AdminProductGroup[] }>('/admin/product-groups', { auth: 'admin' })
}

/** PUT /api/v1/admin/product-groups/:id —— 分组重命名 / 改排序（admin / finance）。 */
export function updateAdminProductGroup(
  id: number,
  input: { name?: string; sort?: number },
): Promise<AdminProductGroup> {
  return http.put<AdminProductGroup>(`/admin/product-groups/${id}`, input, { auth: 'admin' })
}
