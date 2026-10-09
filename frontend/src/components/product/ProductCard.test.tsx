import { render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import ProductCard from '@/components/product/ProductCard'
import { makeProductSummary } from '@/test/fixtures'

function renderCard(product = makeProductSummary()) {
  return render(
    <MemoryRouter>
      <ProductCard product={product} groupName="香港二区" />
    </MemoryRouter>,
  )
}

describe('商品卡（R5 配置简介）', () => {
  it('逐行渲染 description_lines，且位于价格上方', () => {
    renderCard()

    const list = screen.getByTestId('product-config-lines')
    const items = within(list).getAllByRole('listitem')
    expect(items.map((item) => item.textContent)).toEqual(['CPU:2核心', '内存:1G', '带宽:20M'])

    // 信息顺序：配置列表 → 价格 → 按钮（参考 lyew 的商品卡）。
    const card = list.closest('[data-slot="card"]')
    expect(card).not.toBeNull()
    const text = (card as HTMLElement).textContent ?? ''
    expect(text.indexOf('CPU:2核心')).toBeGreaterThan(-1)
    expect(text.indexOf('CPU:2核心')).toBeLessThan(text.indexOf('¥22.00'))
    expect(text.indexOf('¥22.00')).toBeLessThan(text.indexOf('立即购买'))
  })

  it('无简介（空数组）时不渲染配置区块', () => {
    renderCard(makeProductSummary({ description_lines: [] }))

    expect(screen.queryByTestId('product-config-lines')).not.toBeInTheDocument()
  })

  it('最多展示 7 行（超出截断）', () => {
    const many = Array.from({ length: 9 }, (_, index) => `配置:${index + 1}`)
    renderCard(makeProductSummary({ description_lines: many }))

    const items = within(screen.getByTestId('product-config-lines')).getAllByRole('listitem')
    expect(items).toHaveLength(7)
    expect(items[6].textContent).toBe('配置:7')
    expect(screen.queryByText('配置:8')).not.toBeInTheDocument()
  })
})
