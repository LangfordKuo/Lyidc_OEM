import { z } from 'zod'

// 表单校验规则严格对齐契约 6.2：
// 用户名 3-32 位字母/数字/下划线；邮箱符合邮箱格式且 ≤128 字节；密码 8-72 **字节**（bcrypt 上限）。
// 长度按 UTF-8 字节数计算（中文密码 3 个字就是 9 字节），与后端一致。

export const USERNAME_PATTERN = /^[A-Za-z0-9_]{3,32}$/
export const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

export function byteLength(value: string): number {
  return new TextEncoder().encode(value).length
}

/** 登录表单：只做「必填」校验，格式错误由后端给出准确 message。 */
export const loginSchema = z.object({
  username: z.string().min(1, '请输入用户名'),
  password: z.string().min(1, '请输入密码'),
})

export type LoginForm = z.infer<typeof loginSchema>

/** 注册表单：与契约 6.2 的注册规则逐条对齐。 */
export const registerSchema = z
  .object({
    username: z
      .string()
      .min(1, '请输入用户名')
      .regex(USERNAME_PATTERN, '用户名需为 3-32 位字母、数字或下划线'),
    email: z
      .string()
      .min(1, '请输入邮箱')
      .refine((value) => byteLength(value) <= 128, '邮箱最多 128 字节')
      .refine((value) => EMAIL_PATTERN.test(value), '邮箱格式不正确'),
    password: z
      .string()
      .min(1, '请输入密码')
      .refine((value) => byteLength(value) >= 8, '密码至少 8 个字节')
      .refine((value) => byteLength(value) <= 72, '密码最多 72 个字节'),
    confirm: z.string().min(1, '请再次输入密码'),
  })
  .superRefine((data, ctx) => {
    // confirm 为空时由上面的 min(1) 报错，这里只处理「已填但不一致」。
    if (data.confirm && data.password !== data.confirm) {
      ctx.addIssue({
        code: 'custom',
        path: ['confirm'],
        message: '两次输入的密码不一致',
      })
    }
  })

export type RegisterForm = z.infer<typeof registerSchema>

// ---------------------------------------------------------------------------
// 会员区：充值金额、实例密码、工单字段、取消原因的前端预校验。
// 规则严格对齐契约，前端只做「提前提示」，最终以后端判定为准。
// ---------------------------------------------------------------------------
export const RECHARGE_MIN_CENTS = 100
export const RECHARGE_MAX_CENTS = 5_000_000

/** 充值金额：契约 12.4 —— 十进制、最多两位小数、1.00 ~ 50000.00。 */
export function validateRechargeAmount(value: string): string {
  const trimmed = value.trim()
  if (!trimmed) {
    return '请输入充值金额'
  }
  if (!/^\d+(\.\d{1,2})?$/.test(trimmed)) {
    return '金额格式不正确（最多两位小数）'
  }
  const cents = Math.round(Number(trimmed) * 100)
  if (cents < RECHARGE_MIN_CENTS) {
    return '充值金额不能低于 ¥1.00'
  }
  if (cents > RECHARGE_MAX_CENTS) {
    return '充值金额不能超过 ¥50000.00'
  }
  return ''
}

/** 实例密码：契约 15.1 —— 8-64 个字符且同时包含字母与数字。 */
export function validateInstancePassword(value: string): string {
  if (!value) {
    return '请输入密码，或选择由系统自动生成'
  }
  const length = [...value].length
  if (length < 8) {
    return '密码至少 8 个字符'
  }
  if (length > 64) {
    return '密码最多 64 个字符'
  }
  if (!/[A-Za-z]/.test(value) || !/\d/.test(value)) {
    return '密码需同时包含字母与数字'
  }
  return ''
}

/** 工单标题：契约 16.4 —— 裁剪首尾空白后 5-100 个字符。 */
export function validateTicketSubject(value: string): string {
  const length = [...value.trim()].length
  if (length < 5 || length > 100) {
    return `标题需为 5-100 个字符（当前 ${length}）`
  }
  return ''
}

/** 工单内容：契约 16.4 —— 裁剪首尾空白后 1-5000 个字符。 */
export function validateTicketContent(value: string): string {
  const trimmed = value.trim()
  if (!trimmed) {
    return '请输入工单内容'
  }
  if ([...trimmed].length > 5000) {
    return '内容最多 5000 个字符'
  }
  return ''
}

/** 取消申请原因：契约 15.8.2 —— 会员端可空，最长 200 字符。 */
export function validateCancelReason(value: string): string {
  return [...value.trim()].length > 200 ? '原因最多 200 个字符' : ''
}
