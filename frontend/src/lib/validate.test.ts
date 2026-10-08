import { describe, expect, it } from 'vitest'

import {
  byteLength,
  hasErrors,
  validateEmail,
  validateLoginForm,
  validatePassword,
  validateRegisterForm,
  validateUsername,
} from './validate'

// 规则来源：契约 6.2（用户名 3-32 位字母/数字/下划线、邮箱 ≤128 字节、密码 8-72 字节）。
describe('注册/登录字段校验', () => {
  it('用户名：3-32 位字母/数字/下划线', () => {
    expect(validateUsername('alice_01')).toBe('')
    expect(validateUsername('')).toBe('请输入用户名')
    expect(validateUsername('ab')).toContain('3-32')
    expect(validateUsername('a'.repeat(33))).toContain('3-32')
    expect(validateUsername('中文名')).toContain('3-32')
    expect(validateUsername('with space')).toContain('3-32')
  })

  it('密码：按 UTF-8 字节数判定 8-72 字节', () => {
    expect(validatePassword('alice123456')).toBe('')
    expect(validatePassword('1234567')).toContain('至少 8 个字节')
    // 3 个中文字 = 9 字节，合法
    expect(validatePassword('中文密码')).toBe('')
    expect(validatePassword('a'.repeat(73))).toContain('最多 72 个字节')
  })

  it('邮箱：格式与 128 字节上限', () => {
    expect(validateEmail('alice@example.com')).toBe('')
    expect(validateEmail('')).toBe('请输入邮箱')
    expect(validateEmail('alice@')).toBe('邮箱格式不正确')
    expect(validateEmail('alice example.com')).toBe('邮箱格式不正确')
    expect(validateEmail(`${'a'.repeat(120)}@example.com`)).toBe('邮箱最多 128 字节')
  })

  it('byteLength 按 UTF-8 计算', () => {
    expect(byteLength('abc')).toBe(3)
    expect(byteLength('中文')).toBe(6)
  })

  it('注册表单：两次密码不一致时报错', () => {
    const errors = validateRegisterForm({
      username: 'alice',
      email: 'alice@example.com',
      password: 'alice123456',
      confirm: 'alice123457',
    })
    expect(errors.confirm).toBe('两次输入的密码不一致')
    expect(hasErrors(errors)).toBe(true)

    const ok = validateRegisterForm({
      username: 'alice',
      email: 'alice@example.com',
      password: 'alice123456',
      confirm: 'alice123456',
    })
    expect(hasErrors(ok)).toBe(false)
  })

  it('登录表单只校验必填（格式错误由后端 message 兜底）', () => {
    expect(validateLoginForm('', '')).toEqual({ username: '请输入用户名', password: '请输入密码' })
    expect(validateLoginForm('alice', 'anything')).toEqual({ username: '', password: '' })
  })
})
