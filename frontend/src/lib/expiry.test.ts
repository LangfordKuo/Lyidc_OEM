import { describe, expect, it } from 'vitest'

import { daysUntil, expiryColorClass, expiryState, expiryText } from './expiry'

// 到期展示口径：7 天内为临期（与契约 17.5 的到期提醒缺省天数一致），已过期为危险色。
const NOW = new Date('2026-10-08T00:00:00Z')

describe('到期时间展示', () => {
  it('daysUntil 按天向上取整，已过期为负', () => {
    expect(daysUntil('2026-10-08T00:00:00Z', NOW)).toBe(0)
    expect(daysUntil('2026-10-11T00:00:00Z', NOW)).toBe(3)
    // 不满一天也算一天
    expect(daysUntil('2026-10-08T06:00:00Z', NOW)).toBe(1)
    expect(daysUntil('2026-10-06T00:00:00Z', NOW)).toBe(-2)
    expect(daysUntil(null, NOW)).toBeNull()
    expect(daysUntil('not-a-date', NOW)).toBeNull()
  })

  it('expiryState 区分 无到期时间 / 正常 / 临期 / 已过期', () => {
    expect(expiryState(null, 7)).toBe('none')
    expect(expiryState('2026-11-08T00:00:00Z', 7)).toBe('ok')
    expect(expiryState('2026-10-11T00:00:00Z', 7)).toBe('soon')
    expect(expiryState('2026-10-01T00:00:00Z', 7)).toBe('expired')
  })

  it('expiryText 输出中文文案', () => {
    const future = new Date(Date.now() + 3 * 86_400_000).toISOString()
    const past = new Date(Date.now() - 2 * 86_400_000).toISOString()
    expect(expiryText(future)).toBe('3 天后到期')
    expect(expiryText(past)).toBe('已过期 2 天')
    expect(expiryText(null)).toBe('')
  })

  it('颜色：临期警告色、过期危险色', () => {
    expect(expiryColorClass('soon')).toBe('text-warning')
    expect(expiryColorClass('expired')).toBe('text-danger')
    expect(expiryColorClass('ok')).toBe('text-foreground')
  })
})
