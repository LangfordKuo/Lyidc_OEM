// 分页计算：后端缺省 page_size=20，列表页统一按「总条数 + 每页条数」推导页码。
export const DEFAULT_PAGE_SIZE = 20

export function totalPages(total: number, pageSize: number = DEFAULT_PAGE_SIZE): number {
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
