import { Alert, Button, FieldError, Input, Label, TextField } from '@heroui/react'
import { useState, type FormEvent } from 'react'
import { Link, Navigate, useNavigate, useSearchParams } from 'react-router-dom'

import { errorMessage } from '../api/client'
import { useAuth } from '../auth/authContext'
import AuthShell from '../components/layout/AuthShell'
import { validateLoginForm } from '../lib/validate'
import { buildRegisterUrl, safeRedirect } from '../lib/redirect'

// Login 是会员登录页：字段必填校验 + 后端 message 直出 + 登录后回跳 redirect。
export default function Login() {
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const { login, isAuthenticated, initializing } = useAuth()

  const redirectTarget = safeRedirect(searchParams.get('redirect'))

  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [errors, setErrors] = useState({ username: '', password: '' })
  const [serverError, setServerError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  // 已登录用户直接回到目标页，不再展示登录表单。
  if (isAuthenticated && !initializing) {
    return <Navigate to={redirectTarget} replace />
  }

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const nextErrors = validateLoginForm(username, password)
    setErrors(nextErrors)
    setServerError('')
    if (nextErrors.username || nextErrors.password) {
      return
    }

    setSubmitting(true)
    try {
      await login(username.trim(), password)
      navigate(redirectTarget, { replace: true })
    } catch (error) {
      setServerError(errorMessage(error, '登录失败，请稍后重试'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <AuthShell
      title="登录会员账号"
      footer={
        <>
          还没有账号？
          <Link
            className="ml-1 font-medium text-accent hover:underline"
            to={buildRegisterUrl(searchParams.get('redirect') ?? '')}
          >
            立即注册
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
          value={username}
          onChange={setUsername}
          isRequired
          isInvalid={Boolean(errors.username)}
          autoComplete="username"
        >
          <Label>用户名</Label>
          <Input placeholder="请输入用户名" autoFocus />
          <FieldError>{errors.username}</FieldError>
        </TextField>

        <TextField
          name="password"
          type="password"
          value={password}
          onChange={setPassword}
          isRequired
          isInvalid={Boolean(errors.password)}
          autoComplete="current-password"
        >
          <Label>密码</Label>
          <Input placeholder="请输入密码" />
          <FieldError>{errors.password}</FieldError>
        </TextField>

        <Button type="submit" fullWidth variant="primary" isDisabled={submitting}>
          {submitting ? '登录中…' : '登录'}
        </Button>

        <p className="text-center text-xs text-muted">
          登录状态保存在浏览器本地，7 天内有效；如需在其他设备登录请重新输入密码。
        </p>
      </form>
    </AuthShell>
  )
}
