import { describe, expect, it } from 'vitest'

import { formatFileSize } from '@/lib/format'

describe('文件体积展示（1 KB = 1024 B）', () => {
  it('不足 1 KB 按字节整数展示', () => {
    expect(formatFileSize(0)).toBe('0 B')
    expect(formatFileSize(1023)).toBe('1023 B')
  })

  it('逐级换算并保留一位小数', () => {
    expect(formatFileSize(1024)).toBe('1.0 KB')
    expect(formatFileSize(1536)).toBe('1.5 KB')
    expect(formatFileSize(12345678)).toBe('11.8 MB')
    expect(formatFileSize(2 * 1024 ** 3)).toBe('2.0 GB')
  })

  it('按一位小数的展示值进位：不出现 1024.0 KB', () => {
    expect(formatFileSize(1023.96 * 1024)).toBe('1.0 MB')
  })

  it('非法值显示占位符', () => {
    expect(formatFileSize(Number.NaN)).toBe('—')
    expect(formatFileSize(-1)).toBe('—')
  })
})
