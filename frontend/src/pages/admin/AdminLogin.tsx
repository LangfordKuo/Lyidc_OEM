import { Alert, Button, FieldError, Input, Label, TextField } from '@heroui/react'
import { useState, type FormEvent } from 'react'
import { Link, Navigate, useNavigate, useSearchParams } from 'react-router-dom'

import { errorMessage } from '../../api/client'
import { useAdminAuth } from '../../auth/adminAuthContext'
import AuthShell from '../../components/layout/AuthShell'
import { ADMIN_ROLE_DESCRIPTIONS, ADMIN_ROLE_LABELS } from '../../lib/adminRoles'
import { safeRedirect } from '../../lib/redirect'
import { validateLoginForm } from '../../lib/validate'

// AdminLogin 是管理后台登录页（契约 6.3）：用户名 + 密码，错误信息直出后端 message
// （401 用户名或密码错误 / 403 账号已被禁用），登录成功回跳 redirect（默认 /admin）。
export default function AdminLogin() {
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const { login, isAuthenticated, admin, initializing } = useAdminAuth()

  const redirectTarget = safeRedirect(searchParams.get('redirect'), '/admin')

  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [errors, setErrors] = useState({ username: '', password: '' })
  const [serverError, setServerError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  // 已登录的管理员直接回到目标页，不再展示登录表单。
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
      title="登录管理后台"
      subtitle="岭云互联 · 管理员账号"
      footer={
        <Link className="font-medium text-accent hover:underline" to="/">
          返回官网
        </Link>
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

        {admin ? (
          <Alert status="default">
            <Alert.Indicator />
            <Alert.Content>
              <Alert.Description>
                当前已登录 {ADMIN_ROLE_LABELS[admin.role] ?? admin.role}（
                {admin.username}）：{ADMIN_ROLE_DESCRIPTIONS[admin.role] ?? ''}
              </Alert.Description>
            </Alert.Content>
          </Alert>
        ) : null}

        <TextField
          name="admin_username"
          type="text"
          value={username}
          onChange={setUsername}
          isRequired
          isInvalid={Boolean(errors.username)}
          autoComplete="username"
        >
          <Label>管理员用户名</Label>
          <Input placeholder="请输入管理员用户名" autoFocus />
          <FieldError>{errors.username}</FieldError>
        </TextField>

        <TextField
          name="admin_password"
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
          管理端凭据与会员账号相互独立；登录状态仅保存在本机浏览器，请勿在公共设备上登录。
        </p>
      </form>
    </AuthShell>
  )
}
