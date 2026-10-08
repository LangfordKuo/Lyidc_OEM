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

// ---------------------------------------------------------------------------
// 会员区（7b）：充值金额、实例密码、工单字段的前端预校验。
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
