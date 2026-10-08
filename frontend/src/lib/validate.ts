// 表单校验规则严格对齐契约 6.2：
// 用户名 3-32 位字母/数字/下划线；邮箱符合邮箱格式且 ≤128 字节；密码 8-72 **字节**（bcrypt 上限）。
// 长度按 UTF-8 字节数计算（中文密码 3 个字就是 9 字节），与后端一致。

const USERNAME_PATTERN = /^[A-Za-z0-9_]{3,32}$/
const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

export function byteLength(value: string): number {
  return new TextEncoder().encode(value).length
}

export function validateUsername(value: string): string {
  if (!value) {
    return '请输入用户名'
  }
  if (!USERNAME_PATTERN.test(value)) {
    return '用户名需为 3-32 位字母、数字或下划线'
  }
  return ''
}

export function validatePassword(value: string): string {
  if (!value) {
    return '请输入密码'
  }
  const bytes = byteLength(value)
  if (bytes < 8) {
    return '密码至少 8 个字节'
  }
  if (bytes > 72) {
    return '密码最多 72 个字节'
  }
  return ''
}

export function validateEmail(value: string): string {
  if (!value) {
    return '请输入邮箱'
  }
  if (byteLength(value) > 128) {
    return '邮箱最多 128 字节'
  }
  if (!EMAIL_PATTERN.test(value)) {
    return '邮箱格式不正确'
  }
  return ''
}

// validateLoginForm 只做「必填」校验：登录接口的格式错误由后端给出准确 message。
export function validateLoginForm(username: string, password: string): { username: string; password: string } {
  return {
    username: username ? '' : '请输入用户名',
    password: password ? '' : '请输入密码',
  }
}

export interface RegisterFormErrors {
  username: string
  email: string
  password: string
  confirm: string
}

export function validateRegisterForm(form: {
  username: string
  email: string
  password: string
  confirm: string
}): RegisterFormErrors {
  return {
    username: validateUsername(form.username),
    email: validateEmail(form.email),
    password: validatePassword(form.password),
    confirm:
      form.password === form.confirm ? '' : form.confirm ? '两次输入的密码不一致' : '请再次输入密码',
  }
}

// hasErrors 判断任意「字段 → 错误文案」对象里是否存在非空错误。
export function hasErrors<T extends object>(errors: T): boolean {
  return Object.values(errors).some((message) => message !== '')
}
