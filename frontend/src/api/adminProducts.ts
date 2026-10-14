import { http } from './client'
import type {
  AdminProduct,
  AdminProductDetail,
  AdminProductGroup,
  ImportProductsInput,
  Paged,
  ProductGroupCategoryMap,
  ProductImportResult,
  ProductSyncResult,
  SyncProductsInput,
  UpdateProductInput,
} from './types'

// 管理端商品接口（契约 10.4）。
// 角色矩阵（契约 10.5）：查看类（列表/详情/分组）三类角色均可读；导入 / 改定价 / 上下架 /
// 改分组为 admin + finance，support 一律 403（界面侧按同一矩阵隐藏/禁用入口）。

/**
 * GET /api/v1/admin/products —— 商品分页（含下架商品）。
 * 过滤：group_id（本地分组 ID）/ status / keyword / category_id（本地子分类 ID，0 = 只看未归类）。
 */
export function listAdminProducts(
  params: {
    page?: number
    page_size?: number
    group_id?: number
    status?: 'on' | 'off'
    keyword?: string
    category_id?: number
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

/**
 * POST /api/v1/admin/products/import —— 从上游导入商品目录（admin / finance，可能耗时数十秒）。
 *
 * R12：可选 `{ upstream_group_ids: [...] }` 只导入选中的上游分组；**省略请求体 = 全量导入**
 * （调用方在全选时传 undefined，保持「缺省即全量」语义）。
 */
export function importAdminProducts(input?: ImportProductsInput): Promise<ProductImportResult> {
  return http.post<ProductImportResult>('/admin/products/import', input, {
    auth: 'admin',
    // 导入整次上限 5 分钟（契约 10.4），前端超时放宽到 5 分钟 + 缓冲。
    timeoutMs: 320_000,
  })
}

/**
 * POST /api/v1/admin/products/sync —— 把选中的**已存在**商品刷成上游最新（R20，**仅 admin**，契约 10.7）。
 *
 * 语义边界：只更新已存在的商品，**不新增、不删除**；上游来源字段按导入同一口径刷新，
 * 本地字段（定价/售价/上下架/排序/返佣/手工分类）一律不动。
 * 单条失败不影响其它条目——响应里逐条给出成功/失败与可读原因。
 */
export function syncAdminProducts(input: SyncProductsInput): Promise<ProductSyncResult> {
  return http.post<ProductSyncResult>('/admin/products/sync', input, {
    auth: 'admin',
    // 后端整次同步的上游调用预算 2 分钟（契约 10.7），前端超时放宽到 3 分钟 + 缓冲。
    timeoutMs: 200_000,
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

// —— 上游分组 → 本地子分类映射（R12，契约 10.4）——
// 角色矩阵：查看类（GET）三角色只读；写操作沿用 categories.write（**仅 admin**）。

/** GET /api/v1/admin/product-group-maps —— 全部映射（按 upstream_group_id 升序，不分页）。 */
export function listAdminProductGroupMaps(): Promise<{ items: ProductGroupCategoryMap[] }> {
  return http.get<{ items: ProductGroupCategoryMap[] }>('/admin/product-group-maps', {
    auth: 'admin',
  })
}

/** PUT /api/v1/admin/product-group-maps/:upstream_group_id —— 新增/更新映射（仅 admin）。 */
export function upsertAdminProductGroupMap(
  upstreamGroupId: number,
  categoryId: number,
): Promise<ProductGroupCategoryMap> {
  return http.put<ProductGroupCategoryMap>(
    `/admin/product-group-maps/${upstreamGroupId}`,
    { category_id: categoryId },
    { auth: 'admin' },
  )
}

/** DELETE /api/v1/admin/product-group-maps/:upstream_group_id —— 删除映射（仅 admin）。 */
export function deleteAdminProductGroupMap(upstreamGroupId: number): Promise<null> {
  return http.del<null>(`/admin/product-group-maps/${upstreamGroupId}`, { auth: 'admin' })
}
