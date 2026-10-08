// 分页计算：商品目录接口一次返回全量上架商品，前端按固定每页条数切分。
export const PRODUCT_PAGE_SIZE = 12

/** 会员区列表每页条数：与后端缺省一致（契约 12.4 / 14.4 等分页接口 page_size 缺省 20）。 */
export const CONSOLE_PAGE_SIZE = 20

/** 管理后台列表每页条数：与后端缺省一致（管理端分页接口 page_size 缺省 20，上限 100）。 */
export const ADMIN_PAGE_SIZE = 20

export function totalPages(total: number, pageSize: number = PRODUCT_PAGE_SIZE): number {
  if (!Number.isFinite(total) || !Number.isFinite(pageSize) || pageSize <= 0) {
    return 1
  }
  return Math.max(1, Math.ceil(total / pageSize))
}

/** 页码窗口：当前页前后各 span 页，超出部分用 'gap' 表示省略号（last = 总页数）。 */
export function pageWindow(page: number, last: number, span = 1): (number | 'gap')[] {
  const lastPage = Math.max(1, last)
  const pages = new Set<number>([1, lastPage])
  for (let i = page - span; i <= page + span; i += 1) {
    if (i >= 1 && i <= lastPage) {
      pages.add(i)
    }
  }
  const sorted = [...pages].sort((a, b) => a - b)
  const result: (number | 'gap')[] = []
  let previous = 0
  for (const value of sorted) {
    if (previous && value - previous > 1) {
      result.push('gap')
    }
    result.push(value)
    previous = value
  }
  return result
}
