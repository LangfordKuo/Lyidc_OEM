import { useState } from 'react'
import { Link, Navigate, useNavigate, useSearchParams } from 'react-router-dom'
import { zodResolver } from '@hookform/resolvers/zod'
import { AlertCircleIcon, LoaderCircleIcon } from 'lucide-react'
import { useForm } from 'react-hook-form'

import { errorMessage } from '@/api/client'
import { useAdminAuth } from '@/auth/adminAuthContext'
import AuthShell from '@/components/layout/AuthShell'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { ADMIN_ROLE_DESCRIPTIONS, ADMIN_ROLE_LABELS } from '@/lib/adminRoles'
import { safeRedirect } from '@/lib/redirect'
import { loginSchema, type LoginForm } from '@/lib/validate'

/**
 * AdminLogin 是管理后台登录页（契约 6.3）：用户名 + 密码，错误信息直出后端 message
 * （401 用户名或密码错误 / 403 账号已被禁用），登录成功回跳 redirect（默认 /admin）。
 * 管理端 token 与会员 token 分离存储，两处登录互不覆盖。
 */
export default function AdminLogin() {
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const { login, isAuthenticated, admin, initializing } = useAdminAuth()

  const redirectTarget = safeRedirect(searchParams.get('redirect'), '/admin')
  const [serverError, setServerError] = useState('')

  const form = useForm<LoginForm>({
    resolver: zodResolver(loginSchema),
    defaultValues: { username: '', password: '' },
  })

  // 已登录的管理员直接回到目标页，不再展示登录表单。
  if (isAuthenticated && !initializing) {
    return <Navigate to={redirectTarget} replace />
  }

  const onSubmit = form.handleSubmit(async (values) => {
    setServerError('')
    try {
      await login(values.username.trim(), values.password)
      navigate(redirectTarget, { replace: true })
    } catch (error) {
      setServerError(errorMessage(error, '登录失败，请稍后重试'))
    }
  })

  return (
    <AuthShell
      title="登录管理后台"
      description="岭云互联 · 管理员账号"
      badge="管理后台"
      footer={
        <Link className="font-medium text-primary hover:underline" to="/">
          返回官网
        </Link>
      }
    >
      <form className="space-y-5" onSubmit={onSubmit} noValidate aria-label="管理员登录表单">
        {serverError ? (
          <Alert variant="destructive">
            <AlertCircleIcon aria-hidden />
            <AlertDescription>{serverError}</AlertDescription>
          </Alert>
        ) : null}

        {admin ? (
          <Alert>
            <AlertDescription>
              当前已登录 {ADMIN_ROLE_LABELS[admin.role] ?? admin.role}（{admin.username}）：
              {ADMIN_ROLE_DESCRIPTIONS[admin.role] ?? ''}
            </AlertDescription>
          </Alert>
        ) : null}

        <Field data-invalid={form.formState.errors.username ? true : undefined}>
          <FieldLabel htmlFor="admin-username">管理员用户名</FieldLabel>
          <Input
            id="admin-username"
            autoComplete="username"
            placeholder="请输入管理员用户名"
            autoFocus
            aria-invalid={form.formState.errors.username ? true : undefined}
            {...form.register('username')}
          />
          <FieldError errors={[form.formState.errors.username]} />
        </Field>

        <Field data-invalid={form.formState.errors.password ? true : undefined}>
          <FieldLabel htmlFor="admin-password">密码</FieldLabel>
          <Input
            id="admin-password"
            type="password"
            autoComplete="current-password"
            placeholder="请输入密码"
            aria-invalid={form.formState.errors.password ? true : undefined}
            {...form.register('password')}
          />
          <FieldError errors={[form.formState.errors.password]} />
        </Field>

        <Button type="submit" className="h-10 w-full text-base" disabled={form.formState.isSubmitting}>
          {form.formState.isSubmitting ? (
            <>
              <LoaderCircleIcon className="animate-spin" data-icon="inline-start" aria-hidden />
              登录中…
            </>
          ) : (
            '登录'
          )}
        </Button>

        <p className="text-center text-xs text-muted-foreground">
          管理端凭据与会员账号相互独立；登录状态仅保存在本机浏览器，请勿在公共设备上登录。
        </p>
      </form>
    </AuthShell>
  )
}
