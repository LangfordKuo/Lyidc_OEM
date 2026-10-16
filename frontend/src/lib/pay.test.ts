import { describe, expect, it, vi } from 'vitest'

import { gotoPayurl, isAllowedPayurl } from '@/lib/pay'

// P5 安全整改 finding#07（契约 12.2.1）：渠道返回的 payurl 只允许 http / https 绝对地址。
//
// 这里用真实的 `window.location.assign` 替身记录跳转，因此断言的是「到底跳没跳」，
// 而不是「函数有没有被调用」——后者在别的页面测试里被整体 mock 掉了（那是另一层）。

/** 装一个可断言的 location.assign 替身；afterEach 的 unstubAllGlobals 会还原。 */
function stubLocation() {
  const assign = vi.fn()
  vi.stubGlobal('location', { assign })
  return assign
}

describe('isAllowedPayurl 协议白名单', () => {
  it('放行 http / https 绝对地址（含查询串、大小写与首尾空白）', () => {
    expect(isAllowedPayurl('https://pay.example.com/pay/abc123')).toBe(true)
    expect(isAllowedPayurl('http://127.0.0.1:8080/pay/x')).toBe(true)
    expect(isAllowedPayurl('https://gw/x?a=1&sign=deadbeef')).toBe(true)
    expect(isAllowedPayurl('HTTPS://GW/x')).toBe(true)
    expect(isAllowedPayurl('  https://gw/x  ')).toBe(true)
  })

  it('拒绝脚本协议（在站点源执行任意 JS、可读到 localStorage 里的会员 JWT）', () => {
    expect(isAllowedPayurl('javascript:alert(1)')).toBe(false)
    expect(isAllowedPayurl('JavaScript:alert(document.cookie)')).toBe(false)
    expect(isAllowedPayurl('data:text/html,<script>fetch("//evil")</script>')).toBe(false)
    expect(isAllowedPayurl('vbscript:msgbox(1)')).toBe(false)
    expect(isAllowedPayurl('file:///C:/Windows/System32/calc.exe')).toBe(false)
  })

  it('拒绝开放重定向形态：协议相对地址与相对地址', () => {
    expect(isAllowedPayurl('//evil.example.com/pay')).toBe(false)
    expect(isAllowedPayurl('/pay/relative')).toBe(false)
    expect(isAllowedPayurl('pay/relative')).toBe(false)
  })

  it('拒绝非字符串与空值', () => {
    expect(isAllowedPayurl('')).toBe(false)
    expect(isAllowedPayurl('   ')).toBe(false)
    expect(isAllowedPayurl(null)).toBe(false)
    expect(isAllowedPayurl(undefined)).toBe(false)
    expect(isAllowedPayurl(42)).toBe(false)
    expect(isAllowedPayurl({ payurl: 'https://gw/x' })).toBe(false)
  })
})

describe('gotoPayurl 跳转闸门', () => {
  it('放行时以原地址调用 location.assign 并返回 true', () => {
    const assign = stubLocation()

    expect(gotoPayurl('https://pay.example.com/pay/abc123')).toBe(true)
    expect(assign).toHaveBeenCalledTimes(1)
    expect(assign).toHaveBeenCalledWith('https://pay.example.com/pay/abc123')
  })

  it('拒绝时一次都不跳转并返回 false（调用方据此给出可读提示）', () => {
    const assign = stubLocation()
    const rejected = [
      'javascript:alert(1)',
      'data:text/html,<script>1</script>',
      'vbscript:msgbox(1)',
      '//evil.example.com/pay',
      '/pay/relative',
      '',
      '   ',
    ]

    for (const payurl of rejected) {
      expect(gotoPayurl(payurl)).toBe(false)
    }
    expect(assign).not.toHaveBeenCalled()
  })

  it('拒绝非字符串与空值且不抛错', () => {
    const assign = stubLocation()

    expect(gotoPayurl(null)).toBe(false)
    expect(gotoPayurl(undefined)).toBe(false)
    expect(assign).not.toHaveBeenCalled()
  })
})
