import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import ChoiceChips, { HIDDEN_RADIO_CLASS } from './ChoiceChips'

/**
 * R7 空白修复回归：Tailwind v4 里 `.sr-only` 与 shadcn 基类 `.relative` 同特异性、
 * 后者胜出，导致「隐藏」单选落回文档流成为 flex 项，再吃 Label 的 gap-2，
 * 每个选项文字前多出 23px 幽灵空白。修法是显式 `absolute`（见 HIDDEN_RADIO_CLASS 注释）。
 * 这几条断言锁住修复不被回退。
 */
describe('ChoiceChips 空白修复回归', () => {
  it('HIDDEN_RADIO_CLASS 保持 absolute + sr-only（不能被改回纯 sr-only）', () => {
    expect(HIDDEN_RADIO_CLASS).toBe('absolute sr-only')
  })

  it('隐藏单选带 absolute 类（脱离文档流，不再撑出幽灵空白）', () => {
    render(
      <ChoiceChips
        items={[{ value: '1', label: '香港' }]}
        value="1"
        onChange={() => {}}
        ariaLabel="区域"
        idPrefix="t"
      />,
    )

    const radio = screen.getByRole('radio', { name: '香港' })
    expect(radio).toHaveClass('absolute')
    expect(radio).toHaveClass('sr-only')
    // 选项文字前无多余空白：Label 的文本内容与标签完全一致（无前导空白/空元素残留）
    const label = radio.closest('label')
    expect(label).not.toBeNull()
    expect(label?.textContent).toBe('香港')
  })

  it('点击选项触发 onChange 并带上对应值', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    render(
      <ChoiceChips
        items={[
          { value: '1', label: '香港' },
          { value: '2', label: '美国' },
        ]}
        value="1"
        onChange={onChange}
        ariaLabel="区域"
        idPrefix="t"
      />,
    )

    await user.click(screen.getByText('美国'))
    expect(onChange).toHaveBeenCalledWith('2')
  })
})
