import { Alert, Button, FieldError, Input, Label, TextField } from '@heroui/react'
import { useState, type FormEvent } from 'react'
import { Link, Navigate, useNavigate, useSearchParams } from 'react-router-dom'

import { errorMessage } from '../api/client'
import { useAuth } from '../auth/authContext'
import AuthShell from '../components/layout/AuthShell'
import { hasErrors, validateRegisterForm, type RegisterFormErrors } from '../lib/validate'
import { buildLoginUrl, safeRedirect } from '../lib/redirect'

const EMPTY_ERRORS: RegisterFormErrors = { username: '', email: '', password: '', confirm: '' }

// Register 是会员注册页：字段规则严格对齐契约 6.2（用户名 3-32 位、邮箱 ≤128 字节、密码 8-72 字节），
// 注册成功后自动登录并回跳 redirect。
export default function Register() {
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const { register, isAuthenticated, initializing } = useAuth()

  const redirectTarget = safeRedirect(searchParams.get('redirect'))

  const [form, setForm] = useState({ username: '', email: '', password: '', confirm: '' })
  const [errors, setErrors] = useState<RegisterFormErrors>(EMPTY_ERRORS)
  const [serverError, setServerError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  if (isAuthenticated && !initializing) {
    return <Navigate to={redirectTarget} replace />
  }

  const update = (field: keyof typeof form) => (value: string) => {
    setForm((prev) => ({ ...prev, [field]: value }))
  }

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const nextErrors = validateRegisterForm(form)
    setErrors(nextErrors)
    setServerError('')
    if (hasErrors(nextErrors)) {
      return
    }

    setSubmitting(true)
    try {
      await register({ username: form.username.trim(), email: form.email.trim(), password: form.password })
      navigate(redirectTarget, { replace: true })
    } catch (error) {
      setServerError(errorMessage(error, '注册失败，请稍后重试'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <AuthShell
      title="注册会员账号"
      footer={
        <>
          已有账号？
          <Link
            className="ml-1 font-medium text-accent hover:underline"
            to={buildLoginUrl(searchParams.get('redirect') ?? '')}
          >
            去登录
          </Link>
        </>
      }
    >
      <form className="space-y-5" onSubmit={handleSubmit} noValidate>
        {serverError ? (
          <Alert status="danger">
            <Alert.Indicator />
            <Alert.Content>
              <Alert.Description>{serverError}</Alert.Description>
            </Alert.Content>
          </Alert>
        ) : null}

        <TextField
          name="username"
          type="text"
          value={form.username}
          onChange={update('username')}
          isRequired
          isInvalid={Boolean(errors.username)}
          autoComplete="username"
        >
          <Label>用户名</Label>
          <Input placeholder="3-32 位字母、数字或下划线" autoFocus />
          <FieldError>{errors.username}</FieldError>
        </TextField>

        <TextField
          name="email"
          type="email"
          value={form.email}
          onChange={update('email')}
          isRequired
          isInvalid={Boolean(errors.email)}
          autoComplete="email"
        >
          <Label>邮箱</Label>
          <Input placeholder="用于接收订单与到期通知" />
          <FieldError>{errors.email}</FieldError>
        </TextField>

        <TextField
          name="password"
          type="password"
          value={form.password}
          onChange={update('password')}
          isRequired
          isInvalid={Boolean(errors.password)}
          autoComplete="new-password"
        >
          <Label>密码</Label>
          <Input placeholder="至少 8 个字节（约 8 个字符）" />
          <FieldError>{errors.password}</FieldError>
        </TextField>

        <TextField
          name="confirm"
          type="password"
          value={form.confirm}
          onChange={update('confirm')}
          isRequired
          isInvalid={Boolean(errors.confirm)}
          autoComplete="new-password"
        >
          <Label>确认密码</Label>
          <Input placeholder="请再次输入密码" />
          <FieldError>{errors.confirm}</FieldError>
        </TextField>

        <Button type="submit" fullWidth variant="primary" isDisabled={submitting}>
          {submitting ? '注册中…' : '注册并登录'}
        </Button>

        <p className="text-center text-xs text-muted">
          注册即表示同意本站服务条款；用户名与邮箱唯一且大小写不敏感。
        </p>
      </form>
    </AuthShell>
  )
}
