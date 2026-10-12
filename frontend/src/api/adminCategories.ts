import { http } from './client'
import type { AdminCategory, AdminSubcategory, UpdateCategoryInput } from './types'

// 管理端分类接口（契约 10.4）。
// 角色矩阵（契约 10.5 / 12.7）：分类树（GET）三类角色只读；
// 改名 / 改排序 / 子分类改挂主分类（PUT）**仅 admin**——界面按 categories.write
// 隐藏或禁用写入口，且不发无权限请求（服务端仍独立 403）。

/** GET /api/v1/admin/categories —— 分类树（主分类 → 子分类）与每个子分类的商品计数。 */
export function listAdminCategories(): Promise<{ items: AdminCategory[] }> {
  return http.get<{ items: AdminCategory[] }>('/admin/categories', { auth: 'admin' })
}

/**
 * PUT /api/v1/admin/categories/:id —— 改名 / 改排序 / 子分类改挂主分类（仅 admin）。
 * 至少提供一个字段；主分类不可传 parent_id。返回更新后的分类（主分类带 subcategories）。
 */
export function updateAdminCategory(
  id: number,
  input: UpdateCategoryInput,
): Promise<AdminCategory | AdminSubcategory> {
  return http.put<AdminCategory | AdminSubcategory>(`/admin/categories/${id}`, input, {
    auth: 'admin',
  })
}
