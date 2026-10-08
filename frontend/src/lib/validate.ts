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
