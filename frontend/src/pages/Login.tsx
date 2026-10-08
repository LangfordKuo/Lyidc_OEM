import { useState } from 'react'
import { Link, Navigate, useNavigate, useSearchParams } from 'react-router-dom'
import { zodResolver } from '@hookform/resolvers/zod'
import { AlertCircleIcon, LoaderCircleIcon } from 'lucide-react'
import { useForm } from 'react-hook-form'

import { errorMessage } from '@/api/client'
import { useAuth } from '@/auth/authContext'
import AuthShell from '@/components/layout/AuthShell'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { buildRegisterUrl, safeRedirect } from '@/lib/redirect'
import { loginSchema, type LoginForm } from '@/lib/validate'

/** 会员登录页：zod 校验 + 后端 message 直出 + 登录后回跳 redirect。 */
export default function Login() {
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const { login, isAuthenticated, initializing } = useAuth()

  const redirectTarget = safeRedirect(searchParams.get('redirect'))
  const [serverError, setServerError] = useState('')

  const form = useForm<LoginForm>({
    resolver: zodResolver(loginSchema),
    defaultValues: { username: '', password: '' },
  })

  // 已登录用户直接回到目标页，不再展示登录表单。
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
      title="登录会员账号"
      description="登录后可下单、支付并管理您的服务"
      footer={
        <>
          还没有账号？
          <Link
            className="ml-1 font-medium text-primary hover:underline"
            to={buildRegisterUrl(searchParams.get('redirect') ?? '')}
          >
            立即注册
          </Link>
        </>
      }
    >
      <form className="space-y-5" onSubmit={onSubmit} noValidate aria-label="登录表单">
        {serverError ? (
          <Alert variant="destructive">
            <AlertCircleIcon aria-hidden />
            <AlertDescription>{serverError}</AlertDescription>
          </Alert>
        ) : null}

        <Field data-invalid={form.formState.errors.username ? true : undefined}>
          <FieldLabel htmlFor="login-username">用户名</FieldLabel>
          <Input
            id="login-username"
            autoComplete="username"
            placeholder="请输入用户名"
            autoFocus
            aria-invalid={form.formState.errors.username ? true : undefined}
            {...form.register('username')}
          />
          <FieldError errors={[form.formState.errors.username]} />
        </Field>

        <Field data-invalid={form.formState.errors.password ? true : undefined}>
          <FieldLabel htmlFor="login-password">密码</FieldLabel>
          <Input
            id="login-password"
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
          登录状态保存在浏览器本地，7 天内有效；如需在其他设备登录请重新输入密码。
        </p>
      </form>
    </AuthShell>
  )
}
