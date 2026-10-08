import { http } from './client'
import type { ProductCatalog, ProductDetail } from './types'

// 商品目录接口（契约 10.3）：无需鉴权，只暴露已上架商品，且不含任何上游 ID。

/** GET /api/v1/products —— 按分组组织的上架商品目录。 */
export function fetchProductCatalog(): Promise<ProductCatalog> {
  return http.get<ProductCatalog>('/products')
}

/**
 * GET /api/v1/products/:id —— 商品详情。
 * 商品不存在或已下架统一返回 404（对会员端等同于不存在）。
 */
export function fetchProductDetail(id: number): Promise<ProductDetail> {
  return http.get<ProductDetail>(`/products/${id}`)
}
