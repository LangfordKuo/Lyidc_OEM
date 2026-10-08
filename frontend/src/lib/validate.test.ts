import { describe, expect, it } from 'vitest'

import {
  byteLength,
  hasErrors,
  validateCancelReason,
  validateEmail,
  validateInstancePassword,
  validateLoginForm,
  validatePassword,
  validateRechargeAmount,
  validateRegisterForm,
  validateTicketContent,
  validateTicketSubject,
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

// 会员区（7b）字段规则：充值金额（契约 12.4）、实例密码（15.1）、工单字段（16.4）、取消原因（15.8.2）。
describe('会员区字段校验', () => {
  it('充值金额：1.00 ~ 50000.00，最多两位小数', () => {
    expect(validateRechargeAmount('1')).toBe('')
    expect(validateRechargeAmount('100.50')).toBe('')
    expect(validateRechargeAmount('50000')).toBe('')
    expect(validateRechargeAmount('')).toBe('请输入充值金额')
    expect(validateRechargeAmount('0.5')).toContain('不能低于')
    expect(validateRechargeAmount('50000.01')).toContain('不能超过')
    expect(validateRechargeAmount('100.555')).toContain('金额格式不正确')
    expect(validateRechargeAmount('abc')).toContain('金额格式不正确')
    expect(validateRechargeAmount('-10')).toContain('金额格式不正确')
  })

  it('实例密码：8-64 个字符且同时包含字母与数字', () => {
    expect(validateInstancePassword('Abc12345')).toBe('')
    expect(validateInstancePassword('')).toContain('自动生成')
    expect(validateInstancePassword('Abc1234')).toContain('至少 8 个字符')
    expect(validateInstancePassword('a'.repeat(65))).toContain('最多 64 个字符')
    expect(validateInstancePassword('abcdefgh')).toContain('同时包含字母与数字')
    expect(validateInstancePassword('12345678')).toContain('同时包含字母与数字')
  })

  it('工单标题与内容：按裁剪后的字符数判定', () => {
    expect(validateTicketSubject('主机无法连接')).toBe('')
    expect(validateTicketSubject('短标题')).toContain('5-100')
    expect(validateTicketSubject(` ${'长'.repeat(101)} `)).toContain('5-100')
    expect(validateTicketContent('内容')).toBe('')
    expect(validateTicketContent('   ')).toBe('请输入工单内容')
    expect(validateTicketContent('字'.repeat(5001))).toContain('最多 5000')
  })

  it('取消原因：可空，最长 200 字符', () => {
    expect(validateCancelReason('')).toBe('')
    expect(validateCancelReason('业务迁移')).toBe('')
    expect(validateCancelReason('原'.repeat(201))).toContain('最多 200')
  })
})
