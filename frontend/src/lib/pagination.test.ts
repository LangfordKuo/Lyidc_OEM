import { describe, expect, it } from 'vitest'

import { pageWindow, totalPages } from './pagination'

describe('分页计算', () => {
  it('totalPages：向上取整，非法输入回落 1 页', () => {
    expect(totalPages(0, 20)).toBe(1)
    expect(totalPages(20, 20)).toBe(1)
    expect(totalPages(21, 20)).toBe(2)
    expect(totalPages(100, 20)).toBe(5)
    expect(totalPages(10, 0)).toBe(1)
  })

  it('pageWindow：首尾页 + 当前页邻域，超出部分用 gap 表示', () => {
    expect(pageWindow(1, 5)).toEqual([1, 2, 'gap', 5])
    expect(pageWindow(3, 5)).toEqual([1, 2, 3, 4, 5])
    expect(pageWindow(5, 9)).toEqual([1, 'gap', 4, 5, 6, 'gap', 9])
    expect(pageWindow(1, 1)).toEqual([1])
  })
})
